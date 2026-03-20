package schemas

import "testing"

func TestOneHopExpandWithEdgeTypeFilter(t *testing.T) {
	nodes := []GraphNode{
		{ObjectID: "mem_1", ObjectType: "memory", Label: "Memory 1"},
		{ObjectID: "evt_1", ObjectType: "event", Label: "Event 1"},
		{ObjectID: "art_1", ObjectType: "artifact", Label: "Artifact 1"},
	}

	edges := []Edge{
		{
			EdgeID:      "edge_1",
			SrcObjectID: "mem_1",
			SrcType:     "memory",
			EdgeType:    "derived_from",
			DstObjectID: "evt_1",
			DstType:     "event",
			Weight:      1.0,
		},
		{
			EdgeID:      "edge_2",
			SrcObjectID: "mem_1",
			SrcType:     "memory",
			EdgeType:    "linked_to",
			DstObjectID: "art_1",
			DstType:     "artifact",
			Weight:      0.8,
		},
	}

	subgraph := OneHopExpand("mem_1", nodes, edges, []string{"derived_from"})

	if len(subgraph.SeedIDs) != 1 || subgraph.SeedIDs[0] != "mem_1" {
		t.Fatalf("unexpected seed ids: %+v", subgraph.SeedIDs)
	}

	if len(subgraph.Edges) != 1 {
		t.Fatalf("expected 1 filtered edge, got %d", len(subgraph.Edges))
	}

	if len(subgraph.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(subgraph.Nodes))
	}

	if len(subgraph.ProofTrace) == 0 {
		t.Fatal("expected proof trace to be non-empty")
	}
}

func TestExpandFromRequest(t *testing.T) {
	nodes := []GraphNode{
		{ObjectID: "mem_1", ObjectType: "memory", Label: "Memory 1"},
		{ObjectID: "evt_1", ObjectType: "event", Label: "Event 1"},
		{ObjectID: "art_1", ObjectType: "artifact", Label: "Artifact 1"},
	}

	edges := []Edge{
		{
			EdgeID:      "edge_1",
			SrcObjectID: "mem_1",
			SrcType:     "memory",
			EdgeType:    "derived_from",
			DstObjectID: "evt_1",
			DstType:     "event",
			Weight:      1.0,
		},
		{
			EdgeID:      "edge_2",
			SrcObjectID: "mem_1",
			SrcType:     "memory",
			EdgeType:    "linked_to",
			DstObjectID: "art_1",
			DstType:     "artifact",
			Weight:      0.8,
		},
	}

	req := GraphExpandRequest{
		SeedObjectIDs: []string{"mem_1"},
		Hops:          1,
		EdgeTypes:     []string{"derived_from"},
		IncludeProps:  true,
	}

	resp := ExpandFromRequest(req, nodes, edges)

	if len(resp.Subgraph.SeedIDs) != 1 || resp.Subgraph.SeedIDs[0] != "mem_1" {
		t.Fatalf("unexpected seed ids: %+v", resp.Subgraph.SeedIDs)
	}

	if len(resp.Subgraph.Edges) != 1 {
		t.Fatalf("expected 1 filtered edge, got %d", len(resp.Subgraph.Edges))
	}

	if len(resp.Subgraph.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(resp.Subgraph.Nodes))
	}
}