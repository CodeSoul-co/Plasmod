package eventbackbone

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"andb/src/internal/schemas"
)

// FileWAL persists WALEntry as JSONL and rebuilds memory index on startup.
// It satisfies the WAL interface and publishes appended entries to Bus.
type FileWAL struct {
	mu      sync.RWMutex
	path    string
	entries []WALEntry
	clock   *HybridClock
	bus     Bus
}

func NewFileWAL(path string, bus Bus, clock *HybridClock) *FileWAL {
	w := &FileWAL{
		path:  path,
		clock: clock,
		bus:   bus,
	}
	w.loadFromDisk()
	if n := len(w.entries); n > 0 {
		last := w.entries[n-1].LSN
		for clock.Next() < last {
		}
	}
	return w
}

func (w *FileWAL) Append(event schemas.Event) (WALEntry, error) {
	lsn := w.clock.Next()
	entry := WALEntry{LSN: lsn, Event: event}

	w.mu.Lock()
	defer w.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(w.path), 0o755); err != nil {
		return WALEntry{}, err
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return WALEntry{}, err
	}
	b, err := json.Marshal(entry)
	if err != nil {
		_ = f.Close()
		return WALEntry{}, err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		return WALEntry{}, err
	}
	_ = f.Close()

	w.entries = append(w.entries, entry)
	w.bus.Publish(Message{Channel: "wal.events", Body: entry})
	return entry, nil
}

func (w *FileWAL) Scan(fromLSN int64) []WALEntry {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]WALEntry, 0, len(w.entries))
	for _, e := range w.entries {
		if e.LSN >= fromLSN {
			out = append(out, e)
		}
	}
	return out
}

func (w *FileWAL) LatestLSN() int64 {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if len(w.entries) == 0 {
		return 0
	}
	return w.entries[len(w.entries)-1].LSN
}

func (w *FileWAL) loadFromDisk() {
	f, err := os.Open(w.path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var e WALEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err == nil {
			w.entries = append(w.entries, e)
		}
	}
}

// Wipe clears the in-memory replay buffer and deletes the WAL file on disk (admin full data wipe).
func (w *FileWAL) Wipe() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	w.entries = nil
	w.mu.Unlock()
	if w.path == "" {
		return nil
	}
	if err := os.Remove(w.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
