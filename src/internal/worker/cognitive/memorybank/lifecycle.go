package memorybank

import (
	"andb/src/internal/schemas"
)

// LifecycleTransition records a state change for audit/debugging purposes.
type LifecycleTransition struct {
	MemoryID  string
	From, To  string
	Reason    string
	Timestamp string
}

// nextLifecycle returns the next lifecycle state given the current state.
// This implements the MemoryBank state machine.
//
// State transitions:
//   "" (nil) → candidate         (Ingest: below admission threshold)
//   "" → active                  (Ingest: meets admission threshold)
//   candidate → active           (Update/Decay: admission score rises)
//   candidate → candidate         (Update/Decay: still below threshold)
//   active → reinforced          (Recall: successfully recalled)
//   reinforced → reinforced      (Recall: repeated recall)
//   active → compressed          (Compress: absorbed by summary)
//   active → stale               (Decay: retention drops)
//   reinforced → stale           (Decay: retention drops)
//   stale → archived             (Decay: retention drops to archive threshold)
//   compressed → stale            (Decay: retention drops)
//   any → quarantined            (Conflict: confirmed conflict detected)
//   quarantined → quarantined     (stays until explicitly cleared)
func nextLifecycle(cfg Config, st schemas.MemoryAlgorithmState, currentState string) string {
	switch currentState {
	case "":
		// No prior state: initial placement based on admission
		if st.SuggestedLifecycleState != "" {
			return st.SuggestedLifecycleState
		}
		return string(schemas.MemoryLifecycleCandidate)

	case string(schemas.MemoryLifecycleCandidate):
		// Re-evaluate if it should activate
		if st.SuggestedLifecycleState == string(schemas.MemoryLifecycleActive) {
			return string(schemas.MemoryLifecycleActive)
		}
		return string(schemas.MemoryLifecycleCandidate)

	case string(schemas.MemoryLifecycleActive):
		if st.SuggestedLifecycleState == string(schemas.MemoryLifecycleCompressed) {
			return string(schemas.MemoryLifecycleCompressed)
		}
		if st.RetentionScore < cfg.RetentionThresholdStale {
			return string(schemas.MemoryLifecycleStale)
		}
		if st.SuggestedLifecycleState == string(schemas.MemoryLifecycleReinforced) {
			return string(schemas.MemoryLifecycleReinforced)
		}
		return string(schemas.MemoryLifecycleActive)

	case string(schemas.MemoryLifecycleReinforced):
		if st.SuggestedLifecycleState == string(schemas.MemoryLifecycleCompressed) {
			return string(schemas.MemoryLifecycleCompressed)
		}
		if st.RetentionScore < cfg.RetentionThresholdStale {
			return string(schemas.MemoryLifecycleStale)
		}
		return string(schemas.MemoryLifecycleReinforced)

	case string(schemas.MemoryLifecycleCompressed):
		if st.RetentionScore < cfg.RetentionThresholdStale {
			return string(schemas.MemoryLifecycleStale)
		}
		return string(schemas.MemoryLifecycleCompressed)

	case string(schemas.MemoryLifecycleStale):
		if st.RetentionScore < cfg.RetentionThresholdArchive {
			return string(schemas.MemoryLifecycleArchived)
		}
		return string(schemas.MemoryLifecycleStale)

	case string(schemas.MemoryLifecycleArchived):
		return string(schemas.MemoryLifecycleArchived)

	case string(schemas.MemoryLifecycleQuarantined):
		// Quarantine is sticky: only cleared by explicit Update
		if st.SuggestedLifecycleState != "" &&
			st.SuggestedLifecycleState != string(schemas.MemoryLifecycleQuarantined) {
			return st.SuggestedLifecycleState
		}
		return string(schemas.MemoryLifecycleQuarantined)

	default:
		// Honour explicit suggestions for unknown states
		if st.SuggestedLifecycleState != "" {
			return st.SuggestedLifecycleState
		}
		return currentState
	}
}

// evalLifecycle evaluates and returns the appropriate lifecycle state for a memory.
// This should be called after updating RetentionScore, Strength, and SuggestedLifecycleState.
func evalLifecycle(cfg Config, st schemas.MemoryAlgorithmState, currentState string) string {
	// Confirmed conflict always triggers quarantine
	if len(st.SummaryRefs) == 0 && st.SuggestedLifecycleState == string(schemas.MemoryLifecycleQuarantined) {
		return string(schemas.MemoryLifecycleQuarantined)
	}
	return nextLifecycle(cfg, st, currentState)
}
