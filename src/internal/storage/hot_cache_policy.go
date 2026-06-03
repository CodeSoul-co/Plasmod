package storage

// HotCachePolicy holds tunable hot-tier scoring, eviction, and placement settings
// loaded from configs/memory_tiering.yaml. ConfigurePolicy stores the policy on
// HotObjectCache; the simple salience/LRU implementation uses only maxSize today.
// Advanced watermark/pool eviction can read c.policy when implemented.
type HotCachePolicy struct {
	HighWatermarkPercent float64
	LowWatermarkPercent  float64
	EvictionBatchSize    int

	Wr    float64
	Wf    float64
	Ws    float64
	Lambda float64

	AlphaSize     float64
	BetaWriteBack float64
	GammaHitProb  float64
	DeltaReload   float64

	FrequencyNormWindow int
	RecencyTauSeconds   float64
	RecencyTauByType    map[string]float64
	HitProbEWMAAlpha    float64
	EstimatedPoolBytes  float64
	ObjectTypeWeight    map[string]float64
	ReloadEaseByType    map[string]float64
	ReloadEaseEWMAAlpha float64
	WriteBackByType     map[string]float64

	ProtectedRatio    float64
	LRUKValue         int
	CoolingTTLSeconds float64

	FixedPoolRatio   float64
	FixedStrict      bool
	HotPoolRatio     float64
	CoolingPoolRatio float64
	FreePoolRatio    float64

	MaxFlushRetries  int
	DirtyJournalPath string
	LocatorBlockSize int64

	SemanticKeywordBoost map[string]float64
	GraphSemanticEnabled bool
	WSourceEvents        float64
	WPolicyTags          float64
	WImportance          float64
	WConfidence          float64
	WOutDegree           float64
	WInDegree            float64
	NormalizeByMaxEdges  float64

	ForceClass1Keywords  []string
	ForceClass2Keywords  []string
	ForceClass3Keywords  []string
	FixedTypes           []string
	FixedKeywords        []string
	ForceClassByType     map[string]int
	MemoryLifecycleClass map[string]int

	Class1Types []string
	Class2Types []string
	Class3Types []string
}

// ConfigurePolicy applies memory-tiering settings to the hot cache.
func (c *HotObjectCache) ConfigurePolicy(p HotCachePolicy) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.policy = p
	c.mu.Unlock()
}
