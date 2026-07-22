package eventbackbone

import (
	"testing"

	"plasmod/src/internal/schemas"
)

func TestTransientWALOrdersWithoutRetaining(t *testing.T) {
	wal := NewTransientWAL(NewInMemoryBus(), NewHybridClock())
	entry, err := wal.Append(schemas.Event{EventID: "transient-event"})
	if err != nil {
		t.Fatal(err)
	}
	if entry.LSN <= 0 || wal.LatestLSN() != entry.LSN {
		t.Fatalf("unexpected LSN state: entry=%d latest=%d", entry.LSN, wal.LatestLSN())
	}
	if got := wal.Scan(0); len(got) != 0 {
		t.Fatalf("transient WAL retained %d entries", len(got))
	}
}
