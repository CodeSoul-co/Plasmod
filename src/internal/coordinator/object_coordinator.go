package coordinator

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"plasmod/src/internal/schemas"
	"plasmod/src/internal/storage"
)

// ObjectCoordinator is the authoritative gateway for all canonical object
// mutations.  Every write goes through here so that version records and
// snapshot tags are produced consistently.
type ObjectCoordinator struct {
	store    storage.ObjectStore
	versions storage.SnapshotVersionStore
	mu       sync.Mutex
}

func NewObjectCoordinator(store storage.ObjectStore, versions storage.SnapshotVersionStore) *ObjectCoordinator {
	return &ObjectCoordinator{store: store, versions: versions}
}

// PutAgent persists an agent and records a version snapshot.
func (c *ObjectCoordinator) PutAgent(agent schemas.Agent, mutationEventID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store.PutAgent(agent)
	c.recordVersion(agent.AgentID, string(schemas.ObjectTypeAgent), mutationEventID, agent, 0, schemas.CanonicalAccess{})
}

// PutSession persists a session and records a version snapshot.
func (c *ObjectCoordinator) PutSession(session schemas.Session, mutationEventID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store.PutSession(session)
	c.recordVersion(session.SessionID, string(schemas.ObjectTypeSession), mutationEventID, session, 0, schemas.CanonicalAccess{})
}

// PutMemory persists a memory object and records a version snapshot.
func (c *ObjectCoordinator) PutMemory(mem schemas.Memory, mutationEventID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	nextVersion := c.nextVersion(mem.MemoryID)
	mem.Version = nextVersion
	c.store.PutMemory(mem)
	c.recordVersionWithNumber(mem.MemoryID, string(schemas.ObjectTypeMemory), nextVersion, mutationEventID, mem, mem.MutationLSN, mem.Access)
}

// PutState persists a state object and records a version snapshot.
func (c *ObjectCoordinator) PutState(state schemas.State, mutationEventID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	nextVersion := c.nextVersion(state.StateID)
	state.Version = nextVersion
	c.store.PutState(state)
	c.recordVersionWithNumber(state.StateID, string(schemas.ObjectTypeAgentState), nextVersion, mutationEventID, state, state.MutationLSN, state.Access)
}

// PutArtifact persists an artifact and records a version snapshot.
func (c *ObjectCoordinator) PutArtifact(artifact schemas.Artifact, mutationEventID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	nextVersion := c.nextVersion(artifact.ArtifactID)
	artifact.Version = nextVersion
	c.store.PutArtifact(artifact)
	c.recordVersionWithNumber(artifact.ArtifactID, string(schemas.ObjectTypeArtifact), nextVersion, mutationEventID, artifact, artifact.MutationLSN, artifact.Access)
}

func (c *ObjectCoordinator) nextVersion(objectID string) int64 {
	nextVersion := int64(1)
	if latest, ok := c.versions.LatestVersion(objectID); ok {
		nextVersion = latest.Version + 1
	}
	return nextVersion
}

func (c *ObjectCoordinator) recordVersion(
	objectID, objectType, mutationEventID string,
	snapshotValue any,
	mutationLSN int64,
	access schemas.CanonicalAccess,
) {
	c.recordVersionWithNumber(objectID, objectType, c.nextVersion(objectID), mutationEventID, snapshotValue, mutationLSN, access)
}

func (c *ObjectCoordinator) recordVersionWithNumber(
	objectID, objectType string,
	version int64,
	mutationEventID string,
	snapshotValue any,
	mutationLSN int64,
	access schemas.CanonicalAccess,
) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if latest, ok := c.versions.LatestVersion(objectID); ok && latest.ValidTo == "" {
		latest.ValidTo = now
		c.versions.PutVersion(latest)
	}
	if mutationEventID == "" {
		mutationEventID = fmt.Sprintf("direct:%s:%d", objectID, time.Now().UnixNano())
	}
	c.versions.PutVersion(schemas.ObjectVersion{
		ObjectID:        objectID,
		ObjectType:      objectType,
		Version:         version,
		MutationEventID: mutationEventID,
		ValidFrom:       now,
		SnapshotTag:     "direct_put",
		MutationLSN:     mutationLSN,
		Snapshot:        objectSnapshot(snapshotValue),
		Access:          access,
	})
}

func objectSnapshot(value any) map[string]any {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var snapshot map[string]any
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return nil
	}
	return snapshot
}
