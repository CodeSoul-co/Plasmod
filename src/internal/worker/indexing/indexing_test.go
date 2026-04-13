package indexing

import (
	"testing"

	"plasmod/src/internal/schemas"
	"plasmod/src/internal/storage"
)

// ─── IndexBuildWorker ────────────────────────────────────────────────────────

func TestIndexBuildWorker_IndexObject_StoresSegment(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	w := CreateInMemoryIndexBuildWorker("test-idx", store.Segments(), store.Indexes())

	err := w.IndexObject("mem_1", "memory", "workspace1", "hello andb")
	if err != nil {
		t.Fatalf("IndexObject failed: %v", err)
	}

	segs := store.Segments().List("workspace1")
	if len(segs) == 0 {
		t.Error("expected at least one segment after IndexObject")
	}
}

func TestIndexBuildWorker_Run_TypedDispatch(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	w := CreateInMemoryIndexBuildWorker("test-idx", store.Segments(), store.Indexes())

	out, err := w.Run(schemas.IndexBuildInput{
		ObjectID:   "mem_2",
		ObjectType: "memory",
		Namespace:  "ns1",
		Text:       "test content",
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if _, ok := out.(schemas.IndexBuildOutput); !ok {
		t.Fatalf("unexpected output type %T", out)
	}
}

func TestIndexBuildWorker_Run_WrongInputType(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	w := CreateInMemoryIndexBuildWorker("test-idx", store.Segments(), store.Indexes())
	_, err := w.Run(schemas.IngestInput{})
	if err == nil {
		t.Error("expected error for wrong input type")
	}
}

// ─── GraphRelationWorker ─────────────────────────────────────────────────────

func TestGraphRelationWorker_IndexEdge_StoresEdge(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	w := CreateInMemoryGraphRelationWorker("test-gr", store.Edges())

	err := w.IndexEdge("mem_src", "memory", "evt_dst", "event", string(schemas.EdgeTypeDerivedFrom), 1.0)
	if err != nil {
		t.Fatalf("IndexEdge failed: %v", err)
	}

	edges := store.Edges().EdgesFrom("mem_src")
	if len(edges) == 0 {
		t.Error("expected edge from mem_src")
	}
	if edges[0].EdgeType != string(schemas.EdgeTypeDerivedFrom) {
		t.Errorf("wrong edge type: %q", edges[0].EdgeType)
	}
}

func TestGraphRelationWorker_Run_TypedDispatch(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	w := CreateInMemoryGraphRelationWorker("test-gr", store.Edges())

	out, err := w.Run(schemas.GraphRelationInput{
		SrcID:    "s1",
		SrcType:  "memory",
		DstID:    "d1",
		DstType:  "event",
		EdgeType: string(schemas.EdgeTypeDerivedFrom),
		Weight:   0.8,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if _, ok := out.(schemas.GraphRelationOutput); !ok {
		t.Fatalf("unexpected output type %T", out)
	}
}

// ─── SubgraphExecutorWorker ──────────────────────────────────────────────────

func TestSubgraphExecutorWorker_Expand_EmptySeeds(t *testing.T) {
	w := CreateInMemorySubgraphExecutorWorker("test-sg")

	resp := w.Expand(schemas.GraphExpandRequest{
		SeedObjectIDs: []string{},
		Hops:          1,
	}, nil, nil)
	if len(resp.Subgraph.Nodes) != 0 {
		t.Errorf("expected empty subgraph for empty seeds, got %d nodes", len(resp.Subgraph.Nodes))
	}
}

func TestSubgraphExecutorWorker_Expand_WithEdges(t *testing.T) {
	w := CreateInMemorySubgraphExecutorWorker("test-sg")

	nodes := []schemas.GraphNode{
		{ObjectID: "mem_A", ObjectType: "memory"},
	}
	edges := []schemas.Edge{
		{
			EdgeID:      "e1",
			SrcObjectID: "mem_A",
			SrcType:     "memory",
			DstObjectID: "evt_A",
			DstType:     "event",
			EdgeType:    string(schemas.EdgeTypeDerivedFrom),
			Weight:      1.0,
		},
	}

	resp := w.Expand(schemas.GraphExpandRequest{
		SeedObjectIDs: []string{"mem_A"},
		Hops:          1,
	}, nodes, edges)
	_ = resp
}

func TestSubgraphExecutorWorker_Run_TypedDispatch(t *testing.T) {
	w := CreateInMemorySubgraphExecutorWorker("test-sg")

	out, err := w.Run(schemas.SubgraphExpandInput{
		Req: schemas.GraphExpandRequest{SeedObjectIDs: []string{"x"}, Hops: 1},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if _, ok := out.(schemas.SubgraphExpandOutput); !ok {
		t.Fatalf("unexpected output type %T", out)
	}
}
