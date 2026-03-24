package worker

import (
	"context"
	"fmt"
	"strings"
	"time"
	"andb/src/internal/coordinator"
	"andb/src/internal/dataplane"
	"andb/src/internal/eventbackbone"
	"andb/src/internal/evidence"
	"andb/src/internal/materialization"
	"andb/src/internal/schemas"
	"andb/src/internal/semantic"
	"andb/src/internal/storage"
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
	ingest            IngestWorker
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
	var sched *coordinator.WorkerScheduler
	if coord != nil {
		sched = coord.Schedule
	}
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
		ingest:            NewPipelineIngestWorker(sched, wal, materializer, preCompute, nodeManager, plane, store),
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
	return r.ingest.Accept(ev)
}

// IngestWorker returns the active ingest pipeline (for registry wiring or tests).
func (r *Runtime) IngestWorker() IngestWorker {
	return r.ingest
}

func (r *Runtime) ExecuteQuery(req schemas.QueryRequest) schemas.QueryResponse {
	plan := r.planner.Build(req)
	searchInput := dataplane.SearchInput{
		QueryText:      req.QueryText,
		TopK:           plan.TopK,
		Namespace:      plan.Namespace,
		Constraints:    plan.Constraints,
		TimeFromUnixTS: plan.TimeFromUnixTS,
		TimeToUnixTS:   plan.TimeToUnixTS,
		IncludeGrowing: plan.IncludeGrowing,
		ObjectTypes:    plan.ObjectTypes,
		MemoryTypes:    plan.MemoryTypes,
	}
	result := r.nodeManager.DispatchQuery(searchInput, r.plane)
	result.ObjectIDs = r.includeStateCandidates(req, result.ObjectIDs, plan.ObjectTypes)
	result.ObjectIDs = semantic.FilterObjectIDsByTypes(result.ObjectIDs, plan.ObjectTypes)
	result.ObjectIDs = r.rebuildWithMemoryView(req, result.ObjectIDs)
	filters := r.policy.ApplyQueryFilters(req)
	resp := r.assembler.Build(searchInput, result, filters)

	// ── Post-retrieval reasoning via QueryChain ───────────────────────────────
	// QueryChain handles:
	//   1. Pre-fetching Memory objects as GraphNodes for node population.
	//   2. Pre-fetching BulkEdges for edge pre-population.
	//   3. Multi-hop BFS proof trace via ProofTraceWorker.
	//   4. Subgraph expansion via SubgraphExecutorWorker.
	//   5. Merging subgraph edges with the assembler's edges (deduplicated).
	if len(result.ObjectIDs) > 0 {
		preNodes := make([]schemas.GraphNode, 0, len(result.ObjectIDs))
		for _, id := range result.ObjectIDs {
			if m, ok := r.storage.Objects().GetMemory(id); ok {
				preNodes = append(preNodes, schemas.MemoryToGraphNode(m))
			}
		}
		preEdges := r.storage.Edges().BulkEdges(result.ObjectIDs)
		chainOut, chainResult := r.queryChain.Run(chain.QueryChainInput{
			ObjectIDs:      result.ObjectIDs,
			MaxDepth:       0, // default cap of 8
			GraphNodes:     preNodes,
			GraphEdges:     preEdges,
			EdgeTypeFilter: req.EdgeTypes,
			ObjectStore:    r.storage.Objects(),
			EdgeStore:      r.storage.Edges(),
		})
		_ = chainResult // chainResult.OK is advisory; non-fatal

		if len(chainOut.ProofTrace) > 0 {
			resp.ProofTrace = append(resp.ProofTrace, chainOut.ProofTrace...)
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
	}

	if len(req.EdgeTypes) > 0 {
		resp.Edges = filterEdgesByType(resp.Edges, req.EdgeTypes)
	}

	return resp
}

func (r *Runtime) rebuildWithMemoryView(req schemas.QueryRequest, ids []string) []string {
	if len(ids) == 0 {
		return ids
	}
	memories := make([]schemas.Memory, 0, len(ids))
	memoryIDs := make([]string, 0, len(ids))
	otherIDs := make([]string, 0, len(ids))
	for _, id := range ids {
		if m, ok := r.storage.Objects().GetMemory(id); ok {
			memories = append(memories, m)
			memoryIDs = append(memoryIDs, id)
			continue
		}
		otherIDs = append(otherIDs, id)
	}
	if len(memories) == 0 {
		return ids
	}

	visibleScopes := []string{
		string(schemas.MemoryScopePrivateUser),
		string(schemas.MemoryScopePrivateAgent),
		string(schemas.MemoryScopeSessionLocal),
		string(schemas.MemoryScopeWorkspaceShared),
		string(schemas.MemoryScopeTeamShared),
		string(schemas.MemoryScopeGlobalShared),
		string(schemas.MemoryScopeRestrictedShared),
	}
	snapshot := &schemas.AccessGraphSnapshot{
		SnapshotID:    "local-query",
		AgentID:       req.AgentID,
		SessionID:     req.SessionID,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		VisibleScopes: visibleScopes,
	}

	scorer := func(query string, candidates []schemas.Memory, ctx schemas.AlgorithmContext) []schemas.ScoredMemory {
		candidateIDs := make([]string, 0, len(candidates))
		byID := make(map[string]schemas.Memory, len(candidates))
		for _, c := range candidates {
			candidateIDs = append(candidateIDs, c.MemoryID)
			byID[c.MemoryID] = c
		}
		out, err := r.nodeManager.DispatchAlgorithm(
			"recall",
			candidateIDs,
			query,
			ctx.Timestamp,
			req.AgentID,
			req.SessionID,
			nil,
		)
		if err != nil || len(out.ScoredRefs) == 0 {
			return nil
		}
		scored := make([]schemas.ScoredMemory, 0, len(out.ScoredRefs))
		for i, id := range out.ScoredRefs {
			if m, ok := byID[id]; ok {
				scored = append(scored, schemas.ScoredMemory{
					Memory: m,
					Score:  float64(len(out.ScoredRefs) - i),
					Signal: "algorithm_dispatch_recall",
				})
			}
		}
		return scored
	}

	view := storage.NewMemoryViewBuilder(req.SessionID, req.TenantID, req.AgentID).
		WithSnapshot(snapshot).
		WithPolicyStore(r.storage.Policies()).
		WithAlgorithmScorer(scorer).
		Build(memories, req.QueryText)

	out := make([]string, 0, len(view.VisibleMemoryRefs)+len(otherIDs))
	out = append(out, view.VisibleMemoryRefs...)
	out = append(out, otherIDs...)
	if len(out) == 0 {
		return ids
	}
	return out
}

func (r *Runtime) includeStateCandidates(req schemas.QueryRequest, ids []string, objectTypes []string) []string {
	needState := false
	for _, ot := range objectTypes {
		if strings.TrimSpace(ot) == string(schemas.ObjectTypeState) {
			needState = true
			break
		}
	}
	if !needState {
		return ids
	}
	states := r.storage.Objects().ListStates(req.AgentID, req.SessionID)
	if len(states) == 0 {
		return ids
	}
	seen := make(map[string]bool, len(ids)+len(states))
	out := make([]string, 0, len(ids)+len(states))
	for _, id := range ids {
		out = append(out, id)
		seen[id] = true
	}
	for _, st := range states {
		if st.StateID == "" || seen[st.StateID] {
			continue
		}
		out = append(out, st.StateID)
		seen[st.StateID] = true
	}
	return out
}

func filterEdgesByType(edges []schemas.Edge, allowed []string) []schemas.Edge {
	if len(allowed) == 0 || len(edges) == 0 {
		return edges
	}
	allow := make(map[string]bool, len(allowed))
	for _, t := range allowed {
		allow[strings.TrimSpace(t)] = true
	}
	out := make([]schemas.Edge, 0, len(edges))
	for _, e := range edges {
		if allow[e.EdgeType] {
			out = append(out, e)
		}
	}
	return out
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
