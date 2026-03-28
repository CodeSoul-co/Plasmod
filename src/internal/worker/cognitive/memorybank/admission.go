package memorybank

import (
	"math"

	"andb/src/internal/schemas"
)

// MemorySignals holds the raw governance signals for a single memory.
// Values are extracted from the Memory object and used to compute
// admission and retention scores.
type MemorySignals struct {
	Salience         float64           // from Memory.Importance (0-1)
	Stability        float64           // computed: recurrence across sessions (0-1)
	TaskRelevance    float64           // from Memory.Level (0=episodic, 1+=summary)
	UserExplicitness float64           // from Memory.Confidence (0-1)
	Noise            float64           // from Memory.PolicyTags: "noise" tag → high noise
	MemoryType       schemas.MemoryType
}

// ComputeAdmissionScore returns a score in [0,1] using the weighted sum formula:
// score = w1*salience + w2*stability + w3*task_relevance + w4*user_explicitness - w5*noise
func ComputeAdmissionScore(cfg Config, sig MemorySignals) float64 {
	score := cfg.WSalience*sig.Salience +
		cfg.WStability*sig.Stability +
		cfg.WTaskRelevance*sig.TaskRelevance +
		cfg.WUserExplicitness*sig.UserExplicitness -
		cfg.WNoise*sig.Noise
	return clamp01(score)
}

// IsHighValue returns true if the given MemoryType warrants high initial retention.
func IsHighValue(memType schemas.MemoryType) bool {
	return memType == schemas.MemoryTypePreferenceConstraint ||
		memType == schemas.MemoryTypeFactual
}

// extractSignals derives MemorySignals from a Memory object.
// Stability is computed as the fraction of unique source event IDs
// relative to a baseline (more sources = more stable).
func extractSignals(m schemas.Memory) MemorySignals {
	// Stability: unique source events / 5, capped at 1.0
	stability := float64(len(m.SourceEventIDs)) / 5.0
	if stability > 1.0 {
		stability = 1.0
	}

	// Noise: 1.0 if "noise" tag present, else 0.0
	noise := 0.0
	for _, tag := range m.PolicyTags {
		if tag == "noise" {
			noise = 1.0
			break
		}
	}

	// TaskRelevance: scale Level 0-3 to 0-1
	taskRelevance := float64(m.Level) / 3.0
	if taskRelevance > 1.0 {
		taskRelevance = 1.0
	}

	return MemorySignals{
		Salience:         m.Importance,
		Stability:        stability,
		TaskRelevance:    taskRelevance,
		UserExplicitness: m.Confidence,
		Noise:            noise,
		MemoryType:       schemas.MemoryType(m.MemoryType),
	}
}

// clamp01 clamps a float64 to the [0,1] range.
func clamp01(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

// log1pCapped returns log(1+x) / log(11), capped at 1.0.
// Used for recall-count scoring to apply diminishing returns.
func log1pCapped(n int) float64 {
	v := math.Log1p(float64(n)) / math.Log1p(10.0) // log_10(n+1), capped at 1
	if v > 1.0 {
		return 1.0
	}
	return v
}
