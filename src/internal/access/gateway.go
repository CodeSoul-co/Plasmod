package access

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"plasmod/src/internal/config"
	"plasmod/src/internal/coordinator"
	"plasmod/src/internal/metrics"
	"plasmod/src/internal/schemas"
	"plasmod/src/internal/storage"
	"plasmod/src/internal/worker"
)

type Gateway struct {
	coord      *coordinator.Hub
	runtime    *worker.Runtime
	store      storage.RuntimeStorage
	storageCfg *storage.ConfigSnapshot
	bundle     *storage.RuntimeBundle // optional; used for admin Badger.DropAll
	modeMu     sync.RWMutex
	consistencyMode string
	hardDeleteMgr   *hardDeleteManager
	stopCh          chan struct{}
}

func resolveDatasetPurgeWorkers(tieredEnabled bool) int {
	const (
		defaultTieredWorkers = 8
		defaultWarmWorkers   = 1
		maxWorkers           = 64
	)
	raw := strings.TrimSpace(os.Getenv("PLASMOD_DATASET_PURGE_WORKERS"))
	if raw == "" {
		if tieredEnabled {
			return defaultTieredWorkers
		}
		return defaultWarmWorkers
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return defaultWarmWorkers
	}
	if n > maxWorkers {
		return maxWorkers
	}
	return n
}

func resolveDatasetPurgeBatchSize() int {
	const (
		defaultBatchSize = 512
		maxBatchSize     = 20000
	)
	raw := strings.TrimSpace(os.Getenv("PLASMOD_DATASET_PURGE_BATCH_SIZE"))
	if raw == "" {
		return defaultBatchSize
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return defaultBatchSize
	}
	if n > maxBatchSize {
		return maxBatchSize
	}
	return n
}

func resolveDatasetPurgeQueueSize(workers int) int {
	const maxQueueSize = 20000
	raw := strings.TrimSpace(os.Getenv("PLASMOD_DATASET_PURGE_QUEUE_SIZE"))
	if raw == "" {
		q := workers * 4
		if q < 16 {
			return 16
		}
		if q > maxQueueSize {
			return maxQueueSize
		}
		return q
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		q := workers * 4
		if q < 16 {
			return 16
		}
		if q > maxQueueSize {
			return maxQueueSize
		}
		return q
	}
	if n > maxQueueSize {
		return maxQueueSize
	}
	return n
}

type purgePhaseDurations struct {
	deleteNs int64
	auditNs  int64
	outboxNs int64
}

func (g *Gateway) purgeOneMemory(memoryID string, tiered *storage.TieredObjectStore) purgePhaseDurations {
	var phase purgePhaseDurations
	startDelete := time.Now()
	if tiered != nil {
		tiered.HardDeleteMemory(memoryID)
	} else {
		storage.PurgeMemoryWarmOnly(g.store, memoryID)
	}
	// Clean up orphaned segment records referencing this memory.
	if g.store != nil {
		if seg := g.store.Segments(); seg != nil {
			_ = seg.DeleteByStorageRef(memoryID)
		}
	}
	phase.deleteNs = time.Since(startDelete).Nanoseconds()

	startAudit := time.Now()
	if g.store.Audits() != nil {
		now := time.Now().UTC().Format(time.RFC3339)
		g.store.Audits().AppendAudit(schemas.AuditRecord{
			RecordID:       fmt.Sprintf("audit_purge_%s_%d", memoryID, time.Now().UnixNano()),
			TargetMemoryID: memoryID,
			OperationType:  string(schemas.AuditOpDelete),
			ActorType:      "system",
			ActorID:        "admin_api",
			Decision:       "allow",
			ReasonCode:     "dataset_purge",
			Timestamp:      now,
		})
	}
	phase.auditNs = time.Since(startAudit).Nanoseconds()

	startOutbox := time.Now()
	if g.runtime != nil {
		g.runtime.EnqueueMemoryDelete(memoryID, true, "dataset_purge")
	}
	phase.outboxNs = time.Since(startOutbox).Nanoseconds()
	return phase
}

// NewGateway wires HTTP handlers. storageCfg may be nil (tests); when set,
// GET /v1/admin/storage returns the resolved backend configuration.
// bundle may be nil in tests; admin data wipe still clears in-memory state and omits Badger.DropAll.
func NewGateway(coord *coordinator.Hub, runtime *worker.Runtime, store storage.RuntimeStorage, storageCfg *storage.ConfigSnapshot, bundle *storage.RuntimeBundle) *Gateway {
	g := &Gateway{
		coord:           coord,
		runtime:         runtime,
		store:           store,
		storageCfg:      storageCfg,
		bundle:          bundle,
		consistencyMode: "strict_visible",
		stopCh:          make(chan struct{}),
	}
	g.hardDeleteMgr = newHardDeleteManagerFromEnv()
	go g.hardDeleteMgr.run(g.stopCh, context.Background(), g.processHardDeleteTaskBatch)
	return g
}

// Shutdown stops background goroutines owned by Gateway.
func (g *Gateway) Shutdown() {
	if g == nil || g.stopCh == nil {
		return
	}
	close(g.stopCh)
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
	mux.HandleFunc("/v1/admin/config/effective", g.handleEffectiveConfig)
	mux.HandleFunc("/v1/admin/s3/export", g.handleS3Export)
	mux.HandleFunc("/v1/admin/s3/snapshot-export", g.handleS3SnapshotExport)
	mux.HandleFunc("/v1/admin/s3/cold-purge", g.handleS3ColdPurge)
	mux.HandleFunc("/v1/admin/warm/prebuild", g.handleAdminWarmPrebuild)
	mux.HandleFunc("/v1/admin/dataset/delete", g.handleDatasetDelete)
	mux.HandleFunc("/v1/admin/dataset/purge", g.handleDatasetPurge)
	mux.HandleFunc("/v1/admin/dataset/purge/task", g.handleDatasetPurgeTask)
	mux.HandleFunc("/v1/admin/data/wipe", g.handleAdminDataWipe)
	mux.HandleFunc("/v1/admin/rollback", g.handleAdminRollback)
	mux.HandleFunc("/v1/admin/consistency-mode", g.handleAdminConsistencyMode)
	mux.HandleFunc("/v1/admin/replay", g.handleAdminReplay)
	if isTestMode() {
		mux.HandleFunc("/v1/debug/echo", g.handleDebugEcho)
	}

	// Event ingest & query
	mux.HandleFunc("/v1/ingest/events", g.handleIngest)
	mux.HandleFunc("/v1/ingest/vectors", g.handleIngestVectors)
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
	mux.HandleFunc("/v1/internal/memory/stale", g.handleMemoryMarkStale)
	mux.HandleFunc("/v1/internal/memory/conflict/inject", g.handleMemoryConflictInject)

	// Observability
	mux.HandleFunc("/v1/admin/metrics", g.handleAdminMetrics)
	mux.HandleFunc("/v1/admin/governance-mode", g.handleAdminGovernanceMode)
	mux.HandleFunc("/v1/admin/runtime-mode", g.handleAdminRuntimeMode)
	mux.HandleFunc("/v1/admin/memory/providers/mode", g.handleAdminMemoryProvidersMode)
	mux.HandleFunc("/v1/admin/memory/providers/health", g.handleAdminMemoryProvidersHealth)

	// Task lifecycle (3-MT1~MT7, 4-M6)
	mux.HandleFunc("/v1/internal/task/start", g.handleTaskStart)
	mux.HandleFunc("/v1/internal/task/complete", g.handleTaskComplete)
	mux.HandleFunc("/v1/internal/task/tokens", g.handleTaskTokens)
	mux.HandleFunc("/v1/internal/task/claim", g.handleTaskClaim)

	// Plan step tracking (3-T5, 3-MT6)
	mux.HandleFunc("/v1/internal/plan/step", g.handlePlanStep)
	mux.HandleFunc("/v1/internal/plan/repair", g.handlePlanRepair)

	// Long-document chunked ingest (3-T1)
	mux.HandleFunc("/v1/ingest/document", g.handleIngestDocument)

	// Multi-stage report progress (3-T3)
	mux.HandleFunc("/v1/internal/task/stage", g.handleTaskStage)

	// MAS coordination (4-T2, 4-M5)
	mux.HandleFunc("/v1/internal/mas/answer-consistency", g.handleMASAnswerConsistency)
	mux.HandleFunc("/v1/internal/mas/aggregate", g.handleMASAggregate)

	// Stateful tool interaction query (3-T4)
	mux.HandleFunc("/v1/internal/tool-state", g.handleToolState)

	// Agent role management & handoff (4-T1)
	mux.HandleFunc("/v1/internal/agent/handoff", g.handleAgentHandoff)
	mux.HandleFunc("/v1/agent/list", g.handleAgentList)

	// Multi-round session context aggregation (3-T2)
	mux.HandleFunc("/v1/internal/session/context", g.handleSessionContext)

	// Eval ground-truth store (eval harness support)
	mux.HandleFunc("/v1/internal/eval/ground-truth", g.handleEvalGroundTruth)
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
	if strings.TrimSpace(ev.EventID) == "" {
		ev.EventID = generateObjectID("evt")
	}
	t0Visible := time.Now()
	ack, err := g.runtime.SubmitIngest(ev)
	if err != nil {
		metrics.Global().RecordRetrievalError()
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	metrics.Global().RecordWriteToVisible(time.Since(t0Visible))
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
	if strings.TrimSpace(req.WarmSegmentID) != "" {
		ids, err := g.runtime.SearchWarmSegment(req.WarmSegmentID, req.QueryText, req.TopK)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{
			"status":          "ok",
			"objects":         ids,
			"warm_segment_id": req.WarmSegmentID,
			"tier":            "warm_segment",
		})
		return
	}
	if req.LatestBatchOnly {
		workspaceID := strings.TrimSpace(req.WorkspaceID)
		datasetName := strings.TrimSpace(req.DatasetName)
		sourceFileName := strings.TrimSpace(req.SourceFileName)
		if workspaceID == "" {
			http.Error(w, "latest_batch_only requires workspace_id", http.StatusBadRequest)
			return
		}
		if datasetName == "" && sourceFileName == "" {
			http.Error(w, "latest_batch_only requires dataset_name or source_file_name", http.StatusBadRequest)
			return
		}
	}
	resp := g.runtime.ExecuteQuery(req)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (g *Gateway) handleIngestVectors(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	type reqBody struct {
		SegmentID string      `json:"segment_id"`
		ObjectIDs []string    `json:"object_ids"`
		Vectors   [][]float32 `json:"vectors"`
	}
	var req reqBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req.SegmentID = strings.TrimSpace(req.SegmentID)
	if req.SegmentID == "" {
		req.SegmentID = "warm.default"
	}
	if len(req.Vectors) == 0 {
		http.Error(w, "vectors is required", http.StatusBadRequest)
		return
	}
	if len(req.ObjectIDs) == 0 {
		req.ObjectIDs = make([]string, len(req.Vectors))
		for i := range req.Vectors {
			req.ObjectIDs[i] = fmt.Sprintf("%s_%d", req.SegmentID, i)
		}
	}
	if len(req.ObjectIDs) != len(req.Vectors) {
		http.Error(w, "object_ids/vectors length mismatch", http.StatusBadRequest)
		return
	}
	n, err := g.runtime.IngestVectorsToWarmSegment(req.SegmentID, req.ObjectIDs, req.Vectors)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{
		"status":      "ok",
		"segment_id":  req.SegmentID,
		"ingested":    n,
		"vector_dim":  len(req.Vectors[0]),
		"direct_warm": true,
	})
}

func (g *Gateway) handleAdminWarmPrebuild(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := g.runtime.AdminWarmPrebuild(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{
		"status":     "ok",
		"prebuilt":   true,
		"segment_id": "warm.default",
	})
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

func (g *Gateway) handleEffectiveConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfg, err := config.LoadSharedAlgorithmConfig()
	if err != nil {
		cfg = schemas.DefaultAlgorithmConfig()
	}
	if sz := os.Getenv("PLASMOD_EVIDENCE_CACHE_SIZE"); sz != "" {
		if n, convErr := strconv.Atoi(sz); convErr == nil && n > 0 {
			cfg.EvidenceCacheSize = n
		}
	}
	if d := os.Getenv("PLASMOD_MAX_PROOF_DEPTH"); d != "" {
		if n, convErr := strconv.Atoi(d); convErr == nil && n > 0 {
			cfg.MaxProofDepth = n
		}
	}
	if t := os.Getenv("PLASMOD_HOT_TIER_THRESHOLD"); t != "" {
		if f, convErr := strconv.ParseFloat(t, 64); convErr == nil && f > 0 {
			cfg.HotTierSalienceThreshold = f
		}
	}
	writeJSON(w, map[string]any{
		"algorithm_config": cfg,
	})
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
		// Fast path: workspace_id is required; skip cross-workspace rows early.
		if m.Scope != req.WorkspaceID {
			continue
		}
		if !schemas.MemoryDatasetMatchTrimmedInWorkspace(m, req.FileName, req.DatasetName, req.Prefix) {
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
		if g.runtime != nil {
			g.runtime.EnqueueMemoryDelete(m.MemoryID, false, "dataset_delete")
		}
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
		IncludeMemoryIDs *bool `json:"include_memory_ids,omitempty"`
		Async          *bool  `json:"async,omitempty"`
		IdempotencyKey string `json:"idempotency_key,omitempty"`
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
	includeMemoryIDs := true
	if req.IncludeMemoryIDs != nil {
		includeMemoryIDs = *req.IncludeMemoryIDs
	}
	asyncMode := false
	if req.Async != nil {
		asyncMode = *req.Async
	}
	tiered := g.runtime.TieredObjects()
	purgeBackend := "tiered"
	if tiered == nil {
		purgeBackend = "warm_only"
	}
	requestStartedAt := time.Now()
	scanStartedAt := requestStartedAt
	ctx := r.Context()
	mems := g.store.Objects().ListMemories("", "")
	scanned := len(mems)
	workspaceCandidates := 0
	matched := 0
	skippedActive := 0
	purgeable := 0
	purged := 0
	cancelled := false
	cancelReason := ""
	ids := make([]string, 0)
	purgeIDs := make([]string, 0)
	for i, m := range mems {
		if i%256 == 0 {
			select {
			case <-ctx.Done():
				cancelled = true
				cancelReason = ctx.Err().Error()
				break
			default:
			}
			if cancelled {
				break
			}
		}
		// Fast path: workspace_id is required; skip cross-workspace rows early.
		if m.Scope != req.WorkspaceID {
			continue
		}
		workspaceCandidates++
		if !schemas.MemoryDatasetMatchTrimmedInWorkspace(m, req.FileName, req.DatasetName, req.Prefix) {
			continue
		}
		matched++
		if includeMemoryIDs {
			ids = append(ids, m.MemoryID)
		}
		if m.IsActive && onlyIfInactive {
			skippedActive++
			continue
		}
		purgeable++
		purgeIDs = append(purgeIDs, m.MemoryID)
	}
	scanElapsedMs := time.Since(scanStartedAt).Milliseconds()
	purgeWorkers := resolveDatasetPurgeWorkers(tiered != nil)
	purgeBatchSize := resolveDatasetPurgeBatchSize()
	purgeQueueSize := resolveDatasetPurgeQueueSize(purgeWorkers)
	var purgeDeleteObjectNs int64
	var purgeDeleteAuditNs int64
	var purgeDeleteOutboxNs int64
	deleteStartedAt := time.Now()
	if asyncMode && !req.DryRun && len(purgeIDs) > 0 {
		if g.hardDeleteMgr == nil {
			http.Error(w, "hard delete manager unavailable", http.StatusServiceUnavailable)
			return
		}
		idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
		if existing, ok := g.hardDeleteMgr.getActiveByIdempotencyKey(idempotencyKey); ok {
			writeJSON(w, map[string]any{
				"status":                "accepted",
				"async":                 true,
				"task_id":               existing.TaskID,
				"workspace_id":          req.WorkspaceID,
				"matched":               matched,
				"purgeable":             purgeable,
				"purge_backend":         purgeBackend,
				"purge_scan_elapsed_ms": scanElapsedMs,
				"include_memory_ids":    includeMemoryIDs,
				"idempotency_key":       idempotencyKey,
				"deduplicated":          true,
			})
			return
		}
		task := &hardDeleteTask{
			TaskID:         generateObjectID("purge_task"),
			WorkspaceID:    req.WorkspaceID,
			DatasetName:    req.DatasetName,
			MemoryIDs:      purgeIDs,
			State:          hardDeleteStateQueued,
			CreatedAt:      time.Now().UTC().Format(time.RFC3339),
			UpdatedAt:      time.Now().UTC().Format(time.RFC3339),
			IdempotencyKey: idempotencyKey,
			PurgeBackend:   purgeBackend,
			Workers:        purgeWorkers,
			BatchSize:      purgeBatchSize,
		}
		if g.hardDeleteMgr.enqueue(task) {
			writeJSON(w, map[string]any{
				"status":                "accepted",
				"async":                 true,
				"task_id":               task.TaskID,
				"workspace_id":          req.WorkspaceID,
				"matched":               matched,
				"purgeable":             purgeable,
				"purge_backend":         purgeBackend,
				"purge_scan_elapsed_ms": scanElapsedMs,
				"include_memory_ids":    includeMemoryIDs,
				"idempotency_key":       task.IdempotencyKey,
			})
			return
		}
		http.Error(w, "failed to enqueue hard delete task", http.StatusInternalServerError)
		return
	}
	if !req.DryRun && len(purgeIDs) > 0 && !cancelled {
		for start := 0; start < len(purgeIDs); start += purgeBatchSize {
			select {
			case <-ctx.Done():
				cancelled = true
				cancelReason = ctx.Err().Error()
			default:
			}
			if cancelled {
				break
			}
			end := start + purgeBatchSize
			if end > len(purgeIDs) {
				end = len(purgeIDs)
			}
			batch := purgeIDs[start:end]
			workerCount := purgeWorkers
			if workerCount > len(batch) {
				workerCount = len(batch)
			}
			if workerCount <= 1 {
				for _, id := range batch {
					select {
					case <-ctx.Done():
						cancelled = true
						cancelReason = ctx.Err().Error()
					default:
					}
					if cancelled {
						break
					}
					phase := g.purgeOneMemory(id, tiered)
					purgeDeleteObjectNs += phase.deleteNs
					purgeDeleteAuditNs += phase.auditNs
					purgeDeleteOutboxNs += phase.outboxNs
					purged++
				}
			} else {
				jobs := make(chan string, purgeQueueSize)
				var wg sync.WaitGroup
				var batchPurged int64
				var batchDeleteNs int64
				var batchAuditNs int64
				var batchOutboxNs int64
				for i := 0; i < workerCount; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						for {
							select {
							case <-ctx.Done():
								return
							case id, ok := <-jobs:
								if !ok {
									return
								}
								phase := g.purgeOneMemory(id, tiered)
								atomic.AddInt64(&batchDeleteNs, phase.deleteNs)
								atomic.AddInt64(&batchAuditNs, phase.auditNs)
								atomic.AddInt64(&batchOutboxNs, phase.outboxNs)
								atomic.AddInt64(&batchPurged, 1)
							}
						}
					}()
				}
				for _, id := range batch {
					select {
					case <-ctx.Done():
						cancelled = true
						cancelReason = ctx.Err().Error()
					case jobs <- id:
					}
					if cancelled {
						break
					}
				}
				close(jobs)
				wg.Wait()
				purged += int(batchPurged)
				purgeDeleteObjectNs += atomic.LoadInt64(&batchDeleteNs)
				purgeDeleteAuditNs += atomic.LoadInt64(&batchAuditNs)
				purgeDeleteOutboxNs += atomic.LoadInt64(&batchOutboxNs)
			}
			log.Printf(
				"admin purge progress: workspace=%s dataset=%s batch=%d/%d purged=%d/%d workers=%d queue=%d elapsed_ms=%d cancelled=%t",
				req.WorkspaceID,
				req.DatasetName,
				(start/purgeBatchSize)+1,
				(len(purgeIDs)+purgeBatchSize-1)/purgeBatchSize,
				purged,
				len(purgeIDs),
				workerCount,
				purgeQueueSize,
				time.Since(deleteStartedAt).Milliseconds(),
				cancelled,
			)
			if cancelled {
				break
			}
		}
	}
	status := "ok"
	if cancelled {
		status = "cancelled"
	}
	dataPresence := "has_data"
	if matched == 0 || purgeable == 0 {
		dataPresence = "no_data"
	}
	progressPercent := 0
	if purgeable > 0 {
		progressPercent = int((float64(purged) / float64(purgeable)) * 100)
		if progressPercent > 100 {
			progressPercent = 100
		}
	}
	deleteElapsedMs := time.Since(deleteStartedAt).Milliseconds()
	responseStartedAt := time.Now()
	resp := map[string]any{
		"status":                 status,
		"data_presence":          dataPresence,
		"file_name":              req.FileName,
		"dataset_name":           req.DatasetName,
		"prefix":                 req.Prefix,
		"workspace_id":           req.WorkspaceID,
		"dry_run":                req.DryRun,
		"only_if_inactive":       onlyIfInactive,
		"purge_backend":          purgeBackend,
		"scanned":                scanned,
		"workspace_scanned":      workspaceCandidates,
		"matched":                matched,
		"skipped_active":         skippedActive,
		"purgeable":              purgeable,
		"purged":                 purged,
		"cancelled":              cancelled,
		"cancel_reason":          cancelReason,
		"purge_workers":          purgeWorkers,
		"purge_batch_size":       purgeBatchSize,
		"purge_queue_size":       purgeQueueSize,
		"purge_elapsed_ms":       time.Since(requestStartedAt).Milliseconds(),
		"purge_scan_elapsed_ms":  scanElapsedMs,
		"purge_delete_elapsed_ms": deleteElapsedMs,
		"purge_progress_percent": progressPercent,
		"purge_delete_object_ms": float64(purgeDeleteObjectNs) / float64(time.Millisecond),
		"purge_delete_audit_ms":  float64(purgeDeleteAuditNs) / float64(time.Millisecond),
		"purge_delete_outbox_ms": float64(purgeDeleteOutboxNs) / float64(time.Millisecond),
		"include_memory_ids":     includeMemoryIDs,
	}
	if includeMemoryIDs {
		resp["memory_ids"] = ids
		resp["purged_memory_ids"] = purgeIDs
	} else {
		resp["memory_ids_omitted"] = true
	}
	resp["purge_response_build_elapsed_ms"] = time.Since(responseStartedAt).Milliseconds()
	writeJSON(w, resp)
}

func (g *Gateway) handleDatasetPurgeTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	taskID := strings.TrimSpace(r.URL.Query().Get("task_id"))
	if taskID == "" {
		http.Error(w, "task_id is required", http.StatusBadRequest)
		return
	}
	if g.hardDeleteMgr == nil {
		http.Error(w, "hard delete manager unavailable", http.StatusServiceUnavailable)
		return
	}
	task, ok := g.hardDeleteMgr.get(taskID)
	if !ok {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	total := len(task.MemoryIDs)
	progressPercent := 0
	if total > 0 {
		progressPercent = int((float64(task.Processed+task.Failed) / float64(total)) * 100)
		if progressPercent > 100 {
			progressPercent = 100
		}
	}
	writeJSON(w, map[string]any{
		"status":           "ok",
		"task":             task,
		"total":            total,
		"progress_percent": progressPercent,
	})
}

func (g *Gateway) processHardDeleteTaskBatch(task *hardDeleteTask, batchSize int) (processed, failed int, done bool, stats hardDeleteBatchStats, err error) {
	if task == nil {
		return 0, 0, true, hardDeleteBatchStats{}, nil
	}
	tiered := g.runtime.TieredObjects()
	start := task.Processed + task.Failed
	if start >= len(task.MemoryIDs) {
		return 0, 0, true, hardDeleteBatchStats{}, nil
	}
	end := start + batchSize
	if end > len(task.MemoryIDs) {
		end = len(task.MemoryIDs)
	}
	ids := task.MemoryIDs[start:end]
	itemTimeout := resolveHardDeleteItemTimeout()
	workers := resolveAdaptiveHardDeleteBatchWorkers(task, len(ids), itemTimeout)
	stats = hardDeleteBatchStats{
		Workers:   workers,
		BatchSize: len(ids),
	}
	if workers <= 1 {
		var pAtomic, fAtomic int64
		var deleteNsAtomic int64
		var auditNsAtomic int64
		var outboxNsAtomic int64
		for _, id := range ids {
			phase, timedOut, panicked := runPurgeOneMemoryWithTimeout(id, tiered, itemTimeout, g.purgeOneMemory)
			if timedOut || panicked {
				atomic.AddInt64(&fAtomic, 1)
				continue
			}
			atomic.AddInt64(&pAtomic, 1)
			atomic.AddInt64(&deleteNsAtomic, phase.deleteNs)
			atomic.AddInt64(&auditNsAtomic, phase.auditNs)
			atomic.AddInt64(&outboxNsAtomic, phase.outboxNs)
		}
		processed = int(atomic.LoadInt64(&pAtomic))
		failed = int(atomic.LoadInt64(&fAtomic))
		stats.DeleteObjectNs = atomic.LoadInt64(&deleteNsAtomic)
		stats.DeleteAuditNs = atomic.LoadInt64(&auditNsAtomic)
		stats.DeleteOutboxNs = atomic.LoadInt64(&outboxNsAtomic)
		done = (task.Processed + task.Failed + processed + failed) >= len(task.MemoryIDs)
		return processed, failed, done, stats, nil
	}
	var processedAtomic int64
	var failedAtomic int64
	var deleteNsAtomic int64
	var auditNsAtomic int64
	var outboxNsAtomic int64
	jobs := make(chan string, workers*2)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				phase, timedOut, panicked := runPurgeOneMemoryWithTimeout(id, tiered, itemTimeout, g.purgeOneMemory)
				if timedOut || panicked {
					atomic.AddInt64(&failedAtomic, 1)
					continue
				}
				atomic.AddInt64(&deleteNsAtomic, phase.deleteNs)
				atomic.AddInt64(&auditNsAtomic, phase.auditNs)
				atomic.AddInt64(&outboxNsAtomic, phase.outboxNs)
				atomic.AddInt64(&processedAtomic, 1)
			}
		}()
	}
	for _, id := range ids {
		jobs <- id
	}
	close(jobs)
	wg.Wait()
	processed = int(atomic.LoadInt64(&processedAtomic))
	failed = int(atomic.LoadInt64(&failedAtomic))
	stats.DeleteObjectNs = atomic.LoadInt64(&deleteNsAtomic)
	stats.DeleteAuditNs = atomic.LoadInt64(&auditNsAtomic)
	stats.DeleteOutboxNs = atomic.LoadInt64(&outboxNsAtomic)
	done = (task.Processed + task.Failed + processed + failed) >= len(task.MemoryIDs)
	return processed, failed, done, stats, nil
}

func resolveHardDeleteBatchWorkers(batchLen int) int {
	const defaultWorkers = 8
	const maxWorkers = 64
	n := defaultWorkers
	raw := strings.TrimSpace(os.Getenv("PLASMOD_HARD_DELETE_BATCH_WORKERS"))
	if raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			n = parsed
		}
	}
	if n > maxWorkers {
		n = maxWorkers
	}
	if batchLen > 0 && n > batchLen {
		n = batchLen
	}
	if n < 1 {
		return 1
	}
	return n
}

func resolveAdaptiveHardDeleteBatchWorkers(task *hardDeleteTask, batchLen int, itemTimeout time.Duration) int {
	baseWorkers := resolveHardDeleteBatchWorkers(batchLen)
	workers := baseWorkers
	if task != nil && task.Workers > 0 {
		workers = task.Workers
	}
	var failRate float64
	var avgDeleteMs float64
	if task != nil {
		total := task.Processed + task.Failed
		if total > 0 {
			failRate = float64(task.Failed) / float64(total)
		}
		if task.Processed > 0 {
			avgDeleteMs = task.DeleteObjectMs / float64(task.Processed)
		}
	}
	snap := metrics.Global().Snapshot()
	pressureHigh := snap.ConcurrentQueries > 4 || snap.GoAllocBytes > 2*1024*1024*1024
	pressureMedium := !pressureHigh && (snap.ConcurrentQueries > 2 || snap.GoAllocBytes > 1*1024*1024*1024)
	return tuneHardDeleteWorkers(workers, baseWorkers, batchLen, pressureHigh, pressureMedium, failRate, avgDeleteMs, itemTimeout)
}

func tuneHardDeleteWorkers(
	current int,
	base int,
	batchLen int,
	pressureHigh bool,
	pressureMedium bool,
	failRate float64,
	avgDeleteMs float64,
	itemTimeout time.Duration,
) int {
	const maxWorkers = 64
	if current < 1 {
		current = 1
	}
	if base < 1 {
		base = 1
	}
	adaptiveMax := base * 2
	if adaptiveMax > maxWorkers {
		adaptiveMax = maxWorkers
	}
	switch {
	case pressureHigh:
		current = current / 2
		if current < 1 {
			current = 1
		}
	case pressureMedium:
		current -= 2
		if current < 1 {
			current = 1
		}
	default:
		switch {
		case failRate >= 0.10:
			current -= 2
		case failRate >= 0.03:
			current -= 1
		default:
			slowThresholdMs := float64(itemTimeout.Milliseconds()) * 0.6
			if slowThresholdMs < 200 {
				slowThresholdMs = 200
			}
			if avgDeleteMs > 0 && avgDeleteMs >= slowThresholdMs {
				current -= 1
			} else {
				current += 2
			}
		}
		if current < 1 {
			current = 1
		}
	}
	if current > adaptiveMax {
		current = adaptiveMax
	}
	if batchLen > 0 && current > batchLen {
		current = batchLen
	}
	if current < 1 {
		current = 1
	}
	return current
}

func resolveHardDeleteItemTimeout() time.Duration {
	const (
		defaultTimeoutMs = 5000
		minTimeoutMs     = 100
		maxTimeoutMs     = 600000
	)
	raw := strings.TrimSpace(os.Getenv("PLASMOD_HARD_DELETE_ITEM_TIMEOUT_MS"))
	if raw == "" {
		return time.Duration(defaultTimeoutMs) * time.Millisecond
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return time.Duration(defaultTimeoutMs) * time.Millisecond
	}
	if n < minTimeoutMs {
		n = minTimeoutMs
	}
	if n > maxTimeoutMs {
		n = maxTimeoutMs
	}
	return time.Duration(n) * time.Millisecond
}

func runPurgeOneMemoryWithTimeout(
	memoryID string,
	tiered *storage.TieredObjectStore,
	timeout time.Duration,
	purgeFn func(string, *storage.TieredObjectStore) purgePhaseDurations,
) (phase purgePhaseDurations, timedOut bool, panicked bool) {
	if purgeFn == nil {
		return purgePhaseDurations{}, false, true
	}
	if timeout <= 0 {
		timeout = 1 * time.Millisecond
	}
	type result struct {
		phase    purgePhaseDurations
		panicked bool
	}
	done := make(chan result, 1)
	go func() {
		defer func() {
			if recover() != nil {
				done <- result{panicked: true}
			}
		}()
		done <- result{phase: purgeFn(memoryID, tiered)}
	}()
	select {
	case r := <-done:
		return r.phase, false, r.panicked
	case <-time.After(timeout):
		return purgePhaseDurations{}, true, false
	}
}

// handleAdminDataWipe clears all application data (Badger DropAll when enabled, in-memory stores,
// retrieval planes, tier caches, WAL/derivation logs, evidence cache). Destructive: requires confirm token.
func (g *Gateway) handleAdminDataWipe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if g.runtime == nil {
		http.Error(w, "runtime not configured", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Confirm string `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	const adminWipeConfirm = "delete_all_data"
	if strings.TrimSpace(body.Confirm) != adminWipeConfirm {
		http.Error(w, `confirm must be "delete_all_data"`, http.StatusBadRequest)
		return
	}
	algoCfg := schemas.DefaultAlgorithmConfig()
	out, err := g.runtime.AdminWipeAll(g.bundle, algoCfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, out)
}

func isSupportedConsistencyMode(mode string) bool {
	switch mode {
	case "strict_visible", "bounded_staleness", "eventual_visibility":
		return true
	default:
		return false
	}
}

func (g *Gateway) handleAdminConsistencyMode(w http.ResponseWriter, r *http.Request) {
	supported := []string{"strict_visible", "bounded_staleness", "eventual_visibility"}
	switch r.Method {
	case http.MethodGet:
		g.modeMu.RLock()
		mode := g.consistencyMode
		g.modeMu.RUnlock()
		writeJSON(w, map[string]any{
			"status":          "ok",
			"mode":            mode,
			"supported_modes": supported,
			"note":            "control-plane mode exposed; query path currently remains single-mode",
		})
	case http.MethodPost:
		var req struct {
			Mode string `json:"mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mode := strings.TrimSpace(req.Mode)
		if !isSupportedConsistencyMode(mode) {
			http.Error(w, "unsupported mode", http.StatusBadRequest)
			return
		}
		g.modeMu.Lock()
		g.consistencyMode = mode
		g.modeMu.Unlock()
		writeJSON(w, map[string]any{
			"status":          "ok",
			"mode":            mode,
			"supported_modes": supported,
			"note":            "control-plane mode exposed; query path currently remains single-mode",
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (g *Gateway) handleAdminReplay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if g.runtime == nil {
		http.Error(w, "runtime not configured", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		FromLSN int64 `json:"from_lsn"`
		Limit   int   `json:"limit"`
		DryRun  *bool `json:"dry_run,omitempty"`
		Apply   bool  `json:"apply,omitempty"`
		Confirm string `json:"confirm,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	dryRun := true
	if req.DryRun != nil {
		dryRun = *req.DryRun
	}
	applyRequested := req.Apply || !dryRun
	var (
		summary map[string]any
		err     error
	)
	if applyRequested {
		if strings.TrimSpace(req.Confirm) != "apply_replay" {
			http.Error(w, `confirm must be "apply_replay" when apply=true`, http.StatusBadRequest)
			return
		}
		summary, err = g.runtime.AdminReplayApply(req.FromLSN, req.Limit)
	} else {
		summary, err = g.runtime.AdminReplayPreview(req.FromLSN, req.Limit)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	summary["dry_run"] = !applyRequested
	summary["apply"] = applyRequested
	writeJSON(w, summary)
}

// handleAdminRollback performs a minimal memory-level rollback action for
// operational recovery: reactivate or deactivate one memory record.
func (g *Gateway) handleAdminRollback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	type reqBody struct {
		MemoryID string `json:"memory_id"`
		Action   string `json:"action"` // reactivate | deactivate
		DryRun   bool   `json:"dry_run,omitempty"`
		Reason   string `json:"reason,omitempty"`
	}
	var req reqBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req.MemoryID = strings.TrimSpace(req.MemoryID)
	req.Action = strings.TrimSpace(req.Action)
	if req.MemoryID == "" {
		http.Error(w, "memory_id is required", http.StatusBadRequest)
		return
	}
	if req.Action != "reactivate" && req.Action != "deactivate" {
		http.Error(w, `action must be "reactivate" or "deactivate"`, http.StatusBadRequest)
		return
	}
	mem, ok := g.store.Objects().GetMemory(req.MemoryID)
	if !ok {
		http.Error(w, "memory not found", http.StatusNotFound)
		return
	}
	beforeActive := mem.IsActive
	afterActive := beforeActive
	switch req.Action {
	case "reactivate":
		afterActive = true
	case "deactivate":
		afterActive = false
	}
	if req.DryRun {
		writeJSON(w, map[string]any{
			"status":        "ok",
			"dry_run":       true,
			"memory_id":     req.MemoryID,
			"action":        req.Action,
			"before_active": beforeActive,
			"after_active":  afterActive,
			"note":          "dry-run only; no mutation performed",
		})
		return
	}

	mem.IsActive = afterActive
	if afterActive {
		mem.ValidTo = ""
	} else if mem.ValidTo == "" {
		mem.ValidTo = time.Now().UTC().Format(time.RFC3339)
	}
	g.store.Objects().PutMemory(mem)
	if !afterActive {
		if tiered := g.runtime.TieredObjects(); tiered != nil {
			tiered.SoftDeleteMemoryTierCleanup(mem.MemoryID)
		}
	}
	if g.store.Audits() != nil {
		g.store.Audits().AppendAudit(schemas.AuditRecord{
			RecordID:       fmt.Sprintf("audit_rollback_%s_%d", mem.MemoryID, time.Now().UnixNano()),
			TargetMemoryID: mem.MemoryID,
			OperationType:  string(schemas.AuditOpPolicyChange),
			ActorType:      "system",
			ActorID:        "admin_api",
			Decision:       "allow",
			ReasonCode:     "admin_rollback",
			Timestamp:      time.Now().UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, map[string]any{
		"status":        "ok",
		"dry_run":       false,
		"memory_id":     req.MemoryID,
		"action":        req.Action,
		"before_active": beforeActive,
		"after_active":  afterActive,
		"reason":        strings.TrimSpace(req.Reason),
	})
}

func (g *Gateway) handleS3ColdPurge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Confirm string `json:"confirm"`
		DryRun  bool   `json:"dry_run"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Confirm) != "purge_cold_tier" {
		http.Error(w, `confirm must be "purge_cold_tier"`, http.StatusBadRequest)
		return
	}
	tiered := g.runtime.TieredObjects()
	if tiered == nil {
		writeJSON(w, map[string]any{
			"status":   "ok",
			"dry_run":  req.DryRun,
			"result":   "no_tiered_store",
			"purged":   false,
			"note":     "tiered object store not configured",
		})
		return
	}
	if req.DryRun {
		writeJSON(w, map[string]any{
			"status":  "ok",
			"dry_run": true,
			"purged":  false,
			"note":    "dry-run only; no mutation performed",
		})
		return
	}
	result := tiered.ClearColdIfInMemory()
	writeJSON(w, map[string]any{
		"status":  "ok",
		"dry_run": false,
		"result":  result,
		"purged":  result == "in_memory_cleared",
		"note":    "S3-backed cold objects require bucket-side lifecycle/manual cleanup",
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
		if strings.TrimSpace(obj.AgentID) == "" {
			obj.AgentID = generateObjectID("agent")
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
		if strings.TrimSpace(obj.SessionID) == "" {
			obj.SessionID = generateObjectID("sess")
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
		workspaceID := r.URL.Query().Get("workspace_id")
		all := g.store.Objects().ListMemories(agentID, sessionID)
		if workspaceID == "" {
			writeJSON(w, all)
			return
		}
		filtered := all[:0]
		for _, m := range all {
			if m.Scope == workspaceID {
				filtered = append(filtered, m)
			}
		}
		writeJSON(w, filtered)
	case http.MethodPost:
		var obj schemas.Memory
		if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(obj.MemoryID) == "" {
			obj.MemoryID = generateObjectID("mem")
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
		if strings.TrimSpace(obj.StateID) == "" {
			obj.StateID = generateObjectID("state")
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
		if strings.TrimSpace(obj.ArtifactID) == "" {
			obj.ArtifactID = generateObjectID("art")
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
		if strings.TrimSpace(obj.EdgeID) == "" {
			obj.EdgeID = generateObjectID("edge")
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
		if strings.TrimSpace(obj.PolicyID) == "" {
			obj.PolicyID = generateObjectID("policy")
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
		if strings.TrimSpace(obj.ContractID) == "" {
			obj.ContractID = generateObjectID("contract")
		}
		g.store.Contracts().PutContract(obj)
		writeJSON(w, map[string]string{"status": "ok", "contract_id": obj.ContractID})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func generateObjectID(prefix string) string {
	// Keep IDs lexically time-sortable while preserving randomness:
	// <prefix>_<unix_millis_base36>_<12hex random bytes>
	ts := strconv.FormatInt(time.Now().UTC().UnixMilli(), 36)
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("%s_%s", prefix, ts)
	}
	return fmt.Sprintf("%s_%s_%s", prefix, ts, hex.EncodeToString(buf[:]))
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

// ── /v1/admin/metrics ────────────────────────────────────────────────────────
//
// GET /v1/admin/metrics
//
// Returns a point-in-time snapshot of all runtime metrics.  Covers:
//   3-MS1 query latency, 3-MS2 write latency, 3-MS3 write-to-visible latency,
//   3-MS4 storage growth, 3-MS5 retrieval error rate, 3-MS6 resource overhead,
//   1-M8 memory footprint, 1-M10 scale-out efficiency,
//   3-MT1~MT7 task-level metrics, 4-M1/M4/M5/M6 MAS metrics.
//
// Optional query param:
//   ?storage=true   populate storage_bytes_total / storage_memory_count /
//                   storage_event_count from the live ObjectStore (slightly
//                   heavier; defaults to the atomic counters updated at ingest).
func (g *Gateway) handleAdminMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	mc := metrics.Global()
	if r.URL.Query().Get("storage") == "true" {
		mems := g.store.Objects().ListMemories("", "")
		evts := g.store.Objects().ListEvents("", "")
		mc.StorageMemoryCount.Store(int64(len(mems)))
		mc.StorageEventCount.Store(int64(len(evts)))
	}
	snap := mc.Snapshot()
	writeJSON(w, snap)
}

// ── /v1/admin/governance-mode ────────────────────────────────────────────────
//
// GET  /v1/admin/governance-mode          → return current state
// POST /v1/admin/governance-mode          → {"enabled": true/false}
//
// When governance is disabled the runtime skips TTL/quarantine/ACL enforcement
// (used by 4-B4 baseline).
func (g *Gateway) handleAdminGovernanceMode(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, map[string]any{
			"governance_disabled": g.runtime.GovernanceDisabled,
		})
	case http.MethodPost:
		var body struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		g.runtime.GovernanceDisabled = !body.Enabled
		writeJSON(w, map[string]any{
			"status":              "ok",
			"governance_disabled": g.runtime.GovernanceDisabled,
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ── /v1/admin/runtime-mode ───────────────────────────────────────────────────
//
// GET  /v1/admin/runtime-mode             → return current flags
// POST /v1/admin/runtime-mode             → {"vector_only_mode": bool, "minimal_mode": bool}
//
// Allows hot-switching between:
//   - full Plasmod            (both false)
//   - vector-only baseline    (vector_only_mode=true)  3-B1
//   - minimal/stripped mode   (minimal_mode=true)      3-B4 / 4-B4
func (g *Gateway) handleAdminRuntimeMode(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, map[string]any{
			"vector_only_mode":    g.runtime.VectorOnlyMode,
			"minimal_mode":        g.runtime.MinimalMode,
			"governance_disabled": g.runtime.GovernanceDisabled,
		})
	case http.MethodPost:
		var body struct {
			VectorOnlyMode    *bool `json:"vector_only_mode"`
			MinimalMode       *bool `json:"minimal_mode"`
			GovernanceDisabled *bool `json:"governance_disabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if body.VectorOnlyMode != nil {
			g.runtime.VectorOnlyMode = *body.VectorOnlyMode
		}
		if body.MinimalMode != nil {
			g.runtime.MinimalMode = *body.MinimalMode
		}
		if body.GovernanceDisabled != nil {
			g.runtime.GovernanceDisabled = *body.GovernanceDisabled
		}
		writeJSON(w, map[string]any{
			"status":              "ok",
			"vector_only_mode":    g.runtime.VectorOnlyMode,
			"minimal_mode":        g.runtime.MinimalMode,
			"governance_disabled": g.runtime.GovernanceDisabled,
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (g *Gateway) handleAdminMemoryProvidersMode(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, map[string]any{
			"status": "ok",
			"mode":   g.runtime.MemoryBackendMode(),
		})
	case http.MethodPost:
		var req struct {
			Mode string `json:"mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if ok := g.runtime.SetMemoryBackendMode(strings.TrimSpace(req.Mode)); !ok {
			http.Error(w, "unsupported mode", http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{
			"status": "ok",
			"mode":   g.runtime.MemoryBackendMode(),
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (g *Gateway) handleAdminMemoryProvidersHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, g.runtime.MemoryBackendHealth(r.Context()))
}

// ── /v1/internal/memory/stale ────────────────────────────────────────────────
//
// POST /v1/internal/memory/stale
// Body: {"memory_id": "...", "reason": "optional explanation"}
//
// Marks a memory as stale (LifecycleState="stale", IsActive=false) without
// deleting it.  Stale memories can still be retrieved via include_cold=true
// but are excluded from normal warm-tier queries.  Used by 4-D6 to inject
// stale information for MAS contamination / resilience experiments.
func (g *Gateway) handleMemoryMarkStale(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		MemoryID string `json:"memory_id"`
		Reason   string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.MemoryID) == "" {
		http.Error(w, "memory_id is required", http.StatusBadRequest)
		return
	}
	mem, ok := g.store.Objects().GetMemory(req.MemoryID)
	if !ok {
		http.Error(w, "memory not found", http.StatusNotFound)
		return
	}
	mem.LifecycleState = string(schemas.MemoryLifecycleStale)
	mem.IsActive = false
	g.store.Objects().PutMemory(mem)
	g.store.Audits().AppendAudit(schemas.AuditRecord{
		RecordID:       fmt.Sprintf("stale_%s_%d", req.MemoryID, time.Now().UnixNano()),
		OperationType:  string(schemas.AuditOpAlgorithmUpdate),
		ActorType:      "system",
		TargetMemoryID: req.MemoryID,
		ReasonCode:     req.Reason,
		Decision:       "allow",
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
	})
	writeJSON(w, map[string]any{
		"status":          "ok",
		"memory_id":       req.MemoryID,
		"lifecycle_state": mem.LifecycleState,
	})
}

// ── /v1/internal/memory/conflict/inject ─────────────────────────────────────
//
// POST /v1/internal/memory/conflict/inject
// Body: {
//   "agent_id":     "...",
//   "session_id":   "...",
//   "content_a":    "conflicting claim A",
//   "content_b":    "conflicting claim B",
//   "importance":   0.7       (optional, default 0.5)
// }
//
// Synthesises two episodic Memory objects with identical provenance but
// contradictory content and links them with a "conflict" edge.  The pair is
// retrievable via normal query so downstream logic can be tested for
// conflict awareness / preservation (4-D5, 4-M4).
func (g *Gateway) handleMemoryConflictInject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		AgentID    string  `json:"agent_id"`
		SessionID  string  `json:"session_id"`
		ContentA   string  `json:"content_a"`
		ContentB   string  `json:"content_b"`
		Importance float64 `json:"importance"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.ContentA == "" || req.ContentB == "" {
		http.Error(w, "content_a and content_b are required", http.StatusBadRequest)
		return
	}
	if req.Importance <= 0 {
		req.Importance = 0.5
	}
	now := time.Now().UTC().Format(time.RFC3339)
	idA := generateObjectID("cmem")
	idB := generateObjectID("cmem")
	scope := req.AgentID
	if scope == "" {
		scope = "default"
	}
	memA := schemas.Memory{
		MemoryID:       idA,
		AgentID:        req.AgentID,
		SessionID:      req.SessionID,
		Content:        req.ContentA,
		MemoryType:     string(schemas.MemoryTypeEpisodic),
		Importance:     req.Importance,
		IsActive:       true,
		Scope:          scope,
		LifecycleState: string(schemas.MemoryLifecycleActive),
		ValidFrom:      now,
	}
	memB := schemas.Memory{
		MemoryID:       idB,
		AgentID:        req.AgentID,
		SessionID:      req.SessionID,
		Content:        req.ContentB,
		MemoryType:     string(schemas.MemoryTypeEpisodic),
		Importance:     req.Importance,
		IsActive:       true,
		Scope:          scope,
		LifecycleState: string(schemas.MemoryLifecycleActive),
		ValidFrom:      now,
	}
	g.store.Objects().PutMemory(memA)
	g.store.Objects().PutMemory(memB)

	conflictEdge := schemas.Edge{
		EdgeID:       generateObjectID("cedge"),
		SrcObjectID:  idA,
		SrcType:      string(schemas.ObjectTypeMemory),
		DstObjectID:  idB,
		DstType:      string(schemas.ObjectTypeMemory),
		EdgeType:     "conflict",
		CreatedTS:    now,
		Properties:   map[string]any{"injected": true},
	}
	g.store.Edges().PutEdge(conflictEdge)

	metrics.Global().RecordConflict(true)
	g.store.Audits().AppendAudit(schemas.AuditRecord{
		RecordID:       fmt.Sprintf("conflict_inject_%s_%d", idA, time.Now().UnixNano()),
		OperationType:  string(schemas.AuditOpWrite),
		ActorType:      "system",
		TargetMemoryID: idA,
		ReasonCode:     "conflict_injection:" + idB,
		Decision:       "allow",
		Timestamp:      now,
	})
	writeJSON(w, map[string]any{
		"status":     "ok",
		"memory_id_a": idA,
		"memory_id_b": idB,
		"edge_id":    conflictEdge.EdgeID,
	})
}

// ── Task Lifecycle API ────────────────────────────────────────────────────────
//
// These endpoints allow agents (or eval harnesses) to report task lifecycle
// events so the metrics collector can compute 3-MT1~MT7 and 4-M6 without
// the server needing to intercept every LLM call.

// POST /v1/internal/task/start
// Body: {"session_id":"...", "task_type":"...", "goal":"..."}
//
// Registers the session in the metrics tracker and optionally updates the
// Session object in the store.  Idempotent: calling start multiple times
// on the same session only refreshes goal/task_type.
func (g *Gateway) handleTaskStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		SessionID string `json:"session_id"`
		TaskType  string `json:"task_type"`
		Goal      string `json:"goal"`
		AgentID   string `json:"agent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.SessionID) == "" {
		http.Error(w, "session_id is required", http.StatusBadRequest)
		return
	}
	// Ensure the session record is initialised in the tracker.
	_ = metrics.Global().Session(req.SessionID)

	// Update or create the Session object in the canonical store.
	sess, ok := g.store.Objects().GetSession(req.SessionID)
	if !ok {
		sess = schemas.Session{
			SessionID: req.SessionID,
			AgentID:   req.AgentID,
			StartTS:   time.Now().UTC().Format(time.RFC3339),
			Status:    "active",
		}
	}
	if req.TaskType != "" {
		sess.TaskType = req.TaskType
	}
	if req.Goal != "" {
		sess.Goal = req.Goal
	}
	if req.AgentID != "" {
		sess.AgentID = req.AgentID
	}
	g.store.Objects().PutSession(sess)
	writeJSON(w, map[string]any{
		"status":     "ok",
		"session_id": req.SessionID,
		"task_type":  sess.TaskType,
	})
}

// POST /v1/internal/task/complete
// Body: {"session_id":"...", "success":true, "duration_ms":1234}
//
// Marks the task as complete in the metrics tracker and updates the Session
// EndTS + Status in the store.  Also increments MASTaskTotal/Success when
// the agent is part of a multi-agent system (presence of AgentID in session).
func (g *Gateway) handleTaskComplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		SessionID  string  `json:"session_id"`
		Success    bool    `json:"success"`
		DurationMs float64 `json:"duration_ms"`
		MAS        bool    `json:"mas"` // true → also count in MAS metrics (4-M6)
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.SessionID) == "" {
		http.Error(w, "session_id is required", http.StatusBadRequest)
		return
	}
	metrics.Global().Session(req.SessionID).Complete(req.Success, req.DurationMs)
	metrics.Global().RecordTaskDuration(req.DurationMs)
	if req.MAS {
		metrics.Global().RecordMASTask(req.Success)
	}

	// Update Session record.
	if sess, ok := g.store.Objects().GetSession(req.SessionID); ok {
		sess.EndTS = time.Now().UTC().Format(time.RFC3339)
		if req.Success {
			sess.Status = "completed"
		} else {
			sess.Status = "failed"
		}
		g.store.Objects().PutSession(sess)
	}
	writeJSON(w, map[string]any{
		"status":     "ok",
		"session_id": req.SessionID,
		"success":    req.Success,
	})
}

// POST /v1/internal/task/tokens
// Body: {"session_id":"...", "tokens":512}
//
// Adds token usage to the session tracker (3-MT4).
func (g *Gateway) handleTaskTokens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		SessionID string `json:"session_id"`
		Tokens    int64  `json:"tokens"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.SessionID == "" {
		http.Error(w, "session_id is required", http.StatusBadRequest)
		return
	}
	metrics.Global().Session(req.SessionID).AddTokens(req.Tokens)
	writeJSON(w, map[string]any{
		"status":     "ok",
		"session_id": req.SessionID,
		"tokens":     req.Tokens,
	})
}

// POST /v1/internal/task/claim
// Body: {"session_id":"...", "evidence_supported":true, "unsupported":false}
//
// Records a single agent claim.  evidence_supported=true counts towards
// 3-MT5; unsupported=true (hallucination) counts towards 3-MT7.
func (g *Gateway) handleTaskClaim(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		SessionID         string `json:"session_id"`
		EvidenceSupported bool   `json:"evidence_supported"`
		Unsupported       bool   `json:"unsupported"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	mc := metrics.Global()
	mc.RecordEvidenceSupported(req.EvidenceSupported)
	if req.Unsupported {
		mc.RecordUnsupportedClaim()
	}
	if req.SessionID != "" {
		tr := mc.Session(req.SessionID)
		tr.RecordQuery(req.EvidenceSupported)
		if req.Unsupported {
			tr.RecordUnsupportedClaim()
		}
	}
	writeJSON(w, map[string]any{"status": "ok"})
}

// ── Plan Step & Repair API ────────────────────────────────────────────────────

// POST /v1/internal/plan/step
// Body: {"session_id":"...", "step_description":"...", "step_index":1}
//
// Records that a plan step has been executed.  Increments the session step
// counter (3-MT2).
func (g *Gateway) handlePlanStep(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		SessionID       string `json:"session_id"`
		StepDescription string `json:"step_description"`
		StepIndex       int    `json:"step_index"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.SessionID != "" {
		metrics.Global().Session(req.SessionID).AddStep()
	}
	writeJSON(w, map[string]any{
		"status":     "ok",
		"session_id": req.SessionID,
		"step_index": req.StepIndex,
	})
}

// POST /v1/internal/plan/repair
// Body: {"session_id":"...", "success":true, "reason":"..."}
//
// Records a plan repair attempt.  Updates both the global PlanRepair counters
// (3-MT6) and the per-session tracker.
func (g *Gateway) handlePlanRepair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		SessionID string `json:"session_id"`
		Success   bool   `json:"success"`
		Reason    string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	metrics.Global().RecordPlanRepair(req.Success)
	if req.SessionID != "" {
		metrics.Global().Session(req.SessionID).RecordPlanRepair(req.Success)
	}
	writeJSON(w, map[string]any{
		"status":  "ok",
		"success": req.Success,
	})
}

// ── /v1/ingest/document ───────────────────────────────────────────────────────
//
// POST /v1/ingest/document
// Body: {
//   "agent_id":     "...",
//   "session_id":   "...",
//   "workspace_id": "...",
//   "title":        "...",
//   "text":         "<full document text>",
//   "chunk_size":   512,    // chars per chunk, default 1000
//   "overlap":      50,     // overlap chars between chunks, default 0
//   "importance":   0.6
// }
//
// Splits the document into chunks and ingests each as an episodic memory
// event.  All chunks share the same ImportBatchID so they can be queried
// as a unit.  Returns the batch_id and per-chunk memory_ids.
//
// Covers 3-T1 (long-document analysis).
func (g *Gateway) handleIngestDocument(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		AgentID     string  `json:"agent_id"`
		SessionID   string  `json:"session_id"`
		WorkspaceID string  `json:"workspace_id"`
		Title       string  `json:"title"`
		Text        string  `json:"text"`
		ChunkSize   int     `json:"chunk_size"`
		Overlap     int     `json:"overlap"`
		Importance  float64 `json:"importance"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		http.Error(w, "text is required", http.StatusBadRequest)
		return
	}
	if req.ChunkSize <= 0 {
		req.ChunkSize = 1000
	}
	if req.Overlap < 0 {
		req.Overlap = 0
	}
	if req.Importance <= 0 {
		req.Importance = 0.5
	}

	chunks := splitTextIntoChunks(req.Text, req.ChunkSize, req.Overlap)
	batchID := generateObjectID("batch")
	now := time.Now().UTC().Format(time.RFC3339)
	memoryIDs := make([]string, 0, len(chunks))
	errs := []string{}

	for i, chunk := range chunks {
		evID := generateObjectID("evt")
		title := req.Title
		if title == "" {
			title = fmt.Sprintf("chunk_%d", i)
		} else {
			title = fmt.Sprintf("%s_chunk_%d", title, i)
		}
		ev := schemas.Event{
			EventID:     evID,
			AgentID:     req.AgentID,
			SessionID:   req.SessionID,
			WorkspaceID: req.WorkspaceID,
			EventType:   string(schemas.EventTypeMemoryWriteRequested),
			EventTime:   now,
			Importance:  req.Importance,
			Payload: map[string]any{
				"text":          chunk,
				"title":         title,
				"chunk_index":   i,
				"chunk_total":   len(chunks),
				"import_batch":  batchID,
			},
		}
		ack, err := g.runtime.SubmitIngest(ev)
		if err != nil {
			errs = append(errs, fmt.Sprintf("chunk_%d: %v", i, err))
			continue
		}
		if mid, ok := ack["memory_id"].(string); ok {
			memoryIDs = append(memoryIDs, mid)
		}
	}

	result := map[string]any{
		"status":     "ok",
		"batch_id":   batchID,
		"chunks":     len(chunks),
		"memory_ids": memoryIDs,
	}
	if len(errs) > 0 {
		result["errors"] = errs
		result["status"] = "partial"
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// splitTextIntoChunks divides text into chunks of at most chunkSize runes,
// with optional overlap.  Splits prefer paragraph / sentence boundaries when
// they fall within the last 20% of the chunk window.
func splitTextIntoChunks(text string, chunkSize, overlap int) []string {
	runes := []rune(text)
	total := len(runes)
	if total == 0 {
		return nil
	}
	if chunkSize >= total {
		return []string{string(runes)}
	}
	var chunks []string
	start := 0
	for start < total {
		end := start + chunkSize
		if end > total {
			end = total
		}
		// Try to split on a paragraph boundary in the last 20% of the window.
		window := end - start
		searchFrom := start + window*4/5
		splitAt := -1
		for i := end - 1; i >= searchFrom; i-- {
			if runes[i] == '\n' {
				splitAt = i + 1
				break
			}
		}
		if splitAt < 0 || splitAt <= start {
			// Fall back to period / space boundary.
			for i := end - 1; i >= searchFrom; i-- {
				if runes[i] == '.' || runes[i] == ' ' {
					splitAt = i + 1
					break
				}
			}
		}
		if splitAt > start {
			end = splitAt
		}
		chunks = append(chunks, string(runes[start:end]))
		nextStart := end - overlap
		if nextStart <= start {
			nextStart = end
		}
		start = nextStart
	}
	return chunks
}

// ── /v1/internal/task/stage ───────────────────────────────────────────────────
//
// POST /v1/internal/task/stage
// Body: {"session_id":"...", "stage":"outline"|"draft"|"review"|"final",
//        "stage_index":2, "total_stages":4, "description":"..."}
//
// Records the current stage of a multi-stage report generation task (3-T3).
// Each stage call is stored as a memory event so the stage progression is
// queryable and visible in the retrieval plane.
func (g *Gateway) handleTaskStage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		SessionID   string `json:"session_id"`
		AgentID     string `json:"agent_id"`
		Stage       string `json:"stage"`
		StageIndex  int    `json:"stage_index"`
		TotalStages int    `json:"total_stages"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.SessionID == "" || req.Stage == "" {
		http.Error(w, "session_id and stage are required", http.StatusBadRequest)
		return
	}
	// Count this as a plan step.
	metrics.Global().Session(req.SessionID).AddStep()

	// Persist stage milestone as a memory-write event for provenance.
	evID := generateObjectID("stage")
	ev := schemas.Event{
		EventID:   evID,
		AgentID:   req.AgentID,
		SessionID: req.SessionID,
		EventType: string(schemas.EventTypePlanUpdated),
		EventTime: time.Now().UTC().Format(time.RFC3339),
		Payload: map[string]any{
			"stage":        req.Stage,
			"stage_index":  req.StageIndex,
			"total_stages": req.TotalStages,
			"description":  req.Description,
		},
	}
	ack, err := g.runtime.SubmitIngest(ev)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"status":      "ok",
		"session_id":  req.SessionID,
		"stage":       req.Stage,
		"stage_index": req.StageIndex,
		"event_id":    ack["event_id"],
		"memory_id":   ack["memory_id"],
	})
}

// ── /v1/internal/mas/answer-consistency ──────────────────────────────────────
//
// POST /v1/internal/mas/answer-consistency
// Body: {"score": 0.85, "session_id":"...", "agent_id":"..."}
//
// Records a [0,1] answer-consistency score for the final answer produced by
// one agent in a MAS run.  The global accumulator computes the mean over all
// submitted scores (4-M5).
func (g *Gateway) handleMASAnswerConsistency(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Score     float64 `json:"score"`
		SessionID string  `json:"session_id"`
		AgentID   string  `json:"agent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Score < 0 || req.Score > 1 {
		http.Error(w, "score must be in [0, 1]", http.StatusBadRequest)
		return
	}
	metrics.Global().RecordAnswerConsistency(req.Score)
	writeJSON(w, map[string]any{
		"status": "ok",
		"score":  req.Score,
	})
}

// ── /v1/internal/mas/aggregate ────────────────────────────────────────────────
//
// POST /v1/internal/mas/aggregate
// Body: {
//   "requester_agent_id": "agent-A",
//   "source_agent_ids":   ["agent-B", "agent-C"],
//   "query":              "recent findings on topic X",
//   "top_k":              5
// }
//
// Aggregates memories contributed by multiple agents into a single result
// list visible to the requester.  Only memories explicitly shared (via a
// ShareContract with ReadACL matching the requester) are included — ensuring
// private memories are never leaked (4-T2, 4-D3).
//
// Returns the aggregated object IDs together with per-source attribution.
func (g *Gateway) handleMASAggregate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		RequesterAgentID string   `json:"requester_agent_id"`
		SourceAgentIDs   []string `json:"source_agent_ids"`
		Query            string   `json:"query"`
		TopK             int      `json:"top_k"`
		SessionID        string   `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.RequesterAgentID == "" {
		http.Error(w, "requester_agent_id is required", http.StatusBadRequest)
		return
	}
	if req.TopK <= 0 {
		req.TopK = 5
	}

	// Build allowed scope set from share contracts.
	contracts := g.store.Contracts().ListContracts()
	allowedScopes := make(map[string]bool)
	for _, c := range contracts {
		if c.ReadACL == req.RequesterAgentID || c.ReadACL == "*" {
			allowedScopes[c.Scope] = true
		}
	}

	type sourceResult struct {
		AgentID   string   `json:"agent_id"`
		ObjectIDs []string `json:"object_ids"`
	}
	results := make([]sourceResult, 0)
	seenIDs := make(map[string]bool)
	contaminationCount := 0

	for _, srcAgentID := range req.SourceAgentIDs {
		if srcAgentID == req.RequesterAgentID {
			continue
		}
		mems := g.store.Objects().ListMemories(srcAgentID, "")
		var ids []string
		for _, m := range mems {
			if seenIDs[m.MemoryID] {
				continue
			}
			ownerScope := m.Scope
			if ownerScope == "" {
				ownerScope = m.AgentID
			}
			if !allowedScopes[ownerScope] {
				contaminationCount++
				metrics.Global().RecordContaminationAttempt()
				continue
			}
			ids = append(ids, m.MemoryID)
			seenIDs[m.MemoryID] = true
			if len(ids) >= req.TopK {
				break
			}
		}
		if len(ids) > 0 {
			results = append(results, sourceResult{AgentID: srcAgentID, ObjectIDs: ids})
		}
	}

	// Collect all shared IDs.
	allIDs := make([]string, 0)
	for _, sr := range results {
		allIDs = append(allIDs, sr.ObjectIDs...)
	}

	writeJSON(w, map[string]any{
		"status":                "ok",
		"requester_agent_id":    req.RequesterAgentID,
		"aggregated_object_ids": allIDs,
		"by_source":             results,
		"contamination_blocked": contaminationCount,
	})
}

// ── /v1/internal/tool-state ───────────────────────────────────────────────────
//
// GET /v1/internal/tool-state?agent_id=...&session_id=...
//
// Returns the current stateful tool-interaction context for a session (3-T4).
// Scans the last N tool_call_issued / tool_result_returned events and pairs
// them up so callers can determine which tool calls are still pending (no
// matching result) and which have completed.
//
// Response: {
//   "pending":   [{"event_id":"...", "tool":"...", "args":{...}}],
//   "completed": [{"call_event_id":"...", "result_event_id":"...", "tool":"...", "result":{...}}]
// }
func (g *Gateway) handleToolState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	agentID := r.URL.Query().Get("agent_id")
	sessionID := r.URL.Query().Get("session_id")

	events := g.store.Objects().ListEvents(agentID, sessionID)

	type pendingCall struct {
		EventID string         `json:"event_id"`
		Tool    string         `json:"tool"`
		Args    map[string]any `json:"args"`
	}
	type completedCall struct {
		CallEventID   string         `json:"call_event_id"`
		ResultEventID string         `json:"result_event_id"`
		Tool          string         `json:"tool"`
		Result        map[string]any `json:"result"`
	}

	callsByID := make(map[string]schemas.Event)
	var resultEvents []schemas.Event

	for _, ev := range events {
		switch ev.EventType {
		case string(schemas.EventTypeToolCallIssued), string(schemas.EventTypeToolCall):
			callsByID[ev.EventID] = ev
		case string(schemas.EventTypeToolResultReturned), string(schemas.EventTypeToolResult):
			resultEvents = append(resultEvents, ev)
		}
	}

	var pending []pendingCall
	completed := make([]completedCall, 0)
	matchedCalls := make(map[string]bool)

	for _, res := range resultEvents {
		parentID, _ := res.Payload["call_event_id"].(string)
		if parentID == "" {
			parentID, _ = res.Payload["parent_event_id"].(string)
		}
		if parentID == "" {
			parentID = res.ParentEventID
		}
		if call, ok := callsByID[parentID]; ok {
			matchedCalls[parentID] = true
			tool, _ := call.Payload["tool"].(string)
			result, _ := res.Payload["result"].(map[string]any)
			completed = append(completed, completedCall{
				CallEventID:   parentID,
				ResultEventID: res.EventID,
				Tool:          tool,
				Result:        result,
			})
		}
	}

	for id, call := range callsByID {
		if matchedCalls[id] {
			continue
		}
		tool, _ := call.Payload["tool"].(string)
		args, _ := call.Payload["args"].(map[string]any)
		pending = append(pending, pendingCall{
			EventID: id,
			Tool:    tool,
			Args:    args,
		})
	}

	writeJSON(w, map[string]any{
		"agent_id":   agentID,
		"session_id": sessionID,
		"pending":    pending,
		"completed":  completed,
	})
}

// ── /v1/internal/agent/handoff ────────────────────────────────────────────────
//
// POST /v1/internal/agent/handoff
// Body: {
//   "from_agent_id": "planner-agent",
//   "to_agent_id":   "executor-agent",
//   "session_id":    "...",
//   "role_from":     "planner",
//   "role_to":       "executor",
//   "context":       {...}   // arbitrary payload forwarded to the next agent
// }
//
// Records a role handoff between agents in a MAS run (4-T1: planner →
// executor → critic triangle).  A HandoffOccurred event is written to the
// store so that handoff frequency and latency can be computed from the event
// stream.  The receiving agent's RoleProfile is also updated if role_to is
// provided.
func (g *Gateway) handleAgentHandoff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		FromAgentID string         `json:"from_agent_id"`
		ToAgentID   string         `json:"to_agent_id"`
		SessionID   string         `json:"session_id"`
		RoleFrom    string         `json:"role_from"`
		RoleTo      string         `json:"role_to"`
		Context     map[string]any `json:"context"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.FromAgentID == "" || req.ToAgentID == "" {
		http.Error(w, "from_agent_id and to_agent_id are required", http.StatusBadRequest)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// Update receiver agent RoleProfile if provided.
	if req.RoleTo != "" {
		if agent, ok := g.store.Objects().GetAgent(req.ToAgentID); ok {
			agent.RoleProfile = req.RoleTo
			g.store.Objects().PutAgent(agent)
		}
	}
	if req.RoleFrom != "" {
		if agent, ok := g.store.Objects().GetAgent(req.FromAgentID); ok {
			agent.RoleProfile = req.RoleFrom
			g.store.Objects().PutAgent(agent)
		}
	}

	// Persist as a HandoffOccurred event for provenance & latency tracking.
	payload := map[string]any{
		"from_agent_id": req.FromAgentID,
		"to_agent_id":   req.ToAgentID,
		"role_from":     req.RoleFrom,
		"role_to":       req.RoleTo,
	}
	for k, v := range req.Context {
		payload[k] = v
	}
	evID := generateObjectID("handoff")
	ev := schemas.Event{
		EventID:   evID,
		AgentID:   req.FromAgentID,
		SessionID: req.SessionID,
		EventType: string(schemas.EventTypeHandoffOccurred),
		EventTime: now,
		Payload:   payload,
	}
	ack, err := g.runtime.SubmitIngest(ev)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"status":        "ok",
		"event_id":      ack["event_id"],
		"from_agent_id": req.FromAgentID,
		"to_agent_id":   req.ToAgentID,
		"role_from":     req.RoleFrom,
		"role_to":       req.RoleTo,
		"timestamp":     now,
	})
}

// ── GET /v1/agent/list ────────────────────────────────────────────────────────
//
// Query params: role=planner, workspace_id=..., tenant_id=...
//
// Returns agents filtered by the supplied query params.  Supports role-based
// lookup needed by the 4-T1 planner/executor/critic architecture.
func (g *Gateway) handleAgentList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	roleFilter := r.URL.Query().Get("role")
	workspaceFilter := r.URL.Query().Get("workspace_id")
	tenantFilter := r.URL.Query().Get("tenant_id")

	agents := g.store.Objects().ListAgents()
	out := agents[:0]
	for _, a := range agents {
		if roleFilter != "" && a.RoleProfile != roleFilter {
			continue
		}
		if workspaceFilter != "" && a.WorkspaceID != workspaceFilter {
			continue
		}
		if tenantFilter != "" && a.TenantID != tenantFilter {
			continue
		}
		out = append(out, a)
	}
	writeJSON(w, out)
}

// ── GET /v1/internal/session/context ─────────────────────────────────────────
//
// Query params: session_id=..., agent_id=..., last_n=20
//
// Aggregates the recent interaction context for a session: the last N
// user/assistant messages, tool calls/results, memory-write events, and
// plan updates.  Useful for multi-round Q&A agents (3-T2) that need a
// compact view of what happened earlier in the session without re-querying
// the full event stream.
//
// Response:
// {
//   "session":  {...},
//   "events":   [...],    // last_n events sorted by event_time desc
//   "memories": [...],    // memories linked to this session
//   "turns":    N         // total event count in session
// }
func (g *Gateway) handleSessionContext(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sessionID := r.URL.Query().Get("session_id")
	agentID := r.URL.Query().Get("agent_id")
	lastN := 20
	if v := r.URL.Query().Get("last_n"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			lastN = n
		}
	}
	if sessionID == "" {
		http.Error(w, "session_id is required", http.StatusBadRequest)
		return
	}

	sess, _ := g.store.Objects().GetSession(sessionID)
	allEvents := g.store.Objects().ListEvents(agentID, sessionID)

	// Filter to interaction-relevant event types.
	relevantTypes := map[string]bool{
		string(schemas.EventTypeUserMessage):        true,
		string(schemas.EventTypeAssistantMessage):   true,
		string(schemas.EventTypeToolCallIssued):     true,
		string(schemas.EventTypeToolCall):           true,
		string(schemas.EventTypeToolResultReturned): true,
		string(schemas.EventTypeToolResult):         true,
		string(schemas.EventTypePlanUpdated):        true,
		string(schemas.EventTypeMemoryWriteRequested): true,
		string(schemas.EventTypeMemoryConsolidated): true,
	}
	var filtered []schemas.Event
	for _, ev := range allEvents {
		if relevantTypes[ev.EventType] {
			filtered = append(filtered, ev)
		}
	}

	// Return last N (keep tail).
	if len(filtered) > lastN {
		filtered = filtered[len(filtered)-lastN:]
	}

	// Linked memories (same agentID + sessionID).
	mems := g.store.Objects().ListMemories(agentID, sessionID)

	writeJSON(w, map[string]any{
		"session":  sess,
		"events":   filtered,
		"memories": mems,
		"turns":    len(allEvents),
	})
}

// ── /v1/internal/eval/ground-truth ───────────────────────────────────────────
//
// In-process ground-truth store keyed by task_id.  Allows eval harnesses to
// register expected answers before running agents and retrieve them
// afterwards for scoring.
//
// POST — register ground truth:
//   Body: {"task_id":"...", "expected":"...", "metadata":{...}}
// GET  — retrieve ground truth:
//   Query: ?task_id=...   (omit to list all)

var (
	groundTruthMu   sync.RWMutex
	groundTruthStore = make(map[string]groundTruthRecord)
)

type groundTruthRecord struct {
	TaskID   string         `json:"task_id"`
	Expected string         `json:"expected"`
	Metadata map[string]any `json:"metadata,omitempty"`
	CreatedAt string        `json:"created_at"`
}

func (g *Gateway) handleEvalGroundTruth(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var rec groundTruthRecord
		if err := json.NewDecoder(r.Body).Decode(&rec); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if rec.TaskID == "" {
			http.Error(w, "task_id is required", http.StatusBadRequest)
			return
		}
		rec.CreatedAt = time.Now().UTC().Format(time.RFC3339)
		groundTruthMu.Lock()
		groundTruthStore[rec.TaskID] = rec
		groundTruthMu.Unlock()
		writeJSON(w, map[string]any{"status": "ok", "task_id": rec.TaskID})

	case http.MethodGet:
		taskID := r.URL.Query().Get("task_id")
		groundTruthMu.RLock()
		defer groundTruthMu.RUnlock()
		if taskID != "" {
			rec, ok := groundTruthStore[taskID]
			if !ok {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			writeJSON(w, rec)
			return
		}
		all := make([]groundTruthRecord, 0, len(groundTruthStore))
		for _, rec := range groundTruthStore {
			all = append(all, rec)
		}
		writeJSON(w, all)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
