package worker

import (
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
	wal          eventbackbone.WAL
	bus          eventbackbone.Bus
	plane        dataplane.DataPlane
	coord        *coordinator.Hub
	policy       *semantic.PolicyEngine
	planner   semantic.QueryPlanner
	assembler *evidence.Assembler
	nodeManager  *nodes.Manager
	storage      storage.RuntimeStorage
	ingest       IngestWorker
}

func NewRuntime(
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
		wal:          wal,
		bus:          bus,
		plane:        plane,
		coord:        coord,
		policy:    policy,
		planner:   planner,
		assembler: assembler,
		nodeManager:  nodeManager,
		storage:      store,
		ingest:       NewPipelineIngestWorker(sched, wal, materializer, preCompute, nodeManager, plane, store),
	}
}

func (r *Runtime) RegisterDefaults() {
	_ = r.bus.Subscribe("wal.events")
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
	}
	result := r.nodeManager.DispatchQuery(searchInput, r.plane)
	filters := r.policy.ApplyQueryFilters(req)
	return r.assembler.Build(result, filters)
}

func (r *Runtime) Topology() map[string]any {
	return map[string]any{
		"nodes":    r.nodeManager.Topology(),
		"segments": r.storage.Segments().List(""),
		"indexes":  r.storage.Indexes().List(),
	}
}
