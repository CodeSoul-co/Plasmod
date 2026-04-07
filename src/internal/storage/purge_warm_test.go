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

func TestPurgeMemoryWarmOnlyWithStats(t *testing.T) {
	store := NewMemoryRuntimeStorage()
	mem := schemas.Memory{
		MemoryID: "m_purge_warm_stats",
		Content:  "x",
		Scope:    "w",
		IsActive: false,
	}
	store.PutMemoryWithBaseEdges(mem)
	store.HotCache().Put(mem.MemoryID, "memory", mem, 1.0)
	store.Edges().PutEdge(schemas.Edge{
		EdgeID:      "e_purge_warm_stats_1",
		SrcObjectID: mem.MemoryID,
		SrcType:     "memory",
		EdgeType:    "derived_from",
		DstObjectID: "other_node",
		DstType:     "memory",
	})

	before := store.Edges().BulkEdges([]string{mem.MemoryID})
	if len(before) == 0 {
		t.Fatal("expected base edges before purge")
	}

	stats := PurgeMemoryWarmOnlyWithStats(store, mem.MemoryID)
	if stats.EdgeDeleteSucceeded <= 0 {
		t.Fatalf("expected edge deletes > 0, got %+v", stats)
	}
	if stats.EdgeDeleteFailed != 0 {
		t.Fatalf("expected edge delete failures = 0, got %+v", stats)
	}
	if stats.EdgeDeleteRetried != 0 {
		t.Fatalf("expected no retries when bulk deleter is available, got %+v", stats)
	}

	after := store.Edges().BulkEdges([]string{mem.MemoryID})
	if len(after) != 0 {
		t.Fatalf("expected all edges removed, got %d", len(after))
	}
	if _, ok := store.Objects().GetMemory(mem.MemoryID); ok {
		t.Fatal("expected memory removed from warm ObjectStore")
	}
}
