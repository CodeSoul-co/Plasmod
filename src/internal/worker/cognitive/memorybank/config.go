// Package memorybank implements the MemoryBank memory governance algorithm
// as a schemas.MemoryManagementAlgorithm plugin.
//
// It provides 8 governance dimensions, 8 lifecycle states, admission/retention
// scoring, conflict detection, profile management, and multi-dimensional recall.
//
// Placement: worker/cognitive/memorybank/ (parallel to baseline/)
//
// Design contract: this algorithm NEVER touches storage directly.
// All state is persisted via MemoryAlgorithmState and restored through LoadState.
// The InMemoryAlgorithmDispatchWorker handles all persistence and lifecycle application.
//
// Configuration: all tunable parameters are loaded from configs/algorithm_memorybank.yaml
// at startup. Code-level defaults in DefaultConfig() are safe fallbacks when the YAML
// file is absent. Environment variable ANDB_ALGORITHM_MEMORYBANK_CONFIG overrides the path.
package memorybank

import (
	"os"

	"andb/src/internal/config"
)

// AlgorithmID is the stable identifier for the MemoryBank algorithm.
const AlgorithmID = "memorybank_v1"

// Config holds all tunable parameters for the MemoryBank algorithm.
// Values are loaded from configs/algorithm_memorybank.yaml via LoadFromYAML.
type Config struct {
	// Admission thresholds
	THActive        float64 `yaml:"th_active"`
	THSession       float64 `yaml:"th_session"`
	WSalience       float64 `yaml:"w_salience"`
	WStability      float64 `yaml:"w_stability"`
	WTaskRelevance  float64 `yaml:"w_task_relevance"`
	WUserExplicitness float64 `yaml:"w_user_explicitness"`
	WNoise          float64 `yaml:"w_noise"`

	// Retention weights
	WRecency   float64 `yaml:"w_recency"`
	WRecallCnt float64 `yaml:"w_recall_cnt"`
	WSalRet    float64 `yaml:"w_sal_ret"`
	WConfidence float64 `yaml:"w_confidence"`
	WConflictP float64 `yaml:"w_conflict_p"`
	WReaffirm  float64 `yaml:"w_reaffirm"`

	// Recall scoring weights
	WRetention  float64 `yaml:"w_retention"`
	WFreshness float64 `yaml:"w_freshness"`
	WConfRecall float64 `yaml:"w_conf_recall"`
	WConflictR  float64 `yaml:"w_conflict_r"`

	// Reinforcement deltas
	DeltaUsedInAnswer  float64 `yaml:"delta_used_in_answer"`
	DeltaUserConfirmed float64 `yaml:"delta_user_confirmed"`
	DeltaToolSupported float64 `yaml:"delta_tool_supported"`

	// Decay
	DefaultDecayRate        float64 `yaml:"default_decay_rate"`
	RetentionThresholdStale   float64 `yaml:"threshold_stale"`
	RetentionThresholdArchive float64 `yaml:"threshold_archive"`

	// Initial strength keyed by memory type string (from YAML)
	InitialStrength map[string]float64 `yaml:"initial_strength"`

	// Per-type decay rate keyed by memory type string
	DecayRate map[string]float64 `yaml:"decay_rate"`

	// Limits
	RecallBoost float64 `yaml:"recall_boost"`
	MaxStrength float64 `yaml:"max_strength"`

	// Compression
	CompressedLevel    int `yaml:"compressed_level"`
	SummarizationLevel int `yaml:"summarization_level"`
}

// DefaultConfig returns production-ready code-level defaults.
// These are used when the YAML config file is absent or missing keys.
func DefaultConfig() Config {
	return Config{
		// Admission
		THActive:          0.65,
		THSession:        0.40,
		WSalience:        0.25,
		WStability:       0.20,
		WTaskRelevance:   0.25,
		WUserExplicitness: 0.15,
		WNoise:           0.15,
		// Retention
		WRecency:   0.20,
		WRecallCnt: 0.20,
		WSalRet:    0.15,
		WConfidence: 0.15,
		WConflictP: 0.20,
		WReaffirm:  0.10,
		// Recall
		WRetention:  0.30,
		WFreshness: 0.20,
		WConfRecall: 0.20,
		WConflictR:  0.10,
		// Reinforcement
		DeltaUsedInAnswer:  0.5,
		DeltaUserConfirmed: 1.0,
		DeltaToolSupported: 1.0,
		// Decay
		DefaultDecayRate:        0.05,
		RetentionThresholdStale:   0.25,
		RetentionThresholdArchive: 0.10,
		// Initial strength
		InitialStrength: map[string]float64{
			"episodic":              1.0,
			"semantic":              2.0,
			"procedural":            2.0,
			"social":                1.5,
			"reflective":            2.0,
			"factual":               3.0,
			"profile":               2.5,
			"affective_state":       0.8,
			"preference_constraint": 3.0,
		},
		// Per-type decay rates (per day)
		DecayRate: map[string]float64{
			"episodic":              0.05,
			"semantic":              0.02,
			"procedural":            0.01,
			"social":                0.03,
			"reflective":            0.02,
			"factual":               0.01,
			"profile":               0.005,
			"affective_state":       0.20,
			"preference_constraint":  0.005,
		},
		// Limits
		RecallBoost:  1.1,
		MaxStrength: 5.0,
		// Compression
		CompressedLevel:    1,
		SummarizationLevel: 2,
	}
}

// LoadFromYAML reads configs/algorithm_memorybank.yaml and merges it with defaults.
// If the file does not exist, returns defaults. Any missing YAML keys retain default values.
// ANDB_ALGORITHM_MEMORYBANK_CONFIG overrides the default path.
func LoadFromYAML() (Config, error) {
	cfg := DefaultConfig()
	path := os.Getenv("ANDB_ALGORITHM_MEMORYBANK_CONFIG")
	if path == "" {
		path = "configs/algorithm_memorybank.yaml"
	}
	if err := config.LoadYAML(path, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}
