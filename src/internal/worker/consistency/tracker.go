package consistency

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"plasmod/src/internal/eventbackbone"
)

type projectionState string

const (
	stateAccepted   projectionState = "accepted"
	stateProjecting projectionState = "projecting"
	stateRetrying   projectionState = "retrying"
	stateVisible    projectionState = "visible"
	stateFailed     projectionState = "failed"
)

type WatermarkAdvancer interface {
	AdvanceTo(lsn int64) eventbackbone.TimeTick
}

type trackedProjection struct {
	lsn        int64
	acceptedAt time.Time
	deadline   time.Time
	state      projectionState
	attempts   int
	err        error
	breached   bool
}

// TrackerStatus is a point-in-time projection and freshness summary.
type TrackerStatus struct {
	LatestLSN        int64         `json:"latest_lsn"`
	VisibleWatermark int64         `json:"visible_watermark"`
	Pending          int           `json:"pending"`
	Retrying         int           `json:"retrying"`
	Failed           int           `json:"failed"`
	OldestPendingAge time.Duration `json:"-"`
	OldestPendingMS  int64         `json:"oldest_pending_ms"`
	SLABreaches      int64         `json:"sla_breaches"`
	LastSLABreachMS  int64         `json:"last_sla_breach_ms"`
	MaxSLABreachMS   int64         `json:"max_sla_breach_ms"`
	LastError        string        `json:"last_error,omitempty"`
}

// ProjectionFailureError identifies the WAL entry preventing a visibility wait.
type ProjectionFailureError struct {
	LSN int64
	Err error
}

func (e *ProjectionFailureError) Error() string {
	return fmt.Sprintf("projection failed at lsn %d: %v", e.LSN, e.Err)
}

func (e *ProjectionFailureError) Unwrap() error { return e.Err }

// Tracker advances visibility over actual accepted WAL order, not numeric LSN adjacency.
type Tracker struct {
	mu               sync.Mutex
	entries          map[int64]*trackedProjection
	order            []int64
	nextVisibleIndex int
	latestLSN        int64
	visibleWatermark int64
	slaBreaches      int64
	lastSLABreachMS  int64
	maxSLABreachMS   int64
	lastError        string
	notify           chan struct{}
	watermark        WatermarkAdvancer
	checkpoint       CheckpointStore
}

func NewTracker(initialLSN int64, watermark WatermarkAdvancer, checkpoint CheckpointStore) *Tracker {
	if checkpoint == nil {
		checkpoint = NewMemoryCheckpoint()
	}
	return &Tracker{
		entries:          make(map[int64]*trackedProjection),
		latestLSN:        initialLSN,
		visibleWatermark: initialLSN,
		notify:           make(chan struct{}),
		watermark:        watermark,
		checkpoint:       checkpoint,
	}
}

func (t *Tracker) Accept(lsn int64, acceptedAt, deadline time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if lsn <= t.visibleWatermark {
		return
	}
	if _, exists := t.entries[lsn]; exists {
		return
	}
	entry := &trackedProjection{
		lsn:        lsn,
		acceptedAt: acceptedAt,
		deadline:   deadline,
		state:      stateAccepted,
	}
	t.entries[lsn] = entry
	idx := sort.Search(len(t.order), func(i int) bool { return t.order[i] >= lsn })
	t.order = append(t.order, 0)
	copy(t.order[idx+1:], t.order[idx:])
	t.order[idx] = lsn
	if lsn > t.latestLSN {
		t.latestLSN = lsn
	}
	t.signalLocked()
}

func (t *Tracker) MarkProjecting(lsn int64, attempt int) error {
	return t.setState(lsn, stateProjecting, attempt, nil)
}

func (t *Tracker) MarkRetrying(lsn int64, attempt int, err error) error {
	return t.setState(lsn, stateRetrying, attempt, err)
}

func (t *Tracker) MarkFailed(lsn int64, err error) error {
	return t.setState(lsn, stateFailed, 0, err)
}

func (t *Tracker) setState(lsn int64, state projectionState, attempt int, err error) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	entry, ok := t.entries[lsn]
	if !ok {
		return fmt.Errorf("unknown projection lsn %d", lsn)
	}
	entry.state = state
	if attempt > 0 {
		entry.attempts = attempt
	}
	entry.err = err
	if err != nil {
		t.lastError = err.Error()
	}
	t.signalLocked()
	return nil
}

func (t *Tracker) MarkVisible(lsn int64) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	entry, ok := t.entries[lsn]
	if !ok {
		return fmt.Errorf("unknown projection lsn %d", lsn)
	}
	entry.state = stateVisible
	entry.err = nil

	now := time.Now()
	advanced := t.visibleWatermark
	for t.nextVisibleIndex < len(t.order) {
		nextLSN := t.order[t.nextVisibleIndex]
		next := t.entries[nextLSN]
		if next == nil || next.state != stateVisible {
			break
		}
		if !next.deadline.IsZero() && now.After(next.deadline) {
			t.markSLABreachLocked(next, now)
		}
		advanced = nextLSN
		t.nextVisibleIndex++
	}
	if advanced > t.visibleWatermark {
		if err := t.checkpoint.Save(advanced); err != nil {
			return err
		}
		t.visibleWatermark = advanced
		if t.watermark != nil {
			t.watermark.AdvanceTo(advanced)
		}
	}
	t.signalLocked()
	return nil
}

func (t *Tracker) MarkSLABreach(lsn int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	entry := t.entries[lsn]
	t.markSLABreachLocked(entry, time.Now())
	t.signalLocked()
}

func (t *Tracker) markSLABreachLocked(entry *trackedProjection, now time.Time) {
	if entry == nil || entry.breached {
		return
	}
	entry.breached = true
	t.slaBreaches++
	lagMS := now.Sub(entry.acceptedAt).Milliseconds()
	if lagMS < 0 {
		lagMS = 0
	}
	t.lastSLABreachMS = lagMS
	if lagMS > t.maxSLABreachMS {
		t.maxSLABreachMS = lagMS
	}
}

func (t *Tracker) WaitThrough(ctx context.Context, targetLSN int64) error {
	for {
		t.mu.Lock()
		if targetLSN <= t.visibleWatermark {
			t.mu.Unlock()
			return nil
		}
		if failed := t.firstFailedThroughLocked(targetLSN); failed != nil {
			err := &ProjectionFailureError{LSN: failed.lsn, Err: failed.err}
			t.mu.Unlock()
			return err
		}
		notify := t.notify
		t.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-notify:
		}
	}
}

func (t *Tracker) WaitWithinLag(ctx context.Context, maxLag time.Duration) error {
	for {
		t.mu.Lock()
		oldest := t.oldestUnresolvedLocked()
		if oldest == nil || time.Since(oldest.acceptedAt) <= maxLag {
			t.mu.Unlock()
			return nil
		}
		if oldest.state == stateFailed {
			err := &ProjectionFailureError{LSN: oldest.lsn, Err: oldest.err}
			t.mu.Unlock()
			return err
		}
		notify := t.notify
		t.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-notify:
		}
	}
}

func (t *Tracker) Status() TrackerStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	status := TrackerStatus{
		LatestLSN:        t.latestLSN,
		VisibleWatermark: t.visibleWatermark,
		SLABreaches:      t.slaBreaches,
		LastSLABreachMS:  t.lastSLABreachMS,
		MaxSLABreachMS:   t.maxSLABreachMS,
		LastError:        t.lastError,
	}
	oldest := t.oldestUnresolvedLocked()
	if oldest != nil {
		status.OldestPendingAge = time.Since(oldest.acceptedAt)
		status.OldestPendingMS = status.OldestPendingAge.Milliseconds()
	}
	for _, entry := range t.entries {
		switch entry.state {
		case stateAccepted, stateProjecting:
			status.Pending++
		case stateRetrying:
			status.Retrying++
		case stateFailed:
			status.Failed++
		}
	}
	return status
}

func (t *Tracker) Reset() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.checkpoint.Reset(); err != nil {
		return err
	}
	t.entries = make(map[int64]*trackedProjection)
	t.order = nil
	t.nextVisibleIndex = 0
	t.latestLSN = 0
	t.visibleWatermark = 0
	t.slaBreaches = 0
	t.lastSLABreachMS = 0
	t.maxSLABreachMS = 0
	t.lastError = ""
	t.signalLocked()
	return nil
}

func (t *Tracker) firstFailedThroughLocked(targetLSN int64) *trackedProjection {
	for _, lsn := range t.order {
		if lsn > targetLSN {
			break
		}
		entry := t.entries[lsn]
		if entry != nil && entry.state == stateFailed {
			return entry
		}
	}
	return nil
}

func (t *Tracker) oldestUnresolvedLocked() *trackedProjection {
	for _, lsn := range t.order {
		entry := t.entries[lsn]
		if entry != nil && entry.state != stateVisible {
			return entry
		}
	}
	return nil
}

func (t *Tracker) signalLocked() {
	close(t.notify)
	t.notify = make(chan struct{})
}
