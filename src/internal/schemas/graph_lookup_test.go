package schemas

import "testing"

func TestGetJoinKey(t *testing.T) {
	node := GraphNode{
		ObjectID:   "mem_1",
		ObjectType: "memory",
		Label:      "memory node",
		Properties: map[string]any{
			"join_key": "mem:mem_1",
		},
	}

	jk, ok := GetJoinKey(node)
	if !ok {
		t.Fatal("expected join_key to exist")
	}
	if jk != "mem:mem_1" {
		t.Fatalf("expected join_key mem:mem_1, got %s", jk)
	}
}

func TestFindNodeByJoinKey(t *testing.T) {
	nodes := []GraphNode{
		{
			ObjectID:   "mem_1",
			ObjectType: "memory",
			Label:      "memory node",
			Properties: map[string]any{
				"join_key": "mem:mem_1",
			},
		},
		{
			ObjectID:   "evt_1",
			ObjectType: "event",
			Label:      "event node",
			Properties: map[string]any{
				"join_key": "evt:evt_1",
			},
		},
	}

	node, ok := FindNodeByJoinKey(nodes, "evt:evt_1")
	if !ok {
		t.Fatal("expected to find node by join_key")
	}
	if node.ObjectID != "evt_1" {
		t.Fatalf("expected object id evt_1, got %s", node.ObjectID)
	}
}
