package worker

import (
	"context"
	"strings"
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
	wal         eventbackbone.WAL
	bus         eventbackbone.Bus
	plane       dataplane.DataPlane
	coord       *coordinator.Hub
	policy      *semantic.PolicyEngine
	planner     semantic.QueryPlanner
	assembler   *evidence.Assembler
	nodeManager *nodes.Manager
	storage     storage.RuntimeStorage
	ingest      IngestWorker
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
) *Runtime {
	var sched *coordinator.WorkerScheduler
	if coord != nil {
		sched = coord.Schedule
	}
	return &Runtime{
		wal:         wal,
		bus:         bus,
		plane:       plane,
		coord:       coord,
		policy:      policy,
		planner:     planner,
		assembler:   assembler,
		nodeManager: nodeManager,
		storage:     store,
		ingest:      NewPipelineIngestWorker(sched, wal, materializer, preCompute, nodeManager, plane, store),
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
	filters := r.policy.ApplyQueryFilters(req)
	resp := r.assembler.Build(searchInput, result, filters)

	// R1 fix: wire QueryChain workers with pre-fetched edges.
	// The assembler already does 1-hop BulkEdges expansion internally; here we
	// additionally run the multi-hop BFS ProofTraceWorker and pass pre-fetched
	// edges to SubgraphExecutorWorker (which does NOT fetch edges itself).
	//
	// TODO(member-D+C): review maxDepth default (0→8) and edge dedup strategy
	// before merging to main.
	if len(result.ObjectIDs) > 0 {
		preEdges := r.storage.Edges().BulkEdges(result.ObjectIDs)

		// 1. Multi-hop BFS proof trace via ProofTraceWorker (maxDepth 0 = default 8).
		if bfsTrace := r.nodeManager.DispatchProofTrace(result.ObjectIDs, 0); len(bfsTrace) > 0 {
			resp.ProofTrace = append(resp.ProofTrace, bfsTrace...)
		}

		// 2. Subgraph expansion — SubgraphExecutorWorker needs pre-fetched edges.
		if len(preEdges) > 0 {
			expResp := r.nodeManager.DispatchSubgraphExpand(
				schemas.GraphExpandRequest{SeedObjectIDs: result.ObjectIDs, Hops: 1},
				nil,
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
