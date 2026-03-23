package worker

import (
	"testing"

	"andb/src/internal/coordinator"
	"andb/src/internal/dataplane"
	"andb/src/internal/eventbackbone"
	"andb/src/internal/materialization"
	"andb/src/internal/schemas"
	"andb/src/internal/storage"
	"andb/src/internal/worker/nodes"
)

func TestPipelineIngestWorker_Accept_requiresEventID(t *testing.T) {
	w := NewPipelineIngestWorker(
		nil,
		eventbackbone.NewInMemoryWAL(eventbackbone.NewInMemoryBus(), eventbackbone.NewHybridClock()),
		materialization.NewService(),
		nil,
		nodes.CreateManager(),
		dataplane.NewSegmentDataPlane(),
		storage.NewMemoryRuntimeStorage(),
	)
	_, err := w.Accept(schemas.Event{})
	if err == nil {
		t.Fatal("expected error for empty event_id")
	}
}

func TestPipelineIngestWorker_Accept_happyPath(t *testing.T) {
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	wal := eventbackbone.NewInMemoryWAL(bus, clock)
	store := storage.NewMemoryRuntimeStorage()
	nm := nodes.CreateManager()
	plane := dataplane.NewSegmentDataPlane()
	nm.RegisterData(nodes.CreateInMemoryDataNode("d1", store.Segments()))
	nm.RegisterIndex(nodes.CreateInMemoryIndexNode("i1", store.Indexes()))

	sched := coordinator.NewWorkerScheduler()
	w := NewPipelineIngestWorker(
		sched,
		wal,
		materialization.NewService(),
		nil,
		nm,
		plane,
		store,
	)

	ack, err := w.Accept(schemas.Event{
		EventID:     "evt_ingest_worker_1",
		TenantID:    "t1",
		WorkspaceID: "w1",
		AgentID:     "a1",
		SessionID:   "s1",
		Payload:     map[string]any{"text": "x"},
	})
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if ack["status"] != "accepted" {
		t.Fatalf("ack status: %v", ack["status"])
	}
	stats := sched.Stats()
	if ing, ok := stats["ingest"]; !ok || ing["dispatched"] != 1 {
		t.Fatalf("expected ingest scheduler dispatch, stats=%v", stats)
	}
}
