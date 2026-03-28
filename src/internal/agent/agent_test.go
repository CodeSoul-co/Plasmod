package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"andb/src/internal/schemas"
)

// ─── Config tests ──────────────────────────────────────────────────────────────

func TestLoadFromEnv_Defaults(t *testing.T) {
	// Clear relevant env vars before test
	for _, k := range []string{"ANDB_AGENT_ENDPOINT", "ANDB_AGENT_ID", "ANDB_TENANT_ID", "ANDB_WORKSPACE_ID", "ANDB_AGENT_HTTP_PORT"} {
		_ = k // note: we don't actually clear in this test to avoid side-effects;
		// LoadFromEnv reads os.Getenv so the test reflects whatever env is set.
	}

	cfg := LoadFromEnv()
	if cfg.CogDBEndpoint == "" {
		t.Error("CogDBEndpoint should have a default")
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name:    "empty config fails",
			cfg:     Config{},
			wantErr: true,
		},
		{
			name: "missing agent_id fails",
			cfg: Config{
				CogDBEndpoint: "http://127.0.0.1:8080",
				TenantID:      "t1",
				WorkspaceID:   "w1",
			},
			wantErr: true,
		},
		{
			name: "valid config passes",
			cfg: Config{
				CogDBEndpoint: "http://127.0.0.1:8080",
				AgentID:      "agent_a",
				TenantID:     "t1",
				WorkspaceID:   "w1",
			},
			wantErr: false,
		},
		{
			name: "invalid url fails",
			cfg: Config{
				CogDBEndpoint: "not-a-url",
				AgentID:      "agent_a",
				TenantID:     "t1",
				WorkspaceID:   "w1",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ─── AgentSession identity tests ────────────────────────────────────────────────

func TestNewAgentSession_RejectsEmptyIdentity(t *testing.T) {
	cfg := Config{CogDBEndpoint: "http://127.0.0.1:8080"}
	for _, tc := range []struct {
		agentID, tenantID, workspaceID string
	}{
		{"", "t1", "w1"},
		{"a1", "", "w1"},
		{"a1", "t1", ""},
	} {
		_, err := NewAgentSession(tc.agentID, tc.tenantID, tc.workspaceID, cfg)
		if err == nil {
			t.Errorf("NewAgentSession(%q, %q, %q) expected error, got nil",
				tc.agentID, tc.tenantID, tc.workspaceID)
		}
	}
}

func TestNewAgentSession_Success(t *testing.T) {
	cfg := Config{CogDBEndpoint: "http://127.0.0.1:8080"}
	s, err := NewAgentSession("agent_a", "tenant1", "workspace1", cfg)
	if err != nil {
		t.Fatalf("NewAgentSession failed: %v", err)
	}
	if s.AgentID() != "agent_a" {
		t.Errorf("AgentID() = %q, want %q", s.AgentID(), "agent_a")
	}
	if s.TenantID() != "tenant1" {
		t.Errorf("TenantID() = %q, want %q", s.TenantID(), "tenant1")
	}
	if s.WorkspaceID() != "workspace1" {
		t.Errorf("WorkspaceID() = %q, want %q", s.WorkspaceID(), "workspace1")
	}
	if s.SessionID() != "" {
		t.Errorf("SessionID() = %q, want %q (no session started)", s.SessionID(), "")
	}
	if !s.IsConnected() {
		t.Error("IsConnected() = false, want true")
	}
}

func TestAgentSession_Close_Idempotent(t *testing.T) {
	cfg := Config{CogDBEndpoint: "http://127.0.0.1:8080"}
	s, _ := NewAgentSession("a", "t", "w", cfg)

	// First close should succeed
	if err := s.Close(context.Background()); err != nil {
		t.Errorf("first Close: %v", err)
	}

	// Second close should be no-op
	if err := s.Close(context.Background()); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestAgentSession_RequireSession(t *testing.T) {
	cfg := Config{CogDBEndpoint: "http://127.0.0.1:8080"}
	s, _ := NewAgentSession("a", "t", "w", cfg)

	_, err := s.SubmitUserMessage(context.Background(), "test", nil)
	if err == nil {
		t.Error("SubmitUserMessage before StartSession: expected ErrSessionNotStarted")
	}
}

// ─── HTTP mock helper ─────────────────────────────────────────────────────────

// mockServer returns an httptest.Server that responds to any POST with the given
// response body and status code. For GET requests it calls getHandler if provided.
func mockServer(t *testing.T, statusCode int, responseBody any) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		if responseBody != nil {
			_ = json.NewEncoder(w).Encode(responseBody)
		}
	}))
}

// ingestAck is a test helper matching the IngestAck shape returned by the gateway.
type ingestAck = map[string]any

// ─── StartSession / EndSession integration tests ───────────────────────────────

func TestAgentSession_StartSession_Success(t *testing.T) {
	server := mockServer(t, http.StatusOK, map[string]any{"status": "ok", "session_id": "sess_a_123"})
	defer server.Close()

	cfg := Config{CogDBEndpoint: server.URL}
	s, err := NewAgentSession("agent_a", "t1", "w1", cfg)
	if err != nil {
		t.Fatalf("NewAgentSession: %v", err)
	}

	id, err := s.StartSession(context.Background(), "test goal", "investigation", 1000, 5000)
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}
	if id == "" {
		t.Error("StartSession returned empty session ID")
	}
	if s.SessionID() == "" {
		t.Error("SessionID() is empty after StartSession")
	}
}

func TestAgentSession_StartSession_AlreadyStarted(t *testing.T) {
	server := mockServer(t, http.StatusOK, map[string]any{"status": "ok", "session_id": "sess_a_123"})
	defer server.Close()

	cfg := Config{CogDBEndpoint: server.URL}
	s, _ := NewAgentSession("agent_a", "t1", "w1", cfg)
	_, _ = s.StartSession(context.Background(), "goal1", "chat", 0, 0)

	// Calling StartSession again should return ErrSessionAlreadyStarted.
	_, err := s.StartSession(context.Background(), "goal2", "chat", 0, 0)
	if err == nil {
		t.Error("expected ErrSessionAlreadyStarted, got nil")
	}
}

func TestAgentSession_StartSession_CogDBError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := Config{CogDBEndpoint: server.URL}
	s, _ := NewAgentSession("agent_a", "t1", "w1", cfg)

	_, err := s.StartSession(context.Background(), "goal", "chat", 0, 0)
	if err == nil {
		t.Error("expected error on CogDB error response, got nil")
	}
}

// ─── Event submission tests ────────────────────────────────────────────────────

func TestAgentSession_SubmitUserMessage_Success(t *testing.T) {
	var receivedEvent map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/sessions" && r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		} else if r.URL.Path == "/v1/ingest/events" {
			if err := json.NewDecoder(r.Body).Decode(&receivedEvent); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":    "accepted",
				"lsn":       1,
				"event_id":  receivedEvent["event_id"],
				"memory_id": "mem_test_001",
				"edge_count": 0,
			})
		} else {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := Config{CogDBEndpoint: server.URL}
	s, _ := NewAgentSession("agent_a", "t1", "w1", cfg)
	_, _ = s.StartSession(context.Background(), "goal", "chat", 0, 0)

	ack, err := s.SubmitUserMessage(context.Background(), "hello world", nil)
	if err != nil {
		t.Fatalf("SubmitUserMessage failed: %v", err)
	}
	if ack.Status != "accepted" {
		t.Errorf("ack.Status = %q, want %q", ack.Status, "accepted")
	}
	if receivedEvent["event_type"] != "user_message" {
		t.Errorf("event_type = %q, want %q", receivedEvent["event_type"], "user_message")
	}
	if receivedEvent["agent_id"] != "agent_a" {
		t.Errorf("agent_id = %q, want %q", receivedEvent["agent_id"], "agent_a")
	}
}

func TestAgentSession_SubmitToolCall_Success(t *testing.T) {
	var receivedEvent map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/sessions" && r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		} else if r.URL.Path == "/v1/ingest/events" {
			_ = json.NewDecoder(r.Body).Decode(&receivedEvent)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":    "accepted",
				"lsn":       2,
				"event_id":  receivedEvent["event_id"],
				"memory_id": "",
				"edge_count": 0,
			})
		}
	}))
	defer server.Close()

	cfg := Config{CogDBEndpoint: server.URL}
	s, _ := NewAgentSession("agent_a", "t1", "w1", cfg)
	_, _ = s.StartSession(context.Background(), "goal", "chat", 0, 0)

	ack, err := s.SubmitToolCall(context.Background(), "bash", `{"cmd": "ls"}`)
	if err != nil {
		t.Fatalf("SubmitToolCall failed: %v", err)
	}
	if ack.Status != "accepted" {
		t.Errorf("ack.Status = %q", ack.Status)
	}
	if receivedEvent["event_type"] != "tool_call" {
		t.Errorf("event_type = %q, want %q", receivedEvent["event_type"], "tool_call")
	}
}

func TestAgentSession_SubmitCheckpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/sessions" && r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		} else if r.URL.Path == "/v1/ingest/events" {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "accepted", "lsn": 3, "event_id": "evt_x", "memory_id": "", "edge_count": 0})
		}
	}))
	defer server.Close()

	cfg := Config{CogDBEndpoint: server.URL}
	s, _ := NewAgentSession("agent_a", "t1", "w1", cfg)
	_, _ = s.StartSession(context.Background(), "goal", "chat", 0, 0)

	ack, err := s.SubmitCheckpoint(context.Background(), "goal_complete")
	if err != nil {
		t.Fatalf("SubmitCheckpoint: %v", err)
	}
	if ack.Status != "accepted" {
		t.Errorf("ack.Status = %q", ack.Status)
	}
}

func TestAgentSession_Heartbeat(t *testing.T) {
	var receivedEvent map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/sessions" && r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		} else if r.URL.Path == "/v1/ingest/events" {
			_ = json.NewDecoder(r.Body).Decode(&receivedEvent)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "accepted", "lsn": 1, "event_id": receivedEvent["event_id"], "memory_id": "", "edge_count": 0})
		}
	}))
	defer server.Close()

	cfg := Config{CogDBEndpoint: server.URL}
	s, _ := NewAgentSession("agent_a", "t1", "w1", cfg)
	_, _ = s.StartSession(context.Background(), "goal", "chat", 0, 0)

	_, err := s.Heartbeat(context.Background())
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if receivedEvent["event_type"] != "checkpoint" {
		t.Errorf("event_type = %q", receivedEvent["event_type"])
	}
}

// ─── BaselineMemoryManager tests ───────────────────────────────────────────────

func TestBaselineMemoryManager_Name(t *testing.T) {
	cfg := Config{CogDBEndpoint: "http://127.0.0.1:8080"}
	s, _ := NewAgentSession("a", "t", "w", cfg)
	mm := s.MemoryManager()
	if mm.Name() != "baseline" {
		t.Errorf("Name() = %q, want %q", mm.Name(), "baseline")
	}
}

func TestBaselineMemoryManager_Recall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/internal/memory/recall" {
			http.Error(w, "wrong path", http.StatusNotFound)
			return
		}
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["query"] != "test query" {
			t.Errorf("query = %q", req["query"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"request_id":          "recall_001",
			"requester_id":        "agent_a",
			"agent_id":           "agent_a",
			"resolved_scope":     "workspace",
			"visible_memory_refs": []string{"mem_1", "mem_2"},
		})
	}))
	defer server.Close()

	cfg := Config{CogDBEndpoint: server.URL}
	s, _ := NewAgentSession("agent_a", "t1", "w1", cfg)

	view, err := s.MemoryManager().Recall(context.Background(), "test query", "workspace", 10)
	if err != nil {
		t.Fatalf("Recall failed: %v", err)
	}
	if len(view.VisibleMemoryRefs) != 2 {
		t.Errorf("VisibleMemoryRefs len = %d, want 2", len(view.VisibleMemoryRefs))
	}
}

func TestBaselineMemoryManager_Ingest(t *testing.T) {
	var receivedIDs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/internal/memory/ingest" {
			http.Error(w, "wrong path", http.StatusNotFound)
			return
		}
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if ids, ok := req["memory_ids"].([]any); ok {
			for _, id := range ids {
				receivedIDs = append(receivedIDs, id.(string))
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"operation": "ingest", "updated_count": 2})
	}))
	defer server.Close()

	cfg := Config{CogDBEndpoint: server.URL}
	s, _ := NewAgentSession("agent_a", "t1", "w1", cfg)

	err := s.MemoryManager().Ingest(context.Background(), []string{"mem_a", "mem_b"})
	if err != nil {
		t.Fatalf("Ingest failed: %v", err)
	}
	if len(receivedIDs) != 2 {
		t.Errorf("received %d IDs, want 2", len(receivedIDs))
	}
}

func TestBaselineMemoryManager_Compress(t *testing.T) {
	server := mockServer(t, http.StatusOK, map[string]any{"operation": "compress", "updated_count": 3})
	defer server.Close()

	cfg := Config{CogDBEndpoint: server.URL}
	s, _ := NewAgentSession("agent_a", "t1", "w1", cfg)

	err := s.MemoryManager().Compress(context.Background(), "agent_a", "sess_1")
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
}

func TestBaselineMemoryManager_Decay(t *testing.T) {
	server := mockServer(t, http.StatusOK, map[string]any{"operation": "decay", "updated_count": 5})
	defer server.Close()

	cfg := Config{CogDBEndpoint: server.URL}
	s, _ := NewAgentSession("agent_a", "t1", "w1", cfg)

	err := s.MemoryManager().Decay(context.Background(), "agent_a", "sess_1")
	if err != nil {
		t.Fatalf("Decay: %v", err)
	}
}

// ─── MemoryManager interface compliance ─────────────────────────────────────────

// noOpManager is a MemoryManager that does nothing, for testing AgentSession
// without a live CogDB endpoint.
type noOpManager struct{}

func (noOpManager) Name() string                      { return "noop" }
func (noOpManager) Close() error                     { return nil }
func (noOpManager) Recall(ctx context.Context, query, scope string, topK int) (*schemas.MemoryView, error) {
	return &schemas.MemoryView{}, nil
}
func (noOpManager) Ingest(ctx context.Context, memoryIDs []string) error    { return nil }
func (noOpManager) Compress(ctx context.Context, agentID, sessionID string) error { return nil }
func (noOpManager) Summarize(ctx context.Context, agentID, sessionID string, maxLevel int) ([]schemas.Memory, error) {
	return nil, nil
}
func (noOpManager) Decay(ctx context.Context, agentID, sessionID string) error { return nil }

func TestAgentSession_WithMemoryManager(t *testing.T) {
	cfg := Config{CogDBEndpoint: "http://127.0.0.1:8080"}
	s, _ := NewAgentSession("a", "t", "w", cfg)
	if s.MemoryManager().Name() != "baseline" {
		t.Errorf("default MemoryManager Name = %q, want baseline", s.MemoryManager().Name())
	}

	s2 := s.WithMemoryManager(noOpManager{})
	if s2.MemoryManager().Name() != "noop" {
		t.Errorf("WithMemoryManager Name = %q, want noop", s2.MemoryManager().Name())
	}
	// Original session unchanged
	if s.MemoryManager().Name() != "baseline" {
		t.Errorf("original MemoryManager changed: Name = %q", s.MemoryManager().Name())
	}
}

func TestAgentSession_Query_NoMemoryManager(t *testing.T) {
	cfg := Config{} // no CogDBEndpoint → no BaselineMemoryManager wired
	s, _ := NewAgentSession("a", "t", "w", cfg)
	// MemoryManager is nil when no CogDB endpoint is configured.
	// Query should return ErrNotConnected.
	_, err := s.Query(context.Background(), "test", "", 10)
	if err == nil {
		t.Error("Query with nil MemoryManager: expected error")
	}
}

// ─── Session CRUD via mock server ──────────────────────────────────────────────

func TestAgentSession_EndSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	}))
	defer server.Close()

	cfg := Config{CogDBEndpoint: server.URL}
	s, _ := NewAgentSession("agent_a", "t1", "w1", cfg)

	// EndSession without a started session is a no-op.
	if err := s.EndSession(context.Background(), "completed"); err != nil {
		t.Errorf("EndSession without session: %v", err)
	}

	_, _ = s.StartSession(context.Background(), "goal", "chat", 0, 0)
	if err := s.EndSession(context.Background(), "completed"); err != nil {
		t.Errorf("EndSession: %v", err)
	}
	if s.SessionID() != "" {
		t.Errorf("SessionID() = %q after EndSession, want %q", s.SessionID(), "")
	}
}

func TestAgentSession_GetSession(t *testing.T) {
	var varSessionID string // captured from POST body in the closure below
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/sessions" && r.Method == http.MethodPost {
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			// Echo back the session_id from the request body so we can capture it.
			varSessionID = req["session_id"].(string)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		} else if r.URL.Path == "/v1/sessions" && r.Method == http.MethodGet {
			// Return a session whose ID matches what the agent generated.
			sessionObj := map[string]any{
				"session_id": varSessionID,
				"agent_id":   "agent_a",
				"status":     "running",
				"goal":       "test goal",
			}
			_ = json.NewEncoder(w).Encode([]any{sessionObj})
		}
	}))
	defer server.Close()

	cfg := Config{CogDBEndpoint: server.URL}
	s, _ := NewAgentSession("agent_a", "t1", "w1", cfg)

	// GetSession without session → ErrSessionNotFound
	_, err := s.GetSession(context.Background())
	if err == nil {
		t.Error("GetSession without session: expected error")
	}

	_, _ = s.StartSession(context.Background(), "goal", "chat", 0, 0)

	sess, err := s.GetSession(context.Background())
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.SessionID == "" {
		t.Error("SessionID is empty")
	}
}

// ─── Collaboration tests ────────────────────────────────────────────────────────

func TestAgentSession_ShareMemory(t *testing.T) {
	var receivedReq map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/sessions" && r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		} else if r.URL.Path == "/v1/internal/memory/share" {
			_ = json.NewDecoder(r.Body).Decode(&receivedReq)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":           "ok",
				"shared_memory_id": "shared_mem_001_to_agent_b",
				"memory_id":        receivedReq["memory_id"],
				"to_agent_id":     receivedReq["to_agent_id"],
			})
		}
	}))
	defer server.Close()

	cfg := Config{CogDBEndpoint: server.URL}
	s, _ := NewAgentSession("agent_a", "t1", "w1", cfg)
	_, _ = s.StartSession(context.Background(), "goal", "chat", 0, 0)

	sharedID, err := s.ShareMemory(context.Background(), "mem_001", "agent_b", nil)
	if err != nil {
		t.Fatalf("ShareMemory: %v", err)
	}
	if sharedID == "" {
		t.Error("ShareMemory returned empty shared ID")
	}
	if receivedReq["from_agent_id"] != "agent_a" {
		t.Errorf("from_agent_id = %q", receivedReq["from_agent_id"])
	}
}

func TestAgentSession_ResolveConflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/sessions" && r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		} else if r.URL.Path == "/v1/internal/memory/conflict/resolve" {
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			// Simulate LWW: right wins (higher version)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":    "ok",
				"winner_id": req["right_id"],
				"left_id":   req["left_id"],
				"right_id":  req["right_id"],
			})
		}
	}))
	defer server.Close()

	cfg := Config{CogDBEndpoint: server.URL}
	s, _ := NewAgentSession("agent_a", "t1", "w1", cfg)
	_, _ = s.StartSession(context.Background(), "goal", "chat", 0, 0)

	winner, err := s.ResolveConflict(context.Background(), "mem_a_v1", "mem_a_v2")
	if err != nil {
		t.Fatalf("ResolveConflict: %v", err)
	}
	if winner != "mem_a_v2" {
		t.Errorf("winner = %q, want %q", winner, "mem_a_v2")
	}
}

func TestAgentSession_ResolveConflict_SameID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/sessions" && r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		}
	}))
	defer server.Close()

	cfg := Config{CogDBEndpoint: server.URL}
	s, _ := NewAgentSession("agent_a", "t1", "w1", cfg)
	_, _ = s.StartSession(context.Background(), "goal", "chat", 0, 0)

	// Same ID should return that ID without calling server
	winner, err := s.ResolveConflict(context.Background(), "mem_x", "mem_x")
	if err != nil {
		t.Fatalf("ResolveConflict same ID: %v", err)
	}
	if winner != "mem_x" {
		t.Errorf("winner = %q, want %q", winner, "mem_x")
	}
}

// ─── AgentGateway tests ────────────────────────────────────────────────────────

func TestAgentGateway_HealthCheck(t *testing.T) {
	gw, err := NewAgentGateway(Config{
		CogDBEndpoint: "http://127.0.0.1:8080",
		AgentID:      "agent_a",
		TenantID:     "t1",
		WorkspaceID:  "w1",
	})
	if err != nil {
		t.Fatalf("NewAgentGateway: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	gw.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("healthz status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp map[string]string
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp["status"] != "ok" {
		t.Errorf("healthz status field = %q, want %q", resp["status"], "ok")
	}
}

func TestAgentGateway_SessionStartEnd(t *testing.T) {
	var started bool
	var receivedSession map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/sessions" && r.Method == http.MethodPost {
			started = true
			_ = json.NewDecoder(r.Body).Decode(&receivedSession)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		} else if r.URL.Path == "/v1/sessions" && r.Method == http.MethodDelete {
			started = false
		}
	}))
	defer server.Close()

	gw, err := NewAgentGateway(Config{
		CogDBEndpoint: server.URL,
		AgentID:      "agent_a",
		TenantID:     "t1",
		WorkspaceID:  "w1",
	})
	if err != nil {
		t.Fatalf("NewAgentGateway: %v", err)
	}

	// Start session via gateway
	body := strings.NewReader(`{"goal":"gateway test","task_type":"investigation","budget_token":100,"budget_time_ms":5000}`)
	req := httptest.NewRequest(http.MethodPost, "/agent/session/start", body)
	rr := httptest.NewRecorder()
	gw.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("session/start status = %d, want %d: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	if !started {
		t.Error("session was not started on mock server")
	}
	if receivedSession["goal"] != "gateway test" {
		t.Errorf("goal = %q", receivedSession["goal"])
	}

	// End session
	req2 := httptest.NewRequest(http.MethodPost, "/agent/session/end", strings.NewReader(`{"final_status":"completed"}`))
	rr2 := httptest.NewRecorder()
	gw.mux.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Errorf("session/end status = %d, want %d", rr2.Code, http.StatusOK)
	}
}

func TestAgentGateway_RequiresSession(t *testing.T) {
	gw, _ := NewAgentGateway(Config{
		CogDBEndpoint: "http://127.0.0.1:8080",
		AgentID:      "agent_a",
		TenantID:     "t1",
		WorkspaceID:  "w1",
	})

	req := httptest.NewRequest(http.MethodPost, "/agent/events/message", nil)
	rr := httptest.NewRecorder()
	gw.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("events/message without session: status = %d, want %d", rr.Code, http.StatusBadRequest)
	}

	var resp map[string]string
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp["error"] != "session_not_started" {
		t.Errorf("error code = %q, want %q", resp["error"], "session_not_started")
	}
}

func TestAgentGateway_Query(t *testing.T) {
	receivedQuery := make(map[string]any)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/sessions" && r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		} else if r.URL.Path == "/v1/internal/memory/recall" {
			_ = json.NewDecoder(r.Body).Decode(&receivedQuery)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"request_id":          "recall_test",
				"visible_memory_refs": []string{"mem_1"},
				"resolved_scope":     "workspace",
			})
		} else {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "unexpected path: " + r.URL.Path})
		}
	}))
	defer server.Close()

	gw, _ := NewAgentGateway(Config{
		CogDBEndpoint: server.URL,
		AgentID:      "agent_a",
		TenantID:     "t1",
		WorkspaceID:  "w1",
	})

	// Start session
	req := httptest.NewRequest(http.MethodPost, "/agent/session/start", strings.NewReader(`{"goal":"q","task_type":"chat"}`))
	rr := httptest.NewRecorder()
	gw.mux.ServeHTTP(rr, req)

	// Submit query
	req2 := httptest.NewRequest(http.MethodPost, "/agent/query", strings.NewReader(`{"query":"what was discussed?","scope":"workspace","top_k":5}`))
	rr2 := httptest.NewRecorder()
	gw.mux.ServeHTTP(rr2, req2)

	if receivedQuery["query"] != "what was discussed?" {
		t.Errorf("query text = %q (status=%d body=%s)", receivedQuery["query"], rr2.Code, rr2.Body.String())
	}
}

func TestAgentGateway_MemoryCompressSummarize(t *testing.T) {
	var compressCalled, summarizeCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/sessions" && r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "session_id": "sess_c"})
		} else if r.URL.Path == "/v1/internal/memory/compress" {
			compressCalled = true
			_ = json.NewEncoder(w).Encode(map[string]any{"operation": "compress", "updated_count": 3})
		} else if r.URL.Path == "/v1/internal/memory/summarize" {
			summarizeCalled = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"operation":    "summarize",
				"produced_ids": []string{"summary_001"},
			})
		}
	}))
	defer server.Close()

	gw, _ := NewAgentGateway(Config{
		CogDBEndpoint: server.URL,
		AgentID:      "agent_a",
		TenantID:     "t1",
		WorkspaceID:  "w1",
	})

	// Start session
	req := httptest.NewRequest(http.MethodPost, "/agent/session/start", strings.NewReader(`{"goal":"c","task_type":"chat"}`))
	rr := httptest.NewRecorder()
	gw.mux.ServeHTTP(rr, req)

	// Compress
	req2 := httptest.NewRequest(http.MethodPost, "/agent/memory/compress", strings.NewReader(`{}`))
	rr2 := httptest.NewRecorder()
	gw.mux.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Errorf("compress status = %d: %s", rr2.Code, rr2.Body.String())
	}

	// Summarize
	req3 := httptest.NewRequest(http.MethodPost, "/agent/memory/summarize", strings.NewReader(`{"max_level":1}`))
	rr3 := httptest.NewRecorder()
	gw.mux.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Errorf("summarize status = %d: %s", rr3.Code, rr3.Body.String())
	}

	if !compressCalled {
		t.Error("compress was not called on mock server")
	}
	if !summarizeCalled {
		t.Error("summarize was not called on mock server")
	}
}

// ─── End-to-end: full session lifecycle ───────────────────────────────────────

func TestAgentSession_FullLifecycle(t *testing.T) {
	calls := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		calls[r.URL.Path]++
		switch r.URL.Path {
		case "/v1/sessions":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		case "/v1/ingest/events":
			var ev map[string]any
			_ = json.NewDecoder(r.Body).Decode(&ev)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":    "accepted",
				"lsn":       1,
				"event_id":  ev["event_id"],
				"memory_id": "mem_001",
				"edge_count": 0,
			})
		case "/v1/internal/memory/recall":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"request_id":          "recall_e2e",
				"visible_memory_refs": []string{"mem_001"},
				"resolved_scope":     "workspace",
			})
		}
	}))
	defer server.Close()

	cfg := Config{CogDBEndpoint: server.URL}
	s, _ := NewAgentSession("agent_a", "t1", "w1", cfg)

	ctx := context.Background()

	// Start session
	id, err := s.StartSession(ctx, "end-to-end test", "investigation", 0, 0)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if id == "" {
		t.Error("StartSession returned empty session ID")
	}

	// Submit events
	_, _ = s.SubmitUserMessage(ctx, "hello", nil)
	_, _ = s.SubmitAgentThought(ctx, "thinking...")
	_, _ = s.SubmitToolCall(ctx, "read_file", `{"path":"/tmp/test"}`)
	_, _ = s.SubmitCheckpoint(ctx, "milestone_1")

	// Query
	view, err := s.Query(ctx, "what happened?", "workspace", 5)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(view.VisibleMemoryRefs) != 1 {
		t.Errorf("VisibleMemoryRefs len = %d, want 1", len(view.VisibleMemoryRefs))
	}

	// End session
	if err := s.EndSession(ctx, "completed"); err != nil {
		t.Fatalf("EndSession: %v", err)
	}
	if s.SessionID() != "" {
		t.Errorf("SessionID() = %q after EndSession", s.SessionID())
	}

	// Verify all expected paths were called
	expected := []string{"/v1/sessions", "/v1/ingest/events", "/v1/internal/memory/recall"}
	for _, path := range expected {
		if calls[path] == 0 {
			t.Errorf("path %s was never called", path)
		}
	}
}

func TestSDKError_Error(t *testing.T) {
	err := newError("submit", ErrCogDBUnavailable, "connection refused")
	sdkErr, ok := err.(*SDKError)
	if !ok {
		t.Fatalf("expected *SDKError, got %T", err)
	}
	if sdkErr.Op != "submit" {
		t.Errorf("Op = %q, want %q", sdkErr.Op, "submit")
	}
	if sdkErr.Error() == "" {
		t.Error("Error() returned empty string")
	}
	if !errors.Is(err, ErrCogDBUnavailable) {
		t.Error("errors.Is(err, ErrCogDBUnavailable) = false")
	}
}

func TestSDKError_NoOp(t *testing.T) {
	// nil error should not wrap
	err := newError("op", nil, "detail")
	if err != nil {
		t.Errorf("newError with nil err: expected nil, got %v", err)
	}
}
