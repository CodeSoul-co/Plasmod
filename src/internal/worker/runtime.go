package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"andb/src/internal/coordinator"
	"andb/src/internal/dataplane"
	"andb/src/internal/eventbackbone"
	"andb/src/internal/evidence"
	"andb/src/internal/materialization"
	"andb/src/internal/schemas"
	"andb/src/internal/semantic"
	"andb/src/internal/storage"
	"time"
	"andb/src/internal/worker/chain"
	"andb/src/internal/worker/nodes"
)

type Runtime struct {
	wal               eventbackbone.WAL
	bus               eventbackbone.Bus
	plane             dataplane.DataPlane
	coord             *coordinator.Hub
	policy            *semantic.PolicyEngine
	planner           semantic.QueryPlanner
	materializer      *materialization.Service
	preCompute        *materialization.PreComputeService
	assembler         *evidence.Assembler
	evCache           *evidence.Cache
	derivationLog     *eventbackbone.DerivationLog
	policyDecisionLog *eventbackbone.PolicyDecisionLog
	nodeManager       *nodes.Manager
	storage           storage.RuntimeStorage
	tieredObjects     *storage.TieredObjectStore
	queryChain        *chain.QueryChain
	// lastMem tracks the most-recent memory ID per "agentID:sessionID" so ConflictMerge
	// can fire synchronously in SubmitIngest (not only async via subscriber).
	lastMem   map[string]string
	lastMemMu sync.RWMutex
}

func CreateRuntime(
	wal eventbackbone.WAL,
	bus eventbackbone.Bus,
	plane dataplane.DataPlane,
	coord *coordinator.Hub,
	policy *semantic.PolicyEngine,
	planner semantic.QueryPlanner,
	materializer *materialization.Service,
	preCompute *materialization.PreComputeService,
	assembler *evidence.Assembler,
	evCache *evidence.Cache,
	derivationLog *eventbackbone.DerivationLog,
	policyDecisionLog *eventbackbone.PolicyDecisionLog,
	nodeManager *nodes.Manager,
	store storage.RuntimeStorage,
	tieredObjs *storage.TieredObjectStore,
) *Runtime {
	return &Runtime{
		wal:               wal,
		bus:               bus,
		plane:             plane,
		coord:             coord,
		policy:            policy,
		planner:           planner,
		materializer:      materializer,
		preCompute:        preCompute,
		assembler:         assembler,
		evCache:           evCache,
		derivationLog:     derivationLog,
		policyDecisionLog: policyDecisionLog,
		nodeManager:       nodeManager,
		storage:           store,
		tieredObjects:     tieredObjs,
		queryChain:        chain.CreateQueryChain(nodeManager),
		lastMem:           make(map[string]string),
	}
}

func (r *Runtime) RegisterDefaults() {
	_ = r.bus.Subscribe("wal.events")
}

// QueryChain returns the post-retrieval reasoning chain (ProofTrace + Subgraph).
// It is nil if the Runtime was constructed without a nodeManager.
func (r *Runtime) QueryChain() *chain.QueryChain {
	return r.queryChain
}

// StartSubscriber launches the EventSubscriber's poll loop as a background
// goroutine tied to ctx.  The goroutine exits cleanly when ctx is cancelled.
func (r *Runtime) StartSubscriber(ctx context.Context, sub *EventSubscriber) {
	go sub.Run(ctx)
}

func (r *Runtime) SubmitIngest(ev schemas.Event) (map[string]any, error) {
	if strings.TrimSpace(ev.EventID) == "" {
		return nil, errors.New("event_id is required")
	}
	// IngestWorker validation: runs all registered IngestWorkers before WAL
	// append so malformed events are rejected before touching durable state.
	if err := r.nodeManager.DispatchIngestValidation(ev); err != nil {
		return nil, err
	}
	if err := validateEmbeddingIngestPayload(ev); err != nil {
		return nil, err
	}
	entry, err := r.wal.Append(ev)
	if err != nil {
		return nil, err
	}
	if ev.LogicalTS == 0 {
		ev.LogicalTS = entry.LSN
	}
	mat := r.materializer.MaterializeEvent(ev)
	record := mat.Record
	if record.Attributes == nil {
		record.Attributes = map[string]string{}
	}
	record.Attributes["embedding_family"] = storage.ResolveEmbeddingFamily(record.Attributes)
	if dim := currentEmbeddingDim(); dim > 0 {
		record.Attributes["embedding_dim"] = strconv.Itoa(dim)
	}

	// Fail fast on retrieval-plane ingest before mutating canonical stores.
	// This reduces partial-success windows (WAL only) where object writes succeed
	// but retrieval ingest fails and query surfaces inconsistent state.
	if err := r.plane.Ingest(record); err != nil {
		return nil, err
	}

	// ── Synchronous object materialization ─────────────────────────────────
	// State and Artifact objects are needed immediately for query correctness.
	// Call the materialization workers here (not only in the async subscriber)
	// so tests and synchronous query paths can read them without waiting for
	// the next WAL poll cycle.
	//
	// Routing:
	//   - Artifact (tool_call/tool_result) → ObjectMaterializationWorker
	//   - State    (state_update/state_change/checkpoint) → StateMaterializationWorker
	//     (NOTE: State is NOT handled by ObjectMaterializationWorker to avoid
	//     creating duplicate State objects with different field values for the
	//     same event. StateMaterializationWorker stores via PutState directly.)
	//   - Memory   → stored directly below via tieredObjects.PutMemory (richer
	//     MaterializeEvent output), not via ObjectMaterializationWorker.
	r.nodeManager.DispatchObjectMaterialization(ev)
	r.nodeManager.DispatchToolTrace(ev)

	// State objects for ALL state events are created synchronously so they are
	// immediately queryable.  The async subscriber's StateMaterialization handler
	// handles the same events (creating a second State record with a different
	// version number), which is intentional — the VersionStore accumulates
	// snapshots rather than overwriting.
	isStateEvent := ev.EventType == string(schemas.EventTypeStateUpdate) ||
		ev.EventType == string(schemas.EventTypeStateChange) ||
		ev.EventType == string(schemas.EventTypeCheckpoint)
	if isStateEvent && ev.AgentID != "" && ev.SessionID != "" {
		r.nodeManager.DispatchStateMaterialization(ev)
		// checkpoint events additionally snapshot all current states.
		if ev.EventType == string(schemas.EventTypeCheckpoint) {
			r.nodeManager.DispatchStateCheckpoint(ev.AgentID, ev.SessionID)
		}
	}

	// ── Persist canonical objects ─────────────────────────────────────────
	// Route Memory writes through TieredObjectStore so the hot/warm/cold tiers
	// are kept in sync and cold queries (IncludeCold=true) can find results.
	if r.tieredObjects != nil {
		// Compute salience from the event importance if available, default 0.5.
		salience := mat.Memory.Importance
		if salience <= 0 {
			salience = 0.5
		}
		r.tieredObjects.PutMemory(mat.Memory, salience)
		r.tieredObjects.ArchiveColdRecord(
			mat.Memory.MemoryID,
			record.Text,
			record.Attributes,
			record.Namespace,
			record.EventUnixTS,
		)
	} else {
		// Fallback for tests or code paths that don't initialise TieredObjectStore.
		r.storage.PutMemoryWithBaseEdges(mat.Memory)
	}
	r.storage.Versions().PutVersion(mat.Version)
	for _, edge := range mat.Edges {
		r.storage.Edges().PutEdge(edge)
	}

	// ── Synchronous ConflictMerge ──────────────────────────────────────────
	// Detect and resolve same-session memory conflicts immediately after the new
	// memory is stored.  The async subscriber also fires ConflictMerge (for
	// cross-event races); this synchronous pass ensures the conflict_resolved
	// edge is present before SubmitIngest returns — critical for test queries
	// and any caller that reads edges immediately after ingest.
	if mat.Memory.AgentID != "" && mat.Memory.SessionID != "" && mat.Memory.MemoryType == string(schemas.MemoryTypeEpisodic) {
		key := mat.Memory.AgentID + ":" + mat.Memory.SessionID
		r.lastMemMu.RLock()
		prevID, hasPrev := r.lastMem[key]
		r.lastMemMu.RUnlock()
		if hasPrev {
			r.nodeManager.DispatchConflictMerge(mat.Memory.MemoryID, prevID, string(schemas.ObjectTypeMemory))
		}
		r.lastMemMu.Lock()
		r.lastMem[key] = mat.Memory.MemoryID
		r.lastMemMu.Unlock()
	}

	// ── Pre-compute evidence fragment ─────────────────────────────────────
	if r.preCompute != nil {
		frag := r.preCompute.Compute(ev, record)
		if frag.SalienceScore >= 0.5 {
			r.storage.HotCache().Put(record.ObjectID, ev.EventType, record, frag.SalienceScore)
		}
	}

	// ── Retrieval plane ───────────────────────────────────────────────────
	r.nodeManager.DispatchIngest(record)
	// Build vector index after ingest so VectorStore.Ready() returns true.
	// This enables vector search on subsequent queries.
	if err := r.plane.Flush(); err != nil {
		return nil, err
	}
	return map[string]any{
		"status":    "accepted",
		"lsn":       entry.LSN,
		"event_id":  ev.EventID,
		"memory_id": mat.Memory.MemoryID,
		"edges":     len(mat.Edges),
	}, nil
}

func (r *Runtime) ExecuteQuery(req schemas.QueryRequest) schemas.QueryResponse {
	if reject, reason := shouldRejectByEmbeddingRoute(req); reject {
		return schemas.QueryResponse{
			Objects:           []string{},
			AppliedFilters:    []string{reason},
			RouteRejected:     true,
			RouteRejectReason: reason,
			ChainTraces: schemas.ChainTraceSlots{
				Main:           []string{"query_route_rejected=true"},
				MemoryPipeline: []string{},
				Query:          []string{reason},
				Collaboration:  []string{},
			},
		}
	}
	plan := r.planner.Build(req)
	searchInput := dataplane.SearchInput{
		QueryText:      req.QueryText,
		TopK:           plan.TopK,
		Namespace:      plan.Namespace,
		Constraints:    plan.Constraints,
		TimeFromUnixTS: plan.TimeFromUnixTS,
		TimeToUnixTS:   plan.TimeToUnixTS,
		IncludeGrowing: plan.IncludeGrowing,
		IncludeCold:    plan.IncludeCold,
		ObjectTypes:    plan.ObjectTypes,
		MemoryTypes:    plan.MemoryTypes,
	}
	result := r.nodeManager.DispatchQuery(searchInput, r.plane)
	result.ObjectIDs = semantic.FilterObjectIDsByTypes(result.ObjectIDs, plan.ObjectTypes)
	result.ObjectIDs = r.filterInactiveMemories(result.ObjectIDs)

	// ── Canonical-object supplemental retrieval ──────────────────────────────
	// State and Artifact objects are stored directly in ObjectStore, not in the
	// retrieval plane.  When query requests these types, fetch them from the
	// canonical store so they appear in the response alongside memory results.
	canonicalIDs := r.fetchCanonicalObjects(plan.ObjectTypes, req.AgentID, req.SessionID, plan.Namespace)
	result.ObjectIDs = append(result.ObjectIDs, canonicalIDs...)
	result.ObjectIDs = r.filterInactiveMemories(result.ObjectIDs)

	filters := r.policy.ApplyQueryFilters(req)
	resp := r.assembler.Build(searchInput, result, filters)

	resp.ChainTraces.Main = formatQueryPathMainChainLines(req, result)
	resp.ChainTraces.MemoryPipeline = formatQueryPathMemoryPipelineLines(r.storage, result.ObjectIDs)

	// ── Post-retrieval reasoning via QueryChain ───────────────────────────────
	// QueryChain handles:
	//   1. Pre-fetching Memory objects as GraphNodes for node population.
	//   2. Pre-fetching BulkEdges for edge pre-population.
	//   3. Multi-hop BFS proof trace via ProofTraceWorker.
	//   4. Subgraph expansion via SubgraphExecutorWorker.
	//   5. Merging subgraph edges with the assembler's edges (deduplicated).
	if len(result.ObjectIDs) > 0 {
		chainOut, chainResult := r.queryChain.Run(chain.QueryChainInput{
			ObjectIDs:   result.ObjectIDs,
			MaxDepth:    0, // default cap of 8
			ObjectStore: r.storage.Objects(),
			EdgeStore:   r.storage.Edges(),
		})

		if len(chainOut.ProofTrace) > 0 {
			resp.ProofTrace = append(resp.ProofTrace, chainOut.ProofTrace...)
		}

		if len(chainOut.Subgraph.Nodes) > 0 {
			resp.Nodes = chainOut.Subgraph.Nodes
		}

		// Merge subgraph-expanded edges into resp.Edges, deduplicating by EdgeID.
		if len(chainOut.MergedEdges) > 0 {
			seen := make(map[string]bool, len(resp.Edges))
			for _, e := range resp.Edges {
				seen[e.EdgeID] = true
			}
			for _, e := range chainOut.MergedEdges {
				if !seen[e.EdgeID] {
					resp.Edges = append(resp.Edges, e)
					seen[e.EdgeID] = true
				}
			}
		}
		resp.ChainTraces.Query = formatQueryChainTraceLines(chainResult, chainOut)
	} else {
		resp.ChainTraces.Query = []string{"query_chain skipped=no_seed_object_ids"}
	}

	resp.ChainTraces.Collaboration = formatQueryPathCollaborationLines(resp.Edges)
	resp = r.attachEmbeddingProvenance(resp, req, result.ObjectIDs)

	return resp
}

func (r *Runtime) filterInactiveMemories(ids []string) []string {
	if r.storage == nil {
		return ids
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if strings.HasPrefix(id, schemas.IDPrefixMemory) || strings.HasPrefix(id, schemas.IDPrefixSummary) || strings.HasPrefix(id, schemas.IDPrefixShared) {
			if mem, ok := r.storage.Objects().GetMemory(id); ok && !mem.IsActive {
				continue
			}
		}
		out = append(out, id)
	}
	return out
}

func currentEmbeddingDim() int {
	if dimStr := strings.TrimSpace(os.Getenv("ANDB_EMBEDDER_DIM")); dimStr != "" {
		if dim, err := strconv.Atoi(dimStr); err == nil && dim > 0 {
			return dim
		}
	}
	embedder := strings.TrimSpace(os.Getenv("ANDB_EMBEDDER"))
	if embedder == "" || embedder == "tfidf" {
		return dataplane.DefaultEmbeddingDim
	}
	return 0
}

func shouldRejectByEmbeddingRoute(req schemas.QueryRequest) (bool, string) {
	currFamily := storage.ResolveEmbeddingFamily(nil)
	if req.TargetEmbeddingFamily != "" && req.TargetEmbeddingFamily != currFamily {
		return true, fmt.Sprintf("embedding_family_mismatch requested=%s runtime=%s", req.TargetEmbeddingFamily, currFamily)
	}
	if req.TargetDim > 0 {
		currDim := currentEmbeddingDim()
		if currDim <= 0 {
			return true, fmt.Sprintf("embedding_dim_unknown requested=%d runtime=unknown", req.TargetDim)
		}
		if req.TargetDim != currDim {
			return true, fmt.Sprintf("embedding_dim_mismatch requested=%d runtime=%d", req.TargetDim, currDim)
		}
	}
	return false, ""
}

func (r *Runtime) attachEmbeddingProvenance(
	resp schemas.QueryResponse,
	req schemas.QueryRequest,
	currentIDs []string,
) schemas.QueryResponse {
	currFamily := storage.ResolveEmbeddingFamily(nil)
	currDim := currentEmbeddingDim()

	// Always stamp runtime embedding route info for downstream auditing.
	resp.Provenance = append(resp.Provenance,
		fmt.Sprintf("embedding_runtime_family=%s", currFamily),
		fmt.Sprintf("embedding_runtime_dim=%d", currDim),
	)

	// Cross-dim fusion hook (RRF at result layer): currently fuses available
	// local candidate lists and is forward-compatible with multi-runtime fanout.
	//
	// Trigger condition:
	// - No explicit hard pin to one target family/dim (route-locked requests).
	if req.TargetEmbeddingFamily == "" && req.TargetDim == 0 {
		candidateLists := [][]string{}
		if len(currentIDs) > 0 {
			candidateLists = append(candidateLists, currentIDs)
		}
		if len(candidateLists) > 0 {
			fused := rrfFuseStringLists(candidateLists, 60, req.TopK)
			if len(fused) > 0 {
				resp.Objects = fused
			}
		}
		resp.Provenance = append(resp.Provenance,
			"cross_dim_fusion=rrf_result_layer",
			fmt.Sprintf("cross_dim_candidates=%d", len(candidateLists)),
		)
	}
	return resp
}

func rrfFuseStringLists(lists [][]string, k int, topK int) []string {
	if k <= 0 {
		k = 60
	}
	scores := map[string]float64{}
	order := make([]string, 0)
	for _, ids := range lists {
		for rank, id := range ids {
			if _, ok := scores[id]; !ok {
				order = append(order, id)
			}
			scores[id] += 1.0 / float64(k+rank+1)
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		return scores[order[i]] > scores[order[j]]
	})
	if topK > 0 && len(order) > topK {
		return order[:topK]
	}
	return order
}

func validateEmbeddingIngestPayload(ev schemas.Event) error {
	if ev.Payload == nil {
		return nil
	}
	runtimeDim := currentEmbeddingDim()
	if runtimeDim <= 0 {
		return nil
	}
	if payloadDim, ok := payloadEmbeddingDim(ev.Payload); ok && payloadDim != runtimeDim {
		return fmt.Errorf("embedding_dim_mismatch payload=%d runtime=%d", payloadDim, runtimeDim)
	}
	if vecLen, ok := payloadVectorLen(ev.Payload); ok && vecLen != runtimeDim {
		return fmt.Errorf("embedding_vector_len_mismatch payload=%d runtime=%d", vecLen, runtimeDim)
	}
	return nil
}

func payloadEmbeddingDim(payload map[string]any) (int, bool) {
	raw, ok := payload["embedding_dim"]
	if !ok {
		raw, ok = payload["dim"]
		if !ok {
			return 0, false
		}
	}
	switch v := raw.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n, true
		}
	}
	return 0, false
}

func payloadVectorLen(payload map[string]any) (int, bool) {
	raw, ok := payload["embedding"]
	if !ok {
		raw, ok = payload["vector"]
		if !ok {
			return 0, false
		}
	}
	switch v := raw.(type) {
	case []float32:
		return len(v), true
	case []float64:
		return len(v), true
	case []any:
		return len(v), true
	}
	return 0, false
}

// fetchCanonicalObjects retrieves State and Artifact object IDs from the canonical
// ObjectStore for the given agent/session/namespace.  These types bypass the
// retrieval plane and are stored directly in ObjectStore by the materialization
// workers, so they must be fetched explicitly to appear in query responses.
func (r *Runtime) fetchCanonicalObjects(objectTypes []string, agentID, sessionID, namespace string) []string {
	var ids []string
	for _, t := range objectTypes {
		switch t {
		case "event":
			if r.storage != nil {
				for _, e := range r.storage.Objects().ListEvents(agentID, sessionID) {
					ids = append(ids, e.EventID)
				}
			}

		case "state":
			if r.storage != nil {
				for _, s := range r.storage.Objects().ListStates(agentID, sessionID) {
					ids = append(ids, s.StateID)
				}
			}
		case "artifact":
			if r.storage != nil {
				for _, a := range r.storage.Objects().ListArtifacts(sessionID) {
					ids = append(ids, a.ArtifactID)
				}
			}
		}
	}
	return ids
}

func (r *Runtime) Topology() map[string]any {
	return map[string]any{
		"nodes":    r.nodeManager.Topology(),
		"segments": r.storage.Segments().List(""),
		"indexes":  r.storage.Indexes().List(),
	}
}

// GetEvidenceFragment returns the pre-computed EvidenceFragment for an object ID
// from the hot cache. Returns nil if not cached.
func (r *Runtime) GetEvidenceFragment(objectID string) any {
	if r.evCache == nil {
		return nil
	}
	if frag, ok := r.evCache.Get(objectID); ok {
		return frag
	}
	return nil
}

// GetDerivationLog returns derivation chain entries for the given object ID
// from the append-only DerivationLog.
func (r *Runtime) GetDerivationLog(objectID string) []string {
	if r.derivationLog == nil {
		return nil
	}
	entries := r.derivationLog.ForDerived(objectID)
	if len(entries) == 0 {
		return nil
	}
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = fmt.Sprintf("lsn=%d op=%s src=%s(%s) → %s(%s)",
			e.LSN, e.Operation, e.SourceID, e.SourceType, e.DerivedID, e.DerivedType)
	}
	return out
}

// GetPolicyDecisions returns governance decision entries for the given object ID
// from the append-only PolicyDecisionLog.
func (r *Runtime) GetPolicyDecisions(objectID string) []string {
	if r.policyDecisionLog == nil {
		return nil
	}
	entries := r.policyDecisionLog.ForObject(objectID)
	if len(entries) == 0 {
		return nil
	}
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = fmt.Sprintf("lsn=%d decision=%s policy=%s reason=%s",
			e.LSN, e.Decision, e.PolicyID, e.Reason)
	}
	return out
}

// ─── Algorithm dispatch (used by Agent SDK internal endpoints) ─────────────────

// DispatchAlgorithm forwards an operation to all registered AlgorithmDispatchWorkers.
// Supported operations: "ingest" | "decay" | "recall" | "compress" | "summarize" | "update".
// Returns an empty output when no algorithm workers are registered.
func (r *Runtime) DispatchAlgorithm(
	operation string,
	memoryIDs []string,
	query, nowTS, agentID, sessionID string,
	signals map[string]float64,
) schemas.AlgorithmDispatchOutput {
	if r.nodeManager == nil {
		return schemas.AlgorithmDispatchOutput{Operation: operation}
	}
	return r.nodeManager.DispatchAlgorithmDispatch(operation, memoryIDs, query, nowTS, agentID, sessionID, signals)
}

// DispatchRecall combines query retrieval with algorithm-level Recall scoring.
// It builds a QueryRequest from the parameters, executes it, then passes the
// top-ranked candidate IDs to AlgorithmDispatchWorker.Recall and returns a MemoryView.
func (r *Runtime) DispatchRecall(
	query, scope string,
	topK int,
	agentID, sessionID, tenantID, workspaceID string,
) schemas.MemoryView {
	now := time.Now().UTC()
	req := schemas.QueryRequest{
		QueryText:   query,
		QueryScope:  scope,
		SessionID:   sessionID,
		AgentID:     agentID,
		TenantID:    tenantID,
		WorkspaceID: workspaceID,
		TopK:        topK,
		TimeWindow: schemas.TimeWindow{
			From: "1970-01-01T00:00:00Z",
			To:   now.Format(time.RFC3339),
		},
		ObjectTypes: []string{"memory"},
		MemoryTypes: []string{"semantic", "episodic", "procedural"},
		ResponseMode: schemas.ResponseModeStructuredEvidence,
	}

	resp := r.ExecuteQuery(req)
	visibleRefs := resp.Objects
	if len(visibleRefs) == 0 {
		return schemas.MemoryView{
			RequestID:     fmt.Sprintf("recall_%d", now.UnixNano()),
			RequesterID:   agentID,
			AgentID:       agentID,
			ResolvedScope: scope,
		}
	}

	// Forward the top candidates to algorithm-level Recall for scored re-ranking.
	algoOut := schemas.AlgorithmDispatchOutput{}
	if r.nodeManager != nil {
		algoOut = r.nodeManager.DispatchAlgorithmDispatch(
			"recall", visibleRefs, query,
			now.Format(time.RFC3339), agentID, sessionID, nil,
		)
	}

	// Use algorithm-ordered refs if available, otherwise fall back to search order.
	orderedRefs := visibleRefs
	if len(algoOut.ScoredRefs) > 0 {
		orderedRefs = algoOut.ScoredRefs
	}

	// Collect full Memory payloads for the ordered refs.
	payloads := make([]schemas.Memory, 0, len(orderedRefs))
	for _, id := range orderedRefs {
		if mem, ok := r.storage.Objects().GetMemory(id); ok {
			payloads = append(payloads, mem)
		}
	}

	var algoNotes []string
	if len(algoOut.ScoredRefs) > 0 {
		algoNotes = []string{fmt.Sprintf("algorithm_scored:%d", len(algoOut.ScoredRefs))}
	} else {
		algoNotes = []string{"search_fallback:no_algo_worker"}
	}

	// Convert ProofStep slice to string slice for MemoryView.ConstructionTrace
	proofStrs := make([]string, len(resp.ProofTrace))
	for i, step := range resp.ProofTrace {
		proofStrs[i] = fmt.Sprintf("%s:%s", step.StepType, step.SourceID)
	}

	return schemas.MemoryView{
		RequestID:         fmt.Sprintf("recall_%d", now.UnixNano()),
		RequesterID:       agentID,
		AgentID:           agentID,
		ResolvedScope:     scope,
		VisibleMemoryRefs: orderedRefs,
		Payloads:          payloads,
		ProvenanceNotes:   []string{fmt.Sprintf("search_rank:%d_algo_rank:%d", len(visibleRefs), len(orderedRefs))},
		AlgorithmNotes:    algoNotes,
		ConstructionTrace: proofStrs,
	}
}

// DispatchShare copies a memory to a target agent's namespace and fires
// CommunicationWorker for any side-effects.
func (r *Runtime) DispatchShare(fromAgentID, toAgentID, memoryID string) (string, error) {
	mem, ok := r.storage.Objects().GetMemory(memoryID)
	if !ok {
		return "", fmt.Errorf("memory not found: %s", memoryID)
	}
	if fromAgentID == toAgentID {
		return "", nil // no-op
	}
	sharedID := "shared_" + memoryID + "_to_" + toAgentID
	shared := schemas.Memory{
		MemoryID:      sharedID,
		AgentID:       toAgentID,
		SessionID:     mem.SessionID,
		OwnerType:     "shared",
		Scope:         "restricted_shared",
		MemoryType:    mem.MemoryType,
		Content:       mem.Content,
		Level:         mem.Level,
		SourceEventIDs: mem.SourceEventIDs,
		Importance:    mem.Importance,
		Confidence:    mem.Confidence,
		IsActive:      mem.IsActive,
		Version:       mem.Version,
		ValidFrom:     time.Now().UTC().Format(time.RFC3339),
		ProvenanceRef: fmt.Sprintf("shared_from:%s/%s", fromAgentID, memoryID),
	}
	r.storage.Objects().PutMemory(shared)
	if r.nodeManager != nil {
		r.nodeManager.DispatchCommunication(fromAgentID, toAgentID, memoryID)
	}
	return sharedID, nil
}

// DispatchConflictResolve resolves a memory conflict and returns the winner ID.
func (r *Runtime) DispatchConflictResolve(leftID, rightID string) string {
	if r.nodeManager != nil {
		return r.nodeManager.DispatchConflictMergeWithWinner(leftID, rightID, "memory")
	}
	return leftID
}

// Manager returns the underlying nodes.Manager. It may be nil.
func (r *Runtime) Manager() *nodes.Manager {
	return r.nodeManager
}

// formatQueryPathMainChainLines summarizes retrieval context on the read path.
// MainChain itself runs on ingest only; we do not replay it during query.
func formatQueryPathMainChainLines(req schemas.QueryRequest, result dataplane.SearchOutput) []string {
	return []string{
		"phase=query_path",
		"main_chain not_reexecuted=runs_on_ingest",
		fmt.Sprintf("retrieval_tier=%s", result.Tier),
		fmt.Sprintf("retrieved_object_ids=%d", len(result.ObjectIDs)),
		fmt.Sprintf("include_cold_requested=%t", req.IncludeCold),
	}
}

// formatQueryPathMemoryPipelineLines summarizes memory seeds from retrieval.
// MemoryPipelineChain runs on ingest / subscriber; query only reports store-backed stats.
func formatQueryPathMemoryPipelineLines(store storage.RuntimeStorage, objectIDs []string) []string {
	lines := []string{
		"phase=query_path",
		"memory_pipeline_chain not_reexecuted=runs_on_ingest_and_subscriber",
	}
	if store == nil {
		lines = append(lines, "retrieved_memory_seeds=0", "object_store=nil")
		return lines
	}
	nMem := 0
	maxLevel := 0
	nChecked := 0
	const maxLevelLookups = 64
	objs := store.Objects()
	for _, id := range objectIDs {
		if !strings.HasPrefix(id, schemas.IDPrefixMemory) {
			continue
		}
		nMem++
		if nChecked >= maxLevelLookups {
			continue
		}
		nChecked++
		if m, ok := objs.GetMemory(id); ok && m.Level > maxLevel {
			maxLevel = m.Level
		}
	}
	lines = append(lines, fmt.Sprintf("retrieved_memory_seeds=%d", nMem))
	if nMem > 0 && nChecked > 0 {
		lines = append(lines, fmt.Sprintf("sample_max_memory_level=%d", maxLevel))
	}
	if nMem > nChecked {
		lines = append(lines, fmt.Sprintf("memory_level_stats_capped=%d", maxLevelLookups))
	}
	return lines
}

// formatQueryPathCollaborationLines summarizes collaboration-related edges present
// in the assembled response (including subgraph merge). CollaborationChain runs on ingest.
func formatQueryPathCollaborationLines(edges []schemas.Edge) []string {
	lines := []string{
		"phase=query_path",
		"collaboration_chain not_reexecuted=runs_on_ingest_and_conflict_merge",
	}
	var nConflict, nSession, nAgent int
	for _, e := range edges {
		switch e.EdgeType {
		case string(schemas.EdgeTypeConflictResolved):
			nConflict++
		case string(schemas.EdgeTypeBelongsToSession):
			nSession++
		case string(schemas.EdgeTypeOwnedByAgent):
			nAgent++
		}
	}
	lines = append(lines,
		fmt.Sprintf("edges_in_response_total=%d", len(edges)),
		fmt.Sprintf("edges_conflict_resolved=%d", nConflict),
		fmt.Sprintf("edges_belongs_to_session=%d", nSession),
		fmt.Sprintf("edges_owned_by_agent=%d", nAgent),
	)
	return lines
}

// formatQueryChainTraceLines turns QueryChain results into API-facing trace lines.
func formatQueryChainTraceLines(res chain.ChainResult, out chain.QueryChainOutput) []string {
	lines := []string{
		fmt.Sprintf("chain_name=%s ok=%t", res.ChainName, res.OK),
	}
	if res.Error != "" {
		lines = append(lines, "error="+res.Error)
	}
	if len(res.Meta) > 0 {
		keys := make([]string, 0, len(res.Meta))
		for k := range res.Meta {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			lines = append(lines, fmt.Sprintf("meta.%s=%v", k, res.Meta[k]))
		}
	}
	lines = append(lines,
		fmt.Sprintf("query_proof_steps=%d", len(out.ProofTrace)),
		fmt.Sprintf("subgraph_nodes=%d", len(out.Subgraph.Nodes)),
		fmt.Sprintf("subgraph_edges=%d", len(out.Subgraph.Edges)),
		fmt.Sprintf("merged_edges=%d", len(out.MergedEdges)),
	)
	return lines
}
