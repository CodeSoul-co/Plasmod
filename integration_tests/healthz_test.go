package integration_test

import (
	"net/http"
	"testing"
)

func TestHealthz(t *testing.T) {
	t.Run("returns 200 with status ok", func(t *testing.T) {
		status, result := doJSON(t, http.MethodGet, "/healthz", nil)
		if status != http.StatusOK {
			t.Fatalf("status: got %d, want 200", status)
		}
		assertKeys(t, result, "status")
		if result["status"] != "ok" {
			t.Errorf("status value: got %v, want ok", result["status"])
		}
	})

	t.Run("response Content-Type is application/json", func(t *testing.T) {
		resp := doRaw(t, http.MethodGet, "/healthz", "", nil)
		defer resp.Body.Close()
		ct := resp.Header.Get("Content-Type")
		if ct == "" {
			t.Error("Content-Type header is missing")
		}
	})
}
