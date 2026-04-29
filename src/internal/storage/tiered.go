package storage

import (
	"hash/fnv"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"plasmod/src/internal/schemas"
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
	EstimatedSize float64
	WriteBackCost float64
	ReloadEase    float64
	Dirty         bool
	Pinned        bool
	HitProb       float64
	Protected     bool
	Tier          string
	CoolingSince  time.Time
	AccessHistory []time.Time
}

type HotPointerPayload struct {
	ObjectID    string
	ObjectType  string
	PointerType string
	Locator     string
}

type physicalLocator struct {
	Tier   string
	Store  string
	Bucket string
	Key    string
	ID     string
	Offset int64
	Length int64
	Page   int64
}

type HotCachePolicy struct {
	HighWatermarkPercent float64
	LowWatermarkPercent  float64
	EvictionBatchSize    int

	Wr, Wf, Ws float64
	Lambda     float64

	AlphaSize     float64
	BetaWriteBack float64
	GammaHitProb  float64
	DeltaReload   float64

	FrequencyNormWindow int
	RecencyTauSeconds   float64
	RecencyTauByType    map[string]float64
	HitProbEWMAAlpha    float64
	EstimatedPoolBytes  float64

	ObjectTypeWeight     map[string]float64
	ReloadEaseByType     map[string]float64
	ReloadEaseEWMAAlpha  float64
	WriteBackByType      map[string]float64
	ProtectedRatio       float64
	CoolingTTLSeconds    float64
	FixedPoolRatio       float64
	HotPoolRatio         float64
	CoolingPoolRatio     float64
	FreePoolRatio        float64
	MaxFlushRetries      int
	LRUKValue            int
	DirtyJournalPath     string
	LocatorBlockSize     int64
	SemanticKeywordBoost map[string]float64
	GraphSemanticEnabled bool
	WSourceEvents        float64
	WPolicyTags          float64
	WImportance          float64
	WConfidence          float64
	NormalizeByMaxEdges  float64
	ForceClass1Keywords  []string
	ForceClass2Keywords  []string
	ForceClass3Keywords  []string
	FixedTypes           []string
	FixedKeywords        []string
	ForceClassByType     map[string]int
	MemoryLifecycleClass map[string]int
	Class1Types          []string
	Class2Types          []string
	Class3Types          []string
}

func defaultHotCachePolicy() HotCachePolicy {
	return HotCachePolicy{
		HighWatermarkPercent: 0.80,
		LowWatermarkPercent:  0.60,
		EvictionBatchSize:    16,
		Wr:                   0.25,
		Wf:                   0.20,
		Ws:                   0.55,
		Lambda:               0.80,
		AlphaSize:            0.45,
		BetaWriteBack:        0.20,
		GammaHitProb:         0.25,
		DeltaReload:          0.10,
		FrequencyNormWindow:  16,
		RecencyTauSeconds:    120.0,
		RecencyTauByType: map[string]float64{
			"memory":   180.0,
			"state":    120.0,
			"artifact": 90.0,
		},
		HitProbEWMAAlpha:   0.35,
		EstimatedPoolBytes: 256 * 1024 * 1024,
		ObjectTypeWeight: map[string]float64{
			"memory":   0.95,
			"state":    0.85,
			"artifact": 0.70,
		},
		ReloadEaseByType: map[string]float64{
			"memory":   0.30,
			"state":    0.50,
			"artifact": 0.70,
		},
		ReloadEaseEWMAAlpha: 0.20,
		WriteBackByType: map[string]float64{
			"memory":   0.40,
			"state":    0.60,
			"artifact": 0.50,
		},
		ProtectedRatio:    0.5,
		CoolingTTLSeconds: 60,
		FixedPoolRatio:    0.20,
		HotPoolRatio:      0.45,
		CoolingPoolRatio:  0.20,
		FreePoolRatio:     0.15,
		MaxFlushRetries:   3,
		LRUKValue:         2,
		DirtyJournalPath:  ".andb_data/hot_dirty_journal.log",
		LocatorBlockSize:  4096,
		SemanticKeywordBoost: map[string]float64{
			"core_fact":    0.20,
			"evidence":     0.15,
			"summary":      0.10,
			"final":        0.10,
			"temporary":    -0.10,
			"retry":        -0.10,
			"intermediate": -0.08,
		},
		GraphSemanticEnabled: true,
		WSourceEvents:        0.45,
		WPolicyTags:          0.25,
		WImportance:          0.15,
		WConfidence:          0.15,
		NormalizeByMaxEdges:  8.0,
		ForceClass1Keywords:  []string{"core_fact", "critical", "must_keep"},
		ForceClass2Keywords:  []string{"thinking", "reasoning", "payload"},
		ForceClass3Keywords:  []string{"archived", "deleted", "obsolete"},
		FixedTypes:           []string{"index_node", "operator", "metadata_index"},
		FixedKeywords:        []string{"fixed", "always_on", "index"},
		ForceClassByType: map[string]int{
			"memory":   1,
			"state":    1,
			"artifact": 2,
		},
		MemoryLifecycleClass: map[string]int{
			"active":      1,
			"reinforced":  1,
			"compressed":  2,
			"stale":       2,
			"archived":    3,
			"deleted":     3,
			"quarantined": 3,
		},
		Class1Types: []string{"memory", "state"},
		Class2Types: []string{"artifact"},
		Class3Types: []string{},
	}
}

// hotness returns a configurable score used for eviction.
// Higher score = keep in hot tier longer.
func (e *HotEntry) hotness(policy HotCachePolicy) float64 {
	tau := policy.RecencyTauSeconds
	if byType, ok := policy.RecencyTauByType[e.ObjectType]; ok && byType > 0 {
		tau = byType
	}
	if tau <= 0 {
		tau = 120
	}
	if policy.FrequencyNormWindow <= 0 {
		policy.FrequencyNormWindow = 16
	}
	if policy.EstimatedPoolBytes <= 0 {
		policy.EstimatedPoolBytes = 256 * 1024 * 1024
	}

	ageSec := math.Max(0, time.Since(e.LastAccess).Seconds())
	recency := math.Exp(-ageSec / tau)
	freq := 1.0 - math.Exp(-float64(e.AccessCount)/float64(policy.FrequencyNormWindow))
	semantic := clamp01(e.SalienceScore)

	sizeNorm := clamp01(e.EstimatedSize / policy.EstimatedPoolBytes)
	writeBack := clamp01(e.WriteBackCost)
	hitProb := clamp01(0.6*freq + 0.4*clamp01(e.HitProb))
	reload := clamp01(e.ReloadEase)
	penalty := policy.AlphaSize*sizeNorm +
		policy.BetaWriteBack*writeBack +
		policy.GammaHitProb*(1.0-hitProb) +
		policy.DeltaReload*reload

	benefit := policy.Wr*recency + policy.Wf*freq + policy.Ws*semantic
	return benefit - policy.Lambda*penalty
}

// HotObjectCache is a bounded in-memory cache for the most activation-critical
// objects (recent session memories, high-salience facts, current agent states).
// It is the fast lane of the memory activation path.
type HotObjectCache struct {
	mu      sync.RWMutex
	entries map[string]*HotEntry
	maxSize int
	// orderKey tracks insertion order for LRU eviction fallback
	order        []string
	policy       HotCachePolicy
	probation    []string
	protected    []string
	cooling      []string
	fixed        []string
	dirtyQ       []string
	dirtyRetry   map[string]int
	flushFn      func(*HotEntry) error
	currentBytes float64
}

type HotPutOptions struct {
	Dirty          bool
	Pinned         bool
	PointerLocator string
}

func NewHotObjectCache(maxSize int) *HotObjectCache {
	if maxSize <= 0 {
		maxSize = 2000
	}
	return &HotObjectCache{
		entries:    make(map[string]*HotEntry, maxSize),
		maxSize:    maxSize,
		order:      make([]string, 0, maxSize),
		policy:     defaultHotCachePolicy(),
		probation:  make([]string, 0, maxSize),
		protected:  make([]string, 0, maxSize),
		cooling:    make([]string, 0, maxSize),
		fixed:      make([]string, 0, maxSize),
		dirtyQ:     make([]string, 0, maxSize),
		dirtyRetry: map[string]int{},
	}
}

func (c *HotObjectCache) ConfigurePolicy(p HotCachePolicy) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if p.HighWatermarkPercent <= 0 || p.HighWatermarkPercent > 1 {
		p.HighWatermarkPercent = c.policy.HighWatermarkPercent
	}
	if p.LowWatermarkPercent <= 0 || p.LowWatermarkPercent > p.HighWatermarkPercent {
		p.LowWatermarkPercent = c.policy.LowWatermarkPercent
	}
	if p.EvictionBatchSize <= 0 {
		p.EvictionBatchSize = c.policy.EvictionBatchSize
	}
	if len(p.RecencyTauByType) == 0 {
		p.RecencyTauByType = c.policy.RecencyTauByType
	}
	if len(p.Class1Types) == 0 && len(c.policy.Class1Types) > 0 {
		p.Class1Types = c.policy.Class1Types
	}
	if len(p.Class2Types) == 0 && len(c.policy.Class2Types) > 0 {
		p.Class2Types = c.policy.Class2Types
	}
	if len(p.Class3Types) == 0 && len(c.policy.Class3Types) > 0 {
		p.Class3Types = c.policy.Class3Types
	}
	if p.HitProbEWMAAlpha <= 0 || p.HitProbEWMAAlpha > 1 {
		p.HitProbEWMAAlpha = c.policy.HitProbEWMAAlpha
	}
	if p.ReloadEaseEWMAAlpha <= 0 || p.ReloadEaseEWMAAlpha > 1 {
		p.ReloadEaseEWMAAlpha = c.policy.ReloadEaseEWMAAlpha
	}
	if p.ProtectedRatio <= 0 || p.ProtectedRatio >= 1 {
		p.ProtectedRatio = c.policy.ProtectedRatio
	}
	if p.CoolingTTLSeconds <= 0 {
		p.CoolingTTLSeconds = c.policy.CoolingTTLSeconds
	}
	if p.FixedPoolRatio < 0 || p.FixedPoolRatio >= 1 {
		p.FixedPoolRatio = c.policy.FixedPoolRatio
	}
	if p.HotPoolRatio <= 0 || p.HotPoolRatio >= 1 {
		p.HotPoolRatio = c.policy.HotPoolRatio
	}
	if p.CoolingPoolRatio < 0 || p.CoolingPoolRatio >= 1 {
		p.CoolingPoolRatio = c.policy.CoolingPoolRatio
	}
	if p.FreePoolRatio < 0 || p.FreePoolRatio >= 1 {
		p.FreePoolRatio = c.policy.FreePoolRatio
	}
	if p.MaxFlushRetries <= 0 {
		p.MaxFlushRetries = c.policy.MaxFlushRetries
	}
	if p.LRUKValue <= 1 {
		p.LRUKValue = c.policy.LRUKValue
	}
	if p.LocatorBlockSize <= 0 {
		p.LocatorBlockSize = c.policy.LocatorBlockSize
	}
	if strings.TrimSpace(p.DirtyJournalPath) == "" {
		p.DirtyJournalPath = c.policy.DirtyJournalPath
	}
	if len(p.SemanticKeywordBoost) == 0 {
		p.SemanticKeywordBoost = c.policy.SemanticKeywordBoost
	}
	if len(p.ForceClass1Keywords) == 0 {
		p.ForceClass1Keywords = c.policy.ForceClass1Keywords
	}
	if len(p.ForceClass2Keywords) == 0 {
		p.ForceClass2Keywords = c.policy.ForceClass2Keywords
	}
	if len(p.ForceClass3Keywords) == 0 {
		p.ForceClass3Keywords = c.policy.ForceClass3Keywords
	}
	if len(p.FixedTypes) == 0 {
		p.FixedTypes = c.policy.FixedTypes
	}
	if len(p.FixedKeywords) == 0 {
		p.FixedKeywords = c.policy.FixedKeywords
	}
	if len(p.ForceClassByType) == 0 {
		p.ForceClassByType = c.policy.ForceClassByType
	}
	if len(p.MemoryLifecycleClass) == 0 {
		p.MemoryLifecycleClass = c.policy.MemoryLifecycleClass
	}
	c.policy = p
}

func (c *HotObjectCache) SetFlushHandler(fn func(*HotEntry) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.flushFn = fn
}

// Put inserts or refreshes an object in the hot cache with the given salience.
func (c *HotObjectCache) Put(objectID, objectType string, payload any, salience float64) {
	c.PutWithOptions(objectID, objectType, payload, salience, HotPutOptions{})
}

func (c *HotObjectCache) PutWithOptions(objectID, objectType string, payload any, salience float64, opts HotPutOptions) {
	c.mu.Lock()
	defer c.mu.Unlock()

	isFixed := c.isFixedObject(objectType, payload)
	class := c.pointerClass(objectType, payload)
	if isFixed {
		class = 1
	}
	if class == 3 {
		return
	}
	if class == 2 {
		locator := opts.PointerLocator
		if strings.TrimSpace(locator) == "" {
			locator = encodeLocator(physicalLocator{
				Tier:   "warm",
				Store:  "memstore",
				ID:     objectID,
				Offset: 0,
				Length: int64(estimatePayloadSize(payload)),
				Page:   0,
			})
		}
		payload = HotPointerPayload{
			ObjectID:    objectID,
			ObjectType:  objectType,
			PointerType: "disk_offset",
			Locator:     locator,
		}
	}

	now := time.Now()
	if existing, ok := c.entries[objectID]; ok {
		prevSize := existing.EstimatedSize
		existing.Payload = payload
		existing.SalienceScore = c.semanticScore(salience, objectType, payload)
		existing.LastAccess = now
		existing.AccessCount++
		existing.EstimatedSize = estimatePayloadSize(payload)
		c.currentBytes += existing.EstimatedSize - prevSize
		existing.Dirty = existing.Dirty || opts.Dirty
		existing.Pinned = opts.Pinned
		existing.WriteBackCost = c.resolveWriteBackCost(objectType, existing.Dirty)
		existing.ReloadEase = c.lookupByType(c.policy.ReloadEaseByType, objectType, 0.5)
		existing.HitProb = c.updateEWMA(existing.HitProb, 1.0, c.policy.HitProbEWMAAlpha)
		existing.AccessHistory = c.appendAccessHistory(existing.AccessHistory, now)
		if isFixed {
			existing.Tier = "fixed"
			existing.CoolingSince = time.Time{}
			c.removeFromQueue(&c.probation, objectID)
			c.removeFromQueue(&c.protected, objectID)
			c.removeFromQueue(&c.cooling, objectID)
			c.touchFixed(objectID)
		}
		if opts.Dirty {
			c.enqueueDirty(objectID)
		}
		c.enforceWatermarkLocked()
		return
	}

	// evict lowest-hotness entry when at capacity
	if len(c.order) >= c.maxSize {
		c.evictOne()
	}

	entryTier := "hot"
	if isFixed {
		entryTier = "fixed"
	}
	size := estimatePayloadSize(payload)
	c.entries[objectID] = &HotEntry{
		ObjectID:      objectID,
		ObjectType:    objectType,
		Payload:       payload,
		SalienceScore: c.semanticScore(salience, objectType, payload),
		AccessCount:   1,
		LastAccess:    now,
		InsertedAt:    now,
		EstimatedSize: size,
		WriteBackCost: c.resolveWriteBackCost(objectType, opts.Dirty),
		ReloadEase:    c.lookupByType(c.policy.ReloadEaseByType, objectType, 0.5),
		Dirty:         opts.Dirty,
		Pinned:        opts.Pinned,
		HitProb:       0.5,
		Protected:     false,
		Tier:          entryTier,
		AccessHistory: []time.Time{now},
	}
	c.currentBytes += size
	c.order = append(c.order, objectID)
	if isFixed {
		c.touchFixed(objectID)
	} else {
		c.touchProbation(objectID)
	}
	if opts.Dirty {
		c.enqueueDirty(objectID)
	}
	c.enforceWatermarkLocked()
}

// Get retrieves an entry, bumping its access count.
func (c *HotObjectCache) Get(objectID string) (*HotEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[objectID]
	if ok {
		now := time.Now()
		e.AccessCount++
		e.LastAccess = now
		e.HitProb = c.updateEWMA(e.HitProb, 1.0, c.policy.HitProbEWMAAlpha)
		e.AccessHistory = c.appendAccessHistory(e.AccessHistory, now)
		if e.Tier == "cooling" {
			e.Tier = "hot"
			e.CoolingSince = time.Time{}
			e.Protected = true
			c.removeFromQueue(&c.cooling, objectID)
			c.moveProbationToProtected(objectID)
		} else if e.Tier != "fixed" {
			if !e.Protected {
				e.Protected = true
				c.moveProbationToProtected(objectID)
			} else {
				c.refreshProtected(objectID)
			}
		}
		c.enforceProtectedRatioLocked()
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

// SetPinned marks whether an entry is currently write-pinned.
// Pinned entries are excluded from hot->cooling and cooling->cold eviction paths.
func (c *HotObjectCache) SetPinned(objectID string, pinned bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[objectID]; ok && e != nil {
		e.Pinned = pinned
	}
}

// Evict explicitly removes an object from the hot cache.
func (c *HotObjectCache) Evict(objectID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.entries[objectID]; !ok {
		return
	}
	c.currentBytes -= c.entries[objectID].EstimatedSize
	if c.currentBytes < 0 {
		c.currentBytes = 0
	}
	delete(c.entries, objectID)
	c.removeFromQueue(&c.probation, objectID)
	c.removeFromQueue(&c.protected, objectID)
	c.removeFromQueue(&c.cooling, objectID)
	c.removeFromQueue(&c.fixed, objectID)
	c.removeFromQueue(&c.dirtyQ, objectID)
	delete(c.dirtyRetry, objectID)
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
	c.probation = c.probation[:0]
	c.protected = c.protected[:0]
	c.cooling = c.cooling[:0]
	c.fixed = c.fixed[:0]
	c.dirtyQ = c.dirtyQ[:0]
	c.dirtyRetry = map[string]int{}
	c.currentBytes = 0
}

// evictOne removes the entry with the lowest hotness score.
// Must be called with c.mu held (write).
func (c *HotObjectCache) evictOne() {
	if len(c.order) == 0 {
		return
	}
	// scan for lowest hotness
	candidates := c.probation
	if len(candidates) == 0 {
		candidates = c.protected
	}
	if len(candidates) == 0 {
		for _, id := range c.order {
			if e, ok := c.entries[id]; ok && e != nil && e.Tier == "hot" && !e.Pinned {
				candidates = append(candidates, id)
			}
		}
	}
	filtered := candidates[:0]
	for _, id := range candidates {
		e := c.entries[id]
		if e != nil && !e.Pinned {
			filtered = append(filtered, id)
		}
	}
	candidates = filtered
	if len(candidates) == 0 {
		return
	}
	worstID := candidates[0]
	worstScore := c.entries[worstID].hotness(c.policy)
	for _, id := range candidates[1:] {
		score := c.entries[id].hotness(c.policy)
		if score < worstScore ||
			(score == worstScore && c.lruKAge(c.entries[id]) > c.lruKAge(c.entries[worstID])) ||
			(score == worstScore && c.lruKAge(c.entries[id]) == c.lruKAge(c.entries[worstID]) && c.entries[id].EstimatedSize > c.entries[worstID].EstimatedSize) {
			worstScore = score
			worstID = id
		}
	}
	blockedByDirty := false
	if e := c.entries[worstID]; e != nil && e.Dirty {
		if ok := c.tryFlushDirtyLocked(worstID, e); !ok {
			blockedByDirty = true
			// keep dirty entry for retry, try another candidate in this round.
			for _, id := range candidates {
				if id == worstID {
					continue
				}
				alt := c.entries[id]
				if alt == nil {
					continue
				}
				if alt.Dirty && !c.tryFlushDirtyLocked(id, alt) {
					continue
				}
				worstID = id
				blockedByDirty = false
				break
			}
		}
	}
	if blockedByDirty {
		return
	}
	e := c.entries[worstID]
	if e != nil {
		e.Tier = "cooling"
		e.Protected = false
		e.CoolingSince = time.Now()
		c.touchCooling(worstID)
	}
	c.removeFromQueue(&c.probation, worstID)
	c.removeFromQueue(&c.protected, worstID)
}

func (c *HotObjectCache) enqueueDirty(id string) {
	c.removeFromQueue(&c.dirtyQ, id)
	c.dirtyQ = append(c.dirtyQ, id)
	c.appendDirtyJournal("dirty_put id=" + id)
}

func (c *HotObjectCache) tryFlushDirtyLocked(id string, e *HotEntry) bool {
	if e == nil || !e.Dirty {
		return true
	}
	if c.flushFn == nil {
		// no flush handler => degrade to best-effort immediate clean.
		e.Dirty = false
		e.WriteBackCost = 0
		return true
	}
	if err := c.flushFn(e); err != nil {
		c.dirtyRetry[id] = c.dirtyRetry[id] + 1
		c.appendDirtyJournal("dirty_flush_fail id=" + id)
		if c.dirtyRetry[id] <= c.policy.MaxFlushRetries {
			return false
		}
		// after retry budget, allow eviction to avoid deadlock.
		return true
	}
	e.Dirty = false
	e.WriteBackCost = 0
	c.dirtyRetry[id] = 0
	c.removeFromQueue(&c.dirtyQ, id)
	c.appendDirtyJournal("dirty_flush_ok id=" + id)
	return true
}

func (c *HotObjectCache) lookupByType(m map[string]float64, objectType string, fallback float64) float64 {
	if len(m) == 0 {
		return fallback
	}
	if v, ok := m[objectType]; ok {
		return v
	}
	return fallback
}

func (c *HotObjectCache) resolveWriteBackCost(objectType string, dirty bool) float64 {
	if !dirty {
		return 0
	}
	base := c.lookupByType(c.policy.WriteBackByType, objectType, 0.5)
	if base < 0.7 {
		return 0.7
	}
	return clamp01(base)
}

func (c *HotObjectCache) pointerClass(objectType string, payload any) int {
	if cls, ok := c.policy.ForceClassByType[objectType]; ok && cls >= 1 && cls <= 3 {
		if mem, okMem := payload.(schemas.Memory); okMem {
			if byLife, okLife := c.policy.MemoryLifecycleClass[strings.ToLower(strings.TrimSpace(mem.LifecycleState))]; okLife && byLife >= 1 && byLife <= 3 {
				return byLife
			}
		}
		return cls
	}
	text := strings.ToLower(c.extractText(payload))
	for _, kw := range c.policy.ForceClass3Keywords {
		if kw != "" && strings.Contains(text, strings.ToLower(kw)) {
			return 3
		}
	}
	for _, kw := range c.policy.ForceClass1Keywords {
		if kw != "" && strings.Contains(text, strings.ToLower(kw)) {
			return 1
		}
	}
	for _, kw := range c.policy.ForceClass2Keywords {
		if kw != "" && strings.Contains(text, strings.ToLower(kw)) {
			return 2
		}
	}
	in := func(list []string, typ string) bool {
		for _, v := range list {
			if v == typ {
				return true
			}
		}
		return false
	}
	if in(c.policy.Class3Types, objectType) {
		return 3
	}
	if in(c.policy.Class2Types, objectType) {
		return 2
	}
	return 1
}

func (c *HotObjectCache) semanticScore(base float64, objectType string, payload any) float64 {
	score := base * c.lookupByType(c.policy.ObjectTypeWeight, objectType, 1.0)
	text := strings.ToLower(c.extractText(payload))
	for kw, boost := range c.policy.SemanticKeywordBoost {
		if kw != "" && strings.Contains(text, strings.ToLower(kw)) {
			score += boost
		}
	}
	if c.policy.GraphSemanticEnabled {
		score += c.graphSemanticScore(payload)
	}
	return clamp01(score)
}

func (c *HotObjectCache) graphSemanticScore(payload any) float64 {
	maxEdges := c.policy.NormalizeByMaxEdges
	if maxEdges <= 0 {
		maxEdges = 8
	}
	switch v := payload.(type) {
	case schemas.Memory:
		se := clamp01(float64(len(v.SourceEventIDs)) / maxEdges)
		pt := clamp01(float64(len(v.PolicyTags)) / maxEdges)
		imp := clamp01(v.Importance)
		conf := clamp01(v.Confidence)
		return c.policy.WSourceEvents*se +
			c.policy.WPolicyTags*pt +
			c.policy.WImportance*imp +
			c.policy.WConfidence*conf
	default:
		return 0
	}
}

func (c *HotObjectCache) isFixedObject(objectType string, payload any) bool {
	for _, t := range c.policy.FixedTypes {
		if objectType == t {
			return true
		}
	}
	text := strings.ToLower(c.extractText(payload))
	for _, kw := range c.policy.FixedKeywords {
		if kw != "" && strings.Contains(text, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

func (c *HotObjectCache) extractText(payload any) string {
	switch v := payload.(type) {
	case schemas.Memory:
		return v.Content + " " + v.Summary + " " + v.LifecycleState
	case schemas.State:
		return v.StateKey + " " + v.StateValue
	case schemas.Artifact:
		return v.ContentRef + " " + v.URI + " " + v.MimeType
	case HotPointerPayload:
		return v.Locator + " " + v.PointerType
	case string:
		return v
	default:
		return ""
	}
}

func (c *HotObjectCache) updateEWMA(prev, sample, alpha float64) float64 {
	if alpha <= 0 || alpha >= 1 {
		alpha = 0.35
	}
	return clamp01(alpha*sample + (1-alpha)*prev)
}

func (c *HotObjectCache) removeFromQueue(q *[]string, id string) {
	for i, v := range *q {
		if v == id {
			*q = append((*q)[:i], (*q)[i+1:]...)
			return
		}
	}
}

func (c *HotObjectCache) touchProbation(id string) {
	c.removeFromQueue(&c.probation, id)
	c.probation = append(c.probation, id)
}

func (c *HotObjectCache) touchCooling(id string) {
	c.removeFromQueue(&c.cooling, id)
	c.cooling = append(c.cooling, id)
}

func (c *HotObjectCache) touchFixed(id string) {
	c.removeFromQueue(&c.fixed, id)
	c.fixed = append(c.fixed, id)
}

func (c *HotObjectCache) refreshProtected(id string) {
	c.removeFromQueue(&c.protected, id)
	c.protected = append(c.protected, id)
}

func (c *HotObjectCache) moveProbationToProtected(id string) {
	c.removeFromQueue(&c.probation, id)
	c.removeFromQueue(&c.cooling, id)
	c.refreshProtected(id)
}

func (c *HotObjectCache) enforceProtectedRatioLocked() {
	if c.maxSize <= 0 {
		return
	}
	maxProtected := int(float64(c.maxSize) * c.policy.ProtectedRatio)
	if maxProtected < 1 {
		maxProtected = 1
	}
	for len(c.protected) > maxProtected {
		demote := c.protected[0]
		c.protected = c.protected[1:]
		if e, ok := c.entries[demote]; ok {
			e.Protected = false
		}
		c.touchProbation(demote)
	}
}

func (c *HotObjectCache) appendAccessHistory(hist []time.Time, ts time.Time) []time.Time {
	k := c.policy.LRUKValue
	if k <= 1 {
		k = 2
	}
	hist = append(hist, ts)
	if len(hist) > k {
		hist = hist[len(hist)-k:]
	}
	return hist
}

func (c *HotObjectCache) lruKAge(e *HotEntry) float64 {
	if e == nil || len(e.AccessHistory) == 0 {
		return math.MaxFloat64
	}
	// K-th recent access age. Older means easier to evict.
	ref := e.AccessHistory[0]
	return time.Since(ref).Seconds()
}

func (c *HotObjectCache) ObserveReloadLatency(objectType string, d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.policy.ReloadEaseByType == nil {
		c.policy.ReloadEaseByType = map[string]float64{}
	}
	sec := d.Seconds()
	// Easier reload => closer to 1.0; slower reload => closer to 0.0.
	sampleEase := 1.0 / (1.0 + sec)
	prev := c.lookupByType(c.policy.ReloadEaseByType, objectType, 0.5)
	c.policy.ReloadEaseByType[objectType] = c.updateEWMA(prev, sampleEase, c.policy.ReloadEaseEWMAAlpha)
}

func (c *HotObjectCache) appendDirtyJournal(line string) {
	path := strings.TrimSpace(c.policy.DirtyJournalPath)
	if path == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line + "\n")
}

func (c *HotObjectCache) enforceWatermarkLocked() {
	if c.policy.EstimatedPoolBytes <= 0 {
		return
	}
	c.expireCoolingLocked()
	totalBudget := c.policy.EstimatedPoolBytes
	high := totalBudget * c.policy.HighWatermarkPercent
	low := totalBudget * c.policy.LowWatermarkPercent
	if high <= 0 {
		high = totalBudget * 0.8
	}
	if low <= 0 || low > high {
		low = high * 0.75
	}
	fixedBudget := totalBudget * c.policy.FixedPoolRatio
	hotBudget := totalBudget * c.policy.HotPoolRatio
	coolingBudget := totalBudget * c.policy.CoolingPoolRatio
	for c.bytesInTier("fixed") > fixedBudget && len(c.fixed) > 0 {
		// fixed pool overflow: demote oldest fixed into hot.
		id := c.fixed[0]
		c.fixed = c.fixed[1:]
		if e := c.entries[id]; e != nil {
			e.Tier = "hot"
			e.Protected = false
			c.touchProbation(id)
		}
	}
	if c.currentBytes <= high && c.bytesInTier("hot") <= hotBudget && c.bytesInTier("cooling") <= coolingBudget {
		return
	}
	maxEvict := c.policy.EvictionBatchSize
	if maxEvict <= 0 {
		maxEvict = len(c.order) + len(c.cooling)
	}
	for (c.currentBytes > low || c.bytesInTier("hot") > hotBudget || c.bytesInTier("cooling") > coolingBudget) && maxEvict > 0 {
		c.evictOne()
		for c.bytesInTier("cooling") > coolingBudget || c.currentBytes > low {
			if !c.evictCoolingOneLocked() {
				break
			}
		}
		maxEvict--
	}
}

func (c *HotObjectCache) bytesInTier(tier string) float64 {
	var sum float64
	for _, e := range c.entries {
		if e != nil && e.Tier == tier {
			sum += e.EstimatedSize
		}
	}
	return sum
}

func (c *HotObjectCache) expireCoolingLocked() {
	ttl := c.policy.CoolingTTLSeconds
	if ttl <= 0 {
		ttl = 60
	}
	deadline := time.Duration(ttl * float64(time.Second))
	now := time.Now()
	for _, id := range append([]string{}, c.cooling...) {
		e := c.entries[id]
		if e == nil || e.Tier != "cooling" {
			continue
		}
		if !e.CoolingSince.IsZero() && now.Sub(e.CoolingSince) >= deadline {
			if e.Pinned {
				continue
			}
			c.deleteEntryLocked(id)
		}
	}
}

func (c *HotObjectCache) evictCoolingOneLocked() bool {
	for _, id := range c.cooling {
		e := c.entries[id]
		if e == nil || e.Pinned {
			continue
		}
		c.deleteEntryLocked(id)
		return true
	}
	return false
}

func (c *HotObjectCache) deleteEntryLocked(id string) {
	e := c.entries[id]
	if e == nil {
		return
	}
	c.currentBytes -= e.EstimatedSize
	if c.currentBytes < 0 {
		c.currentBytes = 0
	}
	delete(c.entries, id)
	c.removeFromQueue(&c.probation, id)
	c.removeFromQueue(&c.protected, id)
	c.removeFromQueue(&c.cooling, id)
	c.removeFromQueue(&c.fixed, id)
	c.removeFromQueue(&c.dirtyQ, id)
	delete(c.dirtyRetry, id)
	for i, oid := range c.order {
		if oid == id {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
}

func estimatePayloadSize(payload any) float64 {
	switch v := payload.(type) {
	case schemas.Memory:
		return float64(len(v.Content)+len(v.Summary)+len(v.MemoryID)) + 128
	case schemas.State:
		return float64(len(v.StateID)+len(v.AgentID)+len(v.SessionID)+len(v.StateValue)+len(v.StateKey)) + 128
	case schemas.Artifact:
		return float64(len(v.ArtifactID)+len(v.OwnerAgentID)+len(v.ContentRef)+len(v.URI)+len(v.MimeType)+len(v.Hash)) + 128
	case string:
		return float64(len(v))
	default:
		return 512
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
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
		// Fast path: if the warm edge store supports object-scoped bulk delete,
		// remove all incident edges in one critical section to reduce lock churn.
		if bulk, ok := t.warmEdge.(warmEdgeBulkDeleter); ok {
			bulk.DeleteEdgesByObjectID(memoryID)
		} else {
			for _, e := range t.warmEdge.BulkEdges([]string{memoryID}) {
				t.warmEdge.DeleteEdge(e.EdgeID)
			}
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
	tiered := &TieredObjectStore{
		hot:          hot,
		warm:         warm,
		warmEdge:     warmEdge,
		cold:         cold,
		embedder:     embedder,
		hotThreshold: hotThreshold,
	}
	hot.SetFlushHandler(func(e *HotEntry) error {
		if e == nil || warm == nil {
			return nil
		}
		switch v := e.Payload.(type) {
		case schemas.Memory:
			warm.PutMemory(v)
		case schemas.State:
			warm.PutState(v)
		case schemas.Artifact:
			warm.PutArtifact(v)
		case HotPointerPayload:
			// pointer payload has no mutable body to flush
			return nil
		}
		return nil
	})
	return tiered
}

// NewTieredObjectStoreWithThreshold creates a TieredObjectStore with an explicit hot-tier
// salience threshold. Use this when the default threshold (0.5) needs tuning.
func NewTieredObjectStoreWithThreshold(hot *HotObjectCache, warm ObjectStore, warmEdge GraphEdgeStore, cold ColdObjectStore, hotThreshold float64) *TieredObjectStore {
	return NewTieredObjectStoreWithEmbedder(hot, warm, warmEdge, cold, nil, hotThreshold)
}

// SetEmbedder wires an embedder into an already-constructed TieredObjectStore.
// Call this after the embedder is initialized (e.g. in bootstrap after the
// embedder selection block) so that ArchiveMemory writes cold-tier embeddings.
func (t *TieredObjectStore) SetEmbedder(e MemoryEmbedder) {
	if t != nil {
		t.embedder = e
	}
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
		if p, ok := entry.Payload.(HotPointerPayload); ok {
			if m, ok := t.resolveMemoryByLocator(p, salience); ok {
				return m, true
			}
		}
	}

	// warm path
	if m, ok := t.warm.GetMemory(memoryID); ok {
		t.hot.PutWithOptions(memoryID, "memory", m, salience, HotPutOptions{
			Dirty: false,
			PointerLocator: encodeLocator(physicalLocator{
				Tier:   "warm",
				Store:  "memstore",
				ID:     memoryID,
				Offset: 0,
				Length: int64(estimatePayloadSize(m)),
				Page:   0,
			}),
		})
		return m, true
	}

	// cold path
	coldStart := time.Now()
	if m, ok := t.cold.GetMemory(memoryID); ok {
		t.hot.ObserveReloadLatency("memory", time.Since(coldStart))
		t.warm.PutMemory(m)
		t.hot.PutWithOptions(memoryID, "memory", m, salience*0.5, HotPutOptions{
			Dirty:          false,
			PointerLocator: t.buildColdMemoryLocator(memoryID, int64(estimatePayloadSize(m))),
		})
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
		t.hot.PutWithOptions(m.MemoryID, "memory", m, salience, HotPutOptions{
			Dirty: true,
			PointerLocator: encodeLocator(physicalLocator{
				Tier:   "warm",
				Store:  "memstore",
				ID:     m.MemoryID,
				Offset: 0,
				Length: int64(estimatePayloadSize(m)),
				Page:   0,
			}),
		})
	}
}

func (t *TieredObjectStore) resolveMemoryByLocator(p HotPointerPayload, salience float64) (schemas.Memory, bool) {
	locator := strings.TrimSpace(p.Locator)
	if locator == "" {
		return schemas.Memory{}, false
	}
	loc, ok := decodeLocator(locator)
	if !ok {
		return schemas.Memory{}, false
	}
	objectID := loc.ID
	switch loc.Tier {
	case "warm":
		if m, ok := t.warm.GetMemory(objectID); ok {
			return m, true
		}
	case "cold":
		start := time.Now()
		if m, ok := t.cold.GetMemory(objectID); ok {
			t.hot.ObserveReloadLatency("memory", time.Since(start))
			if t.warm != nil {
				t.warm.PutMemory(m)
			}
			t.hot.PutWithOptions(objectID, "memory", m, salience, HotPutOptions{
				Dirty: false,
				PointerLocator: encodeLocator(physicalLocator{
					Tier:   "warm",
					Store:  "memstore",
					ID:     objectID,
					Offset: 0,
					Length: int64(estimatePayloadSize(m)),
					Page:   0,
				}),
			})
			return m, true
		}
	}
	return schemas.Memory{}, false
}

func (t *TieredObjectStore) buildColdMemoryLocator(memoryID string, length int64) string {
	if length < 0 {
		length = 0
	}
	blockSize := int64(4096)
	if t != nil && t.hot != nil && t.hot.policy.LocatorBlockSize > 0 {
		blockSize = t.hot.policy.LocatorBlockSize
	}
	offset := stableLocatorOffset(memoryID, blockSize)
	page := int64(0)
	if blockSize > 0 {
		page = offset / blockSize
	}
	if t == nil || t.cold == nil {
		return encodeLocator(physicalLocator{Tier: "cold", Store: "unknown", ID: memoryID, Offset: offset, Length: length, Page: page})
	}
	switch c := t.cold.(type) {
	case *S3ColdStore:
		key := c.memoryKey(memoryID)
		return encodeLocator(physicalLocator{
			Tier:   "cold",
			Store:  "s3",
			Bucket: c.cfg.Bucket,
			Key:    key,
			ID:     memoryID,
			Offset: offset,
			Length: length,
			Page:   page,
		})
	case *InMemoryColdStore:
		return encodeLocator(physicalLocator{
			Tier:   "cold",
			Store:  "inmem",
			Key:    "cold.memory.map",
			ID:     memoryID,
			Offset: offset,
			Length: length,
			Page:   page,
		})
	default:
		return encodeLocator(physicalLocator{
			Tier:   "cold",
			Store:  "generic",
			ID:     memoryID,
			Offset: offset,
			Length: length,
			Page:   page,
		})
	}
}

func stableLocatorOffset(id string, blockSize int64) int64 {
	if blockSize <= 0 {
		blockSize = 4096
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	const spanBlocks int64 = 1 << 20 // bounded pseudo-layout span
	return int64(h.Sum64()%uint64(spanBlocks)) * blockSize
}

func encodeLocator(l physicalLocator) string {
	// v1|tier=<tier>|store=<store>|bucket=<bucket>|key=<key>|id=<id>|offset=<n>|length=<n>|page=<n>
	return "v1|tier=" + l.Tier +
		"|store=" + l.Store +
		"|bucket=" + l.Bucket +
		"|key=" + l.Key +
		"|id=" + l.ID +
		"|offset=" + itoa64(l.Offset) +
		"|length=" + itoa64(l.Length) +
		"|page=" + itoa64(l.Page)
}

func decodeLocator(raw string) (physicalLocator, bool) {
	parts := strings.Split(raw, "|")
	if len(parts) < 2 || parts[0] != "v1" {
		return physicalLocator{}, false
	}
	out := physicalLocator{}
	for _, p := range parts[1:] {
		kv := strings.SplitN(p, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "tier":
			out.Tier = kv[1]
		case "store":
			out.Store = kv[1]
		case "bucket":
			out.Bucket = kv[1]
		case "key":
			out.Key = kv[1]
		case "id":
			out.ID = kv[1]
		case "offset":
			out.Offset = atoi64(kv[1])
		case "length":
			out.Length = atoi64(kv[1])
		case "page":
			out.Page = atoi64(kv[1])
		}
	}
	if strings.TrimSpace(out.ID) == "" || strings.TrimSpace(out.Tier) == "" {
		return physicalLocator{}, false
	}
	return out, true
}

func itoa64(v int64) string {
	return strconv.FormatInt(v, 10)
}

func atoi64(s string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return n
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
		// Archive semantics for cold-tier benchmarking should produce a true
		// cold-only object rather than keeping a second warm copy alive.
		t.warm.DeleteMemory(memoryID)
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
