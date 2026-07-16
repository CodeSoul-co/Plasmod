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

func TestFileCheckpointRecoversLastCompleteJournalRecord(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "checkpoint.json")
	content := []byte("{\"visible_lsn\":4}\n{\"visible_lsn\":9}\n{\"visible_lsn\":")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write checkpoint journal: %v", err)
	}
	if lsn, exists, err := NewFileCheckpoint(path).Load(); err != nil || !exists || lsn != 9 {
		t.Fatalf("Load: lsn=%d exists=%t err=%v, want 9", lsn, exists, err)
	}
}

func TestFileCheckpointRepairsTornTailBeforeNextSave(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "checkpoint.json")
	content := []byte("{\"visible_lsn\":4}\n{\"visible_lsn\":9}\n{\"visible_lsn\":")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write checkpoint journal: %v", err)
	}
	store := NewFileCheckpoint(path)
	if lsn, exists, err := store.Load(); err != nil || !exists || lsn != 9 {
		t.Fatalf("Load: lsn=%d exists=%t err=%v, want 9", lsn, exists, err)
	}
	if err := store.Save(10); err != nil {
		t.Fatalf("Save after torn tail: %v", err)
	}
	if lsn, exists, err := NewFileCheckpoint(path).Load(); err != nil || !exists || lsn != 10 {
		t.Fatalf("reloaded checkpoint: lsn=%d exists=%t err=%v, want 10", lsn, exists, err)
	}
}

func TestFileCheckpointRejectsMalformedCompleteRecord(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "checkpoint.json")
	content := []byte("{\"visible_lsn\":4}\n{not-json}\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write checkpoint journal: %v", err)
	}
	if _, _, err := NewFileCheckpoint(path).Load(); err == nil {
		t.Fatal("expected complete malformed record to fail closed")
	}
}

func TestFileCheckpointRejectsMalformedMiddleRecord(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "checkpoint.json")
	content := []byte("{\"visible_lsn\":4}\n{not-json}\n{\"visible_lsn\":9}\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write checkpoint journal: %v", err)
	}
	if _, _, err := NewFileCheckpoint(path).Load(); err == nil {
		t.Fatal("expected malformed middle record to fail closed")
	}
}

func TestFileCheckpointAppendsAfterLegacySnapshot(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "checkpoint.json")
	if err := os.WriteFile(path, []byte("{\"visible_lsn\":5}"), 0o600); err != nil {
		t.Fatalf("write legacy checkpoint: %v", err)
	}
	store := NewFileCheckpoint(path)
	if err := store.Save(8); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if lsn, exists, err := NewFileCheckpoint(path).Load(); err != nil || !exists || lsn != 8 {
		t.Fatalf("reloaded checkpoint: lsn=%d exists=%t err=%v, want 8", lsn, exists, err)
	}
}

func TestFileCheckpointRejectsRegressionAfterReopen(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "checkpoint.json")
	if err := os.WriteFile(path, []byte("{\"visible_lsn\":12}\n"), 0o600); err != nil {
		t.Fatalf("write checkpoint: %v", err)
	}
	if err := NewFileCheckpoint(path).Save(8); err == nil {
		t.Fatal("expected checkpoint regression error")
	}
}

func TestFileCheckpointRejectsRegressionAcrossInstances(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "checkpoint.json")
	first := NewFileCheckpoint(path)
	stale := NewFileCheckpoint(path)
	if _, _, err := stale.Load(); err != nil {
		t.Fatalf("prime stale checkpoint: %v", err)
	}
	if err := first.Save(12); err != nil {
		t.Fatalf("Save(12): %v", err)
	}
	if err := stale.Save(8); err == nil {
		t.Fatal("expected cross-instance checkpoint regression error")
	}
}

func TestFileCheckpointRejectsRegressionAfterSameSizeReplacement(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "checkpoint.json")
	stale := NewFileCheckpoint(path)
	replacement := NewFileCheckpoint(path)
	if err := stale.Save(12); err != nil {
		t.Fatalf("Save(12): %v", err)
	}
	if err := replacement.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if err := replacement.Save(20); err != nil {
		t.Fatalf("Save(20): %v", err)
	}
	if err := stale.Save(15); err == nil {
		t.Fatal("expected regression after same-size replacement")
	}
}

func TestFileCheckpointCompactsJournalAtConfiguredLimit(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "checkpoint.json")
	store := NewFileCheckpoint(path)
	store.maxJournalBytes = 256
	for lsn := int64(1); lsn <= 100; lsn++ {
		if err := store.Save(lsn); err != nil {
			t.Fatalf("Save(%d): %v", lsn, err)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Size() > store.maxJournalBytes {
		t.Fatalf("journal size = %d, limit = %d", info.Size(), store.maxJournalBytes)
	}
	if lsn, exists, err := NewFileCheckpoint(path).Load(); err != nil || !exists || lsn != 100 {
		t.Fatalf("reloaded checkpoint: lsn=%d exists=%t err=%v, want 100", lsn, exists, err)
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

func BenchmarkFileCheckpointSave(b *testing.B) {
	store := NewFileCheckpoint(filepath.Join(b.TempDir(), "checkpoint.json"))
	b.ResetTimer()
	for i := 1; i <= b.N; i++ {
		if err := store.Save(int64(i)); err != nil {
			b.Fatalf("Save(%d): %v", i, err)
		}
	}
}
