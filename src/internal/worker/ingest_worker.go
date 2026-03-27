package worker

import (
	"errors"
	"strings"

	"andb/src/internal/coordinator"
	"andb/src/internal/dataplane"
	"andb/src/internal/eventbackbone"
	"andb/src/internal/materialization"
	"andb/src/internal/schemas"
	"andb/src/internal/storage"
	"andb/src/internal/worker/nodes"
)

// IngestWorker is the execution-plane boundary for event intake: WAL append,
// materialization, canonical persistence, optional pre-compute, node fan-out,
// and retrieval-plane projection. Implementations may later become async
// consumers without changing Runtime's HTTP-facing API.
type IngestWorker interface {
	Accept(ev schemas.Event) (map[string]any, error)
}

// PipelineIngestWorker is the default v1 ingest pipeline (previously inlined in
// Runtime.SubmitIngest).
type PipelineIngestWorker struct {
	wal          eventbackbone.WAL
	materializer *materialization.Service
	preCompute   *materialization.PreComputeService
	nodeManager  *nodes.Manager
	plane        dataplane.DataPlane
	storage      storage.RuntimeStorage
	scheduler    *coordinator.WorkerScheduler
}

// NewPipelineIngestWorker wires the synchronous ingest stages. sched may be
// nil (scheduler metrics disabled).
func NewPipelineIngestWorker(
	sched *coordinator.WorkerScheduler,
	wal eventbackbone.WAL,
	materializer *materialization.Service,
	preCompute *materialization.PreComputeService,
	nodeManager *nodes.Manager,
	plane dataplane.DataPlane,
	store storage.RuntimeStorage,
) *PipelineIngestWorker {
	return &PipelineIngestWorker{
		wal:          wal,
		materializer: materializer,
		preCompute:   preCompute,
		nodeManager:  nodeManager,
		plane:        plane,
		storage:      store,
		scheduler:    sched,
	}
}

// Accept runs validate → WAL → materialize → persist → pre-compute →
// DispatchIngest (data/index nodes) → DataPlane.Ingest.
func (w *PipelineIngestWorker) Accept(ev schemas.Event) (map[string]any, error) {
	if strings.TrimSpace(ev.EventID) == "" {
		return nil, errors.New("event_id is required")
	}

	if w.scheduler != nil {
		w.scheduler.Dispatch(coordinator.WorkerTypeIngest)
		defer w.scheduler.Complete(coordinator.WorkerTypeIngest)
	}

	entry, err := w.wal.Append(ev)
	if err != nil {
		return nil, err
	}
	if ev.LogicalTS == 0 {
		ev.LogicalTS = entry.LSN
	}

	mat := w.materializer.MaterializeEvent(ev)
	record := mat.Record

	w.storage.Objects().PutMemory(mat.Memory)
	w.storage.Versions().PutVersion(mat.Version)
	for _, edge := range mat.Edges {
		w.storage.Edges().PutEdge(edge)
	}
	if mat.State != nil && mat.StateVersion != nil {
		w.storage.Objects().PutState(*mat.State)
		w.storage.Versions().PutVersion(*mat.StateVersion)
	}
	if mat.Artifact != nil && mat.ArtifactVersion != nil {
		w.storage.Objects().PutArtifact(*mat.Artifact)
		w.storage.Versions().PutVersion(*mat.ArtifactVersion)
	}

	if w.preCompute != nil {
		frag := w.preCompute.Compute(ev, record)
		if frag.SalienceScore >= 0.5 {
			w.storage.HotCache().Put(record.ObjectID, ev.EventType, record, frag.SalienceScore)
		}
	}

	w.nodeManager.DispatchIngest(record)
	if err := w.plane.Ingest(record); err != nil {
		return nil, err
	}
	if _, err := w.nodeManager.DispatchAlgorithm(
		"ingest",
		[]string{mat.Memory.MemoryID},
		"",
		ev.EventTime,
		ev.AgentID,
		ev.SessionID,
		nil,
	); err != nil {
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
