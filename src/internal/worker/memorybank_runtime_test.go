package worker

import (
	"context"
	"encoding/json"
	"testing"

	"plasmod/src/internal/dataplane"
	"plasmod/src/internal/eventbackbone"
	"plasmod/src/internal/schemas"
	"plasmod/src/internal/worker/cognitive"
	"plasmod/src/internal/worker/cognitive/memorybank"
)

func TestRuntimeMemoryBankIngestRecallAndGovernanceExclusion(t *testing.T) {
	r := buildTestRuntime(t)
	r.MemoryBankMode = true
	r.nodeManager.RegisterAlgorithmDispatch(cognitive.CreateAlgorithmDispatchWorker("bank", memorybank.New("memorybank_v1", memorybank.DefaultConfig()), r.storage.Objects(), r.storage.AlgorithmStates(), r.storage.Audits()))
	confidence := 1.0
	ack, err := r.SubmitIngest(schemas.Event{EventID: "evt_bank", TenantID: "tenant", WorkspaceID: "workspace", AgentID: "agent", SessionID: "session", Importance: 1, EventInfo: schemas.EventDescriptor{Confidence: &confidence}, Payload: map[string]any{"text": "I prefer concise replies"}})
	if err != nil {
		t.Fatal(err)
	}
	id := ack["memory_id"].(string)
	memory, _ := r.storage.Objects().GetMemory(id)
	if memory.AlgorithmStateRef != "memorybank_v1" {
		t.Fatalf("strict ingest skipped bank: %+v", memory)
	}
	if err := r.plane.Flush(); err != nil {
		t.Fatal(err)
	}
	query := schemas.QueryRequest{ResponseMode: "objects_only", QueryText: "concise", QueryScope: "workspace", WorkspaceID: "workspace", AgentID: "agent", TopK: 5}
	if got := r.ExecuteQuery(query); !containsString(got.Objects, id) {
		t.Fatalf("missing memory: %+v", got)
	}
	raw, ok := r.storage.Objects().GetState("governance:memorybank_v1:" + id)
	if !ok {
		t.Fatal("no durable recall state")
	}
	var state schemas.MemoryAlgorithmState
	if err := json.Unmarshal([]byte(raw.StateValue), &state); err != nil {
		t.Fatal(err)
	}
	if state.RecallCount != 1 {
		t.Fatalf("query skipped recall: %+v", state)
	}
	view := r.DispatchRecall("concise", "workspace", 5, "agent", "session", "tenant", "workspace")
	if !containsString(view.VisibleMemoryRefs, id) {
		t.Fatalf("missing recalled memory: %+v", view)
	}
	raw, _ = r.storage.Objects().GetState("governance:memorybank_v1:" + id)
	if err := json.Unmarshal([]byte(raw.StateValue), &state); err != nil {
		t.Fatal(err)
	}
	if state.RecallCount != 2 {
		t.Fatalf("DispatchRecall reinforced twice: %+v", state)
	}
	state.SuggestedLifecycleState = "quarantined"
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	raw.StateValue = string(data)
	r.storage.Objects().PutState(raw)
	if got := r.ExecuteQuery(query); len(got.Objects) != 0 {
		t.Fatalf("excluded memory returned: %+v", got)
	}
}

func TestWALReplayDoesNotResurrectForgottenMemory(t *testing.T) {
	r := buildTestRuntime(t)
	ev := schemas.Event{EventID: "evt_forgotten", TenantID: "tenant", WorkspaceID: "workspace", AgentID: "agent", Payload: map[string]any{"text": "morning coffee"}}
	ack, err := r.SubmitIngest(ev)
	if err != nil {
		t.Fatal(err)
	}
	id := ack["memory_id"].(string)
	memory, _ := r.storage.Objects().GetMemory(id)
	memory.IsActive = false
	r.storage.Objects().PutMemory(memory)
	stored, _ := r.storage.Objects().GetEvent(ev.EventID)
	entry := eventbackbone.WALEntry{LSN: stored.NormalizeDynamicEventV04().Time.WalLSN, Event: stored}
	if _, err := r.projectWALEntry(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	after, _ := r.storage.Objects().GetMemory(id)
	if after.IsActive {
		t.Fatal("WAL replay resurrected forgotten memory")
	}
}

func TestMemoryBankDoesNotFilterNonMemoryObjects(t *testing.T) {
	r := buildTestRuntime(t)
	r.MemoryBankMode = true
	r.nodeManager.RegisterAlgorithmDispatch(cognitive.CreateAlgorithmDispatchWorker("bank", memorybank.New("memorybank_v1", memorybank.DefaultConfig()), r.storage.Objects(), r.storage.AlgorithmStates(), r.storage.Audits()))
	r.storage.Objects().PutArtifact(schemas.Artifact{ArtifactID: "art_report", WorkspaceID: "workspace", OwnerAgentID: "agent"})
	if err := r.plane.Ingest(dataplane.IngestRecord{ObjectID: "art_report", Text: "meeting report", Attributes: map[string]string{"workspace_id": "workspace", "agent_id": "agent"}}); err != nil {
		t.Fatal(err)
	}
	if err := r.plane.Flush(); err != nil {
		t.Fatal(err)
	}
	got := r.ExecuteQuery(schemas.QueryRequest{QueryText: "meeting", WorkspaceID: "workspace", AgentID: "agent", QueryScope: "workspace", ObjectTypes: []string{"artifact"}, ResponseMode: "objects_only", TopK: 5})
	if !containsString(got.Objects, "art_report") {
		t.Fatalf("governance discarded non-memory object: %+v", got)
	}
}
