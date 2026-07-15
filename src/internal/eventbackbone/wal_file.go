package eventbackbone

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"plasmod/src/internal/schemas"
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

const acceptancePlaceholder = "0000000000000000000"

type fileWALEntry struct {
	LSN                int64
	AcceptedAtUnixNano string
	Event              schemas.Event
}

func NewFileWAL(path string, bus Bus, clock *HybridClock) (*FileWAL, error) {
	w := &FileWAL{
		path:  path,
		clock: clock,
		bus:   bus,
	}
	if err := w.loadFromDisk(); err != nil {
		return nil, err
	}
	if n := len(w.entries); n > 0 {
		clock.AdvanceTo(w.entries[n-1].LSN)
	}
	return w, nil
}

func (w *FileWAL) Append(event schemas.Event) (WALEntry, error) {
	lsn := w.clock.Next()
	event = event.NormalizeDynamicEventV04()
	event.Time.WalLSN = lsn
	if event.Time.LogicalTS == 0 {
		event.Time.LogicalTS = lsn
	}
	entry := WALEntry{LSN: lsn, Event: event}

	w.mu.Lock()
	defer w.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(w.path), 0o755); err != nil {
		return WALEntry{}, err
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return WALEntry{}, err
	}
	start, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		_ = f.Close()
		return WALEntry{}, err
	}
	diskEntry := fileWALEntry{LSN: lsn, AcceptedAtUnixNano: acceptancePlaceholder, Event: event}
	b, err := json.Marshal(diskEntry)
	if err != nil {
		_ = f.Close()
		return WALEntry{}, err
	}
	marker := []byte(`"AcceptedAtUnixNano":"` + acceptancePlaceholder + `"`)
	markerIndex := bytes.Index(b, marker)
	if markerIndex < 0 {
		_ = f.Close()
		return WALEntry{}, fmt.Errorf("encode WAL acceptance marker")
	}
	rollback := func(cause error) (WALEntry, error) {
		if truncateErr := f.Truncate(start); truncateErr != nil {
			cause = fmt.Errorf("%w; truncate partial WAL append: %v", cause, truncateErr)
		}
		_ = f.Close()
		return WALEntry{}, cause
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		return rollback(err)
	}
	entry.AcceptedAtUnixNano = time.Now().UnixNano()
	acceptedText := strconv.FormatInt(entry.AcceptedAtUnixNano, 10)
	if len(acceptedText) != len(acceptancePlaceholder) {
		return rollback(fmt.Errorf("encode WAL acceptance timestamp %q", acceptedText))
	}
	digitsOffset := start + int64(markerIndex+len(`"AcceptedAtUnixNano":"`))
	if _, err := f.WriteAt([]byte(acceptedText), digitsOffset); err != nil {
		return rollback(err)
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

func (w *FileWAL) loadFromDisk() error {
	f, err := os.OpenFile(w.path, os.O_RDWR, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open WAL %q: %w", w.path, err)
	}
	defer f.Close()

	reader := bufio.NewReader(f)
	var completeBytes int64
	var previousLSN int64
	for index := 0; ; index++ {
		line, readErr := reader.ReadBytes('\n')
		if readErr == io.EOF && len(line) == 0 {
			return nil
		}
		if readErr == io.EOF {
			if err := f.Truncate(completeBytes); err != nil {
				return fmt.Errorf("truncate torn WAL %q at byte %d: %w", w.path, completeBytes, err)
			}
			return nil
		}
		if readErr != nil {
			return fmt.Errorf("read WAL %q entry %d: %w", w.path, index, readErr)
		}
		var diskEntry fileWALEntry
		if err := json.Unmarshal(bytes.TrimSpace(line), &diskEntry); err != nil {
			return fmt.Errorf("decode WAL %q entry %d: %w", w.path, index, err)
		}
		e := WALEntry{LSN: diskEntry.LSN, Event: diskEntry.Event}
		if diskEntry.AcceptedAtUnixNano != "" {
			acceptedAt, err := strconv.ParseInt(diskEntry.AcceptedAtUnixNano, 10, 64)
			if err != nil || acceptedAt <= 0 {
				return fmt.Errorf("decode WAL %q entry %d: invalid acceptance timestamp %q", w.path, index, diskEntry.AcceptedAtUnixNano)
			}
			e.AcceptedAtUnixNano = acceptedAt
		}
		if e.LSN <= 0 || e.LSN == math.MaxInt64 {
			return fmt.Errorf("decode WAL %q entry %d: invalid LSN %d", w.path, index, e.LSN)
		}
		if index > 0 && e.LSN <= previousLSN {
			return fmt.Errorf(
				"decode WAL %q entry %d: non-monotonic LSN %d after %d",
				w.path, index, e.LSN, previousLSN,
			)
		}
		w.entries = append(w.entries, e)
		previousLSN = e.LSN
		completeBytes += int64(len(line))
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
