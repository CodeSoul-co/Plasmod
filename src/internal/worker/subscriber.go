package worker

import (
	"context"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"plasmod/src/internal/eventbackbone"
	"plasmod/src/internal/schemas"
	"plasmod/src/internal/worker/nodes"
)

// DeadLetterEntry describes a WAL entry that could not be processed because the
// handler panicked.  Downstream consumers (e.g. a DLQ consumer goroutine or an
// ops dashboard) can drain deadLetter to replay, alert, or archive these events.
type DeadLetterEntry struct {
	Entry      eventbackbone.WALEntry `json:"entry"`
	PanicValue any                    `json:"panic_value"`
	Timestamp  time.Time              `json:"timestamp"`
}

// SubscriberError is a structured error report sent to ErrorCh when both the
// dead-letter channel and the overflow buffer are at capacity.  It carries the
// same information as DeadLetterEntry plus a human-readable message so that
// ops dashboards can alert on it without parsing unstructured log output.
type SubscriberError struct {
	Entry     eventbackbone.WALEntry `json:"entry"`
	PanicValue any                   `json:"panic_value"`
	Timestamp time.Time              `json:"timestamp"`
	Reason    string                 `json:"reason"`
}

// DLQStats returns statistics about the dead-letter queue.
type DLQStats struct {
	PanicCount       int   `json:"panic_count"`
	TotalProcessed   int64 `json:"total_processed"`
	OverflowCount    int   `json:"overflow_count"`    // entries that bypassed the channel
	OverflowCap      int   `json:"overflow_cap"`      // capacity of the in-memory overflow buffer
}

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
//   - DLQ overflow-safe: panics are never silently dropped — they are either
//     sent to the deadLetter channel or appended to an in-memory overflow buffer.
type EventSubscriber struct {
	wal          eventbackbone.WAL
	manager      *nodes.Manager
	handlers     []DispatchHandler
	lastLSN      atomic.Int64
	pollInterval time.Duration

	// consolidateEvery controls how many events per agent+session trigger a
	// MemoryConsolidation pass.  0 disables automatic consolidation.
	consolidateEvery int

	// dead-letter queue fields
	deadLetter     chan DeadLetterEntry
	ErrorCh        chan SubscriberError // structured error channel; never nil (defaults to discard)
	panicCount     atomic.Int64
	processedCount atomic.Int64
	overflowCount  atomic.Int64

	// overflowBuf stores panics that bypass the deadLetter channel when it is full.
	// It has a fixed capacity (separate from the channel) so no panic is ever lost.
	overflowMu  sync.Mutex
	overflowBuf []DeadLetterEntry
	overflowCap int

	mu              sync.Mutex
	agentEventCount map[string]int    // "agentID:sessionID" → event count
	agentLastMem    map[string]string // "agentID:sessionID" → most-recent memory ID
}

// CreateEventSubscriber constructs a subscriber wired to the given WAL and worker
// Manager.  Built-in dispatch handlers for ReflectionPolicy, ConflictMerge,
// and MemoryConsolidation are registered automatically.
const defaultOverflowCap = 256 // overflow buffer capacity when channel is full

func CreateEventSubscriber(wal eventbackbone.WAL, manager *nodes.Manager) *EventSubscriber {
	s := &EventSubscriber{
		wal:              wal,
		manager:          manager,
		pollInterval:     200 * time.Millisecond,
		consolidateEvery: 10,
		agentEventCount:  make(map[string]int),
		agentLastMem:     make(map[string]string),
		deadLetter:       make(chan DeadLetterEntry, 64),
		ErrorCh:          make(chan SubscriberError, 64),
		overflowCap:      defaultOverflowCap,
		overflowBuf:      make([]DeadLetterEntry, 0, defaultOverflowCap),
	}
	s.addBuiltinHandlers()
	return s
}

// SetPollInterval overrides the default 200 ms WAL poll cadence.
func (s *EventSubscriber) SetPollInterval(d time.Duration) { s.pollInterval = d }

// SetConsolidateEvery sets how many events per agent+session trigger a
// MemoryConsolidation pass.  Pass 0 to disable automatic consolidation.
func (s *EventSubscriber) SetConsolidateEvery(n int) { s.consolidateEvery = n }

func conflictMergeSkipDatasetLoaderEnabled() bool {
	// Default on: bulk dataset imports should not be treated as conversational conflicts.
	raw := strings.TrimSpace(os.Getenv("ANDB_CONFLICT_MERGE_SKIP_DATASET_LOADER"))
	if raw == "" {
		return true
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return true
	}
	return v
}

func shouldSkipConflictMergeForEvent(ev schemas.Event) bool {
	if !conflictMergeSkipDatasetLoaderEnabled() {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(ev.Source), "dataset_loader") {
		return true
	}
	if ev.Payload == nil {
		return false
	}
	ingestMode, ok := ev.Payload["ingest_mode"]
	if !ok {
		return false
	}
	mode, ok := ingestMode.(string)
	if !ok {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(mode), "bulk_dataset")
}

// DeadLetterChannel returns the dead-letter channel.  Entries sent here
// originated from handler panics.  The channel has capacity 64; if the
// consumer cannot keep up the subscriber will drop further panics rather
// than blocking the poll loop.
func (s *EventSubscriber) DeadLetterChannel() <-chan DeadLetterEntry {
	return s.deadLetter
}

// DLQStats returns statistics about the dead-letter queue.
func (s *EventSubscriber) DLQStats() DLQStats {
	return DLQStats{
		PanicCount:     int(s.panicCount.Load()),
		TotalProcessed: s.processedCount.Load(),
		OverflowCount:  int(s.overflowCount.Load()),
		OverflowCap:     s.overflowCap,
	}
}

// OverflowBuffer returns the in-memory slice of entries that bypassed the
// dead-letter channel because it was full.  Entries are appended newest-first.
// Safe for concurrent reads by a single consumer.
func (s *EventSubscriber) OverflowBuffer() []DeadLetterEntry {
	s.overflowMu.Lock()
	// Return a copy so callers can't mutate the internal buffer.
	out := make([]DeadLetterEntry, len(s.overflowBuf))
	copy(out, s.overflowBuf)
	s.overflowMu.Unlock()
	return out
}

// ClearOverflow empties the in-memory overflow buffer.  Call this after
// draining and processing overflow entries.
func (s *EventSubscriber) ClearOverflow() {
	s.overflowMu.Lock()
	s.overflowBuf = s.overflowBuf[:0]
	s.overflowMu.Unlock()
}

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
		for _, h := range s.handlers {
			s.safeDispatch(h, entry)
		}
		s.lastLSN.Store(entry.LSN)
	}
	if len(entries) > 0 {
		_ = s.manager.FlushMicroBatch()
	}
}

// safeDispatch calls h(entry), recovers from any panic, and sends the entry
// to the dead-letter channel.  The poll goroutine is never blocked by a slow
// DLQ consumer.
func (s *EventSubscriber) safeDispatch(h DispatchHandler, entry eventbackbone.WALEntry) {
	defer func() {
		if r := recover(); r != nil {
			s.panicCount.Add(1)
			entry := DeadLetterEntry{
				Entry:      entry,
				PanicValue: r,
				Timestamp:  time.Now(),
			}
			// Try the channel first (preferred path — external consumer).
			select {
			case s.deadLetter <- entry:
				return
			default:
			}
			// Channel full — append to the in-memory overflow buffer.
			// This buffer has its own capacity (256) so panics are NEVER lost.
			s.overflowMu.Lock()
			if len(s.overflowBuf) < cap(s.overflowBuf) {
				s.overflowBuf = append(s.overflowBuf, entry)
			} else {
				// Overflow buffer also full — increment overflow counter.
				// The buffer is append-only (oldest entries are pushed out via
				// oldest-first eviction when full).  The count tracks total lost.
				s.overflowCount.Add(1)
				// Evict oldest so we always keep the newest entries.
				copy(s.overflowBuf, s.overflowBuf[1:])
				s.overflowBuf[len(s.overflowBuf)-1] = entry
			}
			s.overflowMu.Unlock()
			// Structured error reporting: send to ErrorCh so ops dashboards can
			// alert without parsing unstructured log output.
			select {
			case s.ErrorCh <- SubscriberError{
				Entry:     entry.Entry,
				PanicValue: r,
				Timestamp: time.Now(),
				Reason:    "DLQ and overflow buffer both full",
			}:
			default:
				// ErrorCh consumer also slow; drop rather than block.
			}
			log.Printf("subscriber: DLQ and overflow buffer full; panic reported via ErrorCh (lsn=%d event=%s): %v",
				entry.Entry.LSN, entry.Entry.Event.EventID, r)
		}
	}()
	s.processedCount.Add(1)
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
		if shouldSkipConflictMergeForEvent(ev) {
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
