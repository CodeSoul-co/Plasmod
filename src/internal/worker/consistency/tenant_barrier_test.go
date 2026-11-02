package consistency

import (
	"context"
	"errors"
	"testing"
	"time"

	"plasmod/src/internal/eventbackbone"
	"plasmod/src/internal/schemas"
)

func TestTenantReadBarrierIsolatesFailureAndKeepsRecoveryCheckpoint(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxRetries = 1
	fail := errors.New("invalid source quote")
	c, _ := startTestController(t, cfg, func(_ context.Context, entry eventbackbone.WALEntry) (map[string]any, error) {
		if entry.Event.Identity.TenantID == "A" {
			return nil, fail
		}
		return nil, nil
	})
	a := testEvent("a", "sa", "eventual")
	a.Identity.TenantID = "A"
	ack, err := c.Submit(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.WaitForRead(ctx, schemas.QueryRequest{TenantID: "A"}); !errors.Is(err, fail) {
		t.Fatalf("A failure = %v", err)
	}
	b := testEvent("b", "sb", "strict")
	b.Identity.TenantID = "B"
	bAck, err := c.Submit(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"strict", "bounded", "eventual"} {
		frontier, err := c.ReadBarrier(ctx, schemas.QueryRequest{TenantID: "B", AccessConsistency: mode})
		if err != nil || frontier != bAck["lsn"].(int64) {
			t.Fatalf("B %s frontier=%d err=%v", mode, frontier, err)
		}
	}
	bounded := testEvent("bounded-b", "sb", "bounded")
	bounded.Identity.TenantID = "B"
	if _, err := c.Submit(ctx, bounded); err != nil {
		t.Fatalf("unrelated failure blocked bounded write: %v", err)
	}
	if err := c.WaitForRead(ctx, schemas.QueryRequest{TenantID: "B"}); err != nil {
		t.Fatal(err)
	}
	if c.Status().VisibleWatermark >= ack["lsn"].(int64) || c.Status().Failed != 1 {
		t.Fatalf("failure skipped: %+v", c.Status())
	}
	if err := c.WaitForRead(ctx, schemas.QueryRequest{}); !errors.Is(err, fail) {
		t.Fatalf("global query lost failure: %v", err)
	}
}

func TestTenantReadBarrierIncludesLegacyAndPendingEvents(t *testing.T) {
	for _, tenant := range []string{"B", ""} {
		tracker := NewTracker(0, nil, NewMemoryCheckpoint())
		tracker.AcceptTenant(10, time.Now().Add(-time.Hour), time.Time{}, tenant)
		tracker.AcceptTenant(20, time.Now(), time.Time{}, "other")
		if err := tracker.MarkVisible(20); err != nil {
			t.Fatal(err)
		}
		for _, mode := range []Mode{StrictVisible, BoundedStaleness} {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			_, err := tracker.ReadBarrier(ctx, 20, "B", mode, time.Second)
			cancel()
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("pending tenant=%q mode=%s: %v", tenant, mode, err)
			}
		}
		frontier, err := tracker.ReadBarrier(context.Background(), 20, "B", EventualVisibility, time.Second)
		if err != nil || frontier != 9 {
			t.Fatalf("unsafe eventual frontier=%d err=%v", frontier, err)
		}
		if err := tracker.MarkVisible(10); err != nil {
			t.Fatal(err)
		}
		frontier, err = tracker.ReadBarrier(context.Background(), 20, "B", StrictVisible, time.Second)
		if err != nil || frontier != 20 {
			t.Fatalf("completed frontier=%d err=%v", frontier, err)
		}
	}
}

func TestTenantReadBarrierDoesNotExposeFailedCheckpoint(t *testing.T) {
	checkpoint := &failOnceCheckpoint{CheckpointStore: NewMemoryCheckpoint(), err: errors.New("disk unavailable")}
	tracker := NewTracker(0, nil, checkpoint)
	tracker.AcceptTenant(5, time.Now(), time.Time{}, "B")
	if err := tracker.MarkVisible(5); err == nil {
		t.Fatal("expected checkpoint failure")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := tracker.ReadBarrier(ctx, 5, "B", StrictVisible, time.Second); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("checkpoint bypassed: %v", err)
	}
	if err := tracker.MarkVisible(5); err != nil {
		t.Fatal(err)
	}
	if got, err := tracker.ReadBarrier(context.Background(), 5, "B", StrictVisible, time.Second); got != 5 || err != nil {
		t.Fatalf("retry: %d %v", got, err)
	}
}
