package coordinator

import (
	"plasmod/src/internal/schemas"
	"plasmod/src/internal/storage"
)

// ObjectCoordinator is the authoritative gateway for all canonical object
// mutations.  Every write goes through here so that version records and
// snapshot tags are produced consistently.
type ObjectCoordinator struct {
	store    storage.ObjectStore
	versions storage.SnapshotVersionStore
}

func NewObjectCoordinator(store storage.ObjectStore, versions storage.SnapshotVersionStore) *ObjectCoordinator {
	return &ObjectCoordinator{store: store, versions: versions}
}

// PutAgent persists an agent and records a version snapshot.
func (c *ObjectCoordinator) PutAgent(agent schemas.Agent, mutationEventID string) {
	c.store.PutAgent(agent)
	c.recordVersion(agent.AgentID, string(schemas.ObjectTypeAgent), mutationEventID)
}

// PutSession persists a session and records a version snapshot.
func (c *ObjectCoordinator) PutSession(session schemas.Session, mutationEventID string) {
	c.store.PutSession(session)
	c.recordVersion(session.SessionID, string(schemas.ObjectTypeSession), mutationEventID)
}

// PutMemory persists a memory object and records a version snapshot.
func (c *ObjectCoordinator) PutMemory(mem schemas.Memory, mutationEventID string) {
	c.store.PutMemory(mem)
	c.recordVersion(mem.MemoryID, string(schemas.ObjectTypeMemory), mutationEventID)
}

// PutState persists a state object and records a version snapshot.
func (c *ObjectCoordinator) PutState(state schemas.State, mutationEventID string) {
	c.store.PutState(state)
	c.recordVersion(state.StateID, string(schemas.ObjectTypeState), mutationEventID)
}

// PutArtifact persists an artifact and records a version snapshot.
func (c *ObjectCoordinator) PutArtifact(artifact schemas.Artifact, mutationEventID string) {
	c.store.PutArtifact(artifact)
	c.recordVersion(artifact.ArtifactID, string(schemas.ObjectTypeArtifact), mutationEventID)
}

func (c *ObjectCoordinator) recordVersion(objectID, objectType, mutationEventID string) {
	var nextVersion int64 = 1
	if latest, ok := c.versions.LatestVersion(objectID); ok {
		nextVersion = latest.Version + 1
	}
	c.versions.PutVersion(schemas.ObjectVersion{
		ObjectID:        objectID,
		ObjectType:      objectType,
		Version:         nextVersion,
		MutationEventID: mutationEventID,
	})
}
