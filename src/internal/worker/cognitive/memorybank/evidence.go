package memorybank

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"plasmod/src/internal/schemas"
)

func sourceEvents(memories []schemas.Memory) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range memories {
		for _, id := range m.SourceEventIDs {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	sort.Strings(out)
	return out
}
func derivedID(operation string, memories []schemas.Memory) string {
	ids := []string{operation}
	for _, m := range memories {
		data, _ := json.Marshal([]any{m.MemoryID, m.Version, m.Content})
		ids = append(ids, string(data))
	}
	sort.Strings(ids[1:])
	data, _ := json.Marshal(ids)
	sum := sha256.Sum256(data)
	return schemas.IDPrefixSummary + "mb_" + hex.EncodeToString(sum[:16])
}

// RestoreMemories rebuilds content-dependent evidence after restart. It does
// not re-ingest objects or overwrite durable lifecycle/reinforcement state.
func (mb *MemoryBankAlgorithm) RestoreMemories(memories []schemas.Memory) {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	mb.memoryCache = make(map[string]schemas.Memory, len(memories))
	mb.conflictRegistry = NewConflictRegistry()
	for _, m := range memories {
		if m.IsActive {
			mb.memoryCache[m.MemoryID] = m
			mb.signals[m.MemoryID] = extractSignals(m)
		}
	}
	// Index comparisons by explicit attribute or shared source; independent
	// scoped facts do not require an all-pairs conflict rebuild.
	groups := map[string][]schemas.Memory{}
	for _, m := range memories {
		if !m.IsActive || m.LifecycleState == "compressed" {
			continue
		}
		identity, _ := json.Marshal([]string{m.TenantID, m.WorkspaceID, m.AgentID})
		var keys []string
		if m.FactKey != "" {
			keys = []string{string(identity) + "|key:" + m.FactKey}
		} else if m.WorkspaceID != "" {
			for _, source := range m.SourceEventIDs {
				keys = append(keys, string(identity)+"|source:"+source)
			}
		} else {
			keys = []string{string(identity) + "|legacy"}
		}
		var candidates []schemas.Memory
		seen := map[string]bool{}
		for _, key := range keys {
			for _, other := range groups[key] {
				if !seen[other.MemoryID] {
					seen[other.MemoryID] = true
					candidates = append(candidates, other)
				}
			}
		}
		for _, conflict := range DetectConflicts(mb.conflictRegistry, candidates, m) {
			mb.conflictRegistry.Register(conflict)
		}
		for _, key := range keys {
			groups[key] = append(groups[key], m)
		}
	}
}

func (mb *MemoryBankAlgorithm) evaluateAdmissionLocked(m schemas.Memory, st *schemas.MemoryAlgorithmState, confirmed bool) {
	if st.SuggestedLifecycleState != "candidate" {
		return
	}
	sig := extractSignals(m)
	st.AdmissionScore = ComputeAdmissionScore(mb.cfg, sig)
	st.AdmissionReason = "insufficient_independent_evidence"
	events := map[string]bool{}
	sessions := map[string]bool{}
	for _, other := range mb.memoryCache {
		if !other.IsActive || other.TenantID != m.TenantID || other.WorkspaceID != m.WorkspaceID || other.AgentID != m.AgentID || !sameContent(other.Content, m.Content) {
			continue
		}
		for _, id := range other.SourceEventIDs {
			events[id] = true
		}
		if other.SessionID != "" {
			sessions[other.SessionID] = true
		}
	}
	st.EvidenceEventIDs = nil
	for id := range events {
		st.EvidenceEventIDs = append(st.EvidenceEventIDs, id)
	}
	sort.Strings(st.EvidenceEventIDs)
	if mb.conflictRegistry.MaxSeverity(m.MemoryID) >= 0.7 || sig.Noise > 0 {
		st.AdmissionReason = "conflict_or_noise"
		return
	}
	if confirmed {
		st.UserConfirmed = true
		st.SuggestedLifecycleState = "active"
		st.AdmissionReason = "user_confirmed"
		return
	}
	// Repetition is evidence only for extracted facts/preferences, never questions,
	// quotations, assistant guesses or temporary episodic intentions.
	durable := m.MemoryType == "factual" || m.MemoryType == "preference_constraint" || m.MemoryType == "profile"
	if durable && m.Confidence >= 0.7 && len(events) >= 3 && len(sessions) >= 3 {
		st.SuggestedLifecycleState = "active"
		st.AdmissionReason = "repeated_across_sessions"
		return
	}
	if st.AdmissionScore >= mb.cfg.THActive {
		st.SuggestedLifecycleState = "active"
		st.AdmissionReason = "admission_threshold"
	}
}
