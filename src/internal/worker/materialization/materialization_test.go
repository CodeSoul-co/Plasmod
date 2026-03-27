package materialization

import (
	"testing"

	"andb/src/internal/eventbackbone"
	"andb/src/internal/schemas"
	"andb/src/internal/storage"
)

// ─── ObjectMaterializationWorker ─────────────────────────────────────────────

// TestObjectMatWorker_Materialize_DefaultToMemory verifies that ObjectMaterializationWorker
// is a no-op for non-artifact events (e.g. agent_thought). Memory is created by
// Runtime.SubmitIngest via TieredObjectStore.PutMemory, not by this worker.
func TestObjectMatWorker_Materialize_DefaultToMemory(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	w := CreateInMemoryObjectMaterializationWorker("test-obj", store.Objects(), store.Versions())

	ev := schemas.Event{
		EventID:   "evt1",
		AgentID:   "a1",
		SessionID: "s1",
		EventType: "agent_thought",
	}
	if err := w.Materialize(ev); err != nil {
		t.Fatalf("Materialize failed: %v", err)
	}

	// ObjectMaterializationWorker should NOT create Memory for non-artifact events.
	_, ok := store.Objects().GetMemory(schemas.IDPrefixMemory + "evt1")
	if ok {
		t.Fatal("ObjectMaterializationWorker should not create Memory; it is a no-op for agent_thought events")
	}
}

func TestObjectMatWorker_Materialize_ToolCallToArtifact(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	w := CreateInMemoryObjectMaterializationWorker("test-obj", store.Objects(), store.Versions())

	ev := schemas.Event{
		EventID:   "evt2",
		AgentID:   "a1",
		SessionID: "s1",
		EventType: string(schemas.EventTypeToolCall),
	}
	if err := w.Materialize(ev); err != nil {
		t.Fatalf("Materialize failed: %v", err)
	}

	art, ok := store.Objects().GetArtifact(schemas.IDPrefixArtifact + "evt2")
	if !ok {
		t.Fatal("expected Artifact to be stored for tool_call event")
	}
	if art.ArtifactType != string(schemas.EventTypeToolCall) {
		t.Errorf("wrong artifact type: %q", art.ArtifactType)
	}
}

// TestObjectMatWorker_Materialize_StateUpdateToState is removed.
// State is exclusively created by InMemoryStateMaterializationWorker (DispatchStateMaterialization).
// See Runtime.SubmitIngest for the synchronous call site; StateMaterializationWorker.Apply
// is tested in TestStateMatWorker_Apply_StateUpdateEvent.

func TestObjectMatWorker_Run_TypedDispatch(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	w := CreateInMemoryObjectMaterializationWorker("test-obj", store.Objects(), store.Versions())

	out, err := w.Run(schemas.ObjectMaterializationInput{
		Event: schemas.Event{EventID: "evt4", AgentID: "a1", SessionID: "s1"},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	result, ok := out.(schemas.ObjectMaterializationOutput)
	if !ok {
		t.Fatalf("unexpected output type %T", out)
	}
	if result.ObjectID == "" {
		t.Error("expected non-empty ObjectID")
	}
	if result.ObjectType != string(schemas.ObjectTypeMemory) {
		t.Errorf("expected ObjectType=memory, got %q", result.ObjectType)
	}
}

func TestObjectMatWorker_Run_WrongInputType(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	w := CreateInMemoryObjectMaterializationWorker("test-obj", store.Objects(), store.Versions())
	_, err := w.Run(schemas.IngestInput{})
	if err == nil {
		t.Error("expected error for wrong input type")
	}
}

// ─── StateMaterializationWorker ───────────────────────────────────────────────

func TestStateMatWorker_Apply_StateUpdateEvent(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	w := CreateInMemoryStateMaterializationWorker("test-state", store.Objects(), store.Versions())

	ev := schemas.Event{
		EventID:   "evt_state1",
		AgentID:   "a1",
		SessionID: "s1",
		EventType: string(schemas.EventTypeStateUpdate),
		Payload:   map[string]any{schemas.PayloadKeyStateKey: "k", schemas.PayloadKeyStateValue: "v"},
	}
	if err := w.Apply(ev); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	// StateMat stores by agentID + "_" + stateKey, not by eventID
	stateID := schemas.IDPrefixState + "a1_k"
	state, ok := store.Objects().GetState(stateID)
	if !ok {
		t.Fatalf("expected State to be stored with ID %q", stateID)
	}
	if state.StateKey != "k" || state.StateValue != "v" {
		t.Errorf("wrong state kv: key=%q val=%q", state.StateKey, state.StateValue)
	}
}

func TestStateMatWorker_Apply_NonStateEvent_NoOp(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	w := CreateInMemoryStateMaterializationWorker("test-state", store.Objects(), store.Versions())

	ev := schemas.Event{EventID: "evtX", EventType: "agent_thought"}
	if err := w.Apply(ev); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
}

// ─── ToolTraceWorker ──────────────────────────────────────────────────────────

func TestToolTraceWorker_TraceToolCall_StoresArtifact(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	derivLog := eventbackbone.NewDerivationLog(clock, bus)
	w := CreateInMemoryToolTraceWorker("test-tool", store.Objects(), derivLog)

	ev := schemas.Event{
		EventID:   "evt_tool1",
		AgentID:   "a1",
		SessionID: "s1",
		EventType: string(schemas.EventTypeToolCall),
	}
	if err := w.TraceToolCall(ev); err != nil {
		t.Fatalf("TraceToolCall failed: %v", err)
	}

	// ToolTraceWorker stores with IDPrefixToolTrace, not IDPrefixArtifact
	artID := schemas.IDPrefixToolTrace + "evt_tool1"
	art, ok := store.Objects().GetArtifact(artID)
	if !ok {
		t.Fatalf("expected Artifact to be stored with ID %q", artID)
	}
	if art.ArtifactType != string(schemas.ArtifactTypeToolTrace) {
		t.Errorf("wrong artifact type: %q", art.ArtifactType)
	}
}

func TestToolTraceWorker_TraceToolCall_NonToolEvent_NoOp(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	derivLog := eventbackbone.NewDerivationLog(clock, bus)
	w := CreateInMemoryToolTraceWorker("test-tool", store.Objects(), derivLog)

	ev := schemas.Event{EventID: "evtY", EventType: "agent_thought"}
	if err := w.TraceToolCall(ev); err != nil {
		t.Fatalf("TraceToolCall failed: %v", err)
	}
}

func TestToolTraceWorker_Run_TypedDispatch(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	derivLog := eventbackbone.NewDerivationLog(clock, bus)
	w := CreateInMemoryToolTraceWorker("test-tool", store.Objects(), derivLog)

	out, err := w.Run(schemas.ToolTraceInput{
		Event: schemas.Event{
			EventID:   "evt_tool2",
			EventType: string(schemas.EventTypeToolCall),
		},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if _, ok := out.(schemas.ToolTraceOutput); !ok {
		t.Fatalf("unexpected output type %T", out)
	}
}
