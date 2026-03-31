package materialization

import (
	"fmt"
	"time"

	"andb/src/internal/eventbackbone"
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
	edgStore storage.GraphEdgeStore
	verStore storage.SnapshotVersionStore
	derivLog eventbackbone.DerivationLogger
}

func CreateInMemoryObjectMaterializationWorker(
	id string,
	objStore storage.ObjectStore,
	edgStore storage.GraphEdgeStore,
	verStore storage.SnapshotVersionStore,
	derivLog eventbackbone.DerivationLogger,
) *InMemoryObjectMaterializationWorker {
	return &InMemoryObjectMaterializationWorker{
		id:       id,
		objStore: objStore,
		edgStore: edgStore,
		verStore: verStore,
		derivLog: derivLog,
	}
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

// Materialize routes an event to its canonical store(s).
//
// Routing contract:
//   - tool_call / tool_result  → Artifact (ObjectStore)
//   - Memory objects are stored directly by Runtime.SubmitIngest via
//     TieredObjectStore.PutMemory (the richer MaterializeEvent output), NOT here.
//     The default case intentionally skips Memory to avoid duplicate storage.
//   - State creation is handled exclusively by InMemoryStateMaterializationWorker
//     (DispatchStateMaterialization), called synchronously in Runtime.SubmitIngest.
//     Having both ObjectMaterializationWorker and StateMaterializationWorker handle
//     State creates duplicate State objects with different field values for the same
//     event, so State is excluded from this worker.
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
		for _, e := range schemas.BuildArtifactBaseEdges(artifact) {
			w.edgStore.PutEdge(e)
		}
		w.verStore.PutVersion(schemas.ObjectVersion{
			ObjectID:        artifact.ArtifactID,
			ObjectType:      string(schemas.ObjectTypeArtifact),
			Version:         ev.Version + 1,
			MutationEventID: ev.EventID,
			ValidFrom:       now,
		})
		if w.derivLog != nil {
			w.derivLog.Append(ev.EventID, "event", artifact.ArtifactID, "artifact", "object_materialization")
		}

		// State objects are created exclusively by InMemoryStateMaterializationWorker
		// (DispatchStateMaterialization) to avoid duplicate State storage.
		// See Runtime.SubmitIngest for the synchronous call site.
	}
	return nil
}
