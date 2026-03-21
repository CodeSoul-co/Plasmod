package worker

import (
	"context"
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
type EventSubscriber struct {
	wal          eventbackbone.WAL
	manager      *nodes.Manager
	handlers     []DispatchHandler
	lastLSN      atomic.Int64
	pollInterval time.Duration

	// consolidateEvery controls how many events per agent+session trigger a
	// MemoryConsolidation pass.  0 disables automatic consolidation.
	consolidateEvery int

	mu              sync.Mutex
	agentEventCount map[string]int    // "agentID:sessionID" → event count
	agentLastMem    map[string]string // "agentID:sessionID" → most-recent memory ID
}

// CreateEventSubscriber constructs a subscriber wired to the given WAL and worker
// Manager.  Built-in dispatch handlers for ReflectionPolicy, ConflictMerge,
// and MemoryConsolidation are registered automatically.
func CreateEventSubscriber(wal eventbackbone.WAL, manager *nodes.Manager) *EventSubscriber {
	s := &EventSubscriber{
		wal:              wal,
		manager:          manager,
		pollInterval:     200 * time.Millisecond,
		consolidateEvery: 10,
		agentEventCount:  make(map[string]int),
		agentLastMem:     make(map[string]string),
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
func (s *EventSubscriber) drainWAL() {
	fromLSN := s.lastLSN.Load() + 1
	entries := s.wal.Scan(fromLSN)
	for _, entry := range entries {
		for _, h := range s.handlers {
			h(entry)
		}
		s.lastLSN.Store(entry.LSN)
	}
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
			_ = s.manager.DispatchStateMaterialization(ev)
		}
	})

	// ── 0b. ToolTrace ─────────────────────────────────────────────────────
	s.AddHandler(func(entry eventbackbone.WALEntry) {
		ev := entry.Event
		switch ev.EventType {
		case string(schemas.EventTypeToolCall), string(schemas.EventTypeToolResult):
			_ = s.manager.DispatchToolTrace(ev)
		}
	})
	// ── 1. ReflectionPolicy ───────────────────────────────────────────────
	s.AddHandler(func(entry eventbackbone.WALEntry) {
		ev := entry.Event
		memID := schemas.IDPrefixMemory + ev.EventID
		_ = s.manager.DispatchReflectionPolicy(memID, string(schemas.ObjectTypeMemory))
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
			_, _ = s.manager.DispatchConflictMerge(newMemID, prevMemID, string(schemas.ObjectTypeMemory))
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
			_ = s.manager.DispatchMemoryConsolidation(ev.AgentID, ev.SessionID)
		}
	})
}
