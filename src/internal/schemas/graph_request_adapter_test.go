package schemas

import "testing"

func TestResolveSeedObjectIDsFromJoinKeys(t *testing.T) {
	nodes := []GraphNode{
		{
			ObjectID:   "mem_1",
			ObjectType: "memory",
			Label:      "Memory 1",
			Properties: map[string]any{
				"join_key": "mem:mem_1",
			},
		},
		{
			ObjectID:   "evt_1",
			ObjectType: "event",
			Label:      "Event 1",
			Properties: map[string]any{
				"join_key": "evt:evt_1",
			},
		},
	}

	ids := ResolveSeedObjectIDsFromJoinKeys(nodes, []string{"evt:evt_1", "mem:mem_1"})

	if len(ids) != 2 {
		t.Fatalf("expected 2 resolved ids, got %d", len(ids))
	}
	if ids[0] != "evt_1" || ids[1] != "mem_1" {
		t.Fatalf("unexpected resolved ids: %+v", ids)
	}
}

func TestExpandFromJoinKeys(t *testing.T) {
	nodes := []GraphNode{
		{
			ObjectID:   "mem_1",
			ObjectType: "memory",
			Label:      "Memory 1",
			Properties: map[string]any{
				"join_key": "mem:mem_1",
			},
		},
		{
			ObjectID:   "evt_1",
			ObjectType: "event",
			Label:      "Event 1",
			Properties: map[string]any{
				"join_key": "evt:evt_1",
			},
		},
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
	}

	req := GraphExpandRequest{
		Hops:         1,
		EdgeTypes:    []string{"derived_from"},
		IncludeProps: true,
	}

	resp := ExpandFromJoinKeys([]string{"mem:mem_1"}, req, nodes, edges)

	if len(resp.Subgraph.SeedIDs) != 1 || resp.Subgraph.SeedIDs[0] != "mem_1" {
		t.Fatalf("unexpected seed ids: %+v", resp.Subgraph.SeedIDs)
	}

	if len(resp.Subgraph.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(resp.Subgraph.Edges))
	}

	if len(resp.Subgraph.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(resp.Subgraph.Nodes))
	}
}
