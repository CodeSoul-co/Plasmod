package storage

import (
	"time"

	"andb/src/internal/schemas"
)

type SegmentRecord struct {
	SegmentID       string    `json:"segment_id"`
	ObjectType      string    `json:"object_type"`
	Namespace       string    `json:"namespace"`
	TimeBucket      string    `json:"time_bucket"`
	EmbeddingFamily string    `json:"embedding_family"`
	StorageRef      string    `json:"storage_ref"`
	IndexRef        string    `json:"index_ref"`
	RowCount        int       `json:"row_count"`
	MinTS           string    `json:"min_ts"`
	MaxTS           string    `json:"max_ts"`
	Tier            string    `json:"tier"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type IndexRecord struct {
	Namespace string    `json:"namespace"`
	Indexed   int       `json:"indexed"`
	UpdatedAt time.Time `json:"updated_at"`
}

type SegmentStore interface {
	Upsert(record SegmentRecord)
	List(namespace string) []SegmentRecord
}

type IndexStore interface {
	Upsert(record IndexRecord)
	List() []IndexRecord
}

// ObjectStore provides CRUD for the canonical first-class objects (Agent,
// Session, Memory, State, Artifact).  Each method is keyed by the object's
// primary-key string so the store remains generic.
type ObjectStore interface {
	PutAgent(obj schemas.Agent)
	GetAgent(id string) (schemas.Agent, bool)
	ListAgents() []schemas.Agent

	PutSession(obj schemas.Session)
	GetSession(id string) (schemas.Session, bool)
	ListSessions(agentID string) []schemas.Session

	PutEvent(obj schemas.Event)
	GetEvent(id string) (schemas.Event, bool)
	ListEvents(agentID, sessionID string) []schemas.Event

	PutMemory(obj schemas.Memory)
	GetMemory(id string) (schemas.Memory, bool)
	ListMemories(agentID, sessionID string) []schemas.Memory

	PutState(obj schemas.State)
	GetState(id string) (schemas.State, bool)
	ListStates(agentID, sessionID string) []schemas.State

	PutArtifact(obj schemas.Artifact)
	GetArtifact(id string) (schemas.Artifact, bool)
	ListArtifacts(sessionID string) []schemas.Artifact

	PutUser(obj schemas.User)
	GetUser(id string) (schemas.User, bool)
	ListUsers() []schemas.User
}

// GraphEdgeStore persists relation edges between canonical objects.
type GraphEdgeStore interface {
	PutEdge(edge schemas.Edge)
	GetEdge(id string) (schemas.Edge, bool)
	DeleteEdge(id string)
	// EdgesFrom returns all edges originating from the given object.
	// Implemented with a secondary src-index; O(k) where k = out-degree.
	EdgesFrom(srcObjectID string) []schemas.Edge
	// EdgesTo returns all edges pointing to the given object.
	// Implemented with a secondary dst-index; O(k) where k = in-degree.
	EdgesTo(dstObjectID string) []schemas.Edge
	// BulkEdges returns all edges between any of the given object IDs.
	BulkEdges(objectIDs []string) []schemas.Edge
	ListEdges() []schemas.Edge
	// PruneExpiredEdges removes all edges whose ExpiresAt is non-empty and
	// lexicographically ≤ now (RFC-3339 string).  Returns the count pruned.
	PruneExpiredEdges(now string) int
}

// SnapshotVersionStore persists object version / snapshot records.
type SnapshotVersionStore interface {
	PutVersion(v schemas.ObjectVersion)
	GetVersions(objectID string) []schemas.ObjectVersion
	LatestVersion(objectID string) (schemas.ObjectVersion, bool)
}

// PolicyStore persists governance policy records (append-only).
type PolicyStore interface {
	AppendPolicy(p schemas.PolicyRecord)
	GetPolicies(objectID string) []schemas.PolicyRecord
	ListPolicies() []schemas.PolicyRecord
}

// ShareContractStore persists sharing protocol contracts between scopes.
type ShareContractStore interface {
	PutContract(c schemas.ShareContract)
	GetContract(id string) (schemas.ShareContract, bool)
	ContractsByScope(scope string) []schemas.ShareContract
	ListContracts() []schemas.ShareContract
}

// AuditStore is an append-only log of memory governance actions.
// All operations that change memory visibility, sharing, or lifecycle
// should emit an AuditRecord here.
type AuditStore interface {
	AppendAudit(r schemas.AuditRecord)
	GetAudits(targetMemoryID string) []schemas.AuditRecord
	ListAudits() []schemas.AuditRecord
}

// MemoryAlgorithmStateStore persists per-memory, per-algorithm state records.
// Keyed by (memoryID, algorithmID) so multiple algorithms can coexist.
type MemoryAlgorithmStateStore interface {
	PutAlgorithmState(s schemas.MemoryAlgorithmState)
	GetAlgorithmState(memoryID, algorithmID string) (schemas.MemoryAlgorithmState, bool)
	ListAlgorithmStates(memoryID string) []schemas.MemoryAlgorithmState
}

// RuntimeStorage is the unified accessor for all in-process stores.
type RuntimeStorage interface {
	Segments() SegmentStore
	Indexes() IndexStore
	Objects() ObjectStore
	Edges() GraphEdgeStore
	Versions() SnapshotVersionStore
	Policies() PolicyStore
	Contracts() ShareContractStore
	// Audits returns the append-only memory governance audit log.
	Audits() AuditStore
	// AlgorithmStates returns the per-memory, per-algorithm state store.
	AlgorithmStates() MemoryAlgorithmStateStore
	// HotCache exposes the in-memory hot-object cache so the ingest path can
	// immediately promote high-salience objects for instant activation.
	HotCache() *HotObjectCache
}
