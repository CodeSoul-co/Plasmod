//go:build retrieval

package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"plasmod/src/internal/schemas"
	"plasmod/src/internal/worker"
)

func TestMemoryExtractionFailureHTTPAndWALRestart(t *testing.T) {
	var fixed atomic.Bool
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		quote := "wrong source"
		if fixed.Load() {
			quote = "我喜欢茶。"
		}
		data, _ := json.Marshal(map[string]any{"facts": []map[string]any{{"text": "User likes tea.", "kind": "preference_constraint", "quote": quote, "start": 0, "end": 5, "confidence": .95}}})
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]any{"content": string(data)}}}})
	}))
	defer provider.Close()
	for k, v := range map[string]string{"PLASMOD_STORAGE": "disk", "PLASMOD_DATA_DIR": t.TempDir(), "PLASMOD_LISTEN_MODE": "unified", "PLASMOD_GRPC_ENABLED": "0", "PLASMOD_ACTIVE_ALGORITHM": "memorybank", "PLASMOD_EMBEDDER": "tfidf", "PLASMOD_TIER_PROFILE": "no_cold", "PLASMOD_ADMIN_API_KEY": "test-key", "PLASMOD_EMBEDDING_REINDEX": "0", "PLASMOD_MEMORY_LLM_BASE_URL": provider.URL, "PLASMOD_MEMORY_LLM_MODEL": "controlled", "PLASMOD_MEMORY_LLM_API_KEY": "", "PLASMOD_CONSISTENCY_MAX_RETRIES": "1", "PLASMOD_CONSISTENCY_QUERY_TIMEOUT": "2s", "APP_MODE": "prod"} {
		t.Setenv(k, v)
	}
	var bundle *ServerBundle
	start := func() {
		t.Helper()
		var err error
		bundle, err = BuildServer()
		if err != nil {
			t.Fatal(err)
		}
	}
	stop := func() {
		t.Helper()
		if err := bundle.Shutdown(); err != nil {
			t.Fatal(err)
		}
		bundle = nil
	}
	t.Cleanup(func() {
		if bundle != nil {
			stop()
		}
	})
	request := func(method, path string, body any, code int, out any) {
		t.Helper()
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Admin-Key", "test-key")
		res := httptest.NewRecorder()
		bundle.HTTPServers[0].Handler.ServeHTTP(res, req)
		if res.Code != code {
			t.Fatalf("%s %s: %d %s", method, path, res.Code, res.Body.String())
		}
		if out != nil {
			if err := json.Unmarshal(res.Body.Bytes(), out); err != nil {
				t.Fatal(err)
			}
		}
	}
	event := func(id, tenant, kind, text string) map[string]any {
		return map[string]any{"identity": map[string]any{"event_id": id, "tenant_id": tenant, "workspace_id": "w"}, "actor": map[string]any{"agent_id": "agent-" + tenant, "session_id": "session-" + tenant}, "event": map[string]any{"event_type": kind, "importance": 1, "confidence": 1}, "access": map[string]any{"consistency": "strict", "visibility": "private"}, "payload": map[string]any{"text": text}}
	}
	start()
	request("POST", "/v1/ingest/events", event("bad-a", "A", "user_message", "我喜欢茶。"), 503, nil)
	request("POST", "/v1/query", map[string]any{"tenant_id": "A", "workspace_id": "w", "agent_id": "agent-A", "query_text": "tea"}, 503, nil)
	var status worker.ExtractionStatus
	statusPath := "/v1/internal/memory/extractions/bad-a?tenant_id=A&workspace_id=w"
	request("GET", statusPath, nil, 200, &status)
	if status.ErrorCode != "source_quote_mismatch" || status.Status != "failed" || status.LSN == 0 {
		t.Fatalf("bad failure receipt: %+v", status)
	}
	request("GET", "/v1/internal/memory/extractions/bad-a?tenant_id=B&workspace_id=w", nil, 404, nil)
	var b map[string]any
	request("POST", "/v1/ingest/events", event("good-b", "B", "memory_write_requested", "B remembers tea"), 200, &b)
	queryB := func() {
		t.Helper()
		var got schemas.QueryResponse
		request("POST", "/v1/query", map[string]any{"tenant_id": "B", "workspace_id": "w", "agent_id": "agent-B", "query_text": "tea", "response_mode": "objects_only", "target_object_ids": []any{b["memory_id"]}}, 200, &got)
		if len(got.Objects) != 1 || got.Objects[0] != b["memory_id"] {
			t.Fatalf("B memory hidden: %+v", got)
		}
	}
	queryB()
	stop()
	start()
	request("POST", "/v1/query", map[string]any{"tenant_id": "A", "workspace_id": "w", "agent_id": "agent-A", "query_text": "tea"}, 503, nil)
	var restored worker.ExtractionStatus
	request("GET", statusPath, nil, 200, &restored)
	if restored.LSN != status.LSN || restored.Attempts <= status.Attempts || restored.Status != "failed" {
		t.Fatalf("failure lost on restart: %+v", restored)
	}
	queryB()
	fixed.Store(true)
	replay := map[string]any{"from_lsn": status.LSN, "limit": 1, "apply": true, "confirm": "apply_replay"}
	request("POST", "/v1/admin/replay", replay, 200, nil)
	before := calls.Load()
	request("POST", "/v1/admin/replay", replay, 200, nil)
	if calls.Load() != before {
		t.Fatal("successful extraction recomputed on replay")
	}
	restored = worker.ExtractionStatus{}
	request("GET", statusPath, nil, 200, &restored)
	if restored.Status != "completed" || restored.ErrorCode != "" || restored.FactCount != 1 {
		t.Fatalf("incomplete recovery: %+v", restored)
	}
	var memories []schemas.Memory
	request("GET", "/v1/memory?workspace_id=w&agent_id=agent-A", nil, 200, &memories)
	facts := 0
	for _, m := range memories {
		if len(m.SourceSpans) > 0 {
			facts++
			if len(m.SourceSpans) != 1 || m.SourceSpans[0].EventID != "bad-a" || m.SourceSpans[0].Quote != "我喜欢茶。" {
				t.Fatalf("lost provenance: %+v", m)
			}
		}
	}
	if facts != 1 {
		t.Fatalf("facts after recovery=%d", facts)
	}
}
