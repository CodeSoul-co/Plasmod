package consistency

import (
	"context"
	"errors"
	"testing"
	"time"

	"plasmod/src/internal/eventbackbone"
)

type recordingWatermark struct {
	values []int64
}

func (w *recordingWatermark) AdvanceTo(lsn int64) eventbackbone.TimeTick {
	w.values = append(w.values, lsn)
	return eventbackbone.TimeTick{LogicalTS: lsn}
}

func TestTrackerAdvancesAcrossAcceptedLSNGapsOnlyWhenContiguous(t *testing.T) {
	t.Parallel()

	watermark := &recordingWatermark{}
	tracker := NewTracker(0, watermark, NewMemoryCheckpoint())
	now := time.Now()
	tracker.Accept(10, now, time.Time{})
	tracker.Accept(30, now, time.Time{})

	if err := tracker.MarkVisible(30); err != nil {
		t.Fatalf("MarkVisible(30): %v", err)
	}
	if got := tracker.Status().VisibleWatermark; got != 0 {
		t.Fatalf("watermark advanced across unfinished LSN: %d", got)
	}
	if err := tracker.MarkVisible(10); err != nil {
		t.Fatalf("MarkVisible(10): %v", err)
	}
	status := tracker.Status()
	if status.VisibleWatermark != 30 {
		t.Fatalf("watermark = %d, want 30", status.VisibleWatermark)
	}
	if len(watermark.values) != 1 || watermark.values[0] != 30 {
		t.Fatalf("published watermarks = %v, want [30]", watermark.values)
	}
}

func TestTrackerWaitThroughWakesWhenTargetBecomesVisible(t *testing.T) {
	t.Parallel()

	tracker := NewTracker(0, nil, NewMemoryCheckpoint())
	now := time.Now()
	tracker.Accept(5, now, time.Time{})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- tracker.WaitThrough(ctx, 5) }()

	select {
	case err := <-done:
		t.Fatalf("wait returned before visibility: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	if err := tracker.MarkVisible(5); err != nil {
		t.Fatalf("MarkVisible: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("WaitThrough: %v", err)
	}
}

func TestTrackerWaitThroughReturnsProjectionFailure(t *testing.T) {
	t.Parallel()

	tracker := NewTracker(0, nil, NewMemoryCheckpoint())
	tracker.Accept(7, time.Now(), time.Time{})
	want := errors.New("projection failed")
	tracker.MarkFailed(7, want)

	err := tracker.WaitThrough(context.Background(), 7)
	var projectionErr *ProjectionFailureError
	if !errors.As(err, &projectionErr) {
		t.Fatalf("WaitThrough error = %v, want ProjectionFailureError", err)
	}
	if projectionErr.LSN != 7 || !errors.Is(projectionErr, want) {
		t.Fatalf("projection error = %+v", projectionErr)
	}
}

func TestTrackerWaitWithinLagBlocksOnlyWhenOldestPendingExceedsBound(t *testing.T) {
	t.Parallel()

	tracker := NewTracker(0, nil, NewMemoryCheckpoint())
	tracker.Accept(11, time.Now().Add(-2*time.Second), time.Now().Add(-time.Second))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := tracker.WaitWithinLag(ctx, time.Second); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WaitWithinLag error = %v, want deadline exceeded", err)
	}

	if err := tracker.MarkVisible(11); err != nil {
		t.Fatalf("MarkVisible: %v", err)
	}
	if err := tracker.WaitWithinLag(context.Background(), time.Second); err != nil {
		t.Fatalf("visible tracker should be within lag: %v", err)
	}
}

func TestTrackerStatusCountsStatesAndSLABreaches(t *testing.T) {
	t.Parallel()

	tracker := NewTracker(0, nil, NewMemoryCheckpoint())
	now := time.Now()
	tracker.Accept(1, now.Add(-time.Second), now.Add(-500*time.Millisecond))
	tracker.Accept(2, now, now.Add(time.Second))
	tracker.MarkRetrying(1, 2, errors.New("temporary"))
	tracker.MarkSLABreach(1)
	tracker.MarkFailed(2, errors.New("permanent"))

	status := tracker.Status()
	if status.Retrying != 1 || status.Failed != 1 || status.Pending != 0 {
		t.Fatalf("unexpected state counts: %+v", status)
	}
	if status.SLABreaches != 1 || status.LastError == "" {
		t.Fatalf("missing breach/error status: %+v", status)
	}
	if status.LastSLABreachMS < 900 || status.MaxSLABreachMS < status.LastSLABreachMS {
		t.Fatalf("missing breach latency diagnostics: %+v", status)
	}
	if status.LatestLSN != 2 || status.OldestPendingAge <= 0 {
		t.Fatalf("missing LSN/age status: %+v", status)
	}
}

func TestTrackerCountsVisibilityBlockedPastDeadline(t *testing.T) {
	tracker := NewTracker(0, nil, NewMemoryCheckpoint())
	acceptedAt := time.Now()
	deadline := acceptedAt.Add(20 * time.Millisecond)
	tracker.Accept(1, acceptedAt, deadline)
	tracker.Accept(2, acceptedAt, deadline)

	if err := tracker.MarkVisible(2); err != nil {
		t.Fatalf("MarkVisible(2): %v", err)
	}
	if status := tracker.Status(); status.SLABreaches != 0 || status.VisibleWatermark != 0 {
		t.Fatalf("later projection became externally visible too early: %+v", status)
	}
	time.Sleep(30 * time.Millisecond)
	if err := tracker.MarkVisible(1); err != nil {
		t.Fatalf("MarkVisible(1): %v", err)
	}
	status := tracker.Status()
	if status.VisibleWatermark != 2 || status.SLABreaches != 2 {
		t.Fatalf("visibility-frontier breaches not counted: %+v", status)
	}
}

func TestTrackerResetClearsStateAndPersistsZero(t *testing.T) {
	t.Parallel()

	checkpoint := NewMemoryCheckpoint()
	tracker := NewTracker(0, nil, checkpoint)
	tracker.Accept(9, time.Now(), time.Time{})
	if err := tracker.MarkVisible(9); err != nil {
		t.Fatalf("MarkVisible: %v", err)
	}
	if err := tracker.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	status := tracker.Status()
	if status.VisibleWatermark != 0 || status.LatestLSN != 0 || status.Pending != 0 {
		t.Fatalf("status after reset: %+v", status)
	}
	if lsn, exists, err := checkpoint.Load(); err != nil || exists || lsn != 0 {
		t.Fatalf("checkpoint after reset: lsn=%d exists=%t err=%v", lsn, exists, err)
	}
}
