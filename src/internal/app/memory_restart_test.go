//go:build retrieval

package app

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"plasmod/src/internal/schemas"
)

// Exercise the production bootstrap/HTTP path with fresh Badger handles and ANN index.
func TestMemoryBankHTTPRestart(t *testing.T) {
	for k, v := range map[string]string{"PLASMOD_STORAGE": "disk", "PLASMOD_DATA_DIR": t.TempDir(), "PLASMOD_LISTEN_MODE": "unified", "PLASMOD_GRPC_ENABLED": "0", "PLASMOD_ACTIVE_ALGORITHM": "", "PLASMOD_EMBEDDER": "tfidf", "PLASMOD_TIER_PROFILE": "no_cold", "PLASMOD_ADMIN_API_KEY": "test-key", "PLASMOD_EMBEDDING_REINDEX": "0", "APP_MODE": "prod"} {
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
	request := func(method, path string, body any, out any) {
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
		if res.Code < 200 || res.Code >= 300 {
			t.Fatalf("%s %s: %d %s", method, path, res.Code, res.Body.String())
		}
		if out != nil {
			if err := json.Unmarshal(res.Body.Bytes(), out); err != nil {
				t.Fatal(err)
			}
		}
	}
	start()
	ingest := func(id, text string) string {
		t.Helper()
		var ack map[string]any
		request("POST", "/v1/ingest/events", map[string]any{
			"schema_version": "plasmod.dynamic_event.v0.4", "identity": map[string]any{"event_id": id, "tenant_id": "tenant", "workspace_id": "workspace", "source": "test"},
			"actor": map[string]any{"agent_id": "agent", "session_id": "session"}, "event": map[string]any{"event_type": "user_message", "importance": 1, "confidence": 1},
			"access": map[string]any{"consistency": "strict", "visibility": "private"}, "payload": map[string]any{"text": text, "memory_type": "semantic"},
		}, &ack)
		return ack["memory_id"].(string)
	}
	active := ingest("evt_active", "I prefer concise replies")
	removed := ingest("evt_removed", "I prefer morning coffee")
	request("POST", "/v1/admin/memory/delete-by-source", map[string]any{"workspace_id": "workspace", "reference_id": "evt_removed"}, nil)
	request("POST", "/v1/admin/warm/prebuild", map[string]any{}, nil)
	query := func() {
		t.Helper()
		var got schemas.QueryResponse
		request("POST", "/v1/query", map[string]any{"query_text": "concise replies", "tenant_id": "tenant", "workspace_id": "workspace", "agent_id": "agent", "requester_agent_id": "agent", "query_scope": "workspace", "object_types": []string{"memory"}, "response_mode": "objects_only", "top_k": 10}, &got)
		if len(got.Objects) != 1 || got.Objects[0] != active {
			t.Fatalf("query=%+v", got)
		}
	}
	count := func() int {
		t.Helper()
		var rows []schemas.State
		request("GET", "/v1/states?agent_id=agent", nil, &rows)
		for _, row := range rows {
			if row.StateType == "memory_governance" {
				var st schemas.MemoryAlgorithmState
				if err := json.Unmarshal([]byte(row.StateValue), &st); err != nil {
					t.Fatal(err)
				}
				if st.MemoryID == active {
					return st.RecallCount
				}
			}
		}
		t.Fatal("missing governance state")
		return -1
	}
	query()
	if got := count(); got != 1 {
		t.Fatalf("count=%d", got)
	}
	stop()
	start()
	if got := count(); got != 1 {
		t.Fatalf("restart count=%d", got)
	}
	query()
	if got := count(); got != 2 {
		t.Fatalf("post-restart count=%d", got)
	}
	var memories []schemas.Memory
	request("GET", "/v1/memory?workspace_id=workspace&agent_id=agent", nil, &memories)
	for _, m := range memories {
		if m.MemoryID == removed && m.IsActive {
			t.Fatal("forgotten memory resurrected")
		}
	}
}
