package consistency

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var errBufferedCheckpointClosed = errors.New("buffered checkpoint is closed")
var errBufferedCheckpointReset = errors.New("buffered checkpoint was reset")

// BufferedCheckpoint keeps the recovery cursor behind the in-memory visibility
// frontier and coalesces frequent updates before persisting the highest LSN.
// A lagging cursor is safe: recovery replays the idempotent WAL projection from
// the last durable checkpoint.
type BufferedCheckpoint struct {
	inner    CheckpointStore
	interval time.Duration

	mu              sync.Mutex
	pending         int64
	pendingExists   bool
	persisted       int64
	persistedExists bool
	lastErr         error
	closeErr        error
	attempts        uint64
	generation      uint64
	resetting       bool
	closing         bool
	closed          bool
	changed         chan struct{}

	flushMu   sync.Mutex
	wake      chan struct{}
	stop      chan struct{}
	done      chan struct{}
	closeDone chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once
	closeOnce sync.Once
}

func NewBufferedCheckpoint(inner CheckpointStore, interval time.Duration) *BufferedCheckpoint {
	if inner == nil {
		inner = NewMemoryCheckpoint()
	}
	if interval <= 0 {
		interval = defaultCheckpointFlushInterval
	}
	return &BufferedCheckpoint{
		inner:     inner,
		interval:  interval,
		changed:   make(chan struct{}),
		wake:      make(chan struct{}, 1),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
		closeDone: make(chan struct{}),
	}
}

func (s *BufferedCheckpoint) start() {
	s.startOnce.Do(func() { go s.run() })
}

func (s *BufferedCheckpoint) run() {
	ticker := time.NewTicker(s.interval)
	defer func() {
		ticker.Stop()
		close(s.done)
	}()
	for {
		select {
		case <-ticker.C:
			s.flushOne()
		case <-s.wake:
			s.flushOne()
		case <-s.stop:
			return
		}
	}
}

func (s *BufferedCheckpoint) Load() (int64, bool, error) {
	s.flushMu.Lock()
	s.mu.Lock()
	generation := s.generation
	s.mu.Unlock()
	lsn, exists, err := s.inner.Load()
	if err != nil {
		s.flushMu.Unlock()
		return 0, false, err
	}
	s.mu.Lock()
	if generation == s.generation && !s.resetting && exists && (!s.persistedExists || lsn > s.persisted) {
		s.persisted = lsn
		s.persistedExists = true
	}
	s.mu.Unlock()
	s.flushMu.Unlock()
	return lsn, exists, nil
}

func (s *BufferedCheckpoint) Save(lsn int64) error {
	if lsn < 0 {
		return fmt.Errorf("invalid consistency checkpoint LSN %d", lsn)
	}
	s.start()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.resetting {
		return errBufferedCheckpointReset
	}
	if s.closing || s.closed {
		return errBufferedCheckpointClosed
	}
	if s.persistedExists && lsn < s.persisted {
		return fmt.Errorf("checkpoint regression: current=%d next=%d", s.persisted, lsn)
	}
	if s.pendingExists && lsn < s.pending {
		return fmt.Errorf("checkpoint regression: current=%d next=%d", s.pending, lsn)
	}
	if !s.pendingExists || lsn > s.pending {
		s.pending = lsn
		s.pendingExists = true
	}
	return nil
}

func (s *BufferedCheckpoint) Reset() error {
	s.mu.Lock()
	if s.closing || s.closed {
		s.mu.Unlock()
		return errBufferedCheckpointClosed
	}
	if s.resetting {
		s.mu.Unlock()
		return errBufferedCheckpointReset
	}
	s.resetting = true
	s.generation++
	s.pending = 0
	s.pendingExists = false
	s.signalLocked()
	s.mu.Unlock()

	s.flushMu.Lock()
	err := s.inner.Reset()
	s.flushMu.Unlock()

	s.mu.Lock()
	if err == nil {
		s.persisted = 0
		s.persistedExists = false
		s.lastErr = nil
	} else {
		s.lastErr = err
	}
	s.resetting = false
	s.signalLocked()
	s.mu.Unlock()
	return err
}

func (s *BufferedCheckpoint) Flush(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.start()
	s.mu.Lock()
	if s.closed {
		err := s.closeErr
		s.mu.Unlock()
		return err
	}
	if s.resetting {
		s.mu.Unlock()
		return errBufferedCheckpointReset
	}
	generation := s.generation
	if !s.pendingExists || (s.persistedExists && s.persisted >= s.pending) {
		err := s.lastErr
		s.mu.Unlock()
		return err
	}
	target := s.pending
	startAttempt := s.attempts
	changed := s.changed
	s.mu.Unlock()
	s.wakeNow()

	for {
		s.mu.Lock()
		if s.generation != generation {
			s.mu.Unlock()
			return errBufferedCheckpointReset
		}
		if s.persistedExists && s.persisted >= target {
			s.mu.Unlock()
			return nil
		}
		if s.attempts > startAttempt && s.lastErr != nil {
			err := s.lastErr
			s.mu.Unlock()
			return err
		}
		changed = s.changed
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

func (s *BufferedCheckpoint) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closing = true
		s.signalLocked()
		s.mu.Unlock()
		go s.finishClose()
	})
	select {
	case <-s.closeDone:
		s.mu.Lock()
		err := s.closeErr
		s.mu.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *BufferedCheckpoint) finishClose() {
	for {
		s.mu.Lock()
		if !s.resetting {
			s.mu.Unlock()
			break
		}
		changed := s.changed
		s.mu.Unlock()
		<-changed
	}
	flushErr := s.Flush(context.Background())
	s.stopOnce.Do(func() { close(s.stop) })
	<-s.done
	s.mu.Lock()
	s.closed = true
	s.closeErr = flushErr
	s.signalLocked()
	s.mu.Unlock()
	close(s.closeDone)
}

func (s *BufferedCheckpoint) LastError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastErr
}

func (s *BufferedCheckpoint) flushOne() {
	s.mu.Lock()
	if !s.pendingExists || (s.persistedExists && s.persisted >= s.pending) {
		s.mu.Unlock()
		return
	}
	target := s.pending
	generation := s.generation
	s.mu.Unlock()

	s.flushMu.Lock()
	s.mu.Lock()
	stale := generation != s.generation
	s.mu.Unlock()
	if stale {
		s.flushMu.Unlock()
		return
	}
	err := s.inner.Save(target)
	s.mu.Lock()
	s.attempts++
	if generation != s.generation {
		s.signalLocked()
		s.mu.Unlock()
		s.flushMu.Unlock()
		return
	}
	if err != nil {
		s.lastErr = err
	} else {
		if !s.persistedExists || target > s.persisted {
			s.persisted = target
			s.persistedExists = true
		}
		if s.pendingExists && s.pending <= s.persisted {
			s.pendingExists = false
		}
		s.lastErr = nil
	}
	s.signalLocked()
	s.mu.Unlock()
	s.flushMu.Unlock()
}

func (s *BufferedCheckpoint) wakeNow() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *BufferedCheckpoint) signalLocked() {
	close(s.changed)
	s.changed = make(chan struct{})
}
