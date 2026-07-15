package consistency

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"sync"
	"time"

	"plasmod/src/internal/eventbackbone"
	"plasmod/src/internal/schemas"
)

var (
	ErrBackpressure  = errors.New("consistency projection queue is full")
	ErrPaused        = errors.New("consistency controller is paused")
	ErrNotStarted    = errors.New("consistency controller is not started")
	errOldGeneration = errors.New("projection belongs to an old runtime generation")
)

// ProjectFunc applies one accepted WAL entry to canonical and retrieval state.
type ProjectFunc func(context.Context, eventbackbone.WALEntry) (map[string]any, error)

// AcceptedNotVisibleError reports a durable WAL acceptance whose strict
// projection did not become visible before the synchronous attempts ended.
type AcceptedNotVisibleError struct {
	LSN     int64
	EventID string
	Err     error
}

func (e *AcceptedNotVisibleError) Error() string {
	return fmt.Sprintf("event %s accepted at lsn %d but not visible: %v", e.EventID, e.LSN, e.Err)
}

func (e *AcceptedNotVisibleError) Unwrap() error { return e.Err }

type projectionTask struct {
	entry           eventbackbone.WALEntry
	mode            Mode
	lag             time.Duration
	acceptedAt      time.Time
	deadline        time.Time
	generation      uint64
	strictDone      chan projectionResult
	boundedShard    int
	boundedReserved bool
}

type projectionResult struct {
	ack map[string]any
	err error
}

// ControllerStatus combines mode, queue, and tracker health for operations APIs.
type ControllerStatus struct {
	DefaultMode    Mode     `json:"mode"`
	SupportedModes []string `json:"supported_modes"`
	DataPathActive bool     `json:"data_path_active"`
	QueueDepth     int      `json:"queue_depth"`
	QueueCapacity  int      `json:"queue_capacity"`
	TrackerStatus
}

// Controller coordinates WAL acceptance, projection, and query visibility.
type Controller struct {
	wal        eventbackbone.WAL
	project    ProjectFunc
	cfg        Config
	checkpoint CheckpointStore
	tracker    *Tracker
	initialLSN int64

	modeMu      sync.RWMutex
	defaultMode Mode

	admissionMu sync.RWMutex
	// modeGate keeps mixed-mode writes out of a bounded drain-to-append window.
	modeGate     sync.RWMutex
	appendMu     sync.Mutex
	slots        chan struct{}
	queues       []chan projectionTask
	boundedSlots []chan struct{}

	stateMu         sync.RWMutex
	started         bool
	accepting       bool
	generation      uint64
	rootCtx         context.Context
	cancel          context.CancelFunc
	admissionCtx    context.Context
	cancelAdmission context.CancelFunc

	activeMu      sync.Mutex
	active        int
	activeChanged chan struct{}
	workers       sync.WaitGroup
}

func NewController(
	wal eventbackbone.WAL,
	watermark WatermarkAdvancer,
	checkpoint CheckpointStore,
	cfg Config,
	project ProjectFunc,
) (*Controller, error) {
	if wal == nil {
		return nil, errors.New("consistency controller requires WAL")
	}
	if project == nil {
		return nil, errors.New("consistency controller requires projector")
	}
	if cfg.QueueSize <= 0 || cfg.Workers <= 0 || cfg.MaxRetries <= 0 {
		return nil, fmt.Errorf("invalid consistency capacity: queue=%d workers=%d retries=%d", cfg.QueueSize, cfg.Workers, cfg.MaxRetries)
	}
	if cfg.BoundedMaxLag <= 0 || cfg.RetryBaseDelay <= 0 || cfg.RetryMaxDelay < cfg.RetryBaseDelay {
		return nil, errors.New("invalid consistency duration configuration")
	}
	if _, err := ParseMode(string(cfg.DefaultMode)); err != nil {
		return nil, err
	}
	if checkpoint == nil {
		checkpoint = NewMemoryCheckpoint()
	}
	initialLSN, exists, err := checkpoint.Load()
	if err != nil {
		return nil, err
	}
	if !exists && cfg.BootstrapCheckpointAtLatest {
		initialLSN = wal.LatestLSN()
		if err := checkpoint.Save(initialLSN); err != nil {
			return nil, err
		}
	} else if !exists {
		initialLSN = 0
	}

	queues := make([]chan projectionTask, cfg.Workers)
	boundedSlots := make([]chan struct{}, cfg.Workers)
	for i := range queues {
		queues[i] = make(chan projectionTask, cfg.QueueSize)
		boundedSlots[i] = make(chan struct{}, 1)
	}
	return &Controller{
		wal:           wal,
		project:       project,
		cfg:           cfg,
		checkpoint:    checkpoint,
		tracker:       NewTracker(initialLSN, watermark, checkpoint),
		initialLSN:    initialLSN,
		defaultMode:   cfg.DefaultMode,
		slots:         make(chan struct{}, cfg.QueueSize),
		queues:        queues,
		boundedSlots:  boundedSlots,
		generation:    1,
		activeChanged: make(chan struct{}),
	}, nil
}

func (c *Controller) Start(ctx context.Context) error {
	c.stateMu.Lock()
	if c.started {
		c.stateMu.Unlock()
		return nil
	}
	c.rootCtx, c.cancel = context.WithCancel(ctx)
	c.admissionCtx, c.cancelAdmission = context.WithCancel(c.rootCtx)
	c.started = true
	c.accepting = true
	generation := c.generation
	c.stateMu.Unlock()

	for i := range c.queues {
		queue := c.queues[i]
		c.workers.Add(1)
		go c.runWorker(queue)
	}

	if err := c.recoverFromWAL(generation); err != nil {
		c.cancelAdmission()
		c.cancel()
		c.workers.Wait()
		c.drainQueues()
		c.stateMu.Lock()
		c.started = false
		c.accepting = false
		c.stateMu.Unlock()
		return err
	}
	return nil
}

func (c *Controller) recoverFromWAL(generation uint64) error {
	for _, entry := range c.wal.Scan(c.initialLSN + 1) {
		if entry.LSN <= c.initialLSN {
			continue
		}
		mode, lag, normalized, err := ResolveWrite(c.currentDefaultMode(), entry.Event, c.cfg.BoundedMaxLag)
		if err != nil {
			return err
		}
		entry.Event = normalized
		acceptedAt := acceptedTime(entry)
		deadline := time.Time{}
		if mode == BoundedStaleness {
			deadline = acceptedAt.Add(lag)
		}
		boundedShard := 0
		boundedReserved := false
		if mode == BoundedStaleness {
			boundedShard = shardIndex(entry.Event, len(c.queues))
			select {
			case c.boundedSlots[boundedShard] <- struct{}{}:
				boundedReserved = true
			case <-c.rootCtx.Done():
				return c.rootCtx.Err()
			}
		}
		select {
		case c.slots <- struct{}{}:
		case <-c.rootCtx.Done():
			if boundedReserved {
				c.releaseBoundedSlot(boundedShard)
			}
			return c.rootCtx.Err()
		}
		c.tracker.Accept(entry.LSN, acceptedAt, deadline)
		c.enqueue(projectionTask{
			entry: entry, mode: mode, lag: lag, acceptedAt: acceptedAt,
			deadline: deadline, generation: generation,
			boundedShard: boundedShard, boundedReserved: boundedReserved,
		})
	}
	return nil
}

func (c *Controller) Submit(ctx context.Context, ev schemas.Event) (map[string]any, error) {
	c.admissionMu.RLock()
	admissionLocked := true
	defer func() {
		if admissionLocked {
			c.admissionMu.RUnlock()
		}
	}()

	c.stateMu.RLock()
	started, accepting, generation := c.started, c.accepting, c.generation
	rootCtx, admissionCtx := c.rootCtx, c.admissionCtx
	c.stateMu.RUnlock()
	if !started {
		return nil, ErrNotStarted
	}
	if !accepting {
		return nil, ErrPaused
	}

	mode, lag, normalized, err := ResolveWrite(c.currentDefaultMode(), ev, c.cfg.BoundedMaxLag)
	if err != nil {
		return nil, err
	}
	boundedShard := 0
	boundedReserved := false
	if mode == BoundedStaleness {
		boundedShard = shardIndex(normalized, len(c.queues))
		select {
		case c.boundedSlots[boundedShard] <- struct{}{}:
			boundedReserved = true
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-admissionCtx.Done():
			return nil, c.admissionError()
		}
		defer func() {
			if boundedReserved {
				c.releaseBoundedSlot(boundedShard)
			}
		}()
	}
	select {
	case c.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-admissionCtx.Done():
		return nil, c.admissionError()
	}
	releaseReservation := true
	defer func() {
		if releaseReservation {
			c.releaseSlot()
		}
	}()

	gateReadLocked := false
	gateWriteLocked := false
	defer func() {
		if gateReadLocked {
			c.modeGate.RUnlock()
		}
		if gateWriteLocked {
			c.modeGate.Unlock()
		}
	}()
	if mode == BoundedStaleness {
		c.modeGate.RLock()
		gateReadLocked = true
		waitCtx, cancelWait := context.WithCancel(ctx)
		stopAdmissionCancel := context.AfterFunc(admissionCtx, cancelWait)
		err := c.tracker.WaitThrough(waitCtx, c.wal.LatestLSN())
		stopAdmissionCancel()
		cancelWait()
		if err != nil {
			if admissionCtx.Err() != nil {
				return nil, c.admissionError()
			}
			return nil, err
		}
	} else {
		c.modeGate.Lock()
		gateWriteLocked = true
	}

	ingestStartedAt := time.Now()
	if normalized.Time.IngestTime == 0 {
		normalized.Time.IngestTime = ingestStartedAt.UnixMilli()
	}
	acceptedAt := time.Time{}
	deadline := time.Time{}

	c.appendMu.Lock()
	entry, err := c.wal.Append(normalized)
	if err == nil {
		acceptedAt = acceptedTime(entry)
		if mode == BoundedStaleness {
			deadline = acceptedAt.Add(lag)
		}
		c.tracker.Accept(entry.LSN, acceptedAt, deadline)
	}
	c.appendMu.Unlock()
	if err != nil {
		return nil, err
	}
	if gateReadLocked {
		c.modeGate.RUnlock()
		gateReadLocked = false
	}
	if gateWriteLocked {
		c.modeGate.Unlock()
		gateWriteLocked = false
	}
	task := projectionTask{
		entry: entry, mode: mode, lag: lag, acceptedAt: acceptedAt,
		deadline: deadline, generation: generation,
		boundedShard: boundedShard, boundedReserved: boundedReserved,
	}
	if mode == StrictVisible {
		task.strictDone = make(chan projectionResult, 1)
	}
	c.enqueue(task)
	releaseReservation = false
	boundedReserved = false
	c.admissionMu.RUnlock()
	admissionLocked = false
	if mode == StrictVisible {
		var result projectionResult
		select {
		case result = <-task.strictDone:
		case <-ctx.Done():
			result.err = ctx.Err()
		case <-rootCtx.Done():
			select {
			case result = <-task.strictDone:
			default:
				result.err = rootCtx.Err()
			}
		}
		if result.err != nil {
			return nil, &AcceptedNotVisibleError{
				LSN: entry.LSN, EventID: entry.Event.Identity.EventID, Err: result.err,
			}
		}
		ack := baseAcknowledgement(task, "visible")
		for key, value := range result.ack {
			ack[key] = value
		}
		ack["consistency_mode"] = string(mode)
		ack["visibility_status"] = "visible"
		return ack, nil
	}

	return baseAcknowledgement(task, "pending"), nil
}

func (c *Controller) WaitForRead(ctx context.Context, req schemas.QueryRequest) error {
	mode, err := ResolveRead(c.currentDefaultMode(), req)
	if err != nil {
		return err
	}
	if mode == EventualVisibility {
		return nil
	}
	waitCtx, cancel := c.withWaitTimeout(ctx)
	defer cancel()
	if mode == BoundedStaleness {
		return c.tracker.WaitWithinLag(waitCtx, c.cfg.BoundedMaxLag)
	}
	return c.tracker.WaitThrough(waitCtx, c.wal.LatestLSN())
}

func (c *Controller) SetDefaultMode(raw string) (Mode, error) {
	mode, err := ParseMode(raw)
	if err != nil {
		return "", err
	}
	c.modeMu.Lock()
	c.defaultMode = mode
	c.modeMu.Unlock()
	return mode, nil
}

func (c *Controller) Status() ControllerStatus {
	c.stateMu.RLock()
	active := c.started
	c.stateMu.RUnlock()
	return ControllerStatus{
		DefaultMode:    c.currentDefaultMode(),
		SupportedModes: []string{string(StrictVisible), string(BoundedStaleness), string(EventualVisibility)},
		DataPathActive: active,
		QueueDepth:     len(c.slots),
		QueueCapacity:  cap(c.slots),
		TrackerStatus:  c.tracker.Status(),
	}
}

func (c *Controller) Pause(ctx context.Context) error {
	c.stateMu.Lock()
	if !c.started {
		c.stateMu.Unlock()
		return ErrNotStarted
	}
	c.accepting = false
	cancelAdmission := c.cancelAdmission
	c.stateMu.Unlock()
	if cancelAdmission != nil {
		cancelAdmission()
	}

	c.admissionMu.Lock()
	c.stateMu.Lock()
	c.generation++
	c.stateMu.Unlock()
	c.drainQueues()
	c.admissionMu.Unlock()
	return c.waitForNoActive(ctx)
}

func (c *Controller) Reset() error {
	c.stateMu.RLock()
	accepting := c.accepting
	c.stateMu.RUnlock()
	if accepting {
		return errors.New("consistency controller must be paused before reset")
	}
	if len(c.slots) != 0 {
		return errors.New("consistency controller still has reserved projection slots")
	}
	c.initialLSN = 0
	return c.tracker.Reset()
}

func (c *Controller) Resume() {
	c.stateMu.Lock()
	if c.started && !c.accepting && c.rootCtx.Err() == nil {
		c.admissionCtx, c.cancelAdmission = context.WithCancel(c.rootCtx)
		c.accepting = true
	}
	c.stateMu.Unlock()
}

func (c *Controller) Shutdown(ctx context.Context) error {
	c.stateMu.Lock()
	if !c.started {
		c.stateMu.Unlock()
		return nil
	}
	c.accepting = false
	cancelAdmission := c.cancelAdmission
	c.stateMu.Unlock()
	if cancelAdmission != nil {
		cancelAdmission()
	}

	c.admissionMu.Lock()
	c.admissionMu.Unlock()

	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for len(c.slots) > 0 {
		select {
		case <-ctx.Done():
			c.cancel()
			return ctx.Err()
		case <-ticker.C:
		}
	}
	c.cancel()
	done := make(chan struct{})
	go func() {
		c.workers.Wait()
		close(done)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
	}
	c.stateMu.Lock()
	c.started = false
	c.stateMu.Unlock()
	return nil
}

func (c *Controller) admissionError() error {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	if !c.started || c.rootCtx == nil || c.rootCtx.Err() != nil {
		return ErrNotStarted
	}
	if !c.accepting {
		return ErrPaused
	}
	return context.Canceled
}

func (c *Controller) runWorker(queue <-chan projectionTask) {
	defer c.workers.Done()
	for {
		select {
		case <-c.rootCtx.Done():
			return
		case task := <-queue:
			if !c.isCurrentGeneration(task.generation) {
				c.completeStrict(task, nil, errOldGeneration)
				c.releaseTask(task)
				continue
			}
			c.beginActive()
			if task.strictDone != nil {
				ack, err := c.projectWithRetry(c.rootCtx, task, false)
				c.completeStrict(task, ack, err)
				if err != nil && c.isCurrentGeneration(task.generation) {
					_, _ = c.projectWithRetry(c.rootCtx, task, true)
				}
			} else {
				_, _ = c.projectWithRetry(c.rootCtx, task, true)
			}
			c.endActive()
			c.releaseTask(task)
		}
	}
}

func (c *Controller) completeStrict(task projectionTask, ack map[string]any, err error) {
	if task.strictDone == nil {
		return
	}
	task.strictDone <- projectionResult{ack: ack, err: err}
}

func (c *Controller) projectWithRetry(ctx context.Context, task projectionTask, terminal bool) (map[string]any, error) {
	var lastErr error
	for attempt := 1; attempt <= c.cfg.MaxRetries; attempt++ {
		if !c.isCurrentGeneration(task.generation) {
			return nil, errOldGeneration
		}
		_ = c.tracker.MarkProjecting(task.entry.LSN, attempt)
		ack, err := c.project(ctx, task.entry)
		if err == nil {
			if !c.isCurrentGeneration(task.generation) {
				return nil, errOldGeneration
			}
			if !task.deadline.IsZero() && time.Now().After(task.deadline) {
				c.tracker.MarkSLABreach(task.entry.LSN)
			}
			if err := c.tracker.MarkVisible(task.entry.LSN); err != nil {
				return nil, err
			}
			return ack, nil
		}
		lastErr = err
		if attempt == c.cfg.MaxRetries {
			if terminal {
				_ = c.tracker.MarkFailed(task.entry.LSN, err)
			} else {
				_ = c.tracker.MarkRetrying(task.entry.LSN, attempt, err)
			}
			break
		}
		_ = c.tracker.MarkRetrying(task.entry.LSN, attempt, err)
		delay := c.retryDelay(attempt)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	return nil, lastErr
}

func (c *Controller) enqueue(task projectionTask) {
	idx := shardIndex(task.entry.Event, len(c.queues))
	c.queues[idx] <- task
}

func (c *Controller) drainQueues() {
	for _, queue := range c.queues {
		for {
			select {
			case task := <-queue:
				c.completeStrict(task, nil, errOldGeneration)
				c.releaseTask(task)
			default:
				goto nextQueue
			}
		}
	nextQueue:
	}
}

func (c *Controller) beginActive() {
	c.activeMu.Lock()
	c.active++
	c.signalActiveLocked()
	c.activeMu.Unlock()
}

func (c *Controller) endActive() {
	c.activeMu.Lock()
	c.active--
	c.signalActiveLocked()
	c.activeMu.Unlock()
}

func (c *Controller) waitForNoActive(ctx context.Context) error {
	for {
		c.activeMu.Lock()
		if c.active == 0 {
			c.activeMu.Unlock()
			return nil
		}
		notify := c.activeChanged
		c.activeMu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-notify:
		}
	}
}

func (c *Controller) signalActiveLocked() {
	close(c.activeChanged)
	c.activeChanged = make(chan struct{})
}

func (c *Controller) isCurrentGeneration(generation uint64) bool {
	c.stateMu.RLock()
	current := c.generation
	c.stateMu.RUnlock()
	return generation == current
}

func (c *Controller) currentDefaultMode() Mode {
	c.modeMu.RLock()
	mode := c.defaultMode
	c.modeMu.RUnlock()
	return mode
}

func (c *Controller) releaseSlot() {
	select {
	case <-c.slots:
	default:
	}
}

func (c *Controller) releaseTask(task projectionTask) {
	if task.boundedReserved {
		c.releaseBoundedSlot(task.boundedShard)
	}
	c.releaseSlot()
}

func (c *Controller) releaseBoundedSlot(shard int) {
	if shard < 0 || shard >= len(c.boundedSlots) {
		return
	}
	select {
	case <-c.boundedSlots[shard]:
	default:
	}
}

func (c *Controller) retryDelay(attempt int) time.Duration {
	delay := c.cfg.RetryBaseDelay
	for i := 1; i < attempt && delay < c.cfg.RetryMaxDelay; i++ {
		delay *= 2
		if delay > c.cfg.RetryMaxDelay {
			delay = c.cfg.RetryMaxDelay
		}
	}
	return delay
}

func (c *Controller) withWaitTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, hasDeadline := ctx.Deadline(); hasDeadline || c.cfg.QueryWaitTimeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, c.cfg.QueryWaitTimeout)
}

func baseAcknowledgement(task projectionTask, visibility string) map[string]any {
	ack := map[string]any{
		"status":            "accepted",
		"event_id":          task.entry.Event.Identity.EventID,
		"memory_id":         schemas.IDPrefixMemory + task.entry.Event.Identity.EventID,
		"lsn":               task.entry.LSN,
		"consistency_mode":  string(task.mode),
		"visibility_status": visibility,
	}
	if task.mode == BoundedStaleness {
		ack["freshness_sla_ms"] = task.lag.Milliseconds()
	}
	return ack
}

func acceptedTime(entry eventbackbone.WALEntry) time.Time {
	if entry.AcceptedAtUnixNano > 0 {
		return time.Unix(0, entry.AcceptedAtUnixNano)
	}
	return time.Now()
}

func shardIndex(ev schemas.Event, workers int) int {
	key := ev.Identity.WorkspaceID + "\x00" + ev.Actor.SessionID
	if key == "\x00" {
		key = ev.Identity.EventID
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return int(h.Sum32() % uint32(workers))
}
