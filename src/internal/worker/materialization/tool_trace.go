package materialization

import (
	"fmt"

	"plasmod/src/internal/eventbackbone"
	"plasmod/src/internal/schemas"
	"plasmod/src/internal/storage"
	"plasmod/src/internal/worker/nodes"
)

// InMemoryToolTraceWorker records tool_call and tool_result events as
// structured Artifact objects for audit and downstream retrieval.
// When a DerivationLogger is provided, it also appends a derivation entry
// linking the source event to the produced artifact, enabling multi-hop
// ProofTrace to walk backwards through the causal chain.
type InMemoryToolTraceWorker struct {
	id       string
	objStore storage.ObjectStore
	derivLog eventbackbone.DerivationLogger
}

// CreateInMemoryToolTraceWorker creates a ToolTraceWorker.
// derivLog may be nil; when provided, causal derivation entries are recorded.
func CreateInMemoryToolTraceWorker(
	id string,
	objStore storage.ObjectStore,
	derivLog eventbackbone.DerivationLogger,
) *InMemoryToolTraceWorker {
	return &InMemoryToolTraceWorker{id: id, objStore: objStore, derivLog: derivLog}
}

func (w *InMemoryToolTraceWorker) Run(input schemas.WorkerInput) (schemas.WorkerOutput, error) {
	in, ok := input.(schemas.ToolTraceInput)
	if !ok {
		return schemas.ToolTraceOutput{}, fmt.Errorf("tool_trace: unexpected input type %T", input)
	}
	in.Event = in.Event.NormalizeDynamicEventV04()
	err := w.TraceToolCall(in.Event)
	if err != nil {
		return schemas.ToolTraceOutput{}, err
	}
	if in.Event.EventInfo.EventType != string(schemas.EventTypeToolCall) && in.Event.EventInfo.EventType != string(schemas.EventTypeToolResult) {
		return schemas.ToolTraceOutput{}, nil
	}
	return schemas.ToolTraceOutput{
		ArtifactID:       schemas.IDPrefixToolTrace + in.Event.Identity.EventID,
		DerivationLogged: w.derivLog != nil,
	}, nil
}

func (w *InMemoryToolTraceWorker) Info() nodes.NodeInfo {
	return nodes.NodeInfo{
		ID:           w.id,
		Type:         nodes.NodeTypeToolTrace,
		State:        nodes.NodeStateReady,
		Capabilities: []string{"tool_call_trace", "tool_result_capture", "derivation_log"},
	}
}

func (w *InMemoryToolTraceWorker) TraceToolCall(ev schemas.Event) error {
	ev = ev.NormalizeDynamicEventV04()
	if ev.EventInfo.EventType != string(schemas.EventTypeToolCall) && ev.EventInfo.EventType != string(schemas.EventTypeToolResult) {
		return nil
	}
	meta := map[string]any{}
	if ev.Payload != nil {
		for k, v := range ev.Payload {
			meta[k] = v
		}
	}
	meta[schemas.EventIDKey] = ev.Identity.EventID
	meta[schemas.AgentIDKey] = ev.Actor.AgentID
	meta["schema_version"] = ev.SchemaVersion
	meta["tool_call_event_id"] = ev.Causality.CallEventID

	artifactID := fmt.Sprintf("%s%s", schemas.IDPrefixToolTrace, ev.Identity.EventID)
	w.objStore.PutArtifact(schemas.Artifact{
		ArtifactID:        artifactID,
		SessionID:         ev.Actor.SessionID,
		OwnerAgentID:      ev.Actor.AgentID,
		ArtifactType:      string(schemas.ArtifactTypeToolTrace),
		MimeType:          schemas.MimeTypeJSON,
		Metadata:          meta,
		ProducedByEventID: ev.Identity.EventID,
		Version:           1,
	})

	// Record the causal edge: event → artifact in the DerivationLog.
	// This allows ProofTraceWorker.AssembleTrace to follow the full
	// tool_call → artifact derivation path during multi-hop BFS.
	if w.derivLog != nil {
		w.derivLog.Append(ev.Identity.EventID, "event", artifactID, "artifact", ev.EventInfo.EventType)
	}
	return nil
}
