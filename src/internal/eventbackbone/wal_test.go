package eventbackbone

import (
	"path/filepath"
	"testing"

	"andb/src/internal/schemas"
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
	w := NewFileWAL(path, bus, clock)

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
	w2 := NewFileWAL(path, NewInMemoryBus(), NewHybridClock())
	all := w2.Scan(0)
	if len(all) != 2 {
		t.Fatalf("restored entries: want 2, got %d", len(all))
	}
	if all[0].Event.EventID != "evt_1" || all[1].Event.EventID != "evt_2" {
		t.Fatalf("unexpected restored order/content: %#v", all)
	}
}
