package schemas

import "testing"

func TestMemoryToGraphNode(t *testing.T) {
	m := Memory{
		MemoryID:       "mem_1",
		MemoryType:     "episodic",
		AgentID:        "agent_1",
		SessionID:      "sess_1",
		Content:        "user asked about graph retrieval",
		Summary:        "graph retrieval memory",
		Confidence:     0.95,
		Importance:     0.9,
		FreshnessScore: 0.8,
		IsActive:       true,
	}

	node := MemoryToGraphNode(m)

	if node.ObjectID != "mem_1" {
		t.Fatalf("expected object id mem_1, got %s", node.ObjectID)
	}
	if node.ObjectType != "memory" {
		t.Fatalf("expected object type memory, got %s", node.ObjectType)
	}
	if node.Properties["memory_type"] != "episodic" {
		t.Fatalf("expected memory_type episodic, got %v", node.Properties["memory_type"])
	}
	if node.Properties["importance"] != 0.9 {
		t.Fatalf("expected importance 0.9, got %v", node.Properties["importance"])
	}
	if node.Properties["join_key"] != "mem:mem_1" {
		t.Fatalf("expected join_key mem:mem_1, got %v", node.Properties["join_key"])
	}
}

func TestEventToGraphNode(t *testing.T) {
	e := Event{
		EventID:    "evt_1",
		AgentID:    "agent_1",
		SessionID:  "sess_1",
		EventType:  "user_message",
		Payload:    map[string]any{"text": "hello"},
		Importance: 0.7,
	}

	node := EventToGraphNode(e)

	if node.ObjectID != "evt_1" {
		t.Fatalf("expected object id evt_1, got %s", node.ObjectID)
	}
	if node.ObjectType != "event" {
		t.Fatalf("expected object type event, got %s", node.ObjectType)
	}
	if node.Properties["event_type"] != "user_message" {
		t.Fatalf("expected event_type user_message, got %v", node.Properties["event_type"])
	}
	if node.Properties["join_key"] != "evt:evt_1" {
		t.Fatalf("expected join_key evt:evt_1, got %v", node.Properties["join_key"])
	}
}

func TestArtifactToGraphNode(t *testing.T) {
	a := Artifact{
		ArtifactID:   "art_1",
		SessionID:    "sess_1",
		OwnerAgentID: "agent_1",
		ArtifactType: "document",
		MimeType:     "text/plain",
		Version:      1,
	}

	node := ArtifactToGraphNode(a)

	if node.ObjectID != "art_1" {
		t.Fatalf("expected object id art_1, got %s", node.ObjectID)
	}
	if node.ObjectType != "artifact" {
		t.Fatalf("expected object type artifact, got %s", node.ObjectType)
	}
	if node.Properties["artifact_type"] != "document" {
		t.Fatalf("expected artifact_type document, got %v", node.Properties["artifact_type"])
	}
	if node.Properties["join_key"] != "art:art_1" {
		t.Fatalf("expected join_key art:art_1, got %v", node.Properties["join_key"])
	}
}
