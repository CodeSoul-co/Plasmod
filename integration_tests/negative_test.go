package integration_test

import (
	"net/http"
	"testing"
)

func TestNegativeCases(t *testing.T) {
	t.Run("GET /v1/query returns 405", func(t *testing.T) {
		resp := doRaw(t, http.MethodGet, "/v1/query", "", nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("status: got %d, want 405", resp.StatusCode)
		}
	})

	t.Run("POST /v1/query with malformed JSON returns 400", func(t *testing.T) {
		resp := doRaw(t, http.MethodPost, "/v1/query", "application/json", []byte("{bad"))
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status: got %d, want 400", resp.StatusCode)
		}
	})

	t.Run("GET /v1/ingest/events returns 405", func(t *testing.T) {
		resp := doRaw(t, http.MethodGet, "/v1/ingest/events", "", nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("status: got %d, want 405", resp.StatusCode)
		}
	})

	t.Run("POST /v1/ingest/events with malformed JSON returns 400", func(t *testing.T) {
		resp := doRaw(t, http.MethodPost, "/v1/ingest/events", "application/json", []byte("{bad"))
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status: got %d, want 400", resp.StatusCode)
		}
	})

	t.Run("DELETE /v1/agents returns 405", func(t *testing.T) {
		resp := doRaw(t, http.MethodDelete, "/v1/agents", "", nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("status: got %d, want 405", resp.StatusCode)
		}
	})

	t.Run("DELETE /v1/sessions returns 405", func(t *testing.T) {
		resp := doRaw(t, http.MethodDelete, "/v1/sessions", "", nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("status: got %d, want 405", resp.StatusCode)
		}
	})

	t.Run("unknown route returns 404", func(t *testing.T) {
		resp := doRaw(t, http.MethodGet, "/v1/does-not-exist", "", nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status: got %d, want 404", resp.StatusCode)
		}
	})

	t.Run("POST /v1/agents with malformed JSON returns 400", func(t *testing.T) {
		resp := doRaw(t, http.MethodPost, "/v1/agents", "application/json", []byte("{not json"))
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status: got %d, want 400", resp.StatusCode)
		}
	})

	t.Run("POST /v1/memory with malformed JSON returns 400", func(t *testing.T) {
		resp := doRaw(t, http.MethodPost, "/v1/memory", "application/json", []byte("{not json"))
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status: got %d, want 400", resp.StatusCode)
		}
	})
}
