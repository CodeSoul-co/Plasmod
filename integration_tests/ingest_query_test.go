package integration_test

import (
	"fmt"
	"net/http"
	"testing"
)

func sampleEvent(suffix string) map[string]any {
	now := nowISO()
	return map[string]any{
		"event_id":        fmt.Sprintf("evt_it_%s", suffix),
		"tenant_id":       "t_demo",
		"workspace_id":    "w_demo",
		"agent_id":        "agent_a",
		"session_id":      "sess_a",
		"event_type":      "user_message",
		"event_time":      now,
		"ingest_time":     now,
		"visible_time":    now,
		"logical_ts":      1,
		"parent_event_id": "",
		"causal_refs":     []string{},
		"payload":         map[string]any{"text": fmt.Sprintf("hello integration %s", suffix)},
		"source":          "integration_tests",
		"importance":      0.5,
		"visibility":      "private",
		"version":         1,
	}
}

func sampleQuery() map[string]any {
	return map[string]any{
		"query_text":           "hello integration",
		"query_scope":          "workspace",
		"session_id":           "sess_a",
		"agent_id":             "agent_a",
		"tenant_id":            "t_demo",
		"workspace_id":         "w_demo",
		"top_k":                5,
		"time_window":          map[string]any{"from": "2026-01-01T00:00:00Z", "to": "2027-01-01T00:00:00Z"},
		"object_types":         []string{"memory", "state", "artifact"},
		"memory_types":         []string{"semantic", "episodic", "procedural"},
		"relation_constraints": []string{},
		"response_mode":        "structured_evidence",
	}
}

func TestIngestEvent(t *testing.T) {
	t.Run("valid event returns accepted ack with lsn and event_id", func(t *testing.T) {
		ev := sampleEvent(uniqID())
		status, result := doJSON(t, http.MethodPost, "/v1/ingest/events", ev)
		if status != http.StatusOK {
			t.Fatalf("status: got %d, want 200", status)
		}
		assertKeys(t, result, "status", "lsn", "event_id")
		if result["status"] != "accepted" {
			t.Errorf("ack status: got %v, want accepted", result["status"])
		}
		if result["event_id"] != ev["event_id"] {
			t.Errorf("event_id echo: got %v, want %v", result["event_id"], ev["event_id"])
		}
	})

	t.Run("multiple events each get unique lsn", func(t *testing.T) {
		_, r1 := doJSON(t, http.MethodPost, "/v1/ingest/events", sampleEvent(uniqID()))
		_, r2 := doJSON(t, http.MethodPost, "/v1/ingest/events", sampleEvent(uniqID()))
		lsn1, ok1 := r1["lsn"].(float64)
		lsn2, ok2 := r2["lsn"].(float64)
		if !ok1 || !ok2 {
			t.Fatalf("lsn is not a number: r1=%v r2=%v", r1["lsn"], r2["lsn"])
		}
		if lsn2 <= lsn1 {
			t.Errorf("lsn should be monotonically increasing: lsn1=%v lsn2=%v", lsn1, lsn2)
		}
	})

	t.Run("returns 405 for GET", func(t *testing.T) {
		resp := doRaw(t, http.MethodGet, "/v1/ingest/events", "", nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("status: got %d, want 405", resp.StatusCode)
		}
	})

	t.Run("returns 400 for malformed JSON", func(t *testing.T) {
		resp := doRaw(t, http.MethodPost, "/v1/ingest/events", "application/json", []byte("{bad json"))
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status: got %d, want 400", resp.StatusCode)
		}
	})
}

func TestQuery(t *testing.T) {
	// Seed at least one event so the query can be exercised.
	doJSON(t, http.MethodPost, "/v1/ingest/events", sampleEvent(uniqID()))

	t.Run("valid query returns structured evidence fields", func(t *testing.T) {
		status, result := doJSON(t, http.MethodPost, "/v1/query", sampleQuery())
		if status != http.StatusOK {
			t.Fatalf("status: got %d, want 200", status)
		}
		assertKeys(t, result, "objects", "edges", "provenance", "versions", "applied_filters", "proof_trace")
	})

	t.Run("objects field is a list", func(t *testing.T) {
		_, result := doJSON(t, http.MethodPost, "/v1/query", sampleQuery())
		if _, ok := result["objects"].([]any); !ok {
			t.Errorf("objects should be an array, got %T", result["objects"])
		}
	})

	t.Run("provenance is non-empty after ingest", func(t *testing.T) {
		_, result := doJSON(t, http.MethodPost, "/v1/query", sampleQuery())
		provenance, ok := result["provenance"].([]any)
		if !ok || len(provenance) == 0 {
			t.Errorf("expected non-empty provenance, got: %v", result["provenance"])
		}
	})

	t.Run("proof_trace is non-empty", func(t *testing.T) {
		_, result := doJSON(t, http.MethodPost, "/v1/query", sampleQuery())
		trace, ok := result["proof_trace"].([]any)
		if !ok || len(trace) == 0 {
			t.Errorf("expected non-empty proof_trace, got: %v", result["proof_trace"])
		}
	})

	t.Run("applied_filters is non-empty", func(t *testing.T) {
		_, result := doJSON(t, http.MethodPost, "/v1/query", sampleQuery())
		filters, ok := result["applied_filters"].([]any)
		if !ok || len(filters) == 0 {
			t.Errorf("expected non-empty applied_filters, got: %v", result["applied_filters"])
		}
	})

	t.Run("top_k limits result count", func(t *testing.T) {
		q := sampleQuery()
		q["top_k"] = 1
		_, result := doJSON(t, http.MethodPost, "/v1/query", q)
		objects, _ := result["objects"].([]any)
		if len(objects) > 1 {
			t.Errorf("top_k=1 should return at most 1 object, got %d", len(objects))
		}
	})

	t.Run("returns 405 for GET", func(t *testing.T) {
		resp := doRaw(t, http.MethodGet, "/v1/query", "", nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("status: got %d, want 405", resp.StatusCode)
		}
	})

	t.Run("returns 400 for malformed JSON", func(t *testing.T) {
		resp := doRaw(t, http.MethodPost, "/v1/query", "application/json", []byte("{not json"))
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status: got %d, want 400", resp.StatusCode)
		}
	})
}

func TestIngestWithoutWorkspaceID_Queryable(t *testing.T) {
	now := nowISO()
	id := uniqID()
	ev := map[string]any{
		"event_id":    fmt.Sprintf("evt_nowspc_%s", id),
		"agent_id":    "agent_nowspc",
		"session_id":  fmt.Sprintf("sess_nowspc_%s", id),
		"event_type":  "user_message",
		"event_time":  now,
		"ingest_time": now,
		"payload":     map[string]any{"text": fmt.Sprintf("no workspace event %s", id)},
		"importance":  0.5,
		"visibility":  "private",
		"version":     1,
	}
	status, ack := doJSON(t, http.MethodPost, "/v1/ingest/events", ev)
	if status != http.StatusOK {
		t.Fatalf("ingest status: got %d, want 200", status)
	}
	assertKeys(t, ack, "status", "lsn", "event_id")

	q := map[string]any{
		"query_text":           fmt.Sprintf("no workspace event %s", id),
		"session_id":           ev["session_id"],
		"top_k":                5,
		"time_window":          map[string]any{"from": "2026-01-01T00:00:00Z", "to": "2027-01-01T00:00:00Z"},
		"object_types":         []string{"memory"},
		"relation_constraints": []string{},
		"response_mode":        "structured_evidence",
	}
	status, resp := doJSON(t, http.MethodPost, "/v1/query", q)
	if status != http.StatusOK {
		t.Fatalf("query status: got %d, want 200", status)
	}
	assertKeys(t, resp, "objects", "provenance", "proof_trace")
	t.Logf("no-workspace query: objects=%v proof_trace=%v", resp["objects"], resp["proof_trace"])
}

func TestIngestThenQuery_E2E(t *testing.T) {
	id := uniqID()
	ev := sampleEvent(id)
	_, ack := doJSON(t, http.MethodPost, "/v1/ingest/events", ev)
	assertKeys(t, ack, "status", "lsn", "event_id")

	_, resp := doJSON(t, http.MethodPost, "/v1/query", sampleQuery())
	assertKeys(t, resp, "objects", "provenance", "proof_trace")

	objs, _ := resp["objects"].([]any)
	t.Logf("E2E: ingest ack=%v | query objects=%d provenance=%v proof_trace=%v",
		ack, len(objs), resp["provenance"], resp["proof_trace"])
}
