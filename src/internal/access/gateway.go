package access

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"plasmod/src/internal/coordinator"
	"plasmod/src/internal/schemas"
	"plasmod/src/internal/storage"
	"plasmod/src/internal/worker"
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
	mux.HandleFunc("/v1/system/mode", g.handleSystemMode)
	mux.HandleFunc("/v1/admin/topology", g.handleTopology)
	mux.HandleFunc("/v1/admin/storage", g.handleStorage)
	mux.HandleFunc("/v1/admin/s3/export", g.handleS3Export)
	mux.HandleFunc("/v1/admin/s3/snapshot-export", g.handleS3SnapshotExport)
	mux.HandleFunc("/v1/admin/dataset/delete", g.handleDatasetDelete)
	mux.HandleFunc("/v1/admin/dataset/purge", g.handleDatasetPurge)
	if isTestMode() {
		mux.HandleFunc("/v1/debug/echo", g.handleDebugEcho)
	}

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

	// Proof trace queries
	mux.HandleFunc("/v1/traces/", g.handleTraces)

	// Agent SDK internal endpoints — algorithm dispatch bridge
	mux.HandleFunc("/v1/internal/memory/recall", g.handleMemoryRecall)
	mux.HandleFunc("/v1/internal/memory/ingest", g.handleMemoryIngest)
	mux.HandleFunc("/v1/internal/memory/compress", g.handleMemoryCompress)
	mux.HandleFunc("/v1/internal/memory/summarize", g.handleMemorySummarize)
	mux.HandleFunc("/v1/internal/memory/decay", g.handleMemoryDecay)
	mux.HandleFunc("/v1/internal/memory/share", g.handleMemoryShare)
	mux.HandleFunc("/v1/internal/memory/conflict/resolve", g.handleMemoryConflictResolve)
}

func (g *Gateway) handleSystemMode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]any{
		"app_mode":      CurrentAppMode(),
		"debug_enabled": isTestMode(),
	})
}

// handleDebugEcho is test-only endpoint for end-to-end transparency verification.
func (g *Gateway) handleDebugEcho(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{
		"status": "ok",
		"echo":   body,
	})
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

// handleDatasetDelete soft-deletes uploaded dataset memories by dataset selectors.
// Matching prefers Memory.SourceFileName / Memory.DatasetName (from ingest payload) when set;
// otherwise falls back to token-safe parsing of Memory.Content (see schemas.MemoryDatasetMatch).
// Selectors: file_name, dataset_name, prefix — AND semantics; at least one required.
func (g *Gateway) handleDatasetDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	type reqBody struct {
		FileName    string `json:"file_name,omitempty"`
		DatasetName string `json:"dataset_name,omitempty"`
		Prefix      string `json:"prefix,omitempty"`
		WorkspaceID string `json:"workspace_id,omitempty"`
		DryRun      bool   `json:"dry_run,omitempty"`
	}
	var req reqBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req.FileName = strings.TrimSpace(req.FileName)
	req.DatasetName = strings.TrimSpace(req.DatasetName)
	req.Prefix = strings.TrimSpace(req.Prefix)
	req.WorkspaceID = strings.TrimSpace(req.WorkspaceID)
	if req.WorkspaceID == "" {
		http.Error(w, "workspace_id is required", http.StatusBadRequest)
		return
	}
	if req.FileName == "" && req.DatasetName == "" && req.Prefix == "" {
		http.Error(w, "at least one selector is required: file_name, dataset_name, or prefix", http.StatusBadRequest)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	mems := g.store.Objects().ListMemories("", "")
	matched := 0
	updated := 0
	ids := make([]string, 0)
	for _, m := range mems {
		if !schemas.MemoryDatasetMatch(m, req.WorkspaceID, req.FileName, req.DatasetName, req.Prefix) {
			continue
		}
		matched++
		ids = append(ids, m.MemoryID)
		if req.DryRun || !m.IsActive {
			continue
		}
		m.IsActive = false
		if m.ValidTo == "" {
			m.ValidTo = now
		}
		g.store.Objects().PutMemory(m)
		if tiered := g.runtime.TieredObjects(); tiered != nil {
			tiered.SoftDeleteMemoryTierCleanup(m.MemoryID)
		}
		if g.store.Policies() != nil {
			g.store.Policies().AppendPolicy(schemas.PolicyRecord{
				PolicyID:         "policy_delete_" + m.MemoryID,
				ObjectID:         m.MemoryID,
				ObjectType:       string(schemas.ObjectTypeMemory),
				PolicyVersion:    time.Now().UnixNano(),
				Context:          "dataset delete by selector",
				VerifiedState:    string(schemas.VerifiedStateRetracted),
				QuarantineFlag:   true,
				VisibilityPolicy: m.Scope,
				PolicyReason:     "dataset selector matched delete request",
				PolicySource:     "admin_api",
			})
		}
		updated++
	}
	writeJSON(w, map[string]any{
		"status":       "ok",
		"file_name":    req.FileName,
		"dataset_name": req.DatasetName,
		"prefix":       req.Prefix,
		"workspace_id": req.WorkspaceID,
		"dry_run":      req.DryRun,
		"matched":      matched,
		"deleted":      updated,
		"memory_ids":   ids,
	})
}

// handleDatasetPurge removes inactive (soft-deleted) memories when selectors match.
// Requires workspace_id. only_if_inactive defaults to true (active memories are skipped).
// When TieredObjectStore is wired, HardDeleteMemory clears hot/warm/cold; otherwise PurgeMemoryWarmOnly
// removes hot/warm only (cold embeddings may remain — response field purge_backend is "warm_only").
func (g *Gateway) handleDatasetPurge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	type reqBody struct {
		FileName       string `json:"file_name,omitempty"`
		DatasetName    string `json:"dataset_name,omitempty"`
		Prefix         string `json:"prefix,omitempty"`
		WorkspaceID    string `json:"workspace_id,omitempty"`
		DryRun         bool   `json:"dry_run,omitempty"`
		OnlyIfInactive *bool  `json:"only_if_inactive,omitempty"`
	}
	var req reqBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req.FileName = strings.TrimSpace(req.FileName)
	req.DatasetName = strings.TrimSpace(req.DatasetName)
	req.Prefix = strings.TrimSpace(req.Prefix)
	req.WorkspaceID = strings.TrimSpace(req.WorkspaceID)
	if req.WorkspaceID == "" {
		http.Error(w, "workspace_id is required", http.StatusBadRequest)
		return
	}
	if req.FileName == "" && req.DatasetName == "" && req.Prefix == "" {
		http.Error(w, "at least one selector is required: file_name, dataset_name, or prefix", http.StatusBadRequest)
		return
	}
	onlyIfInactive := true
	if req.OnlyIfInactive != nil {
		onlyIfInactive = *req.OnlyIfInactive
	}
	tiered := g.runtime.TieredObjects()
	purgeBackend := "tiered"
	if tiered == nil {
		purgeBackend = "warm_only"
	}
	mems := g.store.Objects().ListMemories("", "")
	matched := 0
	skippedActive := 0
	purgeable := 0
	purged := 0
	ids := make([]string, 0)
	purgeIDs := make([]string, 0)
	for _, m := range mems {
		if !schemas.MemoryDatasetMatch(m, req.WorkspaceID, req.FileName, req.DatasetName, req.Prefix) {
			continue
		}
		matched++
		ids = append(ids, m.MemoryID)
		if m.IsActive && onlyIfInactive {
			skippedActive++
			continue
		}
		purgeable++
		purgeIDs = append(purgeIDs, m.MemoryID)
		if req.DryRun {
			continue
		}
		if tiered != nil {
			tiered.HardDeleteMemory(m.MemoryID)
		} else {
			storage.PurgeMemoryWarmOnly(g.store, m.MemoryID)
		}
		if g.store.Audits() != nil {
			now := time.Now().UTC().Format(time.RFC3339)
			g.store.Audits().AppendAudit(schemas.AuditRecord{
				RecordID:       fmt.Sprintf("audit_purge_%s_%d", m.MemoryID, time.Now().UnixNano()),
				TargetMemoryID: m.MemoryID,
				OperationType:  string(schemas.AuditOpDelete),
				ActorType:      "system",
				ActorID:        "admin_api",
				Decision:       "allow",
				ReasonCode:     "dataset_purge",
				Timestamp:      now,
			})
		}
		purged++
	}
	writeJSON(w, map[string]any{
		"status":            "ok",
		"file_name":         req.FileName,
		"dataset_name":      req.DatasetName,
		"prefix":            req.Prefix,
		"workspace_id":      req.WorkspaceID,
		"dry_run":           req.DryRun,
		"only_if_inactive":  onlyIfInactive,
		"purge_backend":     purgeBackend,
		"matched":           matched,
		"skipped_active":    skippedActive,
		"purgeable":         purgeable,
		"purged":            purged,
		"memory_ids":        ids,
		"purged_memory_ids": purgeIDs,
	})
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

	cfg, err := storage.LoadFromEnv()
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
		"ack":         ack,
		"query":       qReq,
		"response":    qResp,
	}

	bytesWritten, roundTripOK, err := storage.PutBytesAndVerify(r.Context(), nil, cfg, objectKey, mustJSONBytes(capture), "application/json")
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

// ─── /v1/traces/{object_id} ─────────────────────────────────────────────────
//
// Returns the full proof trace for a given object ID, including:
//   - object metadata (type, namespace, timestamps)
//   - pre-computed evidence fragment (salience, level, related IDs)
//   - typed edges incident to this object (1-hop adjacency)
//   - version chain (all ObjectVersions)
//   - policy annotations (TTL, quarantine, visibility)
//   - governance decisions (DerivationLog, PolicyDecisionLog)
//
// This endpoint is stateless: it assembles the trace on-the-fly from the
// RuntimeStorage layer without re-executing a retrieval search.
//
// Future extension: multi-hop graph traversal via depth parameter.
func (g *Gateway) handleTraces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Strip "/v1/traces/" prefix to get the object ID.
	id := strings.TrimPrefix(r.URL.Path, "/v1/traces/")
	id = strings.TrimPrefix(id, "/")
	if id == "" {
		http.Error(w, "object_id is required", http.StatusBadRequest)
		return
	}

	// ── 1. Object type inference ───────────────────────────────────────────
	objType := inferObjectType(id)

	// ── 2. Evidence fragment from hot cache ────────────────────────────────
	var frag any
	if g.runtime != nil {
		frag = g.runtime.GetEvidenceFragment(id)
	}

	// ── 3. 1-hop edges (in + out) ───────────────────────────────────────
	var edges []schemas.Edge
	if g.store.Edges() != nil {
		edges = g.store.Edges().BulkEdges([]string{id})
	}

	// ── 4. Version chain ──────────────────────────────────────────────────
	var versions []schemas.ObjectVersion
	if g.store.Versions() != nil {
		if v, ok := g.store.Versions().LatestVersion(id); ok {
			versions = append(versions, v)
		}
	}

	// ── 5. Policy annotations ─────────────────────────────────────────────
	var policies []schemas.PolicyRecord
	if g.store.Policies() != nil {
		policies = g.store.Policies().GetPolicies(id)
	}

	// ── 6. Canonical object ───────────────────────────────────────────────
	var canonical any
	if g.store.Objects() != nil {
		switch objType {
		case "memory":
			if m, ok := g.store.Objects().GetMemory(id); ok {
				canonical = m
			}
		case "state":
			if s, ok := g.store.Objects().GetState(id); ok {
				canonical = s
			}
		case "artifact":
			if a, ok := g.store.Objects().GetArtifact(id); ok {
				canonical = a
			}
		}
	}

	// ── 7. Governance logs (DerivationLog + PolicyDecisionLog) ───────────
	var derivLog, policyDecisions []string
	if g.runtime != nil {
		if dl := g.runtime.GetDerivationLog(id); dl != nil {
			derivLog = dl
		}
		if pd := g.runtime.GetPolicyDecisions(id); pd != nil {
			policyDecisions = pd
		}
	}

	// ── 8. Assembled trace steps (human-readable) ─────────────────────────
	steps := assembleTraceSteps(id, objType, frag, edges, versions, policies, derivLog, policyDecisions)

	resp := TraceResponse{
		ObjectID:         id,
		ObjectType:       objType,
		CanonicalObject:  canonical,
		EvidenceFragment: frag,
		Edges:            edges,
		Versions:         versions,
		Policies:         policies,
		DerivationLog:    derivLog,
		PolicyDecisions:  policyDecisions,
		ProofSteps:       steps,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// TraceResponse is the structured proof-trace response returned by /v1/traces/{object_id}.
type TraceResponse struct {
	ObjectID         string                  `json:"object_id"`
	ObjectType       string                  `json:"object_type"`
	CanonicalObject  any                     `json:"canonical_object,omitempty"`
	EvidenceFragment any                     `json:"evidence_fragment,omitempty"`
	Edges            []schemas.Edge          `json:"edges"`
	Versions         []schemas.ObjectVersion `json:"versions"`
	Policies         []schemas.PolicyRecord  `json:"policies"`
	DerivationLog    []string                `json:"derivation_log,omitempty"`
	PolicyDecisions  []string                `json:"policy_decisions,omitempty"`
	ProofSteps       []TraceStep             `json:"proof_steps"`
}

// TraceStep is a human-readable step in the assembled proof trace.
type TraceStep struct {
	Phase  string `json:"phase"`           // e.g. "canonical", "fragment", "edges", "versions", "policy"
	Label  string `json:"label"`           // e.g. "salience", "belongs_to_session", "ttl_active"
	Detail string `json:"detail"`          // human-readable description
	Value  string `json:"value,omitempty"` // key value if applicable
}

// assembleTraceSteps builds a flat list of human-readable proof steps.
func assembleTraceSteps(id, objType string, frag any, edges []schemas.Edge, versions []schemas.ObjectVersion, policies []schemas.PolicyRecord, derivLog, policyDecisions []string) []TraceStep {
	var steps []TraceStep

	// Phase 1: Canonical object
	steps = append(steps, TraceStep{
		Phase:  "canonical",
		Label:  "object_id",
		Detail: "canonical object registered in the store",
		Value:  id,
	})
	steps = append(steps, TraceStep{
		Phase:  "canonical",
		Label:  "object_type",
		Detail: "inferred from ID prefix",
		Value:  objType,
	})

	// Phase 2: Evidence fragment
	if frag != nil {
		steps = append(steps, TraceStep{
			Phase:  "fragment",
			Label:  "precomputed",
			Detail: "evidence fragment built at ingest time, stored in hot cache",
		})
	} else {
		steps = append(steps, TraceStep{
			Phase:  "fragment",
			Label:  "not_cached",
			Detail: "no evidence fragment found in hot cache",
		})
	}

	// Phase 3: Edges
	if len(edges) > 0 {
		steps = append(steps, TraceStep{
			Phase:  "edges",
			Label:  "edge_count",
			Detail: "1-hop graph expansion from object",
			Value:  fmt.Sprintf("%d", len(edges)),
		})
		for _, e := range edges {
			steps = append(steps, TraceStep{
				Phase:  "edges",
				Label:  "edge:" + e.EdgeType,
				Detail: fmt.Sprintf("%s --[%s]--> %s", e.SrcObjectID, e.EdgeType, e.DstObjectID),
				Value:  fmt.Sprintf("weight=%.2f", e.Weight),
			})
		}
	} else {
		steps = append(steps, TraceStep{
			Phase:  "edges",
			Label:  "no_edges",
			Detail: "no incident edges found",
		})
	}

	// Phase 4: Versions
	if len(versions) > 0 {
		steps = append(steps, TraceStep{
			Phase:  "versions",
			Label:  "version_count",
			Detail: "version chain from VersionStore",
			Value:  fmt.Sprintf("%d", len(versions)),
		})
		for _, v := range versions {
			steps = append(steps, TraceStep{
				Phase:  "versions",
				Label:  "version",
				Detail: fmt.Sprintf("version=%d event=%s snapshot=%s", v.Version, v.MutationEventID, v.SnapshotTag),
				Value:  v.ValidFrom,
			})
		}
	}

	// Phase 5: Policies
	if len(policies) > 0 {
		for _, pol := range policies {
			if pol.QuarantineFlag {
				steps = append(steps, TraceStep{
					Phase:  "policy",
					Label:  "quarantine",
					Detail: "object is quarantined",
				})
			}
			if pol.VerifiedState == string(schemas.VerifiedStateRetracted) {
				steps = append(steps, TraceStep{
					Phase:  "policy",
					Label:  "retracted",
					Detail: "object version is retracted",
				})
			}
		}
	}

	// Phase 6: Governance logs
	if len(derivLog) > 0 {
		steps = append(steps, TraceStep{
			Phase:  "governance",
			Label:  "derivation_log",
			Detail: fmt.Sprintf("%d derivation decisions recorded", len(derivLog)),
		})
	}
	if len(policyDecisions) > 0 {
		steps = append(steps, TraceStep{
			Phase:  "governance",
			Label:  "policy_decisions",
			Detail: fmt.Sprintf("%d policy decisions recorded", len(policyDecisions)),
		})
	}

	return steps
}

// inferObjectType infers the canonical object type from the well-known ID prefix scheme.
func inferObjectType(id string) string {
	switch {
	case strings.HasPrefix(id, "mem_") || strings.HasPrefix(id, "summary_") || strings.HasPrefix(id, "shared_"):
		return "memory"
	case strings.HasPrefix(id, "state_"):
		return "state"
	case strings.HasPrefix(id, "art_") || strings.HasPrefix(id, "tool_trace_"):
		return "artifact"
	default:
		return "unknown"
	}
}

// ─── /v1/share-contracts ─────────────────────────────────────────────────────

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

// ─── /v1/internal/memory/* — Agent SDK algorithm dispatch bridge ─────────────────

// handleMemoryRecall combines search retrieval with algorithm-level Recall scoring.
func (g *Gateway) handleMemoryRecall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Query       string `json:"query"`
		Scope       string `json:"scope"`
		TopK        int    `json:"top_k"`
		AgentID     string `json:"agent_id"`
		SessionID   string `json:"session_id"`
		TenantID    string `json:"tenant_id"`
		WorkspaceID string `json:"workspace_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.TopK <= 0 {
		req.TopK = 10
	}
	view := g.runtime.DispatchRecall(req.Query, req.Scope, req.TopK,
		req.AgentID, req.SessionID, req.TenantID, req.WorkspaceID)
	writeJSON(w, view)
}

// handleMemoryIngest forwards memory IDs to the algorithm ingest pipeline.
func (g *Gateway) handleMemoryIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		MemoryIDs []string `json:"memory_ids"`
		AgentID   string   `json:"agent_id"`
		SessionID string   `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	out := g.runtime.DispatchAlgorithm("ingest", req.MemoryIDs, "", "", req.AgentID, req.SessionID, nil)
	writeJSON(w, out)
}

// handleMemoryCompress triggers memory consolidation via MemoryConsolidationWorker.
func (g *Gateway) handleMemoryCompress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		AgentID   string `json:"agent_id"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	out := g.runtime.DispatchAlgorithm("compress", nil, "", "", req.AgentID, req.SessionID, nil)
	writeJSON(w, out)
}

// handleMemorySummarize triggers memory summarization via SummarizationWorker.
func (g *Gateway) handleMemorySummarize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		AgentID   string `json:"agent_id"`
		SessionID string `json:"session_id"`
		MaxLevel  int    `json:"max_level"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	out := g.runtime.DispatchAlgorithm("summarize", nil, "", "", req.AgentID, req.SessionID, nil)
	writeJSON(w, out)
}

// handleMemoryDecay applies forgetting decay via AlgorithmDispatchWorker.
func (g *Gateway) handleMemoryDecay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		AgentID   string `json:"agent_id"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	out := g.runtime.DispatchAlgorithm("decay", nil, "", time.Now().UTC().Format(time.RFC3339), req.AgentID, req.SessionID, nil)
	writeJSON(w, out)
}

// handleMemoryShare broadcasts a memory to a target agent via CommunicationWorker.
func (g *Gateway) handleMemoryShare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		MemoryID      string `json:"memory_id"`
		FromAgentID   string `json:"from_agent_id"`
		ToAgentID     string `json:"to_agent_id"`
		ContractScope string `json:"contract_scope"` // "restricted_shared"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.ToAgentID == req.FromAgentID {
		writeJSON(w, map[string]string{"status": "skipped", "reason": "same_agent"})
		return
	}
	sharedID, err := g.runtime.DispatchShare(req.FromAgentID, req.ToAgentID, req.MemoryID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"status":           "ok",
		"shared_memory_id": sharedID,
		"memory_id":        req.MemoryID,
		"to_agent_id":      req.ToAgentID,
	})
}

// handleMemoryConflictResolve resolves a memory conflict via ConflictMergeWorker.
func (g *Gateway) handleMemoryConflictResolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		LeftID  string `json:"left_id"`
		RightID string `json:"right_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	winner := g.runtime.DispatchConflictResolve(req.LeftID, req.RightID)
	writeJSON(w, map[string]string{
		"status":    "ok",
		"winner_id": winner,
		"left_id":   req.LeftID,
		"right_id":  req.RightID,
	})
}
