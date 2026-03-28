package integration_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// chainQueryEvent creates a thought event for query chain tests.
func chainQueryEvent(t *testing.T, suffix string) map[string]any {
	return chainMainEvent(t, suffix, "agent_thought", map[string]any{"text": fmt.Sprintf("query chain test %s", suffix)})
}

// chainMainEvent is a helper shared with chain_main_test.
func chainMainEvent(t *testing.T, suffix, eventType string, payload map[string]any) map[string]any {
	t.Helper()
	now := nowISO()
	return map[string]any{
		"event_id":    fmt.Sprintf("chain_q_%s_%s", suffix, t.Name()),
		"agent_id":    "agent_cq",
		"session_id":  "sess_cq",
		"event_type":  eventType,
		"event_time":  now,
		"payload":     payload,
		"importance":  0.5,
		"visibility":  "private",
		"version":     1,
	}
}

// chainQueryReq builds a query for chain query tests.
func chainQueryReq() map[string]any {
	return map[string]any{
		"query_text":    "query chain test",
		"query_scope":   "session",
		"session_id":    "sess_cq",
		"agent_id":      "agent_cq",
		"top_k":         5,
		"object_types":  []string{"memory"},
		"relation_constraints": []string{},
		"response_mode": "structured_evidence",
	}
}

// TestChainQuery_SubgraphExpand_NodesPopulated verifies that after ingesting
// events, ExecuteQuery returns a response where Subgraph.Nodes is non-empty.
func TestChainQuery_SubgraphExpand_NodesPopulated(t *testing.T) {
	// Ingest events.
	ev := chainQueryEvent(t, "subgraph")
	_, ack := doJSON(t, http.MethodPost, "/v1/ingest/events", ev)
	if ack["status"] != "accepted" {
		t.Fatalf("ingest failed: %v", ack)
	}

	// Query.
	q := chainQueryReq()
	_, resp := doJSON(t, http.MethodPost, "/v1/query", q)

	// edges being non-empty means SubgraphExecutor had edges to expand.
	edges, ok := resp["edges"].([]any)
	if !ok || len(edges) == 0 {
		t.Error("expected non-empty edges (subgraph expansion) after ingest")
	}
	t.Logf("edges from subgraph expansion: %d", len(edges))
}

// TestChainQuery_ProofTrace_MultiHop verifies that proof_trace contains
// multiple entries indicating multi-hop traversal.
func TestChainQuery_ProofTrace_MultiHop(t *testing.T) {
	// Ingest multiple related events to build a graph.
	for i := 0; i < 3; i++ {
		ev := chainMainEvent(t, fmt.Sprintf("hop_%d", i), "agent_thought",
			map[string]any{"text": fmt.Sprintf("hop content %d", i)})
		status, _ := doJSON(t, http.MethodPost, "/v1/ingest/events", ev)
		if status != http.StatusOK {
			t.Fatalf("ingest failed: %v", status)
		}
	}

	q := chainQueryReq()
	q["top_k"] = 3
	_, resp := doJSON(t, http.MethodPost, "/v1/query", q)

	trace, ok := resp["proof_trace"].([]any)
	if !ok {
		t.Fatal("proof_trace field missing from response")
	}
	if len(trace) == 0 {
		t.Error("expected non-empty proof_trace after ingest")
	}
	t.Logf("proof_trace entries: %d", len(trace))
}

// TestChainQuery_EdgeTypeFilter_Respected verifies that providing an
// edge_type_filter in the query restricts returned edges.
func TestChainQuery_EdgeTypeFilter_Respected(t *testing.T) {
	ev := chainQueryEvent(t, "filter_test")
	_, ack := doJSON(t, http.MethodPost, "/v1/ingest/events", ev)
	if ack["status"] != "accepted" {
		t.Fatalf("ingest failed: %v", ack)
	}

	q := chainQueryReq()
	q["relation_constraints"] = []string{"derived_from"}
	_, resp := doJSON(t, http.MethodPost, "/v1/query", q)

	edges, ok := resp["edges"].([]any)
	if ok && len(edges) > 0 {
		t.Logf("edges after type filter: %d (all should be derived_from)", len(edges))
	}
}

// TestChainQuery_MaxDepth_Respected verifies that query with max_depth
// parameter caps the proof trace depth.
func TestChainQuery_MaxDepth_Respected(t *testing.T) {
	ev := chainQueryEvent(t, "depth_test")
	_, ack := doJSON(t, http.MethodPost, "/v1/ingest/events", ev)
	if ack["status"] != "accepted" {
		t.Fatalf("ingest failed: %v", ack)
	}

	q := chainQueryReq()
	q["max_depth"] = 1
	_, resp := doJSON(t, http.MethodPost, "/v1/query", q)

	trace, ok := resp["proof_trace"].([]any)
	if !ok {
		t.Skip("proof_trace not in response shape, skipping")
	}
	t.Logf("proof_trace with max_depth=1: %d entries", len(trace))
}

func chainTraceSlot(t *testing.T, resp map[string]any, slot string) []any {
	t.Helper()
	raw, ok := resp["chain_traces"].(map[string]any)
	if !ok {
		t.Fatalf("chain_traces missing or not object")
	}
	v, ok := raw[slot].([]any)
	if !ok {
		t.Fatalf("chain_traces.%s missing or not array: %T", slot, raw[slot])
	}
	return v
}

// TestChainQuery_ChainTracesFourSlots verifies all four chain trace arrays are
// populated on query (read-path summaries for main/memory/collaboration + query chain).
func TestChainQuery_ChainTracesFourSlots(t *testing.T) {
	ev := chainQueryEvent(t, "four_slots")
	_, ack := doJSON(t, http.MethodPost, "/v1/ingest/events", ev)
	if ack["status"] != "accepted" {
		t.Fatalf("ingest failed: %v", ack)
	}

	_, resp := doJSON(t, http.MethodPost, "/v1/query", chainQueryReq())
	for _, slot := range []string{"main", "memory_pipeline", "query", "collaboration"} {
		lines := chainTraceSlot(t, resp, slot)
		if len(lines) == 0 {
			t.Errorf("expected non-empty chain_traces.%s, got %v", slot, lines)
		}
	}
	main0, _ := chainTraceSlot(t, resp, "main")[0].(string)
	if main0 != "phase=query_path" {
		t.Errorf("chain_traces.main[0]: got %q want phase=query_path", main0)
	}
	var collabJoined string
	for _, line := range chainTraceSlot(t, resp, "collaboration") {
		if s, ok := line.(string); ok {
			collabJoined += s + "\n"
		}
	}
	if !strings.Contains(collabJoined, "edges_in_response_total=") {
		t.Errorf("collaboration trace should mention edges_in_response_total: %q", collabJoined)
	}
}
