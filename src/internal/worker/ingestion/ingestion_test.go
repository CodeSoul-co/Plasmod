package ingestion

import (
	"testing"

	"plasmod/src/internal/schemas"
)

// ─── IngestWorker ─────────────────────────────────────────────────────────────

func TestIngestWorker_Process_ValidEvent(t *testing.T) {
	w := CreateInMemoryIngestWorker("test-ingest")

	err := w.Process(schemas.Event{
		EventID:   "evt_1",
		AgentID:   "a1",
		SessionID: "s1",
		EventType: "agent_thought",
	})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
}

func TestIngestWorker_Process_MissingEventID(t *testing.T) {
	w := CreateInMemoryIngestWorker("test-ingest")

	err := w.Process(schemas.Event{
		AgentID:   "a1",
		SessionID: "s1",
		EventType: "agent_thought",
	})
	if err == nil {
		t.Error("expected error for empty event_id")
	}
}

func TestIngestWorker_Run_TypedDispatch(t *testing.T) {
	w := CreateInMemoryIngestWorker("test-ingest")

	out, err := w.Run(schemas.IngestInput{
		Event: schemas.Event{
			EventID:   "evt_2",
			AgentID:   "a1",
			SessionID: "s1",
			EventType: "agent_thought",
		},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	result, ok := out.(schemas.IngestOutput)
	if !ok {
		t.Fatalf("unexpected output type %T", out)
	}
	if !result.Valid {
		t.Errorf("expected Valid=true for valid event, error: %s", result.Error)
	}
}

func TestIngestWorker_Run_WrongInputType(t *testing.T) {
	w := CreateInMemoryIngestWorker("test-ingest")
	_, err := w.Run(schemas.MemoryExtractionInput{})
	if err == nil {
		t.Error("expected error for wrong input type")
	}
}

func TestIngestWorker_Info_ReturnsReady(t *testing.T) {
	w := CreateInMemoryIngestWorker("test-ingest")
	info := w.Info()
	if info.ID != "test-ingest" {
		t.Errorf("expected ID=test-ingest, got %q", info.ID)
	}
	if info.State != "ready" {
		t.Errorf("expected State=ready, got %q", info.State)
	}
}
