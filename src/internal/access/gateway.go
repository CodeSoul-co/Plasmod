package access

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"andb/src/internal/coordinator"
	"andb/src/internal/s3util"
	"andb/src/internal/schemas"
	"andb/src/internal/storage"
	"andb/src/internal/worker"
)

type Gateway struct {
	coord      *coordinator.Hub
	runtime    *worker.Runtime
	store      storage.RuntimeStorage
	storageCfg *storage.ConfigSnapshot
}

// NewGateway wires HTTP handlers. storageCfg may be nil (tests); when set,
// GET /v1/admin/storage returns the resolved backend configuration.
func NewGateway(coord *coordinator.Hub, runtime *worker.Runtime, store storage.RuntimeStorage, storageCfg *storage.ConfigSnapshot) *Gateway {
	return &Gateway{coord: coord, runtime: runtime, store: store, storageCfg: storageCfg}
}

func (g *Gateway) RegisterRoutes(mux *http.ServeMux) {
	// System
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/v1/admin/topology", g.handleTopology)
	mux.HandleFunc("/v1/admin/storage", g.handleStorage)
	mux.HandleFunc("/v1/admin/s3/export", g.handleS3Export)
	mux.HandleFunc("/v1/admin/s3/snapshot-export", g.handleS3SnapshotExport)

	// Event ingest & query
	mux.HandleFunc("/v1/ingest/events", g.handleIngest)
	mux.HandleFunc("/v1/query", g.handleQuery)

	// Canonical object CRUD
	mux.HandleFunc("/v1/agents", g.handleAgents)
	mux.HandleFunc("/v1/sessions", g.handleSessions)
	mux.HandleFunc("/v1/memory", g.handleMemory)
	mux.HandleFunc("/v1/states", g.handleStates)
	mux.HandleFunc("/v1/artifacts", g.handleArtifacts)
	mux.HandleFunc("/v1/edges", g.handleEdges)
	mux.HandleFunc("/v1/policies", g.handlePolicies)
	mux.HandleFunc("/v1/share-contracts", g.handleShareContracts)
}

func (g *Gateway) handleIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var ev schemas.Event
	if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ack, err := g.runtime.SubmitIngest(ev)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ack)
}

func (g *Gateway) handleQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req schemas.QueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	resp := g.runtime.ExecuteQuery(req)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (g *Gateway) handleTopology(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(g.runtime.Topology())
}

func (g *Gateway) handleStorage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if g.storageCfg == nil {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"mode":            "memory",
			"data_dir":        "",
			"badger_enabled":  false,
			"stores":          map[string]string{},
			"wal_persistence": false,
			"note":            "storage config not wired (nil ConfigSnapshot)",
		})
		return
	}
	_ = json.NewEncoder(w).Encode(g.storageCfg)
}

// ─── /v1/admin/s3/export ────────────────────────────────────────────────────
//
// Dev-only helper:
// 1) Runtime ingests a sample Event
// 2) Runtime executes a sample Query
// 3) Captures {ack, query, response} and uploads it to MinIO/S3 via raw SigV4
// 4) Performs GET round-trip verification after PUT
func (g *Gateway) handleS3Export(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	type request struct {
		ObjectKey string `json:"object_key,omitempty"`
		Prefix    string `json:"prefix,omitempty"`
	}
	var req request
	if r.Body != nil {
		decErr := json.NewDecoder(r.Body).Decode(&req)
		if decErr != nil && decErr != io.EOF {
			http.Error(w, decErr.Error(), http.StatusBadRequest)
			return
		}
	}

	cfg, err := s3util.LoadFromEnv()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	now := time.Now().UTC()
	timestamp := now.Format("20060102T150405Z")

	prefix := cfg.Prefix
	if req.Prefix != "" {
		prefix = strings.TrimRight(req.Prefix, "/")
	}
	if prefix == "" {
		prefix = cfg.Prefix
	}

	objectKey := req.ObjectKey
	if strings.TrimSpace(objectKey) == "" {
		objectKey = prefix + "/runtime_capture_" + timestamp + ".json"
	}

	// Build sample ingest event (based on integration tests).
	ev := schemas.Event{
		EventID:       "evt_rt_" + timestamp,
		TenantID:      "t_demo",
		WorkspaceID:   "w_demo",
		AgentID:       "agent_a",
		SessionID:     "sess_a",
		EventType:     "user_message",
		EventTime:     now.Format(time.RFC3339),
		IngestTime:    now.Format(time.RFC3339),
		VisibleTime:   now.Format(time.RFC3339),
		LogicalTS:     1,
		ParentEventID: "",
		CausalRefs:    []string{},
		Payload:       map[string]any{"text": "hello runtime export"},
		Source:        "runtime_export",
		Importance:    0.5,
		Visibility:    "private",
		Version:       1,
	}

	ack, err := g.runtime.SubmitIngest(ev)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	qReq := schemas.QueryRequest{
		QueryText:           "hello runtime export",
		QueryScope:          "workspace",
		SessionID:           "sess_a",
		AgentID:             "agent_a",
		TenantID:            "t_demo",
		WorkspaceID:         "w_demo",
		TopK:                5,
		TimeWindow:          schemas.TimeWindow{From: "2026-01-01T00:00:00Z", To: "2027-01-01T00:00:00Z"},
		ObjectTypes:         []string{"memory", "state", "artifact"},
		MemoryTypes:         []string{"semantic", "episodic", "procedural"},
		RelationConstraints: []string{},
		ResponseMode:        schemas.ResponseModeStructuredEvidence,
	}

	qResp := g.runtime.ExecuteQuery(qReq)

	capture := map[string]any{
		"captured_at": now.Format(time.RFC3339),
		"object_key":  objectKey,
		"ack":          ack,
		"query":        qReq,
		"response":    qResp,
	}

	bytesWritten, roundTripOK, err := s3util.PutBytesAndVerify(r.Context(), nil, cfg, objectKey, mustJSONBytes(capture), "application/json")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":           "ok",
		"bucket":           cfg.Bucket,
		"object_key":       objectKey,
		"bytes_written":    bytesWritten,
		"roundtrip_ok":     roundTripOK,
		"captured_at":      now.Format(time.RFC3339),
		"minio_endpoint":   cfg.Endpoint,
		"s3_roundtrip_md5": nil,
	})
}

// ─── helper ───────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func mustJSONBytes(v any) []byte {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		// should never happen for map/structs used here
		panic(err)
	}
	return b
}

// ─── /v1/agents ───────────────────────────────────────────────────────────────

func (g *Gateway) handleAgents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, g.store.Objects().ListAgents())
	case http.MethodPost:
		var obj schemas.Agent
		if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		g.coord.Object.PutAgent(obj, "")
		writeJSON(w, map[string]string{"status": "ok", "agent_id": obj.AgentID})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ─── /v1/sessions ─────────────────────────────────────────────────────────────

func (g *Gateway) handleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		agentID := r.URL.Query().Get("agent_id")
		writeJSON(w, g.store.Objects().ListSessions(agentID))
	case http.MethodPost:
		var obj schemas.Session
		if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		g.coord.Object.PutSession(obj, "")
		writeJSON(w, map[string]string{"status": "ok", "session_id": obj.SessionID})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ─── /v1/memory ───────────────────────────────────────────────────────────────

func (g *Gateway) handleMemory(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		agentID := r.URL.Query().Get("agent_id")
		sessionID := r.URL.Query().Get("session_id")
		writeJSON(w, g.store.Objects().ListMemories(agentID, sessionID))
	case http.MethodPost:
		var obj schemas.Memory
		if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		g.coord.Memory.Put(obj)
		writeJSON(w, map[string]string{"status": "ok", "memory_id": obj.MemoryID})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ─── /v1/states ───────────────────────────────────────────────────────────────

func (g *Gateway) handleStates(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		agentID := r.URL.Query().Get("agent_id")
		sessionID := r.URL.Query().Get("session_id")
		writeJSON(w, g.store.Objects().ListStates(agentID, sessionID))
	case http.MethodPost:
		var obj schemas.State
		if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		g.coord.Object.PutState(obj, "")
		writeJSON(w, map[string]string{"status": "ok", "state_id": obj.StateID})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ─── /v1/artifacts ────────────────────────────────────────────────────────────

func (g *Gateway) handleArtifacts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		sessionID := r.URL.Query().Get("session_id")
		writeJSON(w, g.store.Objects().ListArtifacts(sessionID))
	case http.MethodPost:
		var obj schemas.Artifact
		if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		g.coord.Object.PutArtifact(obj, "")
		writeJSON(w, map[string]string{"status": "ok", "artifact_id": obj.ArtifactID})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ─── /v1/edges ────────────────────────────────────────────────────────────────

func (g *Gateway) handleEdges(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, g.store.Edges().ListEdges())
	case http.MethodPost:
		var obj schemas.Edge
		if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		g.store.Edges().PutEdge(obj)
		writeJSON(w, map[string]string{"status": "ok", "edge_id": obj.EdgeID})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ─── /v1/policies ─────────────────────────────────────────────────────────────

func (g *Gateway) handlePolicies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		objectID := r.URL.Query().Get("object_id")
		if objectID != "" {
			writeJSON(w, g.store.Policies().GetPolicies(objectID))
		} else {
			writeJSON(w, g.store.Policies().ListPolicies())
		}
	case http.MethodPost:
		var obj schemas.PolicyRecord
		if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		g.coord.Policy.Append(obj)
		writeJSON(w, map[string]string{"status": "ok", "policy_id": obj.PolicyID})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ─── /v1/share-contracts ──────────────────────────────────────────────────────

func (g *Gateway) handleShareContracts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		scope := r.URL.Query().Get("scope")
		if scope != "" {
			writeJSON(w, g.store.Contracts().ContractsByScope(scope))
		} else {
			writeJSON(w, g.store.Contracts().ListContracts())
		}
	case http.MethodPost:
		var obj schemas.ShareContract
		if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		g.store.Contracts().PutContract(obj)
		writeJSON(w, map[string]string{"status": "ok", "contract_id": obj.ContractID})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
