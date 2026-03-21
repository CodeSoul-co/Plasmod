package materialization

import (
	"testing"

	"andb/src/internal/schemas"
)

func TestService_MaterializeEvent_Basic(t *testing.T) {
	svc := NewService()

	ev := schemas.Event{
		EventID:     "evt_1",
		AgentID:     "agent_1",
		SessionID:   "sess_1",
		WorkspaceID: "ws_1",
		EventType:   "user_message",
		LogicalTS:   42,
		Payload:     map[string]any{"text": "hello from the agent"},
	}
	res := svc.MaterializeEvent(ev)

	if res.Record.ObjectID != "mem_evt_1" {
		t.Errorf("Record.ObjectID: want mem_evt_1, got %q", res.Record.ObjectID)
	}
	if res.Record.Text != "hello from the agent" {
		t.Errorf("Record.Text: want %q, got %q", "hello from the agent", res.Record.Text)
	}
	if res.Record.Namespace != "ws_1" {
		t.Errorf("Record.Namespace: want ws_1, got %q", res.Record.Namespace)
	}
	if res.Memory.MemoryID != "mem_evt_1" {
		t.Errorf("Memory.MemoryID: want mem_evt_1, got %q", res.Memory.MemoryID)
	}
	if res.Memory.MemoryType != "episodic" {
		t.Errorf("Memory.MemoryType: want episodic, got %q", res.Memory.MemoryType)
	}
	if !res.Memory.IsActive {
		t.Error("Memory.IsActive: should be true")
	}
	if res.Version.ObjectID != "mem_evt_1" {
		t.Errorf("Version.ObjectID: want mem_evt_1, got %q", res.Version.ObjectID)
	}
	if res.Version.MutationEventID != "evt_1" {
		t.Errorf("Version.MutationEventID: want evt_1, got %q", res.Version.MutationEventID)
	}
}

func TestService_MaterializeEvent_EdgeDerivation(t *testing.T) {
	svc := NewService()
	ev := schemas.Event{
		EventID:    "evt_2",
		AgentID:    "agent_2",
		SessionID:  "sess_2",
		EventType:  "tool_result_returned",
		CausalRefs: []string{"evt_1"},
	}
	res := svc.MaterializeEvent(ev)

	if len(res.Edges) < 3 {
		t.Errorf("Expected at least 3 edges (session+agent+causal), got %d", len(res.Edges))
	}

	edgeTypes := map[string]bool{}
	for _, e := range res.Edges {
		edgeTypes[e.EdgeType] = true
	}
	for _, want := range []string{"belongs_to_session", "owned_by_agent", "derived_from"} {
		if !edgeTypes[want] {
			t.Errorf("Missing edge type: %q", want)
		}
	}
}

func TestService_MaterializeEvent_StateAndArtifact(t *testing.T) {
	svc := NewService()
	ev := schemas.Event{
		EventID:     "evt_state_1",
		AgentID:     "agent_1",
		SessionID:   "sess_1",
		WorkspaceID: "ws_1",
		EventType:   "user_message",
		LogicalTS:   7,
		Payload: map[string]any{
			"text":          "hello",
			"artifact_uri":  "s3://bucket/obj1.bin",
			"mime_type":     "application/octet-stream",
			"artifact_name": "weights.pt",
		},
	}
	res := svc.MaterializeEvent(ev)
	if res.State == nil || res.StateVersion == nil {
		t.Fatal("expected non-nil State and StateVersion")
	}
	if res.State.StateValue != res.Memory.MemoryID {
		t.Errorf("State.StateValue should point to memory id, got %q want %q", res.State.StateValue, res.Memory.MemoryID)
	}
	if res.Artifact == nil || res.ArtifactVersion == nil {
		t.Fatal("expected non-nil Artifact when artifact_uri is set")
	}
	if res.Artifact.URI != "s3://bucket/obj1.bin" {
		t.Errorf("Artifact.URI: %q", res.Artifact.URI)
	}
	if res.Artifact.Metadata["name"] != "weights.pt" {
		t.Errorf("artifact_name in metadata: %#v", res.Artifact.Metadata)
	}
}

func TestService_MaterializeEvent_NoArtifactWithoutURI(t *testing.T) {
	svc := NewService()
	ev := schemas.Event{
		EventID:   "evt_plain",
		AgentID:   "a",
		SessionID: "s",
		EventType: "user_message",
		LogicalTS: 1,
		Payload:   map[string]any{"text": "only text"},
	}
	res := svc.MaterializeEvent(ev)
	if res.State == nil {
		t.Fatal("expected State")
	}
	if res.Artifact != nil {
		t.Fatal("expected no Artifact without uri in payload")
	}
}

func TestResolveMemoryType(t *testing.T) {
	cases := []struct {
		eventType  string
		wantMemory string
	}{
		{"user_message", "episodic"},
		{"assistant_message", "episodic"},
		{"critique_generated", "reflective"},
		{"plan_updated", "procedural"},
		{"tool_result_returned", "factual"},
		{"unknown_type", "episodic"},
	}
	for _, tc := range cases {
		ev := schemas.Event{EventType: tc.eventType}
		got := resolveMemoryType(ev)
		if got != tc.wantMemory {
			t.Errorf("resolveMemoryType(%q): want %q, got %q", tc.eventType, tc.wantMemory, got)
		}
	}
}
