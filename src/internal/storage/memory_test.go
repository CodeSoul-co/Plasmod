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
