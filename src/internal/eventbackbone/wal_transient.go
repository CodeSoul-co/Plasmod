package eventbackbone

import (
	"sync"
	"time"

	"plasmod/src/internal/schemas"
)

// TransientWAL assigns ordered LSNs and publishes accepted events but retains
// no replay history.
type TransientWAL struct {
	mu     sync.RWMutex
	latest int64
	bus    Bus
	clock  *HybridClock
}

func NewTransientWAL(bus Bus, clock *HybridClock) *TransientWAL {
	return &TransientWAL{bus: bus, clock: clock}
}

func (w *TransientWAL) Append(event schemas.Event) (WALEntry, error) {
	lsn := w.clock.Next()
	event = event.NormalizeDynamicEventV04()
	event.Time.WalLSN = lsn
	if event.Time.LogicalTS == 0 {
		event.Time.LogicalTS = lsn
	}
	entry := WALEntry{LSN: lsn, AcceptedAtUnixNano: time.Now().UnixNano(), Event: event}
	w.mu.Lock()
	w.latest = lsn
	w.mu.Unlock()
	w.bus.Publish(Message{Channel: "wal.events", Body: entry})
	return entry, nil
}

func (w *TransientWAL) Scan(int64) []WALEntry { return nil }

func (w *TransientWAL) LatestLSN() int64 {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.latest
}

func (w *TransientWAL) Wipe() {
	w.mu.Lock()
	w.latest = 0
	w.mu.Unlock()
}
