package eventbackbone

import (
	"sync"

	"plasmod/src/internal/schemas"
)

type WALEntry struct {
	LSN   int64
	Event schemas.Event
}

type InMemoryWAL struct {
	mu      sync.RWMutex
	entries []WALEntry
	clock   *HybridClock
	bus     Bus
}

func NewInMemoryWAL(bus Bus, clock *HybridClock) *InMemoryWAL {
	return &InMemoryWAL{clock: clock, bus: bus}
}

func (w *InMemoryWAL) Append(event schemas.Event) (WALEntry, error) {
	lsn := w.clock.Next()
	entry := WALEntry{LSN: lsn, Event: event}
	w.mu.Lock()
	w.entries = append(w.entries, entry)
	w.mu.Unlock()
	w.bus.Publish(Message{Channel: "wal.events", Body: entry})
	return entry, nil
}

// Scan returns all WAL entries with LSN >= fromLSN.  A fromLSN of 0 returns
// the entire log.  Used by replay, recovery, and bounded-staleness queries.
func (w *InMemoryWAL) Scan(fromLSN int64) []WALEntry {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := []WALEntry{}
	for _, e := range w.entries {
		if e.LSN >= fromLSN {
			out = append(out, e)
		}
	}
	return out
}

// LatestLSN returns the highest LSN currently in the log, or 0 if empty.
func (w *InMemoryWAL) LatestLSN() int64 {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if len(w.entries) == 0 {
		return 0
	}
	return w.entries[len(w.entries)-1].LSN
}
