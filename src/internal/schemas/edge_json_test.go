package schemas

import (
	"encoding/json"
	"testing"
)

func TestEdge_JSONProperties(t *testing.T) {
	edge := Edge{
		EdgeID:      "edge_1",
		SrcObjectID: "mem_1",
		SrcType:     "memory",
		EdgeType:    "derived_from",
		DstObjectID: "evt_1",
		DstType:     "event",
		Weight:      1.0,
		Properties: map[string]any{
			"src_join_key": "mem:mem_1",
			"dst_join_key": "evt:evt_1",
			"link_key":     "mem:mem_1->evt:evt_1",
		},
	}

	data, err := json.Marshal(edge)
	if err != nil {
		t.Fatalf("marshal edge: %v", err)
	}

	s := string(data)
	if !containsSubstring(s, `"properties"`) {
		t.Fatalf("expected marshaled edge to contain properties, got %s", s)
	}
	if !containsSubstring(s, `"src_join_key"`) {
		t.Fatalf("expected marshaled edge to contain src_join_key, got %s", s)
	}
	if !containsSubstring(s, `"dst_join_key"`) {
		t.Fatalf("expected marshaled edge to contain dst_join_key, got %s", s)
	}
}
