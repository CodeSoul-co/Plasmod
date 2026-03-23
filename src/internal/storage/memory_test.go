package storage

import (
	"testing"

	"andb/src/internal/schemas"
)

func TestMemoryRuntimeStorage_Stores(t *testing.T) {
	store := NewMemoryRuntimeStorage()

	if store.Objects() == nil {
		t.Fatal("Objects() should not be nil")
	}
	if store.Segments() == nil {
		t.Fatal("Segments() should not be nil")
	}
	if store.Indexes() == nil {
		t.Fatal("Indexes() should not be nil")
	}
	if store.Edges() == nil {
		t.Fatal("Edges() should not be nil")
	}
	if store.Versions() == nil {
		t.Fatal("Versions() should not be nil")
	}
	if store.Policies() == nil {
		t.Fatal("Policies() should not be nil")
	}
	if store.HotCache() == nil {
		t.Fatal("HotCache() should not be nil")
	}
}

func TestMemoryObjectStore_PutAndGet(t *testing.T) {
	store := NewMemoryRuntimeStorage()

	mem := schemas.Memory{
		MemoryID:   "mem_test_1",
		MemoryType: "episodic",
		AgentID:    "agent_1",
		Content:    "test content",
		IsActive:   true,
	}
	store.Objects().PutMemory(mem)

	got, ok := store.Objects().GetMemory("mem_test_1")
	if !ok {
		t.Fatal("GetMemory: expected to find mem_test_1")
	}
	if got.Content != "test content" {
		t.Errorf("Content: want %q, got %q", "test content", got.Content)
	}
}

func TestMemoryGraphEdgeStore_BulkEdges(t *testing.T) {
	store := NewMemoryRuntimeStorage()
	es := store.Edges()

	e1 := schemas.Edge{
		EdgeID:      "edge_1",
		SrcObjectID: "mem_1",
		DstObjectID: "agent_1",
		EdgeType:    "owned_by_agent",
	}
	e2 := schemas.Edge{
		EdgeID:      "edge_2",
		SrcObjectID: "mem_2",
		DstObjectID: "mem_1",
		EdgeType:    "derived_from",
	}
	es.PutEdge(e1)
	es.PutEdge(e2)

	bulk := es.BulkEdges([]string{"mem_1"})
	if len(bulk) != 2 {
		t.Errorf("BulkEdges: want 2 edges incident to mem_1, got %d", len(bulk))
	}
}

func TestMemoryGraphEdgeStore_DeleteEdge(t *testing.T) {
	store := NewMemoryRuntimeStorage()
	es := store.Edges()

	es.PutEdge(schemas.Edge{EdgeID: "edge_del", SrcObjectID: "a", DstObjectID: "b", EdgeType: "x"})
	es.DeleteEdge("edge_del")

	edges := es.BulkEdges([]string{"a"})
	if len(edges) != 0 {
		t.Errorf("expected 0 edges after DeleteEdge, got %d", len(edges))
	}
}

// R2: secondary-index EdgesFrom/EdgesTo
func TestMemoryGraphEdgeStore_EdgesFrom_Indexed(t *testing.T) {
	es := newMemoryGraphEdgeStore()
	es.PutEdge(schemas.Edge{EdgeID: "e1", SrcObjectID: "src1", DstObjectID: "dst1", EdgeType: "x"})
	es.PutEdge(schemas.Edge{EdgeID: "e2", SrcObjectID: "src1", DstObjectID: "dst2", EdgeType: "y"})
	es.PutEdge(schemas.Edge{EdgeID: "e3", SrcObjectID: "src2", DstObjectID: "dst1", EdgeType: "z"})

	got := es.EdgesFrom("src1")
	if len(got) != 2 {
		t.Errorf("EdgesFrom src1: want 2, got %d", len(got))
	}
	got2 := es.EdgesFrom("nonexistent")
	if len(got2) != 0 {
		t.Errorf("EdgesFrom nonexistent: want 0, got %d", len(got2))
	}
}

func TestMemoryGraphEdgeStore_EdgesTo_Indexed(t *testing.T) {
	es := newMemoryGraphEdgeStore()
	es.PutEdge(schemas.Edge{EdgeID: "e1", SrcObjectID: "s1", DstObjectID: "dst1", EdgeType: "a"})
	es.PutEdge(schemas.Edge{EdgeID: "e2", SrcObjectID: "s2", DstObjectID: "dst1", EdgeType: "b"})
	es.PutEdge(schemas.Edge{EdgeID: "e3", SrcObjectID: "s3", DstObjectID: "dst2", EdgeType: "c"})

	got := es.EdgesTo("dst1")
	if len(got) != 2 {
		t.Errorf("EdgesTo dst1: want 2, got %d", len(got))
	}
}

func TestMemoryGraphEdgeStore_IndexConsistencyAfterDelete(t *testing.T) {
	es := newMemoryGraphEdgeStore()
	es.PutEdge(schemas.Edge{EdgeID: "e1", SrcObjectID: "src1", DstObjectID: "dst1"})
	es.DeleteEdge("e1")

	if len(es.EdgesFrom("src1")) != 0 {
		t.Error("index not cleaned after DeleteEdge from srcIdx")
	}
	if len(es.EdgesTo("dst1")) != 0 {
		t.Error("index not cleaned after DeleteEdge from dstIdx")
	}
}

// R7: ExpiresAt field + PruneExpiredEdges
func TestMemoryGraphEdgeStore_PruneExpiredEdges(t *testing.T) {
	es := newMemoryGraphEdgeStore()
	es.PutEdge(schemas.Edge{EdgeID: "live", SrcObjectID: "a", DstObjectID: "b", ExpiresAt: "2099-01-01T00:00:00Z"})
	es.PutEdge(schemas.Edge{EdgeID: "dead", SrcObjectID: "c", DstObjectID: "d", ExpiresAt: "2000-01-01T00:00:00Z"})
	es.PutEdge(schemas.Edge{EdgeID: "eternal", SrcObjectID: "e", DstObjectID: "f"}) // no expiry

	pruned := es.PruneExpiredEdges("2026-01-01T00:00:00Z")
	if pruned != 1 {
		t.Errorf("PruneExpiredEdges: want 1 pruned, got %d", pruned)
	}
	if _, ok := es.GetEdge("dead"); ok {
		t.Error("expired edge 'dead' should have been removed")
	}
	if _, ok := es.GetEdge("live"); !ok {
		t.Error("non-expired edge 'live' should still exist")
	}
	if _, ok := es.GetEdge("eternal"); !ok {
		t.Error("no-expiry edge 'eternal' should still exist")
	}
	// verify index was cleaned for pruned edge
	if len(es.EdgesFrom("c")) != 0 {
		t.Error("srcIdx not cleaned for pruned edge")
	}
}

// R6: InMemoryColdStore edge methods
func TestInMemoryColdStore_EdgeRoundtrip(t *testing.T) {
	cold := NewInMemoryColdStore()

	e := schemas.Edge{EdgeID: "cold_e1", SrcObjectID: "mem_1", DstObjectID: "evt_1", EdgeType: "derived_from", Weight: 1.0}
	cold.PutEdge(e)

	got, ok := cold.GetEdge("cold_e1")
	if !ok {
		t.Fatal("GetEdge: expected to find cold_e1")
	}
	if got.EdgeType != "derived_from" {
		t.Errorf("EdgeType: want derived_from, got %s", got.EdgeType)
	}

	list := cold.ListEdges()
	if len(list) != 1 {
		t.Errorf("ListEdges: want 1, got %d", len(list))
	}
}

func TestTieredObjectStore_ArchiveEdge(t *testing.T) {
	hot := NewHotObjectCache(100)
	warm := newMemoryObjectStore()
	cold := NewInMemoryColdStore()
	tiered := NewTieredObjectStore(hot, warm, cold)

	warmEdges := newMemoryGraphEdgeStore()
	warmEdges.PutEdge(schemas.Edge{EdgeID: "e_arc", SrcObjectID: "m1", DstObjectID: "m2", EdgeType: "derived_from"})

	tiered.ArchiveEdge(warmEdges, "e_arc")

	if _, ok := warmEdges.GetEdge("e_arc"); ok {
		t.Error("ArchiveEdge: edge should be removed from warm store")
	}
	if _, ok := cold.GetEdge("e_arc"); !ok {
		t.Error("ArchiveEdge: edge should exist in cold store")
	}
}
