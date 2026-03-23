package dataplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPRetrievalAdapter_Search(t *testing.T) {
	// Mock Python retrieval service
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/retrieve" && r.Method == "POST" {
			resp := map[string]interface{}{
				"candidates": []map[string]interface{}{
					{"object_id": "mem_001", "final_score": 0.95, "is_seed": true},
					{"object_id": "mem_002", "final_score": 0.85, "is_seed": false},
				},
				"total_found": 2,
				"dense_hits":  2,
				"sparse_hits": 0,
				"latency_ms":  1.5,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	adapter := NewHTTPRetrievalAdapter(server.URL)

	result := adapter.Search(SearchInput{
		QueryText: "test query",
		TopK:      10,
	})

	if len(result.ObjectIDs) != 2 {
		t.Errorf("expected 2 object IDs, got %d", len(result.ObjectIDs))
	}
	if result.ObjectIDs[0] != "mem_001" {
		t.Errorf("expected first object ID to be mem_001, got %s", result.ObjectIDs[0])
	}
	if result.Tier == "" || len(result.Tier) >= 5 && result.Tier[:5] == "error" {
		t.Errorf("unexpected tier: %s", result.Tier)
	}
}

func TestHTTPRetrievalAdapter_Ingest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ingest" && r.Method == "POST" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"status":"ok","object_id":"mem_test"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	adapter := NewHTTPRetrievalAdapter(server.URL)

	err := adapter.Ingest(IngestRecord{
		ObjectID:    "mem_test",
		Text:        "test document",
		Namespace:   "default",
		EventUnixTS: 1234567890,
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestHTTPRetrievalAdapter_Healthz(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" && r.Method == "GET" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"status":"ok","ready":true}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	adapter := NewHTTPRetrievalAdapter(server.URL)

	err := adapter.Healthz()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestHTTPRetrievalAdapter_SearchError(t *testing.T) {
	// Test with unreachable server
	adapter := NewHTTPRetrievalAdapter("http://127.0.0.1:59999")

	result := adapter.Search(SearchInput{
		QueryText: "test",
		TopK:      10,
	})

	if result.Tier != "error:http" {
		t.Errorf("expected error:http tier, got %s", result.Tier)
	}
}
