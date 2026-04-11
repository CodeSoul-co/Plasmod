package storage

import (
	"sort"
	"strings"
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

// Clear removes all entries (used by admin full data wipe).
func (c *HotObjectCache) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*HotEntry)
	c.order = c.order[:0]
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
// hotThreshold controls the minimum salience required to promote a memory to the hot cache
// (defaults to schemas.DefaultAlgorithmConfig().HotTierSalienceThreshold).
type MemoryEmbedder interface {
	Generate(text string) ([]float32, error)
}

type TieredObjectStore struct {
	hot          *HotObjectCache
	warm         ObjectStore
	warmEdge     GraphEdgeStore
	cold         ColdObjectStore
	embedder     MemoryEmbedder
	hotThreshold float64
}

// DeleteMemoryEmbedding removes a cold-tier embedding for the given memory ID.
// This is best-effort cleanup used by admin dataset deletion to reduce cold-tier bloat.
func (t *TieredObjectStore) DeleteMemoryEmbedding(memoryID string) error {
	if t == nil || t.cold == nil {
		return nil
	}
	return t.cold.DeleteMemoryEmbedding(memoryID)
}

// SoftDeleteMemoryTierCleanup runs after canonical Memory soft-delete (IsActive=false) was
// written to ObjectStore. It evicts the hot-tier copy so stale active payloads are not served;
// it does not remove cold embeddings — those stay aligned with the warm Memory row until
// hard delete (purge / HardDeleteMemory).
func (t *TieredObjectStore) SoftDeleteMemoryTierCleanup(memoryID string) {
	if t == nil || t.hot == nil {
		return
	}
	t.hot.Evict(memoryID)
}

// HotCache returns the hot object cache (may be nil).
func (t *TieredObjectStore) HotCache() *HotObjectCache {
	if t == nil {
		return nil
	}
	return t.hot
}

// HardDeleteMemory removes a memory across hot, warm, cold tiers and graph edges.
// It does not enforce IsActive or selector rules; callers must apply policy first.
func (t *TieredObjectStore) HardDeleteMemory(memoryID string) {
	if t == nil {
		return
	}
	if t.hot != nil {
		t.hot.Evict(memoryID)
	}
	if t.warmEdge != nil {
		for _, e := range t.warmEdge.BulkEdges([]string{memoryID}) {
			t.warmEdge.DeleteEdge(e.EdgeID)
		}
	}
	if t.cold != nil {
		_ = t.cold.DeleteMemoryEmbedding(memoryID)
		_ = t.cold.DeleteMemory(memoryID)
		if ids, err := t.cold.ListEdgeIDsByObjectID(memoryID); err == nil {
			for _, id := range ids {
				_ = t.cold.DeleteEdge(id)
			}
		} else {
			// Fallback for cold stores that cannot index edges by object ID.
			// Note: S3ColdStore.ListEdges returns empty by design.
			for _, e := range t.cold.ListEdges() {
				if e.SrcObjectID == memoryID || e.DstObjectID == memoryID {
					_ = t.cold.DeleteEdge(e.EdgeID)
				}
			}
		}
	}
	if t.warm != nil {
		t.warm.DeleteMemory(memoryID)
	}
}

// ClearColdIfInMemory wipes the in-process cold tier when present. S3-backed cold stores
// are not enumerated/deleted; returns "s3_not_cleared" in that case.
func (t *TieredObjectStore) ClearColdIfInMemory() string {
	if t == nil || t.cold == nil {
		return "none"
	}
	if im, ok := t.cold.(*InMemoryColdStore); ok {
		im.ClearAll()
		return "in_memory_cleared"
	}
	return "s3_not_cleared"
}

func NewTieredObjectStore(hot *HotObjectCache, warm ObjectStore, warmEdge GraphEdgeStore, cold ColdObjectStore) *TieredObjectStore {
	return NewTieredObjectStoreWithThreshold(hot, warm, warmEdge, cold, schemas.DefaultAlgorithmConfig().HotTierSalienceThreshold)
}

func NewTieredObjectStoreWithEmbedder(
	hot *HotObjectCache,
	warm ObjectStore,
	warmEdge GraphEdgeStore,
	cold ColdObjectStore,
	embedder MemoryEmbedder,
	hotThreshold float64,
) *TieredObjectStore {
	if hot == nil {
		hot = NewHotObjectCache(0)
	}
	if cold == nil {
		cold = NewInMemoryColdStore()
	}
	return &TieredObjectStore{
		hot:          hot,
		warm:         warm,
		warmEdge:     warmEdge,
		cold:         cold,
		embedder:     embedder,
		hotThreshold: hotThreshold,
	}
}

// NewTieredObjectStoreWithThreshold creates a TieredObjectStore with an explicit hot-tier
// salience threshold. Use this when the default threshold (0.5) needs tuning.
func NewTieredObjectStoreWithThreshold(hot *HotObjectCache, warm ObjectStore, warmEdge GraphEdgeStore, cold ColdObjectStore, hotThreshold float64) *TieredObjectStore {
	return NewTieredObjectStoreWithEmbedder(hot, warm, warmEdge, cold, nil, hotThreshold)
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
		_ = t.cold.DeleteMemoryEmbedding(memoryID)
		return m, true
	}

	return schemas.Memory{}, false
}

// PutMemory writes to warm store and promotes to hot if salience >= threshold.
func (t *TieredObjectStore) PutMemory(m schemas.Memory, salience float64) {
	if t.warm != nil {
		t.warm.PutMemory(m)
	}
	if t.warmEdge != nil {
		for _, e := range schemas.BuildMemoryBaseEdges(m) {
			t.warmEdge.PutEdge(e)
		}
	}
	if salience >= t.hotThreshold {
		t.hot.Put(m.MemoryID, "memory", m, salience)
	}
}

// ArchiveMemory moves a memory from warm to cold (e.g. on TTL expiry).
func (t *TieredObjectStore) ArchiveMemory(memoryID string) {
	if t.warm == nil {
		return
	}
	if m, ok := t.warm.GetMemory(memoryID); ok {
		t.cold.PutMemory(m)

		if t.embedder != nil {
			textForEmbedding := m.Content
			if strings.TrimSpace(textForEmbedding) == "" {
				textForEmbedding = m.Summary
			}
			if strings.TrimSpace(textForEmbedding) != "" {
				if vec, err := t.embedder.Generate(textForEmbedding); err == nil && len(vec) > 0 {
					_ = t.cold.PutMemoryEmbedding(memoryID, vec)
				}
			}
		}

		t.hot.Evict(memoryID)
	}
}

// GetStateActivated returns a State from warm first, then cold.
// On cold hit, the state is promoted back to warm.
func (t *TieredObjectStore) GetStateActivated(stateID string) (schemas.State, bool) {
	if st, ok := t.warm.GetState(stateID); ok {
		return st, true
	}
	if st, ok := t.cold.GetState(stateID); ok {
		t.warm.PutState(st)
		return st, true
	}
	return schemas.State{}, false
}

// ArchiveState moves a state object from warm to cold.
func (t *TieredObjectStore) ArchiveState(stateID string) {
	if st, ok := t.warm.GetState(stateID); ok {
		t.cold.PutState(st)
	}
}

// GetArtifactActivated returns an Artifact from warm first, then cold.
// On cold hit, the artifact is promoted back to warm.
func (t *TieredObjectStore) GetArtifactActivated(artifactID string) (schemas.Artifact, bool) {
	if art, ok := t.warm.GetArtifact(artifactID); ok {
		return art, true
	}
	if art, ok := t.cold.GetArtifact(artifactID); ok {
		t.warm.PutArtifact(art)
		return art, true
	}
	return schemas.Artifact{}, false
}

// ArchiveArtifact moves an artifact object from warm to cold.
func (t *TieredObjectStore) ArchiveArtifact(artifactID string) {
	if art, ok := t.warm.GetArtifact(artifactID); ok {
		t.cold.PutArtifact(art)
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

// ColdSearch delegates to the cold store's ColdSearch implementation, returning
// the topK memory IDs most relevant to the query text from the cold tier.
func (t *TieredObjectStore) ColdSearch(query string, topK int) []string {
	return t.cold.ColdSearch(query, topK)
}

func (t *TieredObjectStore) ColdVectorSearch(queryVec []float32, topK int) []string {
	return t.cold.ColdVectorSearch(queryVec, topK)
}

func (t *TieredObjectStore) ColdHNSWSearch(queryVec []float32, topK int) []string {
	if t == nil || t.cold == nil || topK <= 0 || len(queryVec) == 0 {
		return nil
	}
	if hnsw, ok := t.cold.(ColdHNSWSearcher); ok {
		return hnsw.ColdHNSWSearch(queryVec, topK)
	}
	return nil
}

// ArchiveColdRecord persists an ingest record directly to the cold tier.
// This is called by TieredDataPlane when an object is explicitly archived
// (e.g. on TTL expiry or manual tier migration) rather than through the
// normal hot→warm→cold lifecycle.  The cold store writes the record as a
// Memory object so it is queryable via ColdSearch.
func (t *TieredObjectStore) ArchiveColdRecord(memoryID, text string, attrs map[string]string, ns string, ts int64) {
	// Prefer archiving the full canonical Memory from the warm tier so cold-path
	// rehydration preserves all fields (summary, provenance, source events,
	// memory type, confidence, etc.). Fall back to reconstructing a minimal
	// Memory only when the warm tier does not currently hold the object.
	if t.warm != nil {
		if m, ok := t.warm.GetMemory(memoryID); ok {
			m.IsActive = false
			if m.Version == 0 {
				m.Version = ts
			}
			if m.Content == "" {
				m.Content = text
			}
			if m.AgentID == "" {
				m.AgentID = attrs["agent_id"]
			}
			if m.SessionID == "" {
				m.SessionID = attrs["session_id"]
			}
			if m.Scope == "" {
				m.Scope = attrs["visibility"]
			}
			if m.OwnerType == "" {
				m.OwnerType = attrs["event_type"]
			}
			t.cold.PutMemory(m)
			return
		}
	}

	// Fallback path: reconstruct the minimal canonical Memory from ingest data.
	m := schemas.Memory{
		MemoryID:  memoryID,
		Content:   text,
		Scope:     attrs["visibility"], // visibility maps to Memory.Scope (access boundary)
		OwnerType: attrs["event_type"], // event_type is the best proxy for owner_type in cold archival
		AgentID:   attrs["agent_id"],
		SessionID: attrs["session_id"],
		Version:   ts,
		IsActive:  false,
	}
	t.cold.PutMemory(m)
}

// ─── ColdObjectStore ─────────────────────────────────────────────────────────
// ColdHNSWSearcher is an optional capability for cold stores that can
// execute HNSW-based ANN search over archived embeddings.
// Stores that do not support HNSW simply do not implement this interface.
type ColdHNSWSearcher interface {
	ColdHNSWSearch(queryVec []float32, topK int) []string
}

// ColdObjectStore is the interface for the cold/disk tier.
// In production this would be backed by a file-based or object storage engine.
type ColdObjectStore interface {
	PutMemory(m schemas.Memory)
	GetMemory(id string) (schemas.Memory, bool)
	// DeleteMemory removes the cold-tier memory record (best-effort).
	DeleteMemory(id string) error
	PutAgent(a schemas.Agent)
	GetAgent(id string) (schemas.Agent, bool)
	PutState(s schemas.State)
	GetState(id string) (schemas.State, bool)
	PutMemoryEmbedding(memoryID string, vec []float32) error
	GetMemoryEmbedding(memoryID string) ([]float32, bool, error)
	DeleteMemoryEmbedding(memoryID string) error
	PutArtifact(a schemas.Artifact)
	GetArtifact(id string) (schemas.Artifact, bool)
	// Edge cold-tier (R6): edges archived when their src/dst memory is promoted to cold.
	PutEdge(e schemas.Edge)
	GetEdge(id string) (schemas.Edge, bool)
	DeleteEdge(id string) error
	// ListEdgeIDsByObjectID returns all cold-tier edge IDs incident to objectID.
	// Implementations should be O(k) where k is node degree, not O(n) over all edges.
	ListEdgeIDsByObjectID(objectID string) ([]string, error)
	ListEdges() []schemas.Edge
	// ColdSearch performs a lexical substring search over all cold-tier memories.
	// This is used by TieredDataPlane when IncludeCold=true in SearchInput.
	// Returns memory IDs matching the query text, sorted by recency (newest first).
	ColdSearch(query string, topK int) []string
	ColdVectorSearch(queryVec []float32, topK int) []string
}

// InMemoryColdStore is the in-process simulation of the cold tier.
// It is functionally identical to the warm store but models the architectural
// boundary.  A real implementation would replace this with a file/RocksDB backend.
type InMemoryColdStore struct {
	mu         sync.RWMutex
	memories   map[string]schemas.Memory
	agents     map[string]schemas.Agent
	states     map[string]schemas.State
	artifacts  map[string]schemas.Artifact
	embeddings map[string][]float32
	edges      map[string]schemas.Edge
}

func NewInMemoryColdStore() *InMemoryColdStore {
	return &InMemoryColdStore{
		memories:   map[string]schemas.Memory{},
		agents:     map[string]schemas.Agent{},
		states:     map[string]schemas.State{},
		artifacts:  map[string]schemas.Artifact{},
		embeddings: map[string][]float32{},
		edges:      map[string]schemas.Edge{},
	}
}

// ClearAll wipes the simulated cold tier in memory (admin wipe; keeps the same store pointer).
func (s *InMemoryColdStore) ClearAll() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.memories = map[string]schemas.Memory{}
	s.agents = map[string]schemas.Agent{}
	s.states = map[string]schemas.State{}
	s.artifacts = map[string]schemas.Artifact{}
	s.embeddings = map[string][]float32{}
	s.edges = map[string]schemas.Edge{}
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

func (s *InMemoryColdStore) DeleteMemory(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.memories, id)
	return nil
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

func (s *InMemoryColdStore) PutArtifact(art schemas.Artifact) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.artifacts[art.ArtifactID] = art
}

func (s *InMemoryColdStore) GetArtifact(id string) (schemas.Artifact, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	art, ok := s.artifacts[id]
	return art, ok
}

func (s *InMemoryColdStore) PutMemoryEmbedding(memoryID string, vec []float32) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	copied := make([]float32, len(vec))
	copy(copied, vec)
	s.embeddings[memoryID] = copied
	return nil
}

func (s *InMemoryColdStore) GetMemoryEmbedding(memoryID string) ([]float32, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	vec, ok := s.embeddings[memoryID]
	if !ok {
		return nil, false, nil
	}
	copied := make([]float32, len(vec))
	copy(copied, vec)
	return copied, true, nil
}

func (s *InMemoryColdStore) DeleteMemoryEmbedding(memoryID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.embeddings, memoryID)
	return nil
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

func (s *InMemoryColdStore) DeleteEdge(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.edges, id)
	return nil
}

func (s *InMemoryColdStore) ListEdgeIDsByObjectID(objectID string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0)
	for id, e := range s.edges {
		if e.SrcObjectID == objectID || e.DstObjectID == objectID {
			out = append(out, id)
		}
	}
	return out, nil
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

// ColdSearch performs a lexical substring search over all cold-tier memories,
// returning the most recent topK matching memory IDs.  This models the
// cold-path search boundary: cold data is queried only by need, not on every
// request.
func (s *InMemoryColdStore) ColdSearch(query string, topK int) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	type scored struct {
		id    string
		score float64
		ts    int64
	}
	var results []scored
	lq := strings.ToLower(query)

	for id, m := range s.memories {
		text := strings.ToLower(m.Content)
		summary := strings.ToLower(m.Summary)
		var score float64
		if strings.Contains(text, lq) || strings.Contains(summary, lq) {
			score = 1.0
		} else {
			// token-level fallback
			qTokens := strings.Fields(lq)
			textTokens := strings.Fields(text)
			match := 0
			for _, qt := range qTokens {
				for _, tt := range textTokens {
					if tt == qt {
						match++
						break
					}
				}
			}
			if len(qTokens) > 0 {
				score = float64(match) / float64(len(qTokens))
			}
		}
		if score > 0 {
			results = append(results, scored{id: id, score: score, ts: m.Version})
		}
	}

	// sort by score desc, then by ts desc (newest first)
	sort.Slice(results, func(i, j int) bool {
		if results[i].score != results[j].score {
			return results[i].score > results[j].score
		}
		return results[i].ts > results[j].ts
	})

	out := make([]string, 0, min(topK, len(results)))
	for i := range results {
		if i >= topK {
			break
		}
		out = append(out, results[i].id)
	}
	return out
}

func dotProduct(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	n := len(a)
	if len(b) < n {
		n = len(b)
	}

	var sum float64
	for i := 0; i < n; i++ {
		sum += float64(a[i] * b[i])
	}
	return sum
}

func (s *InMemoryColdStore) ColdVectorSearch(queryVec []float32, topK int) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if topK <= 0 || len(queryVec) == 0 {
		return nil
	}

	type scored struct {
		id    string
		score float64
		ts    int64
	}

	results := make([]scored, 0, len(s.embeddings))
	for memoryID, emb := range s.embeddings {
		score := dotProduct(queryVec, emb)
		if score <= 0 {
			continue
		}

		var ts int64
		if m, ok := s.memories[memoryID]; ok {
			ts = m.Version
		}

		results = append(results, scored{
			id:    memoryID,
			score: score,
			ts:    ts,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].score != results[j].score {
			return results[i].score > results[j].score
		}
		return results[i].ts > results[j].ts
	})

	out := make([]string, 0, min(topK, len(results)))
	for i := range results {
		if i >= topK {
			break
		}
		out = append(out, results[i].id)
	}
	return out
}

func (s *InMemoryColdStore) ColdHNSWSearch(queryVec []float32, topK int) []string {
	// HNSW index is not yet built for the in-memory cold store.
	// Return nil so callers can fall back to brute-force ColdVectorSearch.
	return nil
}
