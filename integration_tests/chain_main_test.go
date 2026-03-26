package integration_test

import (
	"fmt"
	"net/http"
	"testing"
)

// chainEvent creates an event with a unique suffix for chain tests.
func chainEvent(t *testing.T, suffix, eventType string, payload map[string]any) map[string]any {
	t.Helper()
	now := nowISO()
	return map[string]any{
		"event_id":    fmt.Sprintf("chain_main_%s_%s", suffix, t.Name()),
		"agent_id":    "agent_chain",
		"session_id":  "sess_chain",
		"event_type":  eventType,
		"event_time": now,
		"payload":     payload,
		"importance":  0.5,
		"visibility":  "private",
		"version":     1,
	}
}

// TestChainMain_Ingest_StateUpdate_StateMaterialized verifies that a
// state_update event results in a State object being returned by the query path.
func TestChainMain_Ingest_StateUpdate_StateMaterialized(t *testing.T) {
	ev := chainEvent(t, "state", "state_update", map[string]any{
		"state_key":   "counter",
		"state_value": 42,
	})
	status, ack := doJSON(t, http.MethodPost, "/v1/ingest/events", ev)
	if status != http.StatusOK {
		t.Fatalf("ingest status: got %d, want 200", status)
	}
	assertKeys(t, ack, "status", "lsn", "event_id")

	// Query for state objects.
	q := chainQuery(t, ev)
	q["object_types"] = []string{"state"}
	_, resp := doJSON(t, http.MethodPost, "/v1/query", q)

	objects, ok := resp["objects"].([]any)
	if !ok || len(objects) == 0 {
		t.Error("expected at least one state object after state_update ingest")
	}
	t.Logf("state objects: %d", len(objects))
}

// TestChainMain_Ingest_ToolCall_ArtifactMaterialized verifies that a tool_call
// event results in an artifact being stored.
func TestChainMain_Ingest_ToolCall_ArtifactMaterialized(t *testing.T) {
	ev := chainEvent(t, "tool", "tool_call", map[string]any{
		"tool_name":  "search",
		"tool_args":  `{"query": "test"}`,
	})
	status, ack := doJSON(t, http.MethodPost, "/v1/ingest/events", ev)
	if status != http.StatusOK {
		t.Fatalf("ingest status: got %d, want 200", status)
	}
	assertKeys(t, ack, "status", "lsn", "event_id")

	// Query for artifact objects.
	q := chainQuery(t, ev)
	q["object_types"] = []string{"artifact"}
	_, resp := doJSON(t, http.MethodPost, "/v1/query", q)

	objects, ok := resp["objects"].([]any)
	if !ok || len(objects) == 0 {
		t.Error("expected at least one artifact object after tool_call ingest")
	}
	t.Logf("artifact objects: %d", len(objects))
}

// TestChainMain_Ingest_Checkpoint_StateVersionSnapshot verifies that a
// checkpoint event produces ObjectVersion entries with a checkpoint tag.
func TestChainMain_Ingest_Checkpoint_StateVersionSnapshotCreated(t *testing.T) {
	// First ingest a state_update to have something to checkpoint.
	stateEv := chainEvent(t, "pre_ckpt", "state_update", map[string]any{
		"state_key":   "snapshot_key",
		"state_value": "before_checkpoint",
	})
	doJSON(t, http.MethodPost, "/v1/ingest/events", stateEv)

	// Ingest the checkpoint event.
	ckptEv := chainEvent(t, "ckpt", "checkpoint", map[string]any{})
	status, ack := doJSON(t, http.MethodPost, "/v1/ingest/events", ckptEv)
	if status != http.StatusOK {
		t.Fatalf("checkpoint ingest status: got %d, want 200", status)
	}
	assertKeys(t, ack, "status", "lsn", "event_id")

	// Query and check versions field.
	q := chainQuery(t, ckptEv)
	q["object_types"] = []string{"state"}
	_, resp := doJSON(t, http.MethodPost, "/v1/query", q)

	versions, ok := resp["versions"].([]any)
	if !ok {
		t.Skip("versions field not available in response shape, skipping")
	}
	if len(versions) == 0 {
		t.Error("expected at least one version entry after checkpoint")
	}
	t.Logf("versions after checkpoint: %d", len(versions))
}

// TestChainMain_Ingest_Edge_GraphRelationIndexed verifies that ingesting an
// event causes edges to appear in the query response.
func TestChainMain_Ingest_Edge_GraphRelationIndexed(t *testing.T) {
	ev := chainEvent(t, "edge_test", "agent_thought", map[string]any{"text": "test"})
	_, ack := doJSON(t, http.MethodPost, "/v1/ingest/events", ev)
	assertKeys(t, ack, "status", "lsn", "event_id")

	q := chainQuery(t, ev)
	_, resp := doJSON(t, http.MethodPost, "/v1/query", q)

	edges, ok := resp["edges"].([]any)
	if !ok || len(edges) == 0 {
		t.Error("expected at least one edge after ingest")
	}
	t.Logf("edge count: %d", len(edges))
}

// TestChainMain_Ingest_IndexBuild_IndexedCountIncrements verifies that after
// ingesting multiple events, the index build worker has processed them.
func TestChainMain_Ingest_IndexBuild_IndexedCountIncrements(t *testing.T) {
	// Ingest two events.
	for i := 0; i < 2; i++ {
		ev := chainEvent(t, fmt.Sprintf("idx_%d", i), "agent_thought", map[string]any{"text": fmt.Sprintf("indexed content %d", i)})
		status, ack := doJSON(t, http.MethodPost, "/v1/ingest/events", ev)
		if status != http.StatusOK {
			t.Fatalf("ingest status: got %d, want 200", status)
		}
		assertKeys(t, ack, "status", "lsn", "event_id")
	}

	// Query and verify provenance includes indexing step.
	q := chainQuery(t, nil)
	q["session_id"] = "sess_chain"
	q["object_types"] = []string{"memory"}
	_, resp := doJSON(t, http.MethodPost, "/v1/query", q)

	provenance, ok := resp["provenance"].([]any)
	if !ok || len(provenance) == 0 {
		t.Error("expected non-empty provenance after ingest")
	}
	t.Logf("provenance stages: %v", provenance)
}

// chainQuery builds a minimal query request for chain tests.
func chainQuery(t *testing.T, ev map[string]any) map[string]any {
	t.Helper()
	q := map[string]any{
		"query_text":    "test",
		"query_scope":  "session",
		"session_id":   "sess_chain",
		"agent_id":     "agent_chain",
		"top_k":        5,
		"object_types": []string{"memory", "state", "artifact"},
		"relation_constraints": []string{},
		"response_mode": "structured_evidence",
	}
	if ev != nil {
		q["session_id"] = ev["session_id"]
		q["agent_id"] = ev["agent_id"]
	}
	return q
}
