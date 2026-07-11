package consistency

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileCheckpointRoundTripAndReset(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nested", "checkpoint.json")
	store := NewFileCheckpoint(path)
	if lsn, exists, err := store.Load(); err != nil || exists || lsn != 0 {
		t.Fatalf("initial Load: lsn=%d exists=%t err=%v", lsn, exists, err)
	}
	if err := store.Save(42); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if lsn, exists, err := store.Load(); err != nil || !exists || lsn != 42 {
		t.Fatalf("Load after Save: lsn=%d exists=%t err=%v", lsn, exists, err)
	}
	if matches, err := filepath.Glob(path + ".tmp-*"); err != nil || len(matches) != 0 {
		t.Fatalf("temporary checkpoint files remain: matches=%v err=%v", matches, err)
	}
	if err := store.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("checkpoint still exists after reset: %v", err)
	}
}

func TestMemoryCheckpointRejectsRegression(t *testing.T) {
	t.Parallel()

	store := NewMemoryCheckpoint()
	if err := store.Save(12); err != nil {
		t.Fatalf("Save(12): %v", err)
	}
	if err := store.Save(8); err == nil {
		t.Fatal("expected checkpoint regression error")
	}
}
