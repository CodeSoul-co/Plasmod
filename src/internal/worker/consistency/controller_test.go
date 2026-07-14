package consistency

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"plasmod/src/internal/eventbackbone"
	"plasmod/src/internal/schemas"
)

func testEvent(id, session, mode string) schemas.Event {
	return schemas.Event{
		Identity: schemas.EventIdentity{EventID: id, WorkspaceID: "workspace"},
		Actor:    schemas.EventActor{AgentID: "agent", SessionID: session},
		Access:   schemas.EventAccess{Consistency: mode},
		Payload:  map[string]any{"text": id},
	}
}

func startTestController(
	t *testing.T,
	cfg Config,
	project ProjectFunc,
) (*Controller, *eventbackbone.InMemoryWAL) {
	t.Helper()
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	wal := eventbackbone.NewInMemoryWAL(bus, clock)
	watermark := eventbackbone.NewWatermarkPublisher(clock, bus)
	controller, err := NewController(wal, watermark, NewMemoryCheckpoint(), cfg, project)
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	if err := controller.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = controller.Shutdown(ctx)
	})
	return controller, wal
}

func TestControllerStrictAcknowledgementWaitsForVisibility(t *testing.T) {
	release := make(chan struct{})
	controller, _ := startTestController(t, DefaultConfig(), func(ctx context.Context, entry eventbackbone.WALEntry) (map[string]any, error) {
		select {
		case <-release:
			return map[string]any{"edges": 2}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})

	type result struct {
		ack map[string]any
		err error
	}
	done := make(chan result, 1)
	go func() {
		ack, err := controller.Submit(context.Background(), testEvent("strict-1", "s1", "strict"))
		done <- result{ack: ack, err: err}
	}()

	select {
	case got := <-done:
		t.Fatalf("strict write acknowledged before projection: %+v", got)
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	got := <-done
	if got.err != nil {
		t.Fatalf("Submit: %v", got.err)
	}
	if got.ack["visibility_status"] != "visible" || got.ack["consistency_mode"] != string(StrictVisible) {
		t.Fatalf("strict ack = %+v", got.ack)
	}
	if got.ack["edges"] != 2 {
		t.Fatalf("projection ack fields missing: %+v", got.ack)
	}
	if status := controller.Status(); status.VisibleWatermark != status.LatestLSN || status.Pending != 0 {
		t.Fatalf("status after strict projection: %+v", status)
	}
}

func TestControllerBoundedAndEventualAcknowledgeBeforeProjection(t *testing.T) {
	for _, mode := range []string{"bounded", "eventual"} {
		t.Run(mode, func(t *testing.T) {
			release := make(chan struct{})
			started := make(chan struct{}, 1)
			cfg := DefaultConfig()
			cfg.BoundedMaxLag = 200 * time.Millisecond
			controller, _ := startTestController(t, cfg, func(ctx context.Context, entry eventbackbone.WALEntry) (map[string]any, error) {
				started <- struct{}{}
				select {
				case <-release:
					return map[string]any{"projected": true}, nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			})

			ack, err := controller.Submit(context.Background(), testEvent(mode+"-1", "s1", mode))
			if err != nil {
				t.Fatalf("Submit: %v", err)
			}
			if ack["visibility_status"] != "pending" {
				t.Fatalf("async ack = %+v", ack)
			}
			if mode == "bounded" && ack["freshness_sla_ms"] != int64(200) {
				t.Fatalf("bounded SLA missing from ack: %+v", ack)
			}
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("projector did not start")
			}
			close(release)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := controller.WaitForRead(ctx, schemas.QueryRequest{AccessConsistency: "strict"}); err != nil {
				t.Fatalf("strict wait after release: %v", err)
			}
		})
	}
}

func TestControllerPreservesPerSessionOrderAndRunsSessionsConcurrently(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Workers = 4
	cfg.QueueSize = 32
	var mu sync.Mutex
	started := make(map[string]chan struct{})
	releases := make(map[string]chan struct{})
	order := make(map[string][]string)
	project := func(ctx context.Context, entry eventbackbone.WALEntry) (map[string]any, error) {
		session := entry.Event.Actor.SessionID
		id := entry.Event.Identity.EventID
		mu.Lock()
		order[session] = append(order[session], id)
		start := started[id]
		release := releases[id]
		mu.Unlock()
		close(start)
		select {
		case <-release:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	controller, _ := startTestController(t, cfg, project)

	events := []schemas.Event{
		testEvent("a1", "session-a", "eventual"),
		testEvent("a2", "session-a", "eventual"),
		testEvent("b1", "session-b", "eventual"),
		testEvent("c1", "session-c", "eventual"),
		testEvent("d1", "session-d", "eventual"),
	}
	for _, ev := range events {
		started[ev.Identity.EventID] = make(chan struct{})
		releases[ev.Identity.EventID] = make(chan struct{})
	}
	for _, ev := range events {
		if _, err := controller.Submit(context.Background(), ev); err != nil {
			t.Fatalf("Submit(%s): %v", ev.Identity.EventID, err)
		}
	}

	select {
	case <-started["a1"]:
	case <-time.After(time.Second):
		t.Fatal("first session-a event did not start")
	}
	select {
	case <-started["a2"]:
		t.Fatal("second event in same session started before first completed")
	case <-time.After(25 * time.Millisecond):
	}

	concurrentOther := false
	for _, id := range []string{"b1", "c1", "d1"} {
		select {
		case <-started[id]:
			concurrentOther = true
		default:
		}
	}
	if !concurrentOther {
		t.Fatal("no independent session projected concurrently")
	}

	close(releases["a1"])
	select {
	case <-started["a2"]:
	case <-time.After(time.Second):
		t.Fatal("second session-a event did not start after first completed")
	}
	for _, release := range releases {
		select {
		case <-release:
		default:
			close(release)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := controller.WaitForRead(ctx, schemas.QueryRequest{AccessConsistency: "strict"}); err != nil {
		t.Fatalf("wait for all projections: %v", err)
	}
	if got := order["session-a"]; len(got) != 2 || got[0] != "a1" || got[1] != "a2" {
		t.Fatalf("session order = %v", got)
	}
}

func TestControllerPreservesPerSessionOrderAcrossConsistencyModes(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Workers = 1
	cfg.QueueSize = 4
	firstStarted := make(chan struct{})
	strictStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var mu sync.Mutex
	order := make([]string, 0, 2)
	controller, _ := startTestController(t, cfg, func(ctx context.Context, entry eventbackbone.WALEntry) (map[string]any, error) {
		id := entry.Event.Identity.EventID
		mu.Lock()
		order = append(order, id)
		mu.Unlock()
		switch id {
		case "eventual-first":
			close(firstStarted)
			select {
			case <-releaseFirst:
				return nil, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		case "strict-second":
			close(strictStarted)
		}
		return nil, nil
	})

	if _, err := controller.Submit(context.Background(), testEvent("eventual-first", "same-session", "eventual")); err != nil {
		t.Fatalf("Submit eventual: %v", err)
	}
	<-firstStarted

	strictDone := make(chan error, 1)
	go func() {
		_, err := controller.Submit(context.Background(), testEvent("strict-second", "same-session", "strict"))
		strictDone <- err
	}()

	overtook := false
	select {
	case <-strictStarted:
		overtook = true
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseFirst)
	if err := <-strictDone; err != nil {
		t.Fatalf("Submit strict: %v", err)
	}
	if overtook {
		t.Fatal("strict projection overtook an earlier eventual event in the same session")
	}

	mu.Lock()
	defer mu.Unlock()
	if got := fmt.Sprint(order); got != "[eventual-first strict-second]" {
		t.Fatalf("mixed-mode projection order = %s", got)
	}
}

func TestControllerQueueFullWaitsForCapacityBeforeWALAppend(t *testing.T) {
	cfg := DefaultConfig()
	cfg.QueueSize = 1
	cfg.Workers = 1
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	controller, wal := startTestController(t, cfg, func(ctx context.Context, entry eventbackbone.WALEntry) (map[string]any, error) {
		started <- struct{}{}
		select {
		case <-release:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})

	if _, err := controller.Submit(context.Background(), testEvent("one", "s1", "eventual")); err != nil {
		t.Fatalf("first Submit: %v", err)
	}
	<-started
	before := wal.LatestLSN()
	type result struct {
		ack map[string]any
		err error
	}
	done := make(chan result, 1)
	go func() {
		ack, err := controller.Submit(context.Background(), testEvent("two", "s2", "eventual"))
		done <- result{ack: ack, err: err}
	}()
	select {
	case got := <-done:
		close(release)
		t.Fatalf("second Submit returned before capacity was available: ack=%v err=%v", got.ack, got.err)
	case <-time.After(25 * time.Millisecond):
	}
	if after := wal.LatestLSN(); after != before {
		close(release)
		t.Fatalf("WAL advanced while admission waited: before=%d after=%d", before, after)
	}
	close(release)
	got := <-done
	if got.err != nil {
		t.Fatalf("second Submit after capacity release: %v", got.err)
	}
	if got.ack["event_id"] != "two" {
		t.Fatalf("second Submit ack = %v", got.ack)
	}
	if after := wal.LatestLSN(); after <= before {
		t.Fatalf("WAL did not advance after capacity release: before=%d after=%d", before, after)
	}
}

func TestControllerShutdownCancelsBlockedAdmission(t *testing.T) {
	cfg := DefaultConfig()
	cfg.QueueSize = 1
	cfg.Workers = 1
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	controller, _ := startTestController(t, cfg, func(ctx context.Context, entry eventbackbone.WALEntry) (map[string]any, error) {
		started <- struct{}{}
		select {
		case <-release:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})

	if _, err := controller.Submit(context.Background(), testEvent("one", "s1", "eventual")); err != nil {
		t.Fatalf("first Submit: %v", err)
	}
	<-started
	admissionDone := make(chan error, 1)
	go func() {
		_, err := controller.Submit(context.Background(), testEvent("two", "s2", "eventual"))
		admissionDone <- err
	}()
	select {
	case err := <-admissionDone:
		close(release)
		t.Fatalf("second Submit returned before shutdown: %v", err)
	case <-time.After(25 * time.Millisecond):
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- controller.Shutdown(shutdownCtx)
	}()

	select {
	case err := <-shutdownDone:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Shutdown error = %v, want context deadline exceeded", err)
		}
	case <-time.After(250 * time.Millisecond):
		close(release)
		t.Fatal("Shutdown did not cancel the blocked admission before waiting for projection drain")
	}
	select {
	case err := <-admissionDone:
		if !errors.Is(err, ErrPaused) && !errors.Is(err, ErrNotStarted) {
			t.Fatalf("blocked Submit error = %v, want controller admission error", err)
		}
	case <-time.After(250 * time.Millisecond):
		close(release)
		t.Fatal("blocked Submit did not return after shutdown began")
	}
}

func TestControllerShutdownDoesNotWaitBehindStrictVisibility(t *testing.T) {
	cfg := DefaultConfig()
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	controller, _ := startTestController(t, cfg, func(ctx context.Context, entry eventbackbone.WALEntry) (map[string]any, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-release:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})

	strictDone := make(chan error, 1)
	go func() {
		_, err := controller.Submit(context.Background(), testEvent("strict", "s1", "strict"))
		strictDone <- err
	}()
	<-started

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- controller.Shutdown(shutdownCtx)
	}()
	select {
	case err := <-shutdownDone:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Shutdown error = %v, want context deadline exceeded", err)
		}
	case <-time.After(250 * time.Millisecond):
		close(release)
		t.Fatal("Shutdown remained blocked behind a strict visibility wait")
	}
	select {
	case err := <-strictDone:
		var acceptedErr *AcceptedNotVisibleError
		if !errors.As(err, &acceptedErr) {
			t.Fatalf("strict Submit error = %v, want AcceptedNotVisibleError", err)
		}
	case <-time.After(250 * time.Millisecond):
		close(release)
		t.Fatal("strict Submit did not return after shutdown cancelled the projector")
	}
}

func TestControllerPauseDoesNotStrandQueuedStrictWrite(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Workers = 1
	cfg.QueueSize = 2
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	controller, _ := startTestController(t, cfg, func(ctx context.Context, entry eventbackbone.WALEntry) (map[string]any, error) {
		if entry.Event.Identity.EventID == "active" {
			started <- struct{}{}
			select {
			case <-release:
				return nil, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return nil, nil
	})

	if _, err := controller.Submit(context.Background(), testEvent("active", "same", "eventual")); err != nil {
		t.Fatalf("Submit active: %v", err)
	}
	<-started
	strictDone := make(chan error, 1)
	go func() {
		_, err := controller.Submit(context.Background(), testEvent("queued-strict", "same", "strict"))
		strictDone <- err
	}()
	time.Sleep(25 * time.Millisecond)

	pauseCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	pauseDone := make(chan error, 1)
	go func() {
		pauseDone <- controller.Pause(pauseCtx)
	}()
	select {
	case err := <-strictDone:
		var acceptedErr *AcceptedNotVisibleError
		if !errors.As(err, &acceptedErr) || !errors.Is(err, errOldGeneration) {
			close(release)
			t.Fatalf("queued strict Submit error = %v, want old-generation AcceptedNotVisibleError", err)
		}
	case <-time.After(250 * time.Millisecond):
		close(release)
		t.Fatal("Pause drained a queued strict write without waking its caller")
	}
	close(release)
	if err := <-pauseDone; err != nil {
		t.Fatalf("Pause: %v", err)
	}
}

func TestControllerBoundedAdmissionWaitsForLagRecovery(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BoundedMaxLag = 20 * time.Millisecond
	cfg.QueueSize = 4
	cfg.Workers = 1
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	controller, wal := startTestController(t, cfg, func(ctx context.Context, entry eventbackbone.WALEntry) (map[string]any, error) {
		started <- struct{}{}
		select {
		case <-release:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})

	if _, err := controller.Submit(context.Background(), testEvent("lagged", "s1", "bounded")); err != nil {
		t.Fatalf("first Submit: %v", err)
	}
	<-started
	time.Sleep(30 * time.Millisecond)
	before := wal.LatestLSN()

	done := make(chan error, 1)
	go func() {
		_, err := controller.Submit(context.Background(), testEvent("after-lag", "s2", "bounded"))
		done <- err
	}()
	select {
	case err := <-done:
		close(release)
		t.Fatalf("bounded Submit returned before lag recovered: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	if after := wal.LatestLSN(); after != before {
		close(release)
		t.Fatalf("WAL advanced while bounded admission waited: before=%d after=%d", before, after)
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatalf("bounded Submit after lag recovery: %v", err)
	}
}

func TestControllerBoundedAdmissionDoesNotOversubscribeSLA(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BoundedMaxLag = 45 * time.Millisecond
	cfg.QueueSize = 16
	cfg.Workers = 1
	controller, _ := startTestController(t, cfg, func(ctx context.Context, entry eventbackbone.WALEntry) (map[string]any, error) {
		select {
		case <-time.After(30 * time.Millisecond):
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})

	for i := 0; i < 4; i++ {
		ack, err := controller.Submit(
			context.Background(),
			testEvent(fmt.Sprintf("bounded-%d", i), "same-session", "bounded"),
		)
		if err != nil {
			t.Fatalf("Submit(%d): %v", i, err)
		}
		if ack["visibility_status"] != "pending" {
			t.Fatalf("Submit(%d) ack = %+v, want pending", i, ack)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := controller.WaitForRead(ctx, schemas.QueryRequest{AccessConsistency: "strict"}); err != nil {
		t.Fatalf("WaitForRead: %v", err)
	}
	if status := controller.Status(); status.SLABreaches != 0 {
		t.Fatalf("bounded admission oversubscribed its SLA: %+v", status)
	}
}

func TestControllerBoundedAdmissionExcludesMixedModeInsertions(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BoundedMaxLag = 100 * time.Millisecond
	cfg.QueueSize = 16
	cfg.Workers = 3
	existingStarted := make(chan struct{})
	releaseExisting := make(chan struct{})
	releaseInterloper := make(chan struct{})
	controller, _ := startTestController(t, cfg, func(ctx context.Context, entry eventbackbone.WALEntry) (map[string]any, error) {
		switch entry.Event.Identity.EventID {
		case "existing":
			close(existingStarted)
			select {
			case <-releaseExisting:
				return nil, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		case "interloper":
			select {
			case <-releaseInterloper:
				return nil, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		default:
			return nil, nil
		}
	})

	if _, err := controller.Submit(context.Background(), testEvent("existing", "existing-session", "eventual")); err != nil {
		t.Fatalf("Submit(existing): %v", err)
	}
	<-existingStarted

	boundedDone := make(chan error, 1)
	go func() {
		_, err := controller.Submit(context.Background(), testEvent("bounded", "bounded-session", "bounded"))
		boundedDone <- err
	}()
	time.Sleep(20 * time.Millisecond)

	interloperDone := make(chan error, 1)
	go func() {
		_, err := controller.Submit(context.Background(), testEvent("interloper", "interloper-session", "eventual"))
		interloperDone <- err
	}()
	select {
	case err := <-interloperDone:
		t.Fatalf("mixed-mode write entered bounded drain-to-append window: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(releaseExisting)
	if err := <-boundedDone; err != nil {
		t.Fatalf("Submit(bounded): %v", err)
	}
	if err := <-interloperDone; err != nil {
		t.Fatalf("Submit(interloper): %v", err)
	}
	close(releaseInterloper)
}

func TestControllerRetriesThenAdvancesVisibility(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxRetries = 3
	cfg.RetryBaseDelay = time.Millisecond
	cfg.RetryMaxDelay = 2 * time.Millisecond
	var attempts atomic.Int32
	controller, _ := startTestController(t, cfg, func(ctx context.Context, entry eventbackbone.WALEntry) (map[string]any, error) {
		if attempts.Add(1) < 3 {
			return nil, errors.New("temporary")
		}
		return nil, nil
	})
	if _, err := controller.Submit(context.Background(), testEvent("retry", "s1", "eventual")); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := controller.WaitForRead(ctx, schemas.QueryRequest{AccessConsistency: "strict"}); err != nil {
		t.Fatalf("WaitForRead: %v", err)
	}
	if attempts.Load() != 3 {
		t.Fatalf("attempts = %d, want 3", attempts.Load())
	}
	if status := controller.Status(); status.Retrying != 0 || status.Failed != 0 {
		t.Fatalf("status after retry success: %+v", status)
	}
}

func TestControllerTerminalFailureBlocksStrictRead(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxRetries = 2
	cfg.RetryBaseDelay = time.Millisecond
	cfg.RetryMaxDelay = time.Millisecond
	controller, _ := startTestController(t, cfg, func(context.Context, eventbackbone.WALEntry) (map[string]any, error) {
		return nil, errors.New("permanent")
	})
	if _, err := controller.Submit(context.Background(), testEvent("fail", "s1", "eventual")); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := controller.WaitForRead(ctx, schemas.QueryRequest{AccessConsistency: "strict"})
	var projectionErr *ProjectionFailureError
	if !errors.As(err, &projectionErr) {
		t.Fatalf("strict read error = %v, want ProjectionFailureError", err)
	}
	if status := controller.Status(); status.Failed != 1 || status.LastError == "" {
		t.Fatalf("terminal failure status: %+v", status)
	}
}

func TestControllerStrictFailureReturnsAcceptedNotVisibleAndContinuesRecovery(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxRetries = 1
	cfg.RetryBaseDelay = time.Millisecond
	cfg.RetryMaxDelay = time.Millisecond
	allowRecovery := make(chan struct{})
	var attempts atomic.Int32
	controller, _ := startTestController(t, cfg, func(ctx context.Context, entry eventbackbone.WALEntry) (map[string]any, error) {
		if attempts.Add(1) == 1 {
			return nil, errors.New("temporarily unavailable")
		}
		select {
		case <-allowRecovery:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})

	_, err := controller.Submit(context.Background(), testEvent("strict-recovery", "s1", "strict"))
	var acceptedErr *AcceptedNotVisibleError
	if !errors.As(err, &acceptedErr) {
		t.Fatalf("Submit error = %v, want AcceptedNotVisibleError", err)
	}
	if acceptedErr.LSN == 0 || acceptedErr.EventID != "strict-recovery" {
		t.Fatalf("accepted error = %+v", acceptedErr)
	}
	close(allowRecovery)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := controller.WaitForRead(ctx, schemas.QueryRequest{AccessConsistency: "strict"}); err != nil {
		t.Fatalf("background recovery did not make strict write visible: %v", err)
	}
	if attempts.Load() < 2 {
		t.Fatalf("attempts = %d, want synchronous failure plus background recovery", attempts.Load())
	}
}

func TestControllerBoundedReadWaitsAfterLagExceededAndEventualDoesNot(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BoundedMaxLag = 20 * time.Millisecond
	release := make(chan struct{})
	controller, _ := startTestController(t, cfg, func(ctx context.Context, entry eventbackbone.WALEntry) (map[string]any, error) {
		select {
		case <-release:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	if _, err := controller.Submit(context.Background(), testEvent("bounded", "s1", "bounded")); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	if err := controller.WaitForRead(context.Background(), schemas.QueryRequest{AccessConsistency: "eventual"}); err != nil {
		t.Fatalf("eventual read should not wait: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- controller.WaitForRead(ctx, schemas.QueryRequest{AccessConsistency: "bounded"})
	}()
	select {
	case err := <-done:
		t.Fatalf("bounded read returned while lag exceeded: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("bounded read after visibility: %v", err)
	}
}

func TestControllerPauseIsolatesQueuedGenerationAndResumeAcceptsNewWrites(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Workers = 1
	cfg.QueueSize = 4
	firstRelease := make(chan struct{})
	firstStarted := make(chan struct{})
	var mu sync.Mutex
	projected := make([]string, 0, 2)
	controller, _ := startTestController(t, cfg, func(ctx context.Context, entry eventbackbone.WALEntry) (map[string]any, error) {
		id := entry.Event.Identity.EventID
		mu.Lock()
		projected = append(projected, id)
		mu.Unlock()
		if id == "old-active" {
			close(firstStarted)
			select {
			case <-firstRelease:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return nil, nil
	})
	if _, err := controller.Submit(context.Background(), testEvent("old-active", "same", "eventual")); err != nil {
		t.Fatalf("Submit active: %v", err)
	}
	if _, err := controller.Submit(context.Background(), testEvent("old-queued", "same", "eventual")); err != nil {
		t.Fatalf("Submit queued: %v", err)
	}
	<-firstStarted

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	paused := make(chan error, 1)
	go func() { paused <- controller.Pause(ctx) }()
	select {
	case err := <-paused:
		t.Fatalf("Pause returned while projection active: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(firstRelease)
	if err := <-paused; err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if _, err := controller.Submit(context.Background(), testEvent("paused", "new", "strict")); !errors.Is(err, ErrPaused) {
		t.Fatalf("Submit while paused = %v, want ErrPaused", err)
	}
	if err := controller.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	controller.Resume()
	if _, err := controller.Submit(context.Background(), testEvent("new", "new", "strict")); err != nil {
		t.Fatalf("Submit after resume: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, id := range projected {
		if id == "old-queued" || id == "paused" {
			t.Fatalf("old or paused event projected: %v", projected)
		}
	}
	if fmt.Sprint(projected) != "[old-active new]" {
		t.Fatalf("projected events = %v", projected)
	}
}

func TestControllerStartReplaysOnlyEntriesAfterCheckpoint(t *testing.T) {
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	wal := eventbackbone.NewInMemoryWAL(bus, clock)
	first, err := wal.Append(testEvent("already-visible", "s1", "strict"))
	if err != nil {
		t.Fatalf("append first: %v", err)
	}
	second, err := wal.Append(testEvent("needs-recovery", "s1", "eventual"))
	if err != nil {
		t.Fatalf("append second: %v", err)
	}
	checkpoint := NewMemoryCheckpoint()
	if err := checkpoint.Save(first.LSN); err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}
	projected := make(chan string, 1)
	controller, err := NewController(
		wal,
		eventbackbone.NewWatermarkPublisher(clock, bus),
		checkpoint,
		DefaultConfig(),
		func(ctx context.Context, entry eventbackbone.WALEntry) (map[string]any, error) {
			projected <- entry.Event.Identity.EventID
			return nil, nil
		},
	)
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	if err := controller.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = controller.Shutdown(ctx)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := controller.WaitForRead(ctx, schemas.QueryRequest{AccessConsistency: "strict"}); err != nil {
		t.Fatalf("wait for recovery: %v", err)
	}
	select {
	case id := <-projected:
		if id != "needs-recovery" {
			t.Fatalf("replayed event = %q", id)
		}
	default:
		t.Fatal("entry after checkpoint was not replayed")
	}
	if got := controller.Status().VisibleWatermark; got != second.LSN {
		t.Fatalf("watermark = %d, want %d", got, second.LSN)
	}
}

func TestControllerBootstrapsMissingPersistentCheckpointAtLegacyWALTail(t *testing.T) {
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	wal := eventbackbone.NewInMemoryWAL(bus, clock)
	entry, err := wal.Append(testEvent("legacy-synchronous", "s1", "strict"))
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	checkpoint := NewMemoryCheckpoint()
	cfg := DefaultConfig()
	cfg.BootstrapCheckpointAtLatest = true
	var projected atomic.Int32
	controller, err := NewController(
		wal,
		eventbackbone.NewWatermarkPublisher(clock, bus),
		checkpoint,
		cfg,
		func(context.Context, eventbackbone.WALEntry) (map[string]any, error) {
			projected.Add(1)
			return nil, nil
		},
	)
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	if err := controller.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = controller.Shutdown(ctx)
	}()

	if projected.Load() != 0 {
		t.Fatalf("legacy WAL was unexpectedly replayed %d times", projected.Load())
	}
	status := controller.Status()
	if status.VisibleWatermark != entry.LSN || status.LatestLSN != entry.LSN {
		t.Fatalf("bootstrapped status = %+v", status)
	}
	if lsn, exists, err := checkpoint.Load(); err != nil || !exists || lsn != entry.LSN {
		t.Fatalf("bootstrapped checkpoint: lsn=%d exists=%t err=%v", lsn, exists, err)
	}
}
