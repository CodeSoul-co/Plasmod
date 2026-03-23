package storage

import (
	"sync"
	"time"

	"andb/src/internal/schemas"
)

// Tier enumerates the three storage tiers in CogDB's tiered memory model.
type StorageTier int

const (
	// TierHot is the in-memory hot cache: bounded capacity, highest-salience
	// and most-recently-accessed objects reside here.  Index and metadata are
	// always in the hot tier.
	TierHot StorageTier = iota
	// TierWarm is the full in-memory store (the standard MemoryRuntimeStorage).
	// All objects live here until explicitly archived.
	TierWarm
	// TierCold is the cold/archived tier backed by disk (file or object storage).
	// In the current in-process implementation it is simulated with a separate
	// map and an artificial access latency to model the cold-path behaviour.
	TierCold
)

func (t StorageTier) String() string {
	switch t {
	case TierHot:
		return "hot"
	case TierWarm:
		return "warm"
	default:
		return "cold"
	}
}

// HotEntry wraps a canonical object in the hot cache with metadata used for
// eviction and promotion decisions.
type HotEntry struct {
	ObjectID      string
	ObjectType    string
	Payload       any
	SalienceScore float64
	AccessCount   int
	LastAccess    time.Time
	InsertedAt    time.Time
}

// hotness returns a composite score used for LRU/salience eviction.
// Higher = keep in hot tier longer.
func (e *HotEntry) hotness() float64 {
	recency := 1.0 / (float64(time.Since(e.LastAccess).Seconds()) + 1)
	return e.SalienceScore*0.6 + recency*0.3 + float64(e.AccessCount)*0.001
}

// HotObjectCache is a bounded in-memory cache for the most activation-critical
// objects (recent session memories, high-salience facts, current agent states).
// It is the fast lane of the memory activation path.
type HotObjectCache struct {
	mu      sync.RWMutex
	entries map[string]*HotEntry
	maxSize int
	// orderKey tracks insertion order for LRU eviction fallback
	order []string
}

func NewHotObjectCache(maxSize int) *HotObjectCache {
	if maxSize <= 0 {
		maxSize = 2000
	}
	return &HotObjectCache{
		entries: make(map[string]*HotEntry, maxSize),
		maxSize: maxSize,
		order:   make([]string, 0, maxSize),
	}
}

// Put inserts or refreshes an object in the hot cache with the given salience.
func (c *HotObjectCache) Put(objectID, objectType string, payload any, salience float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	if existing, ok := c.entries[objectID]; ok {
		existing.Payload = payload
		existing.SalienceScore = salience
		existing.LastAccess = now
		existing.AccessCount++
		return
	}

	// evict lowest-hotness entry when at capacity
	if len(c.order) >= c.maxSize {
		c.evictOne()
	}

	c.entries[objectID] = &HotEntry{
		ObjectID:      objectID,
		ObjectType:    objectType,
		Payload:       payload,
		SalienceScore: salience,
		AccessCount:   1,
		LastAccess:    now,
		InsertedAt:    now,
	}
	c.order = append(c.order, objectID)
}

// Get retrieves an entry, bumping its access count.
func (c *HotObjectCache) Get(objectID string) (*HotEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[objectID]
	if ok {
		e.AccessCount++
		e.LastAccess = time.Now()
	}
	return e, ok
}

// Contains reports whether an object is in the hot cache (no access bump).
func (c *HotObjectCache) Contains(objectID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.entries[objectID]
	return ok
}

// Evict explicitly removes an object from the hot cache.
func (c *HotObjectCache) Evict(objectID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.entries[objectID]; !ok {
		return
	}
	delete(c.entries, objectID)
	for i, id := range c.order {
		if id == objectID {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
}

// Len returns the number of objects currently in the hot cache.
func (c *HotObjectCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// evictOne removes the entry with the lowest hotness score.
// Must be called with c.mu held (write).
func (c *HotObjectCache) evictOne() {
	if len(c.order) == 0 {
		return
	}
	// scan for lowest hotness
	worstID := c.order[0]
	worstScore := c.entries[worstID].hotness()
	for _, id := range c.order[1:] {
		if score := c.entries[id].hotness(); score < worstScore {
			worstScore = score
			worstID = id
		}
	}
	delete(c.entries, worstID)
	for i, id := range c.order {
		if id == worstID {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
}

// ─── TieredObjectStore ────────────────────────────────────────────────────────

// TieredObjectStore routes reads and writes across the hot/warm/cold tiers.
// Hot reads are served from HotObjectCache.
// Warm reads fall through to the standard ObjectStore.
// Cold reads use the ColdObjectStore (disk-backed or simulated).
type TieredObjectStore struct {
	hot  *HotObjectCache
	warm ObjectStore
	cold ColdObjectStore
}

func NewTieredObjectStore(hot *HotObjectCache, warm ObjectStore, cold ColdObjectStore) *TieredObjectStore {
	return &TieredObjectStore{hot: hot, warm: warm, cold: cold}
}

// GetMemoryActivated returns a Memory with tier-aware activation.
// Hot cache hit → immediate return.
// Warm miss → warm store → promote to hot.
// Cold miss → cold store → promote to warm + hot.
func (t *TieredObjectStore) GetMemoryActivated(memoryID string, salience float64) (schemas.Memory, bool) {
	// hot path
	if entry, ok := t.hot.Get(memoryID); ok {
		if m, ok := entry.Payload.(schemas.Memory); ok {
			return m, true
		}
	}

	// warm path
	if m, ok := t.warm.GetMemory(memoryID); ok {
		t.hot.Put(memoryID, "memory", m, salience)
		return m, true
	}

	// cold path
	if m, ok := t.cold.GetMemory(memoryID); ok {
		t.warm.PutMemory(m)
		t.hot.Put(memoryID, "memory", m, salience*0.5)
		return m, true
	}

	return schemas.Memory{}, false
}

// PutMemory writes to warm store and promotes to hot if salience >= threshold.
func (t *TieredObjectStore) PutMemory(m schemas.Memory, salience float64) {
	t.warm.PutMemory(m)
	if salience >= 0.5 {
		t.hot.Put(m.MemoryID, "memory", m, salience)
	}
}

// ArchiveMemory moves a memory from warm to cold (e.g. on TTL expiry).
func (t *TieredObjectStore) ArchiveMemory(memoryID string) {
	if m, ok := t.warm.GetMemory(memoryID); ok {
		t.cold.PutMemory(m)
		t.hot.Evict(memoryID)
	}
}

// ArchiveEdge moves an edge from the warm GraphEdgeStore to the cold tier and
// deletes it from warm.  This is typically called when both endpoints have been
// archived, preventing dangling warm-tier edges (R6 / R7 fix).
func (t *TieredObjectStore) ArchiveEdge(warmEdges GraphEdgeStore, edgeID string) {
	if e, ok := warmEdges.GetEdge(edgeID); ok {
		t.cold.PutEdge(e)
		warmEdges.DeleteEdge(edgeID)
	}
}

// ─── ColdObjectStore ─────────────────────────────────────────────────────────

// ColdObjectStore is the interface for the cold/disk tier.
// In production this would be backed by a file-based or object storage engine.
//
// TODO(member-D): extend with PutArtifact/GetArtifact when artifact cold-tier
// promotion is needed.
type ColdObjectStore interface {
	PutMemory(m schemas.Memory)
	GetMemory(id string) (schemas.Memory, bool)
	PutAgent(a schemas.Agent)
	GetAgent(id string) (schemas.Agent, bool)
	PutState(s schemas.State)
	GetState(id string) (schemas.State, bool)
	// Edge cold-tier (R6): edges archived when their src/dst memory is promoted to cold.
	PutEdge(e schemas.Edge)
	GetEdge(id string) (schemas.Edge, bool)
	ListEdges() []schemas.Edge
}

// InMemoryColdStore is the in-process simulation of the cold tier.
// It is functionally identical to the warm store but models the architectural
// boundary.  A real implementation would replace this with a file/RocksDB backend.
type InMemoryColdStore struct {
	mu       sync.RWMutex
	memories map[string]schemas.Memory
	agents   map[string]schemas.Agent
	states   map[string]schemas.State
	edges    map[string]schemas.Edge
}

func NewInMemoryColdStore() *InMemoryColdStore {
	return &InMemoryColdStore{
		memories: map[string]schemas.Memory{},
		agents:   map[string]schemas.Agent{},
		states:   map[string]schemas.State{},
		edges:    map[string]schemas.Edge{},
	}
}

func (s *InMemoryColdStore) PutMemory(m schemas.Memory) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.memories[m.MemoryID] = m
}

func (s *InMemoryColdStore) GetMemory(id string) (schemas.Memory, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.memories[id]
	return m, ok
}

func (s *InMemoryColdStore) PutAgent(a schemas.Agent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agents[a.AgentID] = a
}

func (s *InMemoryColdStore) GetAgent(id string) (schemas.Agent, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.agents[id]
	return a, ok
}

func (s *InMemoryColdStore) PutState(st schemas.State) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[st.StateID] = st
}

func (s *InMemoryColdStore) GetState(id string) (schemas.State, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.states[id]
	return st, ok
}

func (s *InMemoryColdStore) PutEdge(e schemas.Edge) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.edges[e.EdgeID] = e
}

func (s *InMemoryColdStore) GetEdge(id string) (schemas.Edge, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.edges[id]
	return e, ok
}

func (s *InMemoryColdStore) ListEdges() []schemas.Edge {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]schemas.Edge, 0, len(s.edges))
	for _, e := range s.edges {
		out = append(out, e)
	}
	return out
}
