package worker

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"andb/src/internal/eventbackbone"
	"andb/src/internal/schemas"
	"andb/src/internal/worker/nodes"
)

// DispatchHandler is a pluggable function called for every new WAL entry.
// Implementations may filter by EventType or any other field.
type DispatchHandler func(entry eventbackbone.WALEntry)

// DeadLetterEntry represents a failed dispatch that should be retried or
// investigated. Production systems should consume from ErrorCh and persist
// these entries for later analysis or retry.
type DeadLetterEntry struct {
	LSN       int64
	EventID   string
	Handler   int // index of the handler that panicked
	Error     interface{}
	Timestamp time.Time
}

// EventSubscriber polls the WAL for new entries and dispatches them to the
// registered worker pipeline in the background.
//
// Design principles:
//   - Pluggable: call AddHandler to inject additional dispatch logic.
//   - Non-duplicating: it does NOT repeat work already done synchronously in
//     Runtime.SubmitIngest (Memory creation, edge derivation, evidence cache).
//   - Async workers driven here: ReflectionPolicy, ConflictMerge,
//     MemoryConsolidation, and any custom DispatchHandlers.
//   - Safe for concurrent use: lastLSN is atomic; agentState is mutex-guarded.
//   - Dead-letter channel: panics are reported to ErrorCh for structured handling.
type EventSubscriber struct {
	wal          eventbackbone.WAL
	manager      *nodes.Manager
	handlers     []DispatchHandler
	lastLSN      atomic.Int64
	pollInterval time.Duration

	// consolidateEvery controls how many events per agent+session trigger a
	// MemoryConsolidation pass.  0 disables automatic consolidation.
	consolidateEvery int

	// ErrorCh receives DeadLetterEntry for any handler that panics.
	// Consumers should drain this channel to avoid blocking dispatch.
	// Buffer size is 256; excess entries are dropped with a log warning.
	ErrorCh chan DeadLetterEntry

	mu              sync.Mutex
	agentEventCount map[string]int    // "agentID:sessionID" → event count
	agentLastMem    map[string]string // "agentID:sessionID" → most-recent memory ID
}

// CreateEventSubscriber constructs a subscriber wired to the given WAL and worker
// Manager.  Built-in dispatch handlers for ReflectionPolicy, ConflictMerge,
// and MemoryConsolidation are registered automatically.
//
// The returned subscriber has a buffered ErrorCh (capacity 256) for dead-letter
// entries. Callers should start a goroutine to consume from ErrorCh to avoid
// blocking dispatch when the buffer fills.
func CreateEventSubscriber(wal eventbackbone.WAL, manager *nodes.Manager) *EventSubscriber {
	s := &EventSubscriber{
		wal:              wal,
		manager:          manager,
		pollInterval:     200 * time.Millisecond,
		consolidateEvery: 10,
		agentEventCount:  make(map[string]int),
		agentLastMem:     make(map[string]string),
		ErrorCh:          make(chan DeadLetterEntry, 256),
	}
	s.addBuiltinHandlers()
	return s
}

// SetPollInterval overrides the default 200 ms WAL poll cadence.
func (s *EventSubscriber) SetPollInterval(d time.Duration) { s.pollInterval = d }

// SetConsolidateEvery sets how many events per agent+session trigger a
// MemoryConsolidation pass.  Pass 0 to disable automatic consolidation.
func (s *EventSubscriber) SetConsolidateEvery(n int) { s.consolidateEvery = n }

// AddHandler appends a custom DispatchHandler.  Handlers are invoked in
// registration order for every new WAL entry during the drain cycle.
func (s *EventSubscriber) AddHandler(h DispatchHandler) {
	s.handlers = append(s.handlers, h)
}

// Run starts the poll loop and blocks until ctx is cancelled.
// Call it in a goroutine: go sub.Run(ctx)
func (s *EventSubscriber) Run(ctx context.Context) {
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.drainWAL()
		}
	}
}

// drainWAL scans entries with LSN > lastLSN and dispatches each one to all
// registered handlers, then advances lastLSN.
// After processing at least one entry the MicroBatch queue is flushed so that
// payloads enqueued by ConflictMergeWorker / CollaborationChain are drained on
// every poll cycle rather than accumulating indefinitely.
func (s *EventSubscriber) drainWAL() {
	fromLSN := s.lastLSN.Load() + 1
	entries := s.wal.Scan(fromLSN)
	for _, entry := range entries {
		for i, h := range s.handlers {
			s.safeDispatch(h, i, entry)
		}
		s.lastLSN.Store(entry.LSN)
	}
	if len(entries) > 0 {
		_ = s.manager.FlushMicroBatch()
	}
}

// safeDispatch calls h(entry) and recovers from any panic, reporting the
// incident to ErrorCh so the poll goroutine keeps running.
//
// Panics are sent to ErrorCh as DeadLetterEntry for structured error handling.
// If ErrorCh is full, the entry is dropped with a log warning.
func (s *EventSubscriber) safeDispatch(h DispatchHandler, handlerIdx int, entry eventbackbone.WALEntry) {
	defer func() {
		if r := recover(); r != nil {
			dle := DeadLetterEntry{
				LSN:       entry.LSN,
				EventID:   entry.Event.EventID,
				Handler:   handlerIdx,
				Error:     r,
				Timestamp: time.Now(),
			}
			select {
			case s.ErrorCh <- dle:
				// successfully sent to dead-letter channel
			default:
				// channel full, log and drop
				log.Printf("subscriber: ErrorCh full, dropping dead-letter (lsn=%d event=%s handler=%d): %v",
					entry.LSN, entry.Event.EventID, handlerIdx, r)
			}
		}
	}()
	h(entry)
}

// addBuiltinHandlers wires the built-in async dispatch passes.
//
//  1. StateMaterialization — applies state_update / state_change / checkpoint
//     events to the StateMaterializationWorkers.
//  2. ToolTrace            — captures tool_call / tool_result events as
//     structured Artifact records.
//  3. ReflectionPolicy     — enforces TTL / quarantine / confidence / salience
//     for the Memory object produced by this event.
//  4. ConflictMerge        — compares the new memory against the previous one
//     written by the same agent+session; resolves via last-writer-wins.
//  5. MemoryConsolidation  — triggers a level-0→1 distillation pass every
//     consolidateEvery events for the same agent+session.
func (s *EventSubscriber) addBuiltinHandlers() {
	// ── 0. StateMaterialization ───────────────────────────────────────────
	s.AddHandler(func(entry eventbackbone.WALEntry) {
		ev := entry.Event
		switch ev.EventType {
		case string(schemas.EventTypeStateUpdate), string(schemas.EventTypeStateChange), string(schemas.EventTypeCheckpoint):
			s.manager.DispatchStateMaterialization(ev)
		}
	})

	// ── 0b. ToolTrace ─────────────────────────────────────────────────────
	s.AddHandler(func(entry eventbackbone.WALEntry) {
		ev := entry.Event
		switch ev.EventType {
		case string(schemas.EventTypeToolCall), string(schemas.EventTypeToolResult):
			s.manager.DispatchToolTrace(ev)
		}
	})
	// ── 1. ReflectionPolicy ───────────────────────────────────────────────
	s.AddHandler(func(entry eventbackbone.WALEntry) {
		ev := entry.Event
		memID := schemas.IDPrefixMemory + ev.EventID
		s.manager.DispatchReflectionPolicy(memID, string(schemas.ObjectTypeMemory))
	})

	// ── 2. ConflictMerge ──────────────────────────────────────────────────
	// Track the last memory ID written per agent+session so we can compare
	// new arrivals against the previous write.
	s.AddHandler(func(entry eventbackbone.WALEntry) {
		ev := entry.Event
		if ev.AgentID == "" {
			return
		}
		key := ev.AgentID + ":" + ev.SessionID
		newMemID := schemas.IDPrefixMemory + ev.EventID

		s.mu.Lock()
		prevMemID, hasPrev := s.agentLastMem[key]
		s.agentLastMem[key] = newMemID
		s.mu.Unlock()

		if hasPrev {
			s.manager.DispatchConflictMerge(newMemID, prevMemID, string(schemas.ObjectTypeMemory))
		}
	})

	// ── 3. MemoryConsolidation ────────────────────────────────────────────
	s.AddHandler(func(entry eventbackbone.WALEntry) {
		ev := entry.Event
		if ev.AgentID == "" || s.consolidateEvery <= 0 {
			return
		}
		key := ev.AgentID + ":" + ev.SessionID

		s.mu.Lock()
		s.agentEventCount[key]++
		count := s.agentEventCount[key]
		s.mu.Unlock()

		if count%s.consolidateEvery == 0 {
			s.manager.DispatchMemoryConsolidation(ev.AgentID, ev.SessionID)
		}
	})
}
