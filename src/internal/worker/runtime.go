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
	preCompute   *materialization.PreComputeService
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
	if mat.State != nil && mat.StateVersion != nil {
		r.storage.Objects().PutState(*mat.State)
		r.storage.Versions().PutVersion(*mat.StateVersion)
	}
	if mat.Artifact != nil && mat.ArtifactVersion != nil {
		r.storage.Objects().PutArtifact(*mat.Artifact)
		r.storage.Versions().PutVersion(*mat.ArtifactVersion)
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
	ack := map[string]any{
		"status":    "accepted",
		"lsn":       entry.LSN,
		"event_id":  ev.EventID,
		"memory_id": mat.Memory.MemoryID,
		"edges":     len(mat.Edges),
	}
	if mat.State != nil {
		ack["state_id"] = mat.State.StateID
	}
	if mat.Artifact != nil {
		ack["artifact_id"] = mat.Artifact.ArtifactID
	}
	return ack, nil
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
