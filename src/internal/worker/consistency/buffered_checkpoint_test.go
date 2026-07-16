package consistency

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"plasmod/src/internal/eventbackbone"
	"plasmod/src/internal/schemas"
)

type recordingCheckpointStore struct {
	mu    sync.Mutex
	store *MemoryCheckpoint
	saves []int64
}

func newRecordingCheckpointStore() *recordingCheckpointStore {
	return &recordingCheckpointStore{store: NewMemoryCheckpoint()}
}

func (s *recordingCheckpointStore) Load() (int64, bool, error) {
	return s.store.Load()
}

func (s *recordingCheckpointStore) Save(lsn int64) error {
	s.mu.Lock()
	s.saves = append(s.saves, lsn)
	s.mu.Unlock()
	return s.store.Save(lsn)
}

func (s *recordingCheckpointStore) Reset() error {
	return s.store.Reset()
}

func (s *recordingCheckpointStore) savedLSNs() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int64(nil), s.saves...)
}

type blockingCheckpointStore struct {
	CheckpointStore
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

type failingCheckpointStore struct {
	CheckpointStore
	err error
}

type blockingLoadCheckpointStore struct {
	CheckpointStore
	loaded  chan struct{}
	release chan struct{}
}

func (s *blockingLoadCheckpointStore) Load() (int64, bool, error) {
	lsn, exists, err := s.CheckpointStore.Load()
	close(s.loaded)
	<-s.release
	return lsn, exists, err
}

func (s *failingCheckpointStore) Save(int64) error {
	return s.err
}

func (s *blockingCheckpointStore) Save(lsn int64) error {
	s.once.Do(func() { close(s.started) })
	<-s.release
	return s.CheckpointStore.Save(lsn)
}

func TestBufferedCheckpointCoalescesPendingWatermarks(t *testing.T) {
	inner := newRecordingCheckpointStore()
	store := NewBufferedCheckpoint(inner, time.Hour)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = store.Close(ctx)
	})

	for lsn := int64(1); lsn <= 100; lsn++ {
		if err := store.Save(lsn); err != nil {
			t.Fatalf("Save(%d): %v", lsn, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := store.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	saves := inner.savedLSNs()
	if len(saves) != 1 || saves[0] != 100 {
		t.Fatalf("persisted checkpoints = %v, want [100]", saves)
	}
}

func TestBufferedCheckpointCloseAllowsNilContext(t *testing.T) {
	store := NewBufferedCheckpoint(NewMemoryCheckpoint(), time.Hour)
	if err := store.Save(42); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := store.Close(nil); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := store.Close(nil); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestBufferedCheckpointResetSupersedesInFlightFlush(t *testing.T) {
	inner := &blockingCheckpointStore{
		CheckpointStore: NewMemoryCheckpoint(),
		started:         make(chan struct{}),
		release:         make(chan struct{}),
	}
	store := NewBufferedCheckpoint(inner, time.Hour)
	defer func() {
		select {
		case <-inner.release:
		default:
			close(inner.release)
		}
		_ = store.Close(nil)
	}()

	if err := store.Save(100); err != nil {
		t.Fatalf("Save(100): %v", err)
	}
	flushDone := make(chan error, 1)
	go func() { flushDone <- store.Flush(context.Background()) }()
	select {
	case <-inner.started:
	case <-time.After(time.Second):
		t.Fatal("checkpoint save did not start")
	}

	resetDone := make(chan error, 1)
	go func() { resetDone <- store.Reset() }()
	select {
	case err := <-flushDone:
		if !errors.Is(err, errBufferedCheckpointReset) {
			t.Fatalf("Flush error = %v, want reset", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("Flush did not stop after checkpoint reset")
	}

	close(inner.release)
	if err := <-resetDone; err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if err := store.Save(1); err != nil {
		t.Fatalf("post-reset Save(1): %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := store.Flush(ctx); err != nil {
		t.Fatalf("post-reset Flush: %v", err)
	}
	if lsn, exists, err := store.Load(); err != nil || !exists || lsn != 1 {
		t.Fatalf("post-reset checkpoint: lsn=%d exists=%t err=%v", lsn, exists, err)
	}
}

func TestBufferedCheckpointLoadCannotRestorePreResetLSN(t *testing.T) {
	for attempt := 0; attempt < 1000; attempt++ {
		memory := NewMemoryCheckpoint()
		if err := memory.Save(100); err != nil {
			t.Fatalf("seed checkpoint: %v", err)
		}
		inner := &blockingLoadCheckpointStore{
			CheckpointStore: memory,
			loaded:          make(chan struct{}),
			release:         make(chan struct{}),
		}
		store := NewBufferedCheckpoint(inner, time.Hour)
		loadDone := make(chan error, 1)
		go func() {
			_, _, err := store.Load()
			loadDone <- err
		}()
		<-inner.loaded

		resetDone := make(chan error, 1)
		go func() { resetDone <- store.Reset() }()
		for {
			store.mu.Lock()
			resetting := store.resetting
			store.mu.Unlock()
			if resetting {
				break
			}
			runtime.Gosched()
		}
		close(inner.release)
		if err := <-loadDone; err != nil {
			t.Fatalf("Load: %v", err)
		}
		if err := <-resetDone; err != nil {
			t.Fatalf("Reset: %v", err)
		}
		store.mu.Lock()
		persisted, exists := store.persisted, store.persistedExists
		store.mu.Unlock()
		if exists {
			t.Fatalf("attempt %d restored stale checkpoint %d after reset", attempt, persisted)
		}
	}
}

func TestBufferedCheckpointCloseRejectsConcurrentSave(t *testing.T) {
	inner := &blockingCheckpointStore{
		CheckpointStore: NewMemoryCheckpoint(),
		started:         make(chan struct{}),
		release:         make(chan struct{}),
	}
	store := NewBufferedCheckpoint(inner, time.Hour)
	defer func() {
		select {
		case <-inner.release:
		default:
			close(inner.release)
		}
	}()
	if err := store.Save(100); err != nil {
		t.Fatalf("Save(100): %v", err)
	}
	closeDone := make(chan error, 1)
	go func() { closeDone <- store.Close(nil) }()
	select {
	case <-inner.started:
	case <-time.After(time.Second):
		t.Fatal("final checkpoint save did not start")
	}
	if err := store.Save(101); !errors.Is(err, errBufferedCheckpointClosed) {
		t.Fatalf("Save during Close = %v, want closed", err)
	}
	close(inner.release)
	if err := <-closeDone; err != nil {
		t.Fatalf("Close: %v", err)
	}
	if lsn, exists, err := inner.Load(); err != nil || !exists || lsn != 100 {
		t.Fatalf("final checkpoint: lsn=%d exists=%t err=%v", lsn, exists, err)
	}
}

func TestBufferedCheckpointRepeatedClosePreservesFlushError(t *testing.T) {
	wantErr := errors.New("checkpoint unavailable")
	store := NewBufferedCheckpoint(&failingCheckpointStore{
		CheckpointStore: NewMemoryCheckpoint(),
		err:             wantErr,
	}, time.Hour)
	if err := store.Save(1); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := store.Close(nil); !errors.Is(err, wantErr) {
		t.Fatalf("first Close error = %v, want %v", err, wantErr)
	}
	if err := store.Close(nil); !errors.Is(err, wantErr) {
		t.Fatalf("second Close error = %v, want %v", err, wantErr)
	}
}

func TestControllerVisibilityDoesNotWaitForPersistentCheckpointSync(t *testing.T) {
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	checkpoint := &blockingCheckpointStore{
		CheckpointStore: NewMemoryCheckpoint(),
		started:         make(chan struct{}),
		release:         make(chan struct{}),
	}
	cfg := DefaultConfig()
	cfg.CheckpointPath = "persistent-checkpoint"
	cfg.CheckpointFlushInterval = time.Millisecond
	controller, err := NewController(
		eventbackbone.NewInMemoryWAL(bus, clock),
		eventbackbone.NewWatermarkPublisher(clock, bus),
		checkpoint,
		cfg,
		func(context.Context, eventbackbone.WALEntry) (map[string]any, error) {
			return map[string]any{"status": "ok"}, nil
		},
	)
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	if err := controller.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		select {
		case <-checkpoint.release:
		default:
			close(checkpoint.release)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = controller.Shutdown(ctx)
	}()

	ack, err := controller.Submit(context.Background(), testEvent("async-checkpoint", "s1", "eventual"))
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	select {
	case <-checkpoint.started:
	case <-time.After(time.Second):
		t.Fatal("persistent checkpoint did not start")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := controller.WaitForRead(ctx, schemas.QueryRequest{AccessConsistency: "strict"}); err != nil {
		t.Fatalf("visibility waited for checkpoint sync: %v", err)
	}
	if got := controller.Status().VisibleWatermark; got != ack["lsn"].(int64) {
		t.Fatalf("visible watermark = %d, want %d", got, ack["lsn"].(int64))
	}
}

func TestControllerCanKeepSynchronousCheckpointSemantics(t *testing.T) {
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	checkpoint := &blockingCheckpointStore{
		CheckpointStore: NewMemoryCheckpoint(),
		started:         make(chan struct{}),
		release:         make(chan struct{}),
	}
	cfg := DefaultConfig()
	cfg.CheckpointPath = "persistent-checkpoint"
	cfg.CheckpointFlushInterval = 0
	controller, err := NewController(
		eventbackbone.NewInMemoryWAL(bus, clock),
		eventbackbone.NewWatermarkPublisher(clock, bus),
		checkpoint,
		cfg,
		func(context.Context, eventbackbone.WALEntry) (map[string]any, error) {
			return map[string]any{"status": "ok"}, nil
		},
	)
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	if err := controller.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, buffered := controller.checkpoint.(*BufferedCheckpoint); buffered {
		t.Fatal("checkpoint buffering should be disabled")
	}
	defer func() {
		select {
		case <-checkpoint.release:
		default:
			close(checkpoint.release)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = controller.Shutdown(ctx)
	}()

	if _, err := controller.Submit(context.Background(), testEvent("sync-checkpoint", "s1", "eventual")); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	select {
	case <-checkpoint.started:
	case <-time.After(time.Second):
		t.Fatal("synchronous checkpoint did not start")
	}
	close(checkpoint.release)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := controller.WaitForRead(ctx, schemas.QueryRequest{AccessConsistency: "strict"}); err != nil {
		t.Fatalf("WaitForRead after checkpoint release: %v", err)
	}
}

func TestControllerStatusCombinesProjectionAndCheckpointErrors(t *testing.T) {
	wantErr := errors.New("checkpoint unavailable")
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	cfg := DefaultConfig()
	cfg.CheckpointPath = "persistent-checkpoint"
	controller, err := NewController(
		eventbackbone.NewInMemoryWAL(bus, clock),
		eventbackbone.NewWatermarkPublisher(clock, bus),
		&failingCheckpointStore{CheckpointStore: NewMemoryCheckpoint(), err: wantErr},
		cfg,
		func(context.Context, eventbackbone.WALEntry) (map[string]any, error) { return nil, nil },
	)
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	checkpoint := controller.checkpoint.(*BufferedCheckpoint)
	if err := checkpoint.Save(1); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := checkpoint.Flush(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("Flush error = %v, want %v", err, wantErr)
	}
	controller.tracker.mu.Lock()
	controller.tracker.lastError = "projection unavailable"
	controller.tracker.mu.Unlock()

	lastError := controller.Status().LastError
	if !strings.Contains(lastError, "projection unavailable") || !strings.Contains(lastError, wantErr.Error()) {
		t.Fatalf("combined status error = %q", lastError)
	}
	_ = checkpoint.Close(nil)
}

func TestControllerRepeatedShutdownPreservesCheckpointError(t *testing.T) {
	wantErr := errors.New("checkpoint unavailable")
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	cfg := DefaultConfig()
	cfg.CheckpointPath = "persistent-checkpoint"
	cfg.CheckpointFlushInterval = time.Hour
	controller, err := NewController(
		eventbackbone.NewInMemoryWAL(bus, clock),
		eventbackbone.NewWatermarkPublisher(clock, bus),
		&failingCheckpointStore{CheckpointStore: NewMemoryCheckpoint(), err: wantErr},
		cfg,
		func(context.Context, eventbackbone.WALEntry) (map[string]any, error) {
			return map[string]any{"status": "ok"}, nil
		},
	)
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	if err := controller.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := controller.Submit(context.Background(), testEvent("shutdown-error", "s1", "eventual")); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := controller.WaitForRead(ctx, schemas.QueryRequest{AccessConsistency: "strict"}); err != nil {
		t.Fatalf("WaitForRead: %v", err)
	}
	if err := controller.Shutdown(ctx); !errors.Is(err, wantErr) {
		t.Fatalf("first Shutdown error = %v, want %v", err, wantErr)
	}
	if err := controller.Shutdown(ctx); !errors.Is(err, wantErr) {
		t.Fatalf("second Shutdown error = %v, want %v", err, wantErr)
	}
}

func TestControllerShutdownFlushesBufferedCheckpoint(t *testing.T) {
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	checkpoint := NewMemoryCheckpoint()
	cfg := DefaultConfig()
	cfg.CheckpointPath = "persistent-checkpoint"
	cfg.CheckpointFlushInterval = time.Hour
	controller, err := NewController(
		eventbackbone.NewInMemoryWAL(bus, clock),
		eventbackbone.NewWatermarkPublisher(clock, bus),
		checkpoint,
		cfg,
		func(context.Context, eventbackbone.WALEntry) (map[string]any, error) {
			return map[string]any{"status": "ok"}, nil
		},
	)
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	if err := controller.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	ack, err := controller.Submit(context.Background(), testEvent("flush-on-shutdown", "s1", "eventual"))
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := controller.WaitForRead(ctx, schemas.QueryRequest{AccessConsistency: "strict"}); err != nil {
		t.Fatalf("WaitForRead: %v", err)
	}
	if err := controller.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if lsn, exists, err := checkpoint.Load(); err != nil || !exists || lsn != ack["lsn"].(int64) {
		t.Fatalf("checkpoint after shutdown: lsn=%d exists=%t err=%v", lsn, exists, err)
	}
}
