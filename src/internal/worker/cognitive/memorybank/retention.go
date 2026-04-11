package memorybank

import (
	"math"
	"strings"
	"time"

	"plasmod/src/internal/schemas"
)

// ComputeRetentionScore returns a multi-factor retention score in [0,1].
//
// Formula:
//   score = (w_recency*recency + w_recall*recall_log + w_sal*importance
//            + w_conf*confidence + w_conflict*(1-penalty) + w_reaffirm*reaffirm) * type_bonus
//
// Where:
//   - recency: bell-curve bonus peaking at ~7 days, decaying on both sides
//   - recall_log: logarithmic recall-count bonus (diminishing returns)
//   - salience: Memory.Importance
//   - confidence: Memory.Confidence
//   - conflict: 1.0 = confirmed conflict, 0.0 = no conflict
//   - reaffirm: 1.0 if memory was explicitly reaffirmed by user
//   - type_bonus: bonus for slow-decay memory types (factual, profile, preference)
func ComputeRetentionScore(
	cfg Config,
	st schemas.MemoryAlgorithmState,
	m schemas.Memory,
	reaffirmSet map[string]bool,
	conflictPenalty float64,
) float64 {
	recency := recencyFactor(cfg, st.UpdatedAt)
	recallLog := log1pCapped(st.RecallCount)

	// Type bonus: slower-decay types get a retention bonus
	typeBonus := 1.0
	if rate, ok := cfg.DecayRate[string(m.MemoryType)]; ok {
		typeBonus = 1.0 + 0.2*(1.0-rate)
	}

	reaffirmBonus := 0.0
	if reaffirmSet[m.MemoryID] {
		reaffirmBonus = 1.0
	}

	score := (cfg.WRecency*recency +
		cfg.WRecallCnt*recallLog +
		cfg.WSalRet*m.Importance +
		cfg.WConfidence*m.Confidence +
		cfg.WConflictP*(1.0-conflictPenalty) +
		cfg.WReaffirm*reaffirmBonus) * typeBonus

	return clamp01(score)
}

// recencyFactor returns a bell-curve bonus for memory age.
// Peaks at peakDays, decays on both sides with sigma width.
func recencyFactor(cfg Config, updatedAt string) float64 {
	if updatedAt == "" {
		return 0.5
	}
	t, err := time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return 0.5
	}
	days := time.Since(t).Hours() / 24.0
	// Bell-curve: bonus peaks at ~7 days
	peak := 7.0
	sigma := 14.0
	if sigma == 0 {
		return 0.5
	}
	x := (days - peak) / sigma
	return clamp01(math.Exp(-0.5 * x * x))
}

// ComputeRecallScore returns the final recall score combining similarity
// (from retrieval layer) with governance dimensions.
func ComputeRecallScore(
	cfg Config,
	simScore float64,
	retentionScore float64,
	m schemas.Memory,
	freshnessScore float64,
	conflictPenalty float64,
) float64 {
	if freshnessScore == 0 {
		freshnessScore = 0.5
	}

	return simScore +
		cfg.WRetention*retentionScore +
		cfg.WSalRet*m.Importance +
		cfg.WFreshness*freshnessScore +
		cfg.WConfidence*m.Confidence -
		cfg.WConflictR*conflictPenalty
}

// polarity flips "yes"/"no" preference polarity.
// Used by conflict detection to find opposite preference pairs.
func polarity(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "yes", "true", "1", "confirm", "agree":
		return "positive"
	case "no", "false", "0", "deny", "disagree", "never":
		return "negative"
	default:
		return "neutral"
	}
}
