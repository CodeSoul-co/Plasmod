package eventbackbone

import (
	"path/filepath"
	"testing"
)

func TestFileDerivationStore_AppendAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "derivation.log")
	store := NewFileDerivationStore(path)

	e1 := DerivationEntry{LSN: 1, SourceID: "evt_1", SourceType: "event", DerivedID: "mem_1", DerivedType: "memory", Operation: "extract", LogicalTS: 1}
	e2 := DerivationEntry{LSN: 2, SourceID: "evt_2", SourceType: "event", DerivedID: "art_2", DerivedType: "artifact", Operation: "trace", LogicalTS: 2}
	if err := store.Append(e1); err != nil {
		t.Fatalf("append e1: %v", err)
	}
	if err := store.Append(e2); err != nil {
		t.Fatalf("append e2: %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("load count = %d, want 2", len(got))
	}
	if got[0].DerivedID != "mem_1" || got[1].DerivedID != "art_2" {
		t.Fatalf("unexpected load order/content: %#v", got)
	}
}

func TestDerivationLogWithStore_RestoreAndAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "derivation.log")
	store := NewFileDerivationStore(path)
	if err := store.Append(DerivationEntry{
		LSN: 10, SourceID: "evt_prev", SourceType: "event", DerivedID: "mem_prev",
		DerivedType: "memory", Operation: "extract", LogicalTS: 10,
	}); err != nil {
		t.Fatalf("seed append: %v", err)
	}

	log := NewDerivationLogWithStore(NewHybridClock(), NewInMemoryBus(), store)
	restored := log.ForDerived("mem_prev")
	if len(restored) != 1 {
		t.Fatalf("restored entries = %d, want 1", len(restored))
	}

	log.Append("evt_new", "event", "mem_new", "memory", "extract")
	reloaded, err := store.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(reloaded) != 2 {
		t.Fatalf("reloaded entries = %d, want 2", len(reloaded))
	}
}
