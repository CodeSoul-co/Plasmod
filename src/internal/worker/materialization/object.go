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

func (w *InMemoryObjectMaterializationWorker) Run(input schemas.WorkerInput) (schemas.WorkerOutput, error) {
	in, ok := input.(schemas.ObjectMaterializationInput)
	if !ok {
		return schemas.ObjectMaterializationOutput{}, fmt.Errorf("object_mat: unexpected input type %T", input)
	}
	err := w.Materialize(in.Event)
	if err != nil {
		return schemas.ObjectMaterializationOutput{}, err
	}
	objType, objID := objectTypeFromEvent(in.Event)
	return schemas.ObjectMaterializationOutput{ObjectID: objID, ObjectType: objType}, nil
}

// objectTypeFromEvent mirrors the routing logic in Materialize to compute
// the canonical object type and deterministic ID for a given event.
func objectTypeFromEvent(ev schemas.Event) (objectType, objectID string) {
	switch ev.EventType {
	case string(schemas.EventTypeToolCall), string(schemas.EventTypeToolResult):
		return string(schemas.ObjectTypeArtifact), schemas.IDPrefixArtifact + ev.EventID
	case string(schemas.EventTypeStateUpdate), string(schemas.EventTypeStateChange), string(schemas.EventTypeCheckpoint):
		return string(schemas.ObjectTypeState), schemas.IDPrefixState + ev.EventID
	default:
		return string(schemas.ObjectTypeMemory), schemas.IDPrefixMemory + ev.EventID
	}
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
	case string(schemas.EventTypeToolCall), string(schemas.EventTypeToolResult):
		artifact := schemas.Artifact{
			ArtifactID:        schemas.IDPrefixArtifact + ev.EventID,
			SessionID:         ev.SessionID,
			OwnerAgentID:      ev.AgentID,
			ArtifactType:      ev.EventType,
			ProducedByEventID: ev.EventID,
			Version:           ev.Version,
		}
		if ev.Payload != nil {
			if uri, ok := ev.Payload[schemas.PayloadKeyURI].(string); ok {
				artifact.URI = uri
			}
			if mime, ok := ev.Payload[schemas.PayloadKeyMimeType].(string); ok {
				artifact.MimeType = mime
			}
		}
		w.objStore.PutArtifact(artifact)
		w.verStore.PutVersion(schemas.ObjectVersion{
			ObjectID:        artifact.ArtifactID,
			ObjectType:      string(schemas.ObjectTypeArtifact),
			Version:         ev.Version + 1,
			MutationEventID: ev.EventID,
			ValidFrom:       now,
		})

	case string(schemas.EventTypeStateUpdate), string(schemas.EventTypeStateChange), string(schemas.EventTypeCheckpoint):
		state := schemas.State{
			StateID:            schemas.IDPrefixState + ev.EventID,
			AgentID:            ev.AgentID,
			SessionID:          ev.SessionID,
			StateType:          ev.EventType,
			DerivedFromEventID: ev.EventID,
			CheckpointTS:       now,
			Version:            ev.Version + 1,
		}
		if ev.Payload != nil {
			if k, ok := ev.Payload[schemas.PayloadKeyStateKey].(string); ok {
				state.StateKey = k
			}
			if v, ok := ev.Payload[schemas.PayloadKeyStateValue].(string); ok {
				state.StateValue = v
			}
		}
		w.objStore.PutState(state)

	default:
		text := ""
		if ev.Payload != nil {
			if t, ok := ev.Payload[schemas.PayloadKeyText].(string); ok {
				text = t
			}
		}
		w.objStore.PutMemory(schemas.Memory{
			MemoryID:       schemas.IDPrefixMemory + ev.EventID,
			MemoryType:     string(schemas.MemoryTypeEpisodic),
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
