package integration_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestProtocolHTTP(t *testing.T) {
	t.Run("healthz response has Content-Type application/json", func(t *testing.T) {
		resp := doRaw(t, http.MethodGet, "/healthz", "", nil)
		defer resp.Body.Close()
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "application/json") {
			t.Errorf("Content-Type: got %q, want application/json", ct)
		}
	})

	t.Run("query response has Content-Type application/json", func(t *testing.T) {
		resp := doRaw(t, http.MethodPost, "/v1/query",
			"application/json",
			[]byte(`{"query_text":"","relation_constraints":[],"response_mode":"structured_evidence"}`))
		defer resp.Body.Close()
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "application/json") {
			t.Errorf("Content-Type: got %q, want application/json", ct)
		}
	})

	t.Run("ingest response has Content-Type application/json", func(t *testing.T) {
		ev := sampleEvent(uniqID())
		body, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		resp := doRaw(t, http.MethodPost, "/v1/ingest/events", "application/json", body)
		defer resp.Body.Close()
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "application/json") {
			t.Errorf("Content-Type: got %q, want application/json", ct)
		}
	})

	t.Run("topology response has Content-Type application/json", func(t *testing.T) {
		resp := doRaw(t, http.MethodGet, "/v1/admin/topology", "", nil)
		defer resp.Body.Close()
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "application/json") {
			t.Errorf("Content-Type: got %q, want application/json", ct)
		}
	})
}
