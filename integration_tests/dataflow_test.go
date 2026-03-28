package integration_test

import (
	"net/http"
	"testing"
)

// TestDataflowTrace validates that the ingest→query pipeline surfaces
// the expected data-flow metadata fields (provenance, proof_trace, applied_filters).
func TestDataflowTrace(t *testing.T) {
	// Seed an event before querying.
	ev := sampleEvent(uniqID())
	status, ack := doJSON(t, http.MethodPost, "/v1/ingest/events", ev)
	if status != http.StatusOK {
		t.Fatalf("ingest status: got %d, want 200", status)
	}
	assertKeys(t, ack, "status", "lsn", "event_id")

	_, resp := doJSON(t, http.MethodPost, "/v1/query", sampleQuery())

	t.Run("provenance field is present and non-empty", func(t *testing.T) {
		provenance, ok := resp["provenance"].([]any)
		if !ok {
			t.Fatalf("provenance: expected []any, got %T", resp["provenance"])
		}
		if len(provenance) == 0 {
			t.Error("provenance list is empty")
		}
		t.Logf("provenance stages: %v", provenance)
	})

	t.Run("proof_trace field is present and non-empty", func(t *testing.T) {
		trace, ok := resp["proof_trace"].([]any)
		if !ok {
			t.Fatalf("proof_trace: expected []any, got %T", resp["proof_trace"])
		}
		if len(trace) == 0 {
			t.Error("proof_trace list is empty")
		}
		t.Logf("proof_trace: %v", trace)
	})

	t.Run("applied_filters field is present and non-empty", func(t *testing.T) {
		filters, ok := resp["applied_filters"].([]any)
		if !ok {
			t.Fatalf("applied_filters: expected []any, got %T", resp["applied_filters"])
		}
		if len(filters) == 0 {
			t.Error("applied_filters list is empty")
		}
		t.Logf("applied_filters: %v", filters)
	})

	t.Run("edges field is non-empty after ingest", func(t *testing.T) {
		edges, ok := resp["edges"].([]any)
		if !ok {
			t.Fatalf("edges: expected []any, got %T", resp["edges"])
		}
		if len(edges) == 0 {
			t.Error("edges list is empty — expected at least one edge (session+agent) after ingest")
		}
		t.Logf("edge count: %d", len(edges))
	})

	t.Run("versions field is non-empty after ingest", func(t *testing.T) {
		versions, ok := resp["versions"].([]any)
		if !ok {
			t.Fatalf("versions: expected []any, got %T", resp["versions"])
		}
		if len(versions) == 0 {
			t.Error("versions list is empty — expected ObjectVersion for ingested memory")
		}
		t.Logf("version count: %d", len(versions))
	})

	t.Run("ingest ack lsn is positive integer", func(t *testing.T) {
		lsn, ok := ack["lsn"].(float64)
		if !ok || lsn <= 0 {
			t.Errorf("ack lsn: expected positive number, got %v", ack["lsn"])
		}
	})

	t.Run("ingest ack event_id matches request", func(t *testing.T) {
		if ack["event_id"] != ev["event_id"] {
			t.Errorf("ack event_id: got %v, want %v", ack["event_id"], ev["event_id"])
		}
	})

	t.Run("chain_traces.query is populated after ingest", func(t *testing.T) {
		ct, ok := resp["chain_traces"].(map[string]any)
		if !ok {
			t.Fatalf("chain_traces: expected object, got %T", resp["chain_traces"])
		}
		q, ok := ct["query"].([]any)
		if !ok {
			t.Fatalf("chain_traces.query: expected array, got %T", ct["query"])
		}
		if len(q) == 0 {
			t.Error("chain_traces.query is empty — expected QueryChain trace lines")
		}
	})
}
