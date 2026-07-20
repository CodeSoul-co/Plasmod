package materialization

import (
	"fmt"
	"time"

	"plasmod/src/internal/eventbackbone"
	"plasmod/src/internal/schemas"
	"plasmod/src/internal/storage"
	"plasmod/src/internal/worker/nodes"
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
	ev = ev.NormalizeDynamicEventV04()
	if ev.Object.ObjectType != "" && ev.Object.ObjectID != "" {
		return schemas.NormalizeObjectTypeName(ev.Object.ObjectType), ev.Object.ObjectID
	}
	if ev.IsArtifactLike() ||
		ev.EventInfo.EventType == string(schemas.EventTypeToolCall) ||
		ev.EventInfo.EventType == string(schemas.EventTypeToolResult) {
		return string(schemas.ObjectTypeArtifact), ev.ArtifactIDOrDefault()
	}
	switch ev.EventInfo.EventType {
	case string(schemas.EventTypeStateUpdate), string(schemas.EventTypeStateChange), string(schemas.EventTypeCheckpoint):
		return string(schemas.ObjectTypeAgentState), schemas.CanonicalStateID(
			ev.Identity.TenantID,
			ev.Identity.WorkspaceID,
			ev.Actor.AgentID,
			ev.Actor.SessionID,
			ev.StateKey(),
		)
	default:
		return string(schemas.ObjectTypeMemory), schemas.IDPrefixMemory + ev.Identity.EventID
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
	ev = ev.NormalizeDynamicEventV04()
	now := time.Now().UTC().Format(time.RFC3339)
	if ev.IsArtifactLike() ||
		ev.EventInfo.EventType == string(schemas.EventTypeToolCall) ||
		ev.EventInfo.EventType == string(schemas.EventTypeToolResult) {
		access := schemas.CanonicalAccessFromEvent(ev)
		artifact := schemas.Artifact{
			ArtifactID:        ev.ArtifactIDOrDefault(),
			TenantID:          ev.Identity.TenantID,
			WorkspaceID:       ev.Identity.WorkspaceID,
			SessionID:         ev.Actor.SessionID,
			OwnerAgentID:      ev.Actor.AgentID,
			ArtifactType:      firstNonEmpty(ev.Object.ObjectSubtype, ev.EventInfo.EventType, string(schemas.ObjectTypeArtifact)),
			ProducedByEventID: ev.Identity.EventID,
			Version:           ev.Time.LogicalTS,
			MutationLSN:       ev.Time.WalLSN,
			MaterializedAt:    now,
			Access:            access,
		}
		if artifact.Version <= 0 {
			artifact.Version = 1
		}
		if ev.Payload != nil {
			if uri := ev.ArtifactURI(); uri != "" {
				artifact.URI = uri
			}
			if mime := ev.ArtifactMimeType(); mime != "" {
				artifact.MimeType = mime
			}
		}
		if artifact.MimeType == "" && artifact.URI == "" {
			artifact.MimeType = "text/plain"
		}
		if name := ev.ArtifactName(); name != "" {
			if artifact.Metadata == nil {
				artifact.Metadata = map[string]any{}
			}
			artifact.Metadata["name"] = name
		}
		if body := ev.ArtifactBodyString(); body != "" {
			if artifact.Metadata == nil {
				artifact.Metadata = map[string]any{}
			}
			artifact.Metadata["body"] = body
			artifact.ContentRef = "inline"
		}
		w.objStore.PutArtifact(artifact)
		for _, e := range schemas.BuildArtifactBaseEdges(artifact) {
			w.edgStore.PutEdge(e)
		}
		w.verStore.PutVersion(schemas.ObjectVersion{
			ObjectID:        artifact.ArtifactID,
			ObjectType:      string(schemas.ObjectTypeArtifact),
			Version:         artifact.Version,
			MutationEventID: ev.Identity.EventID,
			ValidFrom:       now,
			SnapshotTag:     "object_materialization",
			MutationLSN:     ev.Time.WalLSN,
			Snapshot:        canonicalWorkerSnapshot(artifact),
			Access:          access,
		})
		if w.derivLog != nil {
			w.derivLog.Append(ev.Identity.EventID, "event", artifact.ArtifactID, "artifact", "object_materialization")
		}

		// State objects are created exclusively by InMemoryStateMaterializationWorker
		// (DispatchStateMaterialization) to avoid duplicate State storage.
		// See Runtime.SubmitIngest for the synchronous call site.
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
