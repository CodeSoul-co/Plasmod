package materialization

import (
	"fmt"
	"time"

	"andb/src/internal/schemas"
	"andb/src/internal/storage"
	"andb/src/internal/worker/nodes"
)

// InMemoryObjectMaterializationWorker routes a raw Event to the correct
// canonical object store based on event_type:
//
//	"tool_call" | "tool_result"                    → Artifact
//	"state_update" | "state_change" | "checkpoint" → State
//	everything else                                 → Memory (level-0 episodic)
type InMemoryObjectMaterializationWorker struct {
	id       string
	objStore storage.ObjectStore
	verStore storage.SnapshotVersionStore
}

func CreateInMemoryObjectMaterializationWorker(
	id string,
	objStore storage.ObjectStore,
	verStore storage.SnapshotVersionStore,
) *InMemoryObjectMaterializationWorker {
	return &InMemoryObjectMaterializationWorker{id: id, objStore: objStore, verStore: verStore}
}

func (w *InMemoryObjectMaterializationWorker) Info() nodes.NodeInfo {
	return nodes.NodeInfo{
		ID:           w.id,
		Type:         nodes.NodeTypeObjectMaterialization,
		State:        nodes.NodeStateReady,
		Capabilities: []string{"memory_route", "state_route", "artifact_route"},
	}
}

func (w *InMemoryObjectMaterializationWorker) Materialize(ev schemas.Event) error {
	now := time.Now().UTC().Format(time.RFC3339)
	switch ev.EventType {
	case "tool_call", "tool_result":
		artifact := schemas.Artifact{
			ArtifactID:        fmt.Sprintf("art_%s", ev.EventID),
			SessionID:         ev.SessionID,
			OwnerAgentID:      ev.AgentID,
			ArtifactType:      ev.EventType,
			ProducedByEventID: ev.EventID,
			Version:           ev.Version,
		}
		if ev.Payload != nil {
			if uri, ok := ev.Payload["uri"].(string); ok {
				artifact.URI = uri
			}
			if mime, ok := ev.Payload["mime_type"].(string); ok {
				artifact.MimeType = mime
			}
		}
		w.objStore.PutArtifact(artifact)
		w.verStore.PutVersion(schemas.ObjectVersion{
			ObjectID:        artifact.ArtifactID,
			ObjectType:      "artifact",
			Version:         ev.Version + 1,
			MutationEventID: ev.EventID,
			ValidFrom:       now,
		})

	case "state_update", "state_change", "checkpoint":
		state := schemas.State{
			StateID:            fmt.Sprintf("state_%s", ev.EventID),
			AgentID:            ev.AgentID,
			SessionID:          ev.SessionID,
			StateType:          ev.EventType,
			DerivedFromEventID: ev.EventID,
			CheckpointTS:       now,
			Version:            ev.Version + 1,
		}
		if ev.Payload != nil {
			if k, ok := ev.Payload["state_key"].(string); ok {
				state.StateKey = k
			}
			if v, ok := ev.Payload["state_value"].(string); ok {
				state.StateValue = v
			}
		}
		w.objStore.PutState(state)

	default:
		text := ""
		if ev.Payload != nil {
			if t, ok := ev.Payload["text"].(string); ok {
				text = t
			}
		}
		w.objStore.PutMemory(schemas.Memory{
			MemoryID:       fmt.Sprintf("mem_%s", ev.EventID),
			MemoryType:     "episodic",
			AgentID:        ev.AgentID,
			SessionID:      ev.SessionID,
			Level:          0,
			Content:        text,
			Confidence:     1.0,
			Importance:     ev.Importance,
			IsActive:       true,
			ValidFrom:      now,
			Version:        ev.Version + 1,
			SourceEventIDs: []string{ev.EventID},
		})
	}
	return nil
}
