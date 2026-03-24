package worker

import (
	"context"
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
	"andb/src/internal/worker/nodes"
)

type Runtime struct {
	wal           eventbackbone.WAL
	bus           eventbackbone.Bus
	plane         dataplane.DataPlane
	coord         *coordinator.Hub
	policy        *semantic.PolicyEngine
	planner       semantic.QueryPlanner
	assembler     *evidence.Assembler
	nodeManager   *nodes.Manager
	storage       storage.RuntimeStorage
	tieredObjects *storage.TieredObjectStore
	ingest        IngestWorker
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
	nodeManager *nodes.Manager,
	store storage.RuntimeStorage,
	tieredObjs *storage.TieredObjectStore,
) *Runtime {
	var sched *coordinator.WorkerScheduler
	if coord != nil {
		sched = coord.Schedule
	}
	return &Runtime{
		wal:           wal,
		bus:           bus,
		plane:         plane,
		coord:         coord,
		policy:        policy,
		planner:       planner,
		assembler:     assembler,
		nodeManager:   nodeManager,
		storage:       store,
		tieredObjects: tieredObjs,
		ingest:        NewPipelineIngestWorker(sched, wal, materializer, preCompute, nodeManager, plane, store),
	}
}

func (r *Runtime) RegisterDefaults() {
	_ = r.bus.Subscribe("wal.events")
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
	result.ObjectIDs = semantic.FilterObjectIDsByTypes(result.ObjectIDs, plan.ObjectTypes)
	result.ObjectIDs = r.rebuildWithMemoryView(req, result.ObjectIDs)
	filters := r.policy.ApplyQueryFilters(req)
	resp := r.assembler.Build(searchInput, result, filters)

	// Wire QueryChain workers with pre-fetched edges and nodes.
	// The assembler already does 1-hop BulkEdges expansion internally; here we
	// additionally run the multi-hop BFS ProofTraceWorker and pass pre-fetched
	// edges + nodes to SubgraphExecutorWorker (which does NOT fetch them itself).
	if len(result.ObjectIDs) > 0 {
		preEdges := r.storage.Edges().BulkEdges(result.ObjectIDs)

		// Pre-fetch Memory objects as GraphNodes so OneHopExpand can populate
		// EvidenceSubgraph.Nodes (passing nil left Nodes always empty).
		preNodes := make([]schemas.GraphNode, 0, len(result.ObjectIDs))
		for _, id := range result.ObjectIDs {
			if m, ok := r.storage.Objects().GetMemory(id); ok {
				preNodes = append(preNodes, schemas.MemoryToGraphNode(m))
			}
		}

		// 1. Multi-hop BFS proof trace via ProofTraceWorker (maxDepth 0 = default 8).
		if bfsTrace := r.nodeManager.DispatchProofTrace(result.ObjectIDs, 0); len(bfsTrace) > 0 {
			resp.ProofTrace = append(resp.ProofTrace, bfsTrace...)
		}

		// 2. Subgraph expansion — pass both pre-fetched nodes and edges so
		// EvidenceSubgraph.Nodes and .Edges are both populated.
		if len(preEdges) > 0 {
			expResp := r.nodeManager.DispatchSubgraphExpand(
				schemas.GraphExpandRequest{SeedObjectIDs: result.ObjectIDs},
				preNodes,
				preEdges,
			)
			seen := make(map[string]bool, len(resp.Edges))
			for _, e := range resp.Edges {
				seen[e.EdgeID] = true
			}
			for _, e := range expResp.Subgraph.Edges {
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
