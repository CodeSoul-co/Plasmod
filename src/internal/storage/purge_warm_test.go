package storage

import (
	"testing"

	"andb/src/internal/schemas"
)

func TestPurgeMemoryWarmOnly(t *testing.T) {
	store := NewMemoryRuntimeStorage()
	mem := schemas.Memory{
		MemoryID: "m_purge_warm",
		Content:  "x",
		Scope:    "w",
		IsActive: false,
	}
	store.PutMemoryWithBaseEdges(mem)
	store.HotCache().Put(mem.MemoryID, "memory", mem, 1.0)

	PurgeMemoryWarmOnly(store, mem.MemoryID)

	if store.HotCache().Contains(mem.MemoryID) {
		t.Fatal("expected hot evicted")
	}
	if _, ok := store.Objects().GetMemory(mem.MemoryID); ok {
		t.Fatal("expected memory removed from warm ObjectStore")
	}
}
