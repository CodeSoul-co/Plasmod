// Package baseline provides the default MemoryManagementAlgorithm implementation
// for CogDB.  It encodes a strength-based retention model with configurable
// decay, recall-boost, and compression/summarisation behaviour, and bundles the
// baseline pipeline workers (extraction, consolidation, summarization, reflection)
// that implement each processing stage.
//
// Placement: each algorithm gets its own sub-package under worker/cognitive:
//
//	worker/cognitive/
//	├── baseline/       ← this package (default algorithm + its pipeline)
//	└── memorybank/     ← future extension point
//
// The algorithm interacts with Memory objects only through the
// MemoryManagementAlgorithm interface; it never touches the storage layer
// directly.  State is persisted externally via MemoryAlgorithmState records
// and restored through LoadState before processing.
package baseline

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"andb/src/internal/config"
	"andb/src/internal/schemas"
)

// AlgorithmID is the stable identifier for the baseline algorithm.
const AlgorithmID = "baseline_v1"

// ─── Config ───────────────────────────────────────────────────────────────────

// Config holds all tunable parameters for the baseline algorithm.
// Create with DefaultConfig() for production-ready defaults; override
// individual fields for testing or custom deployments.
type Config struct {
	// Strength assigned to a freshly ingested memory.
	InitialStrength float64
	// Per-day exponential decay constant λ. RetentionScore = exp(-DecayRate * days)
	DecayRate float64
	// Strength below which SuggestedLifecycleState=decayed is emitted.
	DecayThreshold float64
	// Multiplicative boost applied to Strength each time a memory is recalled.
	RecallBoost float64
	// Upper cap on Strength.
	MaxStrength float64
	// Memory level assigned to compressed output memories.
	CompressedLevel int
	// Memory level assigned to summarized output memories.
	SummarizationLevel int
}

// DefaultConfig returns production-ready defaults for the baseline model.
func DefaultConfig() Config {
	return Config{
		InitialStrength:    1.0,
		DecayRate:          0.1,
		DecayThreshold:     0.1,
		RecallBoost:        1.1,
		MaxStrength:        5.0,
		CompressedLevel:    1,
		SummarizationLevel: 2,
	}
}

// BaselineConfig mirrors the Config struct for YAML unmarshaling.
type BaselineConfig struct {
	InitialStrength    float64 `yaml:"initial_strength"`
	DecayRate         float64 `yaml:"decay_rate"`
	DecayThreshold    float64 `yaml:"decay_threshold"`
	RecallBoost       float64 `yaml:"recall_boost"`
	MaxStrength       float64 `yaml:"max_strength"`
	CompressedLevel   int     `yaml:"compressed_level"`
	SummarizationLevel int     `yaml:"summarization_level"`
}

// LoadFromYAML reads configs/algorithm_baseline.yaml and merges it with defaults.
// If the file does not exist, returns defaults. Any missing YAML keys retain default values.
// Environment variable ANDB_ALGORITHM_BASELINE_CONFIG overrides the path.
func LoadFromYAML() (Config, error) {
	defaults := DefaultConfig()
	yc := BaselineConfig{
		InitialStrength:    defaults.InitialStrength,
		DecayRate:         defaults.DecayRate,
		DecayThreshold:    defaults.DecayThreshold,
		RecallBoost:       defaults.RecallBoost,
		MaxStrength:       defaults.MaxStrength,
		CompressedLevel:   defaults.CompressedLevel,
		SummarizationLevel: defaults.SummarizationLevel,
	}
	path := os.Getenv("ANDB_ALGORITHM_BASELINE_CONFIG")
	if path == "" {
		path = "configs/algorithm_baseline.yaml"
	}
	if err := config.LoadYAML(path, &yc); err != nil {
		return defaults, err
	}
	return Config{
		InitialStrength:    yc.InitialStrength,
		DecayRate:         yc.DecayRate,
		DecayThreshold:    yc.DecayThreshold,
		RecallBoost:       yc.RecallBoost,
		MaxStrength:       yc.MaxStrength,
		CompressedLevel:   yc.CompressedLevel,
		SummarizationLevel: yc.SummarizationLevel,
	}, nil
}

// NewDefault creates a BaselineMemoryAlgorithm loading configs/algorithm_baseline.yaml.
func NewDefault() *BaselineMemoryAlgorithm {
	cfg, _ := LoadFromYAML() // falls back to defaults on error
	return New(cfg)
}

// ─── BaselineMemoryAlgorithm ──────────────────────────────────────────────────

// BaselineMemoryAlgorithm implements schemas.MemoryManagementAlgorithm using a
// configurable strength-based retention model.
//
// Behaviour summary:
//   - Ingest:     initialises MemoryAlgorithmState (Strength = InitialStrength); idempotent
//   - Update:     applies caller-supplied signal multipliers per memory_id
//   - Recall:     scores as Confidence × Importance × Strength × recencyFactor;
//                 boosts Strength for recalled memories
//   - Compress:   merges a memory set into a single derived Memory
//   - Decay:      reduces Strength via exponential decay; emits
//                 SuggestedLifecycleState=decayed when Strength < DecayThreshold
//   - Summarize:  produces one higher-level summary Memory per call
//   - ExportState / LoadState: in-process state round-trip
type BaselineMemoryAlgorithm struct {
	cfg    Config
	mu     sync.Mutex
	states map[string]schemas.MemoryAlgorithmState // keyed by memory_id
}

// New creates a BaselineMemoryAlgorithm with the provided config.
func New(cfg Config) *BaselineMemoryAlgorithm {
	return &BaselineMemoryAlgorithm{cfg: cfg, states: make(map[string]schemas.MemoryAlgorithmState)}
}

func (a *BaselineMemoryAlgorithm) AlgorithmID() string { return AlgorithmID }

// ─── Ingest ───────────────────────────────────────────────────────────────────

func (a *BaselineMemoryAlgorithm) Ingest(memories []schemas.Memory, ctx schemas.AlgorithmContext) []schemas.MemoryAlgorithmState {
	now := ctx.Timestamp
	if now == "" {
		now = tsNow()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]schemas.MemoryAlgorithmState, 0, len(memories))
	for _, m := range memories {
		st := a.getOrInit(m.MemoryID, now)
		a.states[m.MemoryID] = st
		out = append(out, st)
	}
	return out
}

// ─── Update ───────────────────────────────────────────────────────────────────

func (a *BaselineMemoryAlgorithm) Update(memories []schemas.Memory, signals map[string]float64) []schemas.MemoryAlgorithmState {
	now := tsNow()
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]schemas.MemoryAlgorithmState, 0, len(memories))
	for _, m := range memories {
		st := a.getOrInit(m.MemoryID, now)
		if multiplier, ok := signals[m.MemoryID]; ok && multiplier > 0 {
			st.Strength = math.Min(st.Strength*multiplier, a.cfg.MaxStrength)
		}
		st.SuggestedLifecycleState = ""
		if st.Strength < a.cfg.DecayThreshold {
			st.SuggestedLifecycleState = string(schemas.MemoryLifecycleDecayed)
		}
		st.UpdatedAt = now
		a.states[m.MemoryID] = st
		out = append(out, st)
	}
	return out
}

// ─── Recall ───────────────────────────────────────────────────────────────────

func (a *BaselineMemoryAlgorithm) Recall(query string, candidates []schemas.Memory, ctx schemas.AlgorithmContext) []schemas.ScoredMemory {
	now := ctx.Timestamp
	if now == "" {
		now = tsNow()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	scored := make([]schemas.ScoredMemory, 0, len(candidates))
	for _, m := range candidates {
		st := a.getOrInit(m.MemoryID, now)
		score := m.Confidence * m.Importance * st.Strength * recencyFactor(m.ValidFrom)
		st.LastRecalledAt = now
		st.RecallCount++
		st.Strength = math.Min(st.Strength*a.cfg.RecallBoost, a.cfg.MaxStrength)
		st.UpdatedAt = now
		st.SuggestedLifecycleState = ""
		a.states[m.MemoryID] = st
		scored = append(scored, schemas.ScoredMemory{Memory: m, Score: score, Signal: "baseline_strength"})
	}
	sort.Slice(scored, func(i, j int) bool { return scored[i].Score > scored[j].Score })
	return scored
}

// ─── Compress ─────────────────────────────────────────────────────────────────

func (a *BaselineMemoryAlgorithm) Compress(memories []schemas.Memory, ctx schemas.AlgorithmContext) []schemas.Memory {
	if len(memories) == 0 {
		return nil
	}
	now := ctx.Timestamp
	if now == "" {
		now = tsNow()
	}
	parts := make([]string, 0, len(memories))
	totalImportance := 0.0
	for _, m := range memories {
		parts = append(parts, m.Content)
		totalImportance += m.Importance
	}
	src := memories[0]
	return []schemas.Memory{{
		MemoryID:       fmt.Sprintf("%scmp_%s_%d", schemas.IDPrefixSummary, src.AgentID, time.Now().UnixNano()),
		MemoryType:     src.MemoryType,
		AgentID:        src.AgentID,
		SessionID:      src.SessionID,
		Level:          a.cfg.CompressedLevel,
		Content:        strings.Join(parts, " | "),
		Summary:        fmt.Sprintf("Compressed from %d memories", len(memories)),
		Confidence:     schemas.DefaultConfidence,
		Importance:     totalImportance / float64(len(memories)),
		IsActive:       true,
		LifecycleState: string(schemas.MemoryLifecycleActive),
		ValidFrom:      now,
		Version:        1,
	}}
}

// ─── Decay ────────────────────────────────────────────────────────────────────

func (a *BaselineMemoryAlgorithm) Decay(memories []schemas.Memory, nowTS string) []schemas.MemoryAlgorithmState {
	if nowTS == "" {
		nowTS = tsNow()
	}
	now, err := time.Parse(time.RFC3339, nowTS)
	if err != nil {
		now = time.Now().UTC()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]schemas.MemoryAlgorithmState, 0, len(memories))
	for _, m := range memories {
		st := a.getOrInit(m.MemoryID, nowTS)
		if st.UpdatedAt != "" {
			if last, err2 := time.Parse(time.RFC3339, st.UpdatedAt); err2 == nil {
				days := now.Sub(last).Hours() / 24.0
				if days > 0 {
					decay := math.Exp(-a.cfg.DecayRate * days)
					st.Strength *= decay
					st.RetentionScore = decay
				}
			}
		}
		st.SuggestedLifecycleState = ""
		if st.Strength < a.cfg.DecayThreshold {
			st.SuggestedLifecycleState = string(schemas.MemoryLifecycleDecayed)
		}
		st.UpdatedAt = nowTS
		a.states[m.MemoryID] = st
		out = append(out, st)
	}
	return out
}

// ─── Summarize ────────────────────────────────────────────────────────────────

func (a *BaselineMemoryAlgorithm) Summarize(memories []schemas.Memory, ctx schemas.AlgorithmContext) []schemas.Memory {
	if len(memories) == 0 {
		return nil
	}
	now := ctx.Timestamp
	if now == "" {
		now = tsNow()
	}
	parts := make([]string, 0, len(memories))
	totalImportance := 0.0
	maxLevel := 0
	for _, m := range memories {
		text := m.Summary
		if text == "" {
			text = m.Content
		}
		parts = append(parts, text)
		totalImportance += m.Importance
		if m.Level > maxLevel {
			maxLevel = m.Level
		}
	}
	src := memories[0]
	return []schemas.Memory{{
		MemoryID:       fmt.Sprintf("%ssum_%s_%d", schemas.IDPrefixSummary, src.AgentID, time.Now().UnixNano()),
		MemoryType:     string(schemas.MemoryTypeSemantic),
		AgentID:        src.AgentID,
		SessionID:      src.SessionID,
		Level:          maxLevel + 1,
		Content:        strings.Join(parts, " · "),
		Summary:        fmt.Sprintf("Summary of %d memories at level %d", len(memories), maxLevel),
		Confidence:     schemas.DefaultConfidence,
		Importance:     totalImportance / float64(len(memories)),
		IsActive:       true,
		LifecycleState: string(schemas.MemoryLifecycleActive),
		ValidFrom:      now,
		Version:        1,
	}}
}

// ─── State management ─────────────────────────────────────────────────────────

func (a *BaselineMemoryAlgorithm) ExportState(memoryID string) (schemas.MemoryAlgorithmState, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	st, ok := a.states[memoryID]
	return st, ok
}

func (a *BaselineMemoryAlgorithm) LoadState(state schemas.MemoryAlgorithmState) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.states[state.MemoryID] = state
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// getOrInit returns existing state or creates a fresh one. MUST be called with a.mu held.
func (a *BaselineMemoryAlgorithm) getOrInit(memoryID, now string) schemas.MemoryAlgorithmState {
	if st, ok := a.states[memoryID]; ok {
		return st
	}
	return schemas.MemoryAlgorithmState{
		MemoryID:       memoryID,
		AlgorithmID:    AlgorithmID,
		Strength:       a.cfg.InitialStrength,
		RetentionScore: 1.0,
		UpdatedAt:      now,
	}
}

// recencyFactor returns a mild exponential bonus for recently written memories.
func recencyFactor(validFrom string) float64 {
	if validFrom == "" {
		return 1.0
	}
	t, err := time.Parse(time.RFC3339, validFrom)
	if err != nil {
		return 1.0
	}
	days := time.Since(t).Hours() / 24.0
	return math.Exp(-0.01 * days)
}

func tsNow() string { return time.Now().UTC().Format(time.RFC3339) }
