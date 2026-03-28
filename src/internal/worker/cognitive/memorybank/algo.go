package memorybank

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"andb/src/internal/schemas"
)

// MemoryBankAlgorithm implements schemas.MemoryManagementAlgorithm using the
// MemoryBank governance model: 8 dimensions, 8 lifecycle states, admission
// scoring, multi-dimensional retention, conflict detection, and profile management.
type MemoryBankAlgorithm struct {
	id     string
	cfg    Config
	mu     sync.RWMutex
	states map[string]schemas.MemoryAlgorithmState // keyed by memoryID

	conflictRegistry *ConflictRegistry
	signals         map[string]MemorySignals // admission signal cache
	profiles        map[string]*ProfileState // keyed by agentID
	// memoryCache stores the full Memory objects needed for conflict detection.
	// DetectConflicts requires MemoryType and Content, which are not stored in
	// MemoryAlgorithmState, so we cache them here keyed by memoryID.
	memoryCache map[string]schemas.Memory
}

// New creates a MemoryBankAlgorithm with the provided config.
func New(id string, cfg Config) *MemoryBankAlgorithm {
	return &MemoryBankAlgorithm{
		id:              id,
		cfg:             cfg,
		states:          make(map[string]schemas.MemoryAlgorithmState),
		conflictRegistry: NewConflictRegistry(),
		signals:        make(map[string]MemorySignals),
		profiles:        make(map[string]*ProfileState),
		memoryCache:     make(map[string]schemas.Memory),
	}
}

// NewDefault creates a MemoryBankAlgorithm loading YAML config.
func NewDefault(id string) *MemoryBankAlgorithm {
	cfg, _ := LoadFromYAML() // falls back to defaults on error
	return New(id, cfg)
}

func (mb *MemoryBankAlgorithm) AlgorithmID() string { return AlgorithmID }

// ─── Ingest ───────────────────────────────────────────────────────────────────

func (mb *MemoryBankAlgorithm) Ingest(memories []schemas.Memory, ctx schemas.AlgorithmContext) []schemas.MemoryAlgorithmState {
	now := ctx.Timestamp
	if now == "" {
		now = tsNow()
	}

	mb.mu.Lock()
	defer mb.mu.Unlock()

	out := make([]schemas.MemoryAlgorithmState, 0, len(memories))
	for _, m := range memories {
		// 0. Cache the full memory for conflict detection (requires MemoryType + Content)
		mb.memoryCache[m.MemoryID] = m

		// 1. Extract governance signals (cache for future use)
		sig := extractSignals(m)
		mb.signals[m.MemoryID] = sig

		// 2. Compute admission score
		admScore := ComputeAdmissionScore(mb.cfg, sig)

		// 3. Detect conflicts with existing memories (uses memoryCache for full objects)
		existing := mb.allMemoriesFromCacheLocked()
		conflicts := DetectConflicts(mb.conflictRegistry, existing, m)
		conflictPenalty := 0.0
		for _, c := range conflicts {
			mb.conflictRegistry.Register(c)
			if c.Severity > conflictPenalty {
				conflictPenalty = c.Severity
			}
		}

		// 4. Determine initial state
		strength := mb.cfg.InitialStrength[string(m.MemoryType)]
		if strength == 0 {
			strength = 1.0
		}

		suggestedState := string(schemas.MemoryLifecycleCandidate)
		if conflictPenalty >= 0.7 {
			suggestedState = string(schemas.MemoryLifecycleQuarantined)
		} else if admScore >= mb.cfg.THActive {
			suggestedState = string(schemas.MemoryLifecycleActive)
		} else if admScore >= mb.cfg.THSession {
			suggestedState = string(schemas.MemoryLifecycleActive)
		}

		st := schemas.MemoryAlgorithmState{
			MemoryID:                m.MemoryID,
			AlgorithmID:            AlgorithmID,
			Strength:               strength,
			RetentionScore:         admScore,
			RecallCount:            0,
			SuggestedLifecycleState: suggestedState,
			UpdatedAt:              now,
		}
		mb.states[m.MemoryID] = st
		out = append(out, st)
	}
	return out
}

// ─── Update ───────────────────────────────────────────────────────────────────

func (mb *MemoryBankAlgorithm) Update(memories []schemas.Memory, signals map[string]float64) []schemas.MemoryAlgorithmState {
	now := tsNow()

	mb.mu.Lock()
	defer mb.mu.Unlock()

	// Parse reaffirmation signals from Extra
	reaffirmSet := make(map[string]bool)
	for id, strength := range signals {
		if id != "__user_reaffirmed__" && id != "__tool_supported__" && strength >= 1.0 {
			reaffirmSet[id] = true
		}
	}

	out := make([]schemas.MemoryAlgorithmState, 0, len(memories))
	for _, m := range memories {
		// Cache full memory for conflict detection
		mb.memoryCache[m.MemoryID] = m

		st := mb.getOrInitLocked(m.MemoryID, now)

		// Apply reinforcement signals
		if delta := signals[m.MemoryID]; delta > 0 {
			st.Strength = math.Min(st.Strength+delta, mb.cfg.MaxStrength)
		}

		// Explicit reaffirmation bonus
		if reaffirmSet[m.MemoryID] {
			st.Strength = math.Min(st.Strength+mb.cfg.DeltaUserConfirmed, mb.cfg.MaxStrength)
		}

		// Compute retention score
		conflictPenalty := mb.conflictRegistry.MaxSeverity(m.MemoryID)
		st.RetentionScore = ComputeRetentionScore(mb.cfg, st, m, reaffirmSet, conflictPenalty)

		// Lifecycle evaluation: only change state if there's a reason to
		currentState := st.SuggestedLifecycleState
		if conflictPenalty >= 0.7 && currentState != string(schemas.MemoryLifecycleQuarantined) {
			st.SuggestedLifecycleState = string(schemas.MemoryLifecycleQuarantined)
		} else if currentState == string(schemas.MemoryLifecycleCandidate) {
			// Re-evaluate admission
			sig := mb.signals[m.MemoryID]
			if ComputeAdmissionScore(mb.cfg, sig) >= mb.cfg.THActive {
				st.SuggestedLifecycleState = string(schemas.MemoryLifecycleActive)
			}
		}

		st.UpdatedAt = now
		mb.states[m.MemoryID] = st
		out = append(out, st)
	}
	return out
}

// ─── Recall ──────────────────────────────────────────────────────────────────

func (mb *MemoryBankAlgorithm) Recall(query string, candidates []schemas.Memory, ctx schemas.AlgorithmContext) []schemas.ScoredMemory {
	now := ctx.Timestamp
	if now == "" {
		now = tsNow()
	}

	mb.mu.Lock()
	defer mb.mu.Unlock()

	// Parse reaffirmation signals from context
	reaffirmSet := make(map[string]bool)
	if ctx.Extra != nil {
		if ur, ok := ctx.Extra["user_reaffirmed_memory_ids"].([]string); ok {
			for _, id := range ur {
				reaffirmSet[id] = true
			}
		}
	}

	scored := make([]schemas.ScoredMemory, 0, len(candidates))
	for _, m := range candidates {
		// Cache full memory for conflict detection
		mb.memoryCache[m.MemoryID] = m

		st := mb.getOrInitLocked(m.MemoryID, now)
		currentState := mb.currentLifecycleState(m)

		// ── Governance filter ────────────────────────────────────────────────
		// Allow: active, reinforced, stale (if not critically decayed)
		allowedStates := map[string]bool{
			string(schemas.MemoryLifecycleActive):     true,
			string(schemas.MemoryLifecycleReinforced): true,
		}
		if st.RetentionScore >= mb.cfg.RetentionThresholdArchive {
			allowedStates[string(schemas.MemoryLifecycleStale)] = true
		}
		if !allowedStates[currentState] && !allowedStates[st.SuggestedLifecycleState] && currentState != "" && st.SuggestedLifecycleState != "" {
			continue // filtered out by governance
		}

		// Conflict filter: quarantined → not recalled
		conflictPenalty := mb.conflictRegistry.MaxSeverity(m.MemoryID)
		if conflictPenalty >= 1.0 {
			continue
		}

		// TTL filter
		if m.TTL > 0 && m.ValidTo != "" {
			validTo, err := time.Parse(time.RFC3339, m.ValidTo)
			if err == nil && validTo.Before(time.Now()) {
				continue // expired
			}
		}

		// ── Multi-dimensional scoring ─────────────────────────────────────────
		retentionScore := ComputeRetentionScore(mb.cfg, st, m, reaffirmSet, conflictPenalty)
		freshnessScore := m.FreshnessScore
		if freshnessScore == 0 {
			freshnessScore = recencyFactor(mb.cfg, m.ValidFrom)
		}
		// simScore = 1.0 as placeholder; retrieval layer provides actual similarity
		simScore := 1.0
		finalScore := simScore +
			mb.cfg.WRetention*retentionScore +
			mb.cfg.WSalRet*m.Importance +
			mb.cfg.WFreshness*freshnessScore +
			mb.cfg.WConfidence*m.Confidence -
			mb.cfg.WConflictR*conflictPenalty

		signal := "retention_score"
		if conflictPenalty > 0 && conflictPenalty < 0.7 {
			signal = "conflict_suspected"
		}
		if reaffirmSet[m.MemoryID] {
			signal = "user_reaffirmed"
		}

		scored = append(scored, schemas.ScoredMemory{
			Memory: m,
			Score:  finalScore,
			Signal: signal,
		})

		// ── Reinforce: update state ───────────────────────────────────────────
		st.LastRecalledAt = now
		st.RecallCount++
		st.Strength = math.Min(st.Strength*mb.cfg.RecallBoost, mb.cfg.MaxStrength)
		if currentState == string(schemas.MemoryLifecycleActive) ||
			currentState == "" ||
			st.SuggestedLifecycleState == string(schemas.MemoryLifecycleReinforced) {
			st.SuggestedLifecycleState = string(schemas.MemoryLifecycleReinforced)
		}
		st.UpdatedAt = now
		mb.states[m.MemoryID] = st
	}

	sort.Slice(scored, func(i, j int) bool { return scored[i].Score > scored[j].Score })
	return scored
}

// ─── Compress ─────────────────────────────────────────────────────────────────

func (mb *MemoryBankAlgorithm) Compress(memories []schemas.Memory, ctx schemas.AlgorithmContext) []schemas.Memory {
	if len(memories) == 0 {
		return nil
	}
	now := ctx.Timestamp
	if now == "" {
		now = tsNow()
	}

	mb.mu.Lock()
	defer mb.mu.Unlock()

	// Mark source memories as compressed
	for _, m := range memories {
		if st, ok := mb.states[m.MemoryID]; ok {
			st.SuggestedLifecycleState = string(schemas.MemoryLifecycleCompressed)
			st.UpdatedAt = now
			mb.states[m.MemoryID] = st
		}
	}

	// Merge content
	parts := make([]string, 0, len(memories))
	totalImportance := 0.0
	highestConfidence := 0.0
	for _, m := range memories {
		parts = append(parts, m.Content)
		totalImportance += m.Importance
		if m.Confidence > highestConfidence {
			highestConfidence = m.Confidence
		}
	}

	// Extract profile from episodic sources
	profile := ExtractProfile(memories)
	portraitState := profile.Serialize()

	src := memories[0]
	summaryID := fmt.Sprintf("%smb_cmp_%s_%d", schemas.IDPrefixSummary, src.AgentID, time.Now().UnixNano())

	// Update source states with summary refs
	for _, m := range memories {
		if st, ok := mb.states[m.MemoryID]; ok {
			st.SummaryRefs = append(st.SummaryRefs, summaryID)
			if portraitState != "" {
				st.PortraitState = portraitState
			}
			mb.states[m.MemoryID] = st
		}
	}

	return []schemas.Memory{{
		MemoryID:           summaryID,
		MemoryType:        string(schemas.MemoryTypeSemantic),
		AgentID:           src.AgentID,
		SessionID:         src.SessionID,
		Level:             mb.cfg.CompressedLevel,
		Content:           strings.Join(parts, " | "),
		Summary:           fmt.Sprintf("Compressed from %d memories", len(memories)),
		Confidence:        highestConfidence,
		Importance:        totalImportance / float64(len(memories)),
		IsActive:          true,
		LifecycleState:    string(schemas.MemoryLifecycleActive),
		ValidFrom:         now,
		Version:           1,
		AlgorithmStateRef: AlgorithmID,
	}}
}

// ─── Decay ────────────────────────────────────────────────────────────────────

func (mb *MemoryBankAlgorithm) Decay(memories []schemas.Memory, nowTS string) []schemas.MemoryAlgorithmState {
	if nowTS == "" {
		nowTS = tsNow()
	}
	nowT, err := time.Parse(time.RFC3339, nowTS)
	if err != nil {
		nowT = time.Now().UTC()
	}

	mb.mu.Lock()
	defer mb.mu.Unlock()

	out := make([]schemas.MemoryAlgorithmState, 0, len(memories))
	for _, m := range memories {
		// Cache full memory for conflict detection
		mb.memoryCache[m.MemoryID] = m

		st := mb.getOrInitLocked(m.MemoryID, nowTS)

		// Get per-type decay rate
		λ := mb.cfg.DefaultDecayRate
		if custom, ok := mb.cfg.DecayRate[string(m.MemoryType)]; ok {
			λ = custom
		}

		if st.UpdatedAt != "" {
			if last, err := time.Parse(time.RFC3339, st.UpdatedAt); err == nil {
				days := nowT.Sub(last).Hours() / 24.0
				if days > 0 {
					decay := math.Exp(-λ * days)
					st.Strength *= decay
					st.RetentionScore *= decay
				}
			}
		}

		// Lifecycle evaluation: quarantine is preserved (never overwritten by Decay).
		// Only states set by Ingest/Update are re-evaluated.
		currentState := st.SuggestedLifecycleState
		if currentState != string(schemas.MemoryLifecycleQuarantined) &&
			currentState != string(schemas.MemoryLifecycleArchived) {
			conflictPenalty := mb.conflictRegistry.MaxSeverity(m.MemoryID)
			if conflictPenalty >= 0.7 {
				st.SuggestedLifecycleState = string(schemas.MemoryLifecycleQuarantined)
			} else if st.RetentionScore < mb.cfg.RetentionThresholdArchive {
				st.SuggestedLifecycleState = string(schemas.MemoryLifecycleArchived)
			} else if st.RetentionScore < mb.cfg.RetentionThresholdStale {
				st.SuggestedLifecycleState = string(schemas.MemoryLifecycleStale)
			}
		}

		st.UpdatedAt = nowTS
		mb.states[m.MemoryID] = st
		out = append(out, st)
	}
	return out
}

// ─── Summarize ────────────────────────────────────────────────────────────────

func (mb *MemoryBankAlgorithm) Summarize(memories []schemas.Memory, ctx schemas.AlgorithmContext) []schemas.Memory {
	if len(memories) == 0 {
		return nil
	}
	now := ctx.Timestamp
	if now == "" {
		now = tsNow()
	}

	mb.mu.Lock()
	defer mb.mu.Unlock()

	// Mark source memories as compressed
	for _, m := range memories {
		if st, ok := mb.states[m.MemoryID]; ok {
			st.SuggestedLifecycleState = string(schemas.MemoryLifecycleCompressed)
			st.UpdatedAt = now
			mb.states[m.MemoryID] = st
		}
	}

	// Build summary content
	parts := make([]string, 0, len(memories))
	maxLevel := 0
	totalImportance := 0.0
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

	// Extract + serialise profile
	profile := ExtractProfile(memories)
	portraitState := profile.Serialize()

	src := memories[0]
	summaryID := fmt.Sprintf("%smb_sum_%s_%d", schemas.IDPrefixSummary, src.AgentID, time.Now().UnixNano())

	// Update source states with summary refs
	for _, m := range memories {
		if st, ok := mb.states[m.MemoryID]; ok {
			st.SummaryRefs = append(st.SummaryRefs, summaryID)
			if portraitState != "" {
				st.PortraitState = portraitState
			}
			mb.states[m.MemoryID] = st
		}
	}

	return []schemas.Memory{{
		MemoryID:           summaryID,
		MemoryType:        string(schemas.MemoryTypeSemantic),
		AgentID:           src.AgentID,
		SessionID:         src.SessionID,
		Level:             maxLevel + 1,
		Content:           strings.Join(parts, " · "),
		Summary:           fmt.Sprintf("Summary of %d memories at level %d", len(memories), maxLevel),
		Confidence:        schemas.DefaultConfidence,
		Importance:        totalImportance / float64(len(memories)),
		IsActive:          true,
		LifecycleState:    string(schemas.MemoryLifecycleActive),
		ValidFrom:         now,
		Version:           1,
		AlgorithmStateRef: AlgorithmID,
	}}
}

// ─── State management ─────────────────────────────────────────────────────────

func (mb *MemoryBankAlgorithm) ExportState(memoryID string) (schemas.MemoryAlgorithmState, bool) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	st, ok := mb.states[memoryID]
	return st, ok
}

func (mb *MemoryBankAlgorithm) LoadState(state schemas.MemoryAlgorithmState) {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	mb.states[state.MemoryID] = state

	// Restore profile from PortraitState
	if state.PortraitState != "" {
		var ps ProfileState
		if err := json.Unmarshal([]byte(state.PortraitState), &ps); err == nil {
			mb.profiles[state.MemoryID] = &ps
		}
	}
}

// ─── internal helpers ─────────────────────────────────────────────────────────

// getOrInitLocked returns existing state or creates a fresh one.
// Caller MUST hold mb.mu.
func (mb *MemoryBankAlgorithm) getOrInitLocked(memoryID, now string) schemas.MemoryAlgorithmState {
	if st, ok := mb.states[memoryID]; ok {
		return st
	}
	return schemas.MemoryAlgorithmState{
		MemoryID:     memoryID,
		AlgorithmID: AlgorithmID,
		Strength:    mb.cfg.InitialStrength["episodic"],
		RetentionScore: 1.0,
		UpdatedAt:   now,
	}
}

// currentLifecycleState returns the effective lifecycle state for a memory.
// It prefers the algorithm's own SuggestedLifecycleState (set by this algorithm's
// Ingest/Update/Recall/Decay) over Memory.LifecycleState (set by the dispatcher).
// This ensures correct governance filtering in Recall even when tests call Ingest
// directly without going through the dispatcher.
func (mb *MemoryBankAlgorithm) currentLifecycleState(m schemas.Memory) string {
	if st, ok := mb.states[m.MemoryID]; ok {
		if st.SuggestedLifecycleState != "" {
			return st.SuggestedLifecycleState
		}
		// Fall back to Memory.LifecycleState only when algorithm has no suggestion yet
		if m.LifecycleState != "" {
			return m.LifecycleState
		}
	}
	if m.LifecycleState != "" {
		return m.LifecycleState
	}
	return ""
}

// allMemoriesFromCacheLocked returns all full Memory objects from the cache.
// Caller MUST hold mb.mu.
func (mb *MemoryBankAlgorithm) allMemoriesFromCacheLocked() []schemas.Memory {
	out := make([]schemas.Memory, 0, len(mb.memoryCache))
	for _, m := range mb.memoryCache {
		out = append(out, m)
	}
	return out
}

// allMemoriesLocked returns all memories stored in mb.states as a slice.
// Caller MUST hold mb.mu.
func (mb *MemoryBankAlgorithm) allMemoriesLocked() []schemas.Memory {
	return mb.allMemoriesFromCacheLocked()
}

func tsNow() string { return time.Now().UTC().Format(time.RFC3339) }
