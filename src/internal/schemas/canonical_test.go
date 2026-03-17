package schemas

import "testing"

// TestCanonicalTypes verifies that the canonical schema types can be
// constructed and that key fields are addressable.  This is a compile-level
// smoke test; it guards against accidental field removal.
func TestCanonicalTypes(t *testing.T) {
	ev := Event{
		EventID:   "evt_1",
		AgentID:   "agent_1",
		SessionID: "sess_1",
		EventType: "user_message",
		Payload:   map[string]any{"text": "hello"},
	}
	if ev.EventID == "" {
		t.Fatal("EventID should not be empty")
	}

	mem := Memory{
		MemoryID:   "mem_1",
		MemoryType: "episodic",
		IsActive:   true,
	}
	if !mem.IsActive {
		t.Fatal("Memory.IsActive should be true")
	}

	edge := Edge{
		EdgeID:      "edge_1",
		SrcObjectID: "mem_1",
		DstObjectID: "agent_1",
		EdgeType:    "owned_by_agent",
	}
	if edge.EdgeType == "" {
		t.Fatal("Edge.EdgeType should not be empty")
	}

	version := ObjectVersion{
		ObjectID:   "mem_1",
		ObjectType: "memory",
		Version:    1,
	}
	if version.Version != 1 {
		t.Fatalf("ObjectVersion.Version: want 1, got %d", version.Version)
	}
}

func TestObjectTypeConstants(t *testing.T) {
	cases := []struct {
		name string
		ot   ObjectType
	}{
		{"agent", ObjectTypeAgent},
		{"session", ObjectTypeSession},
		{"event", ObjectTypeEvent},
		{"memory", ObjectTypeMemory},
		{"state", ObjectTypeState},
		{"artifact", ObjectTypeArtifact},
		{"edge", ObjectTypeEdge},
	}
	for _, tc := range cases {
		if string(tc.ot) == "" {
			t.Errorf("ObjectType %q is empty", tc.name)
		}
	}
}
