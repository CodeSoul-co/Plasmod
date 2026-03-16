package worker

import (
	"errors"
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
	wal          eventbackbone.WAL
	bus          eventbackbone.Bus
	plane        dataplane.DataPlane
	coord        *coordinator.Hub
	policy       *semantic.PolicyEngine
	planner      semantic.QueryPlanner
	materializer *materialization.Service
	assembler    *evidence.Assembler
	nodeManager  *nodes.Manager
	storage      storage.RuntimeStorage
}

func NewRuntime(
	wal eventbackbone.WAL,
	bus eventbackbone.Bus,
	plane dataplane.DataPlane,
	coord *coordinator.Hub,
	policy *semantic.PolicyEngine,
	planner semantic.QueryPlanner,
	materializer *materialization.Service,
	assembler *evidence.Assembler,
	nodeManager *nodes.Manager,
	store storage.RuntimeStorage,
) *Runtime {
	return &Runtime{
		wal:          wal,
		bus:          bus,
		plane:        plane,
		coord:        coord,
		policy:       policy,
		planner:      planner,
		materializer: materializer,
		assembler:    assembler,
		nodeManager:  nodeManager,
		storage:      store,
	}
}

func (r *Runtime) RegisterDefaults() {
	_ = r.bus.Subscribe("wal.events")
}

func (r *Runtime) SubmitIngest(ev schemas.Event) (map[string]any, error) {
	if strings.TrimSpace(ev.EventID) == "" {
		return nil, errors.New("event_id is required")
	}
	entry, err := r.wal.Append(ev)
	if err != nil {
		return nil, err
	}
	if ev.LogicalTS == 0 {
		ev.LogicalTS = entry.LSN
	}
	record := r.materializer.ProjectEvent(ev)
	r.nodeManager.DispatchIngest(record)
	if err := r.plane.Ingest(record); err != nil {
		return nil, err
	}
	return map[string]any{"status": "accepted", "lsn": entry.LSN, "event_id": ev.EventID}, nil
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
	return r.assembler.Build(result, r.policy.ApplyQueryFilters(req))
}

func (r *Runtime) Topology() map[string]any {
	return map[string]any{
		"nodes":    r.nodeManager.Topology(),
		"segments": r.storage.Segments().List(""),
		"indexes":  r.storage.Indexes().List(),
	}
}
