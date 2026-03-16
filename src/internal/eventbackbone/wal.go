package eventbackbone

import "andb/src/internal/schemas"

type WALEntry struct {
	LSN   int64
	Event schemas.Event
}

type InMemoryWAL struct {
	clock *HybridClock
	bus   Bus
	log   []WALEntry
}

func NewInMemoryWAL(bus Bus, clock *HybridClock) *InMemoryWAL {
	return &InMemoryWAL{bus: bus, clock: clock, log: []WALEntry{}}
}

func (w *InMemoryWAL) Append(event schemas.Event) (WALEntry, error) {
	entry := WALEntry{LSN: w.clock.Next(), Event: event}
	w.log = append(w.log, entry)
	w.bus.Publish(Message{Channel: "wal.events", Body: entry})
	return entry, nil
}
