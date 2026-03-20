package materialization

import (
	"fmt"

	"andb/src/internal/schemas"
	"andb/src/internal/storage"
	"andb/src/internal/worker/nodes"
)

// InMemoryToolTraceWorker records tool_call and tool_result events as
// structured Artifact objects for audit and downstream retrieval.
type InMemoryToolTraceWorker struct {
	id       string
	objStore storage.ObjectStore
}

func CreateInMemoryToolTraceWorker(id string, objStore storage.ObjectStore) *InMemoryToolTraceWorker {
	return &InMemoryToolTraceWorker{id: id, objStore: objStore}
}

func (w *InMemoryToolTraceWorker) Info() nodes.NodeInfo {
	return nodes.NodeInfo{
		ID:           w.id,
		Type:         nodes.NodeTypeToolTrace,
		State:        nodes.NodeStateReady,
		Capabilities: []string{"tool_call_trace", "tool_result_capture"},
	}
}

func (w *InMemoryToolTraceWorker) TraceToolCall(ev schemas.Event) error {
	if ev.EventType != "tool_call" && ev.EventType != "tool_result" {
		return nil
	}
	meta := map[string]any{}
	if ev.Payload != nil {
		for k, v := range ev.Payload {
			meta[k] = v
		}
	}
	meta["traced_event_id"] = ev.EventID
	meta["traced_agent_id"] = ev.AgentID

	w.objStore.PutArtifact(schemas.Artifact{
		ArtifactID:        fmt.Sprintf("tool_trace_%s", ev.EventID),
		SessionID:         ev.SessionID,
		OwnerAgentID:      ev.AgentID,
		ArtifactType:      "tool_trace",
		MimeType:          "application/json",
		Metadata:          meta,
		ProducedByEventID: ev.EventID,
		Version:           1,
	})
	return nil
}
