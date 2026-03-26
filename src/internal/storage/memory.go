package storage

import (
	"sync"
	"time"

	"andb/src/internal/schemas"
)

type memorySegmentStore struct {
	mu    sync.RWMutex
	items map[string]SegmentRecord
}

func newMemorySegmentStore() *memorySegmentStore {
	return &memorySegmentStore{items: map[string]SegmentRecord{}}
}

func (s *memorySegmentStore) Upsert(record SegmentRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record.UpdatedAt = time.Now().UTC()
	key := record.Namespace + ":" + record.SegmentID
	s.items[key] = record
}

func (s *memorySegmentStore) List(namespace string) []SegmentRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]SegmentRecord, 0, len(s.items))
	for _, item := range s.items {
		if namespace == "" || item.Namespace == namespace {
			out = append(out, item)
		}
	}
	return out
}

type memoryIndexStore struct {
	mu    sync.RWMutex
	items map[string]IndexRecord
}

func newMemoryIndexStore() *memoryIndexStore {
	return &memoryIndexStore{items: map[string]IndexRecord{}}
}

func (s *memoryIndexStore) Upsert(record IndexRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record.UpdatedAt = time.Now().UTC()
	s.items[record.Namespace] = record
}

func (s *memoryIndexStore) List() []IndexRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]IndexRecord, 0, len(s.items))
	for _, item := range s.items {
		out = append(out, item)
	}
	return out
}

// ─── ObjectStore ─────────────────────────────────────────────────────────────

type memoryObjectStore struct {
	mu        sync.RWMutex
	agents    map[string]schemas.Agent
	sessions  map[string]schemas.Session
	events    map[string]schemas.Event
	memories  map[string]schemas.Memory
	states    map[string]schemas.State
	artifacts map[string]schemas.Artifact
	users     map[string]schemas.User
}

func newMemoryObjectStore() *memoryObjectStore {
	return &memoryObjectStore{
		agents:    map[string]schemas.Agent{},
		sessions:  map[string]schemas.Session{},
		events:    map[string]schemas.Event{},
		memories:  map[string]schemas.Memory{},
		states:    map[string]schemas.State{},
		artifacts: map[string]schemas.Artifact{},
		users:     map[string]schemas.User{},
	}
}

func (s *memoryObjectStore) PutAgent(obj schemas.Agent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agents[obj.AgentID] = obj
}
func (s *memoryObjectStore) GetAgent(id string) (schemas.Agent, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.agents[id]
	return v, ok
}
func (s *memoryObjectStore) ListAgents() []schemas.Agent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]schemas.Agent, 0, len(s.agents))
	for _, v := range s.agents {
		out = append(out, v)
	}
	return out
}

func (s *memoryObjectStore) PutSession(obj schemas.Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[obj.SessionID] = obj
}
func (s *memoryObjectStore) GetSession(id string) (schemas.Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.sessions[id]
	return v, ok
}
func (s *memoryObjectStore) ListSessions(agentID string) []schemas.Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []schemas.Session{}
	for _, v := range s.sessions {
		if agentID == "" || v.AgentID == agentID {
			out = append(out, v)
		}
	}
	return out
}

func (s *memoryObjectStore) PutEvent(obj schemas.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events[obj.EventID] = obj
}

func (s *memoryObjectStore) GetEvent(id string) (schemas.Event, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.events[id]
	return v, ok
}

func (s *memoryObjectStore) ListEvents(agentID, sessionID string) []schemas.Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []schemas.Event{}
	for _, v := range s.events {
		if (agentID == "" || v.AgentID == agentID) &&
			(sessionID == "" || v.SessionID == sessionID) {
			out = append(out, v)
		}
	}
	return out
}

func (s *memoryObjectStore) PutMemory(obj schemas.Memory) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.memories[obj.MemoryID] = obj
}
func (s *memoryObjectStore) GetMemory(id string) (schemas.Memory, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.memories[id]
	return v, ok
}
func (s *memoryObjectStore) ListMemories(agentID, sessionID string) []schemas.Memory {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []schemas.Memory{}
	for _, v := range s.memories {
		if (agentID == "" || v.AgentID == agentID) &&
			(sessionID == "" || v.SessionID == sessionID) {
			out = append(out, v)
		}
	}
	return out
}

func (s *memoryObjectStore) PutState(obj schemas.State) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[obj.StateID] = obj
}
func (s *memoryObjectStore) GetState(id string) (schemas.State, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.states[id]
	return v, ok
}
func (s *memoryObjectStore) ListStates(agentID, sessionID string) []schemas.State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []schemas.State{}
	for _, v := range s.states {
		if (agentID == "" || v.AgentID == agentID) &&
			(sessionID == "" || v.SessionID == sessionID) {
			out = append(out, v)
		}
	}
	return out
}

func (s *memoryObjectStore) PutArtifact(obj schemas.Artifact) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.artifacts[obj.ArtifactID] = obj
}
func (s *memoryObjectStore) GetArtifact(id string) (schemas.Artifact, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.artifacts[id]
	return v, ok
}
func (s *memoryObjectStore) ListArtifacts(sessionID string) []schemas.Artifact {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []schemas.Artifact{}
	for _, v := range s.artifacts {
		if sessionID == "" || v.SessionID == sessionID {
			out = append(out, v)
		}
	}
	return out
}

func (s *memoryObjectStore) PutUser(obj schemas.User) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users[obj.UserID] = obj
}
func (s *memoryObjectStore) GetUser(id string) (schemas.User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.users[id]
	return v, ok
}
func (s *memoryObjectStore) ListUsers() []schemas.User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]schemas.User, 0, len(s.users))
	for _, v := range s.users {
		out = append(out, v)
	}
	return out
}

// ─── GraphEdgeStore ───────────────────────────────────────────────────────────

// memoryGraphEdgeStore stores edges in a primary map keyed by EdgeID plus two
// secondary inverted indices (srcIdx, dstIdx) so EdgesFrom/EdgesTo are O(k)
// where k is the degree of the queried node rather than O(n) over all edges.
type memoryGraphEdgeStore struct {
	mu     sync.RWMutex
	edges  map[string]schemas.Edge
	srcIdx map[string]map[string]bool // srcObjectID → set of edgeIDs
	dstIdx map[string]map[string]bool // dstObjectID → set of edgeIDs
}

func newMemoryGraphEdgeStore() *memoryGraphEdgeStore {
	return &memoryGraphEdgeStore{
		edges:  map[string]schemas.Edge{},
		srcIdx: map[string]map[string]bool{},
		dstIdx: map[string]map[string]bool{},
	}
}

func (s *memoryGraphEdgeStore) PutEdge(edge schemas.Edge) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// If edge already exists, remove old index entries first.
	if old, ok := s.edges[edge.EdgeID]; ok {
		s.removeFromIdx(old)
	}
	s.edges[edge.EdgeID] = edge
	s.addToIdx(edge)
}

func (s *memoryGraphEdgeStore) GetEdge(id string) (schemas.Edge, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.edges[id]
	return v, ok
}

// addToIdx adds edge to both secondary indices. Must be called with lock held.
func (s *memoryGraphEdgeStore) addToIdx(e schemas.Edge) {
	if s.srcIdx[e.SrcObjectID] == nil {
		s.srcIdx[e.SrcObjectID] = map[string]bool{}
	}
	s.srcIdx[e.SrcObjectID][e.EdgeID] = true
	if s.dstIdx[e.DstObjectID] == nil {
		s.dstIdx[e.DstObjectID] = map[string]bool{}
	}
	s.dstIdx[e.DstObjectID][e.EdgeID] = true
}

// removeFromIdx removes edge from both secondary indices. Must be called with lock held.
func (s *memoryGraphEdgeStore) removeFromIdx(e schemas.Edge) {
	if m, ok := s.srcIdx[e.SrcObjectID]; ok {
		delete(m, e.EdgeID)
		if len(m) == 0 {
			delete(s.srcIdx, e.SrcObjectID)
		}
	}
	if m, ok := s.dstIdx[e.DstObjectID]; ok {
		delete(m, e.EdgeID)
		if len(m) == 0 {
			delete(s.dstIdx, e.DstObjectID)
		}
	}
}

// EdgesFrom returns all edges originating from srcObjectID in O(k) time.
func (s *memoryGraphEdgeStore) EdgesFrom(srcObjectID string) []schemas.Edge {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := s.srcIdx[srcObjectID]
	out := make([]schemas.Edge, 0, len(ids))
	for id := range ids {
		if e, ok := s.edges[id]; ok {
			out = append(out, e)
		}
	}
	return out
}

// EdgesTo returns all edges pointing to dstObjectID in O(k) time.
func (s *memoryGraphEdgeStore) EdgesTo(dstObjectID string) []schemas.Edge {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := s.dstIdx[dstObjectID]
	out := make([]schemas.Edge, 0, len(ids))
	for id := range ids {
		if e, ok := s.edges[id]; ok {
			out = append(out, e)
		}
	}
	return out
}

func (s *memoryGraphEdgeStore) DeleteEdge(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.edges[id]; ok {
		s.removeFromIdx(e)
		delete(s.edges, id)
	}
}

func (s *memoryGraphEdgeStore) BulkEdges(objectIDs []string) []schemas.Edge {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := make(map[string]bool)
	out := []schemas.Edge{}
	for _, id := range objectIDs {
		for eid := range s.srcIdx[id] {
			if !seen[eid] {
				seen[eid] = true
				if e, ok := s.edges[eid]; ok {
					out = append(out, e)
				}
			}
		}
		for eid := range s.dstIdx[id] {
			if !seen[eid] {
				seen[eid] = true
				if e, ok := s.edges[eid]; ok {
					out = append(out, e)
				}
			}
		}
	}
	return out
}

func (s *memoryGraphEdgeStore) ListEdges() []schemas.Edge {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]schemas.Edge, 0, len(s.edges))
	for _, e := range s.edges {
		out = append(out, e)
	}
	return out
}

// PruneExpiredEdges removes all edges whose ExpiresAt is non-empty and ≤ now
// (lexicographic RFC-3339 comparison).  Returns the number of edges pruned.
func (s *memoryGraphEdgeStore) PruneExpiredEdges(now string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	pruned := 0
	for id, e := range s.edges {
		if e.ExpiresAt != "" && e.ExpiresAt <= now {
			s.removeFromIdx(e)
			delete(s.edges, id)
			pruned++
		}
	}
	return pruned
}

// ─── SnapshotVersionStore ─────────────────────────────────────────────────────

type memorySnapshotVersionStore struct {
	mu       sync.RWMutex
	versions map[string][]schemas.ObjectVersion
}

func newMemorySnapshotVersionStore() *memorySnapshotVersionStore {
	return &memorySnapshotVersionStore{versions: map[string][]schemas.ObjectVersion{}}
}

func (s *memorySnapshotVersionStore) PutVersion(v schemas.ObjectVersion) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.versions[v.ObjectID] = append(s.versions[v.ObjectID], v)
}
func (s *memorySnapshotVersionStore) GetVersions(objectID string) []schemas.ObjectVersion {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]schemas.ObjectVersion{}, s.versions[objectID]...)
}
func (s *memorySnapshotVersionStore) LatestVersion(objectID string) (schemas.ObjectVersion, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	vs := s.versions[objectID]
	if len(vs) == 0 {
		return schemas.ObjectVersion{}, false
	}
	latest := vs[0]
	for _, v := range vs[1:] {
		if v.Version > latest.Version {
			latest = v
		}
	}
	return latest, true
}

// ─── PolicyStore ──────────────────────────────────────────────────────────────

type memoryPolicyStore struct {
	mu       sync.RWMutex
	policies map[string][]schemas.PolicyRecord
}

func newMemoryPolicyStore() *memoryPolicyStore {
	return &memoryPolicyStore{policies: map[string][]schemas.PolicyRecord{}}
}

func (s *memoryPolicyStore) AppendPolicy(p schemas.PolicyRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policies[p.ObjectID] = append(s.policies[p.ObjectID], p)
}
func (s *memoryPolicyStore) GetPolicies(objectID string) []schemas.PolicyRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]schemas.PolicyRecord{}, s.policies[objectID]...)
}
func (s *memoryPolicyStore) ListPolicies() []schemas.PolicyRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []schemas.PolicyRecord{}
	for _, ps := range s.policies {
		out = append(out, ps...)
	}
	return out
}

// ─── ShareContractStore ───────────────────────────────────────────────────────

type memoryShareContractStore struct {
	mu        sync.RWMutex
	contracts map[string]schemas.ShareContract
}

func newMemoryShareContractStore() *memoryShareContractStore {
	return &memoryShareContractStore{contracts: map[string]schemas.ShareContract{}}
}

func (s *memoryShareContractStore) PutContract(c schemas.ShareContract) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.contracts[c.ContractID] = c
}
func (s *memoryShareContractStore) GetContract(id string) (schemas.ShareContract, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.contracts[id]
	return v, ok
}
func (s *memoryShareContractStore) ContractsByScope(scope string) []schemas.ShareContract {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []schemas.ShareContract{}
	for _, c := range s.contracts {
		if c.Scope == scope {
			out = append(out, c)
		}
	}
	return out
}
func (s *memoryShareContractStore) ListContracts() []schemas.ShareContract {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]schemas.ShareContract, 0, len(s.contracts))
	for _, c := range s.contracts {
		out = append(out, c)
	}
	return out
}

// ─── AuditStore ───────────────────────────────────────────────────────────────

type inMemoryAuditStore struct {
	mu      sync.RWMutex
	records map[string][]schemas.AuditRecord // keyed by TargetMemoryID
}

func newInMemoryAuditStore() *inMemoryAuditStore {
	return &inMemoryAuditStore{records: map[string][]schemas.AuditRecord{}}
}

func (s *inMemoryAuditStore) AppendAudit(r schemas.AuditRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[r.TargetMemoryID] = append(s.records[r.TargetMemoryID], r)
}

func (s *inMemoryAuditStore) GetAudits(targetMemoryID string) []schemas.AuditRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]schemas.AuditRecord{}, s.records[targetMemoryID]...)
}

func (s *inMemoryAuditStore) ListAudits() []schemas.AuditRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []schemas.AuditRecord{}
	for _, rs := range s.records {
		out = append(out, rs...)
	}
	return out
}

// ─── MemoryAlgorithmStateStore ────────────────────────────────────────────────

type inMemoryAlgorithmStateStore struct {
	mu     sync.RWMutex
	states map[string]schemas.MemoryAlgorithmState // keyed by memoryID+":"+algorithmID
}

func newInMemoryAlgorithmStateStore() *inMemoryAlgorithmStateStore {
	return &inMemoryAlgorithmStateStore{states: map[string]schemas.MemoryAlgorithmState{}}
}

func algoStateKey(memoryID, algorithmID string) string { return memoryID + ":" + algorithmID }

func (s *inMemoryAlgorithmStateStore) PutAlgorithmState(st schemas.MemoryAlgorithmState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[algoStateKey(st.MemoryID, st.AlgorithmID)] = st
}

func (s *inMemoryAlgorithmStateStore) GetAlgorithmState(memoryID, algorithmID string) (schemas.MemoryAlgorithmState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.states[algoStateKey(memoryID, algorithmID)]
	return v, ok
}

func (s *inMemoryAlgorithmStateStore) ListAlgorithmStates(memoryID string) []schemas.MemoryAlgorithmState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []schemas.MemoryAlgorithmState{}
	prefix := memoryID + ":"
	for k, v := range s.states {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			out = append(out, v)
		}
	}
	return out
}

// ─── MemoryRuntimeStorage ─────────────────────────────────────────────────────

type MemoryRuntimeStorage struct {
	segmentStore  *memorySegmentStore
	indexStore    *memoryIndexStore
	objectStore   *memoryObjectStore
	edgeStore     *memoryGraphEdgeStore
	versionStore  *memorySnapshotVersionStore
	policyStore   *memoryPolicyStore
	contractStore *memoryShareContractStore
	auditStore    *inMemoryAuditStore
	algoStore     *inMemoryAlgorithmStateStore
	hotCache      *HotObjectCache
}

func NewMemoryRuntimeStorage() *MemoryRuntimeStorage {
	return &MemoryRuntimeStorage{
		segmentStore:  newMemorySegmentStore(),
		indexStore:    newMemoryIndexStore(),
		objectStore:   newMemoryObjectStore(),
		edgeStore:     newMemoryGraphEdgeStore(),
		versionStore:  newMemorySnapshotVersionStore(),
		policyStore:   newMemoryPolicyStore(),
		contractStore: newMemoryShareContractStore(),
		auditStore:    newInMemoryAuditStore(),
		algoStore:     newInMemoryAlgorithmStateStore(),
		hotCache:      NewHotObjectCache(2000),
	}
}

func (s *MemoryRuntimeStorage) Segments() SegmentStore                     { return s.segmentStore }
func (s *MemoryRuntimeStorage) Indexes() IndexStore                        { return s.indexStore }
func (s *MemoryRuntimeStorage) Objects() ObjectStore                       { return s.objectStore }
func (s *MemoryRuntimeStorage) Edges() GraphEdgeStore                      { return s.edgeStore }
func (s *MemoryRuntimeStorage) Versions() SnapshotVersionStore             { return s.versionStore }
func (s *MemoryRuntimeStorage) Policies() PolicyStore                      { return s.policyStore }
func (s *MemoryRuntimeStorage) Contracts() ShareContractStore              { return s.contractStore }
func (s *MemoryRuntimeStorage) Audits() AuditStore                         { return s.auditStore }
func (s *MemoryRuntimeStorage) AlgorithmStates() MemoryAlgorithmStateStore { return s.algoStore }
func (s *MemoryRuntimeStorage) HotCache() *HotObjectCache                  { return s.hotCache }

// ─── Exported constructors for hybrid / composite runtimes ───────────────────
// Used by BuildRuntimeFromEnv when selecting per-store backends (memory vs Badger).

func NewMemorySegmentStore() SegmentStore        { return newMemorySegmentStore() }
func NewMemoryIndexStore() IndexStore           { return newMemoryIndexStore() }
func NewMemoryObjectStore() ObjectStore         { return newMemoryObjectStore() }
func NewMemoryGraphEdgeStore() GraphEdgeStore   { return newMemoryGraphEdgeStore() }
func NewMemorySnapshotVersionStore() SnapshotVersionStore { return newMemorySnapshotVersionStore() }
func NewMemoryPolicyStore() PolicyStore         { return newMemoryPolicyStore() }
func NewMemoryShareContractStore() ShareContractStore { return newMemoryShareContractStore() }
