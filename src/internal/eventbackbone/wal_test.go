package eventbackbone

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"plasmod/src/internal/schemas"
)

func TestInMemoryWAL_AppendAndLatestLSN(t *testing.T) {
	bus := NewInMemoryBus()
	clock := NewHybridClock()
	wal := NewInMemoryWAL(bus, clock)

	if wal.LatestLSN() != 0 {
		t.Fatalf("LatestLSN: want 0 on empty WAL, got %d", wal.LatestLSN())
	}

	ev := schemas.Event{EventID: "evt_1", EventType: "user_message"}
	entry, err := wal.Append(ev)
	if err != nil {
		t.Fatalf("Append: unexpected error: %v", err)
	}
	if entry.LSN <= 0 {
		t.Fatalf("Append: expected LSN > 0, got %d", entry.LSN)
	}
	if wal.LatestLSN() != entry.LSN {
		t.Errorf("LatestLSN: want %d, got %d", entry.LSN, wal.LatestLSN())
	}
}

func TestInMemoryWAL_Scan(t *testing.T) {
	bus := NewInMemoryBus()
	clock := NewHybridClock()
	wal := NewInMemoryWAL(bus, clock)

	for i := 0; i < 5; i++ {
		_, _ = wal.Append(schemas.Event{EventID: "evt", EventType: "tick"})
	}

	all := wal.Scan(0)
	if len(all) != 5 {
		t.Fatalf("Scan(0): want 5 entries, got %d", len(all))
	}

	// Scan from the 3rd LSN — should return entries from that point onward.
	thirdLSN := all[2].LSN
	tail := wal.Scan(thirdLSN)
	if len(tail) != 3 {
		t.Errorf("Scan(lsn[2]): want 3 entries, got %d", len(tail))
	}
}

func TestInMemoryBus_PubSub(t *testing.T) {
	bus := NewInMemoryBus()
	ch := bus.Subscribe("test.channel")

	bus.Publish(Message{Channel: "test.channel", Body: "hello"})

	msg := <-ch
	if msg.Body != "hello" {
		t.Errorf("Bus message body: want %q, got %v", "hello", msg.Body)
	}
}

func TestFileWAL_AppendAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal.log")
	bus := NewInMemoryBus()
	clock := NewHybridClock()
	w, err := NewFileWAL(path, bus, clock)
	if err != nil {
		t.Fatalf("NewFileWAL: %v", err)
	}

	if _, err := w.Append(schemas.Event{EventID: "evt_1", EventType: "user_message"}); err != nil {
		t.Fatalf("append 1: %v", err)
	}
	if _, err := w.Append(schemas.Event{EventID: "evt_2", EventType: "tool_call"}); err != nil {
		t.Fatalf("append 2: %v", err)
	}
	if w.LatestLSN() == 0 {
		t.Fatal("expected LatestLSN > 0")
	}

	// Re-open from same file and verify entries are restored.
	w2, err := NewFileWAL(path, NewInMemoryBus(), NewHybridClock())
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	all := w2.Scan(0)
	if len(all) != 2 {
		t.Fatalf("restored entries: want 2, got %d", len(all))
	}
	if all[0].Event.EventID != "evt_1" || all[1].Event.EventID != "evt_2" {
		t.Fatalf("unexpected restored order/content: %#v", all)
	}
}

func TestFileWAL_ReloadsRecordsLargerThanScannerTokenLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal.log")
	w, err := NewFileWAL(path, NewInMemoryBus(), NewHybridClock())
	if err != nil {
		t.Fatalf("NewFileWAL: %v", err)
	}
	large := schemas.Event{
		EventID:   "large-event",
		EventType: "artifact",
		Payload:   map[string]any{"text": strings.Repeat("x", 128*1024)},
	}
	entry, err := w.Append(large)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	reloaded, err := NewFileWAL(path, NewInMemoryBus(), NewHybridClock())
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	entries := reloaded.Scan(0)
	if len(entries) != 1 {
		t.Fatalf("reloaded entries = %d, want 1", len(entries))
	}
	if entries[0].LSN != entry.LSN || entries[0].Event.Identity.EventID != "large-event" {
		t.Fatalf("reloaded entry = %+v, want LSN %d large-event", entries[0], entry.LSN)
	}
}

func TestFileWAL_PersistsCoreAcceptanceTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal.log")
	w, err := NewFileWAL(path, NewInMemoryBus(), NewHybridClock())
	if err != nil {
		t.Fatalf("NewFileWAL: %v", err)
	}
	event := schemas.Event{
		EventID: "old-client-time",
		Time:    schemas.EventTime{IngestTime: 1},
	}
	before := time.Now()
	entry, err := w.Append(event)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	after := time.Now()
	acceptedAt := time.Unix(0, entry.AcceptedAtUnixNano)
	if acceptedAt.Before(before) || acceptedAt.After(after) {
		t.Fatalf("accepted_at = %v, want within [%v, %v]", acceptedAt, before, after)
	}

	reloaded, err := NewFileWAL(path, NewInMemoryBus(), NewHybridClock())
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	entries := reloaded.Scan(0)
	if len(entries) != 1 || entries[0].AcceptedAtUnixNano != entry.AcceptedAtUnixNano {
		t.Fatalf("reloaded acceptance metadata = %+v, want %d", entries, entry.AcceptedAtUnixNano)
	}
}

func TestFileWAL_TruncatesTornFinalRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal.log")
	complete, _ := json.Marshal(WALEntry{LSN: 1, Event: schemas.Event{EventID: "complete"}})
	data := append(append(complete, '\n'), []byte(`{"LSN":2,"Event":{"event_id":"torn`)...)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	w, err := NewFileWAL(path, NewInMemoryBus(), NewHybridClock())
	if err != nil {
		t.Fatalf("NewFileWAL: %v", err)
	}
	if entries := w.Scan(0); len(entries) != 1 || entries[0].LSN != 1 {
		t.Fatalf("recovered entries = %+v, want only complete LSN 1", entries)
	}
	want := append(complete, '\n')
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("repaired WAL = %q, want %q", got, want)
	}
}

func TestFileWAL_RejectsNonMonotonicLSNs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal.log")
	first, _ := json.Marshal(WALEntry{LSN: 2, Event: schemas.Event{EventID: "newer"}})
	second, _ := json.Marshal(WALEntry{LSN: 1, Event: schemas.Event{EventID: "older"}})
	data := append(append(first, '\n'), append(second, '\n')...)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := NewFileWAL(path, NewInMemoryBus(), NewHybridClock()); err == nil {
		t.Fatal("NewFileWAL accepted non-monotonic LSNs")
	}
}

func TestFileWAL_RejectsConcatenatedRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal.log")
	first, _ := json.Marshal(WALEntry{LSN: 1, Event: schemas.Event{EventID: "first"}})
	second, _ := json.Marshal(WALEntry{LSN: 2, Event: schemas.Event{EventID: "second"}})
	data := append(append(first, second...), '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := NewFileWAL(path, NewInMemoryBus(), NewHybridClock()); err == nil {
		t.Fatal("NewFileWAL accepted concatenated JSON records")
	}
}

func TestFileWAL_AdvancesClockToLargeRecoveredLSN(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal.log")
	entry, _ := json.Marshal(WALEntry{LSN: 1_000_000_000_000, Event: schemas.Event{EventID: "large-lsn"}})
	if err := os.WriteFile(path, append(entry, '\n'), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	started := time.Now()
	w, err := NewFileWAL(path, NewInMemoryBus(), NewHybridClock())
	if err != nil {
		t.Fatalf("NewFileWAL: %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("large LSN recovery took %v", elapsed)
	}
	next, err := w.Append(schemas.Event{EventID: "next"})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if next.LSN != 1_000_000_000_001 {
		t.Fatalf("next LSN = %d, want 1000000000001", next.LSN)
	}
}
