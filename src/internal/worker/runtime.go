package worker

import (
	"context"
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
	preCompute   *materialization.PreComputeService
	assembler    *evidence.Assembler
	nodeManager  *nodes.Manager
	storage      storage.RuntimeStorage
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
	return &Runtime{
		wal:          wal,
		bus:          bus,
		plane:        plane,
		coord:        coord,
		policy:       policy,
		planner:      planner,
		materializer: materializer,
		preCompute:   preCompute,
		assembler:    assembler,
		nodeManager:  nodeManager,
		storage:      store,
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
	mat := r.materializer.MaterializeEvent(ev)
	record := mat.Record

	// ── Persist canonical objects ─────────────────────────────────────────
	r.storage.Objects().PutMemory(mat.Memory)
	r.storage.Versions().PutVersion(mat.Version)
	for _, edge := range mat.Edges {
		r.storage.Edges().PutEdge(edge)
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
	if err := r.plane.Ingest(record); err != nil {
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
