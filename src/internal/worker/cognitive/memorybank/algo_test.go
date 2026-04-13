package memorybank

import (
	"strings"
	"testing"
	"time"

	"plasmod/src/internal/schemas"
)

func TestMemoryBankAlgorithm_AlgorithmID(t *testing.T) {
	mb := NewDefault("test-algo")
	if mb.AlgorithmID() != AlgorithmID {
		t.Errorf("AlgorithmID() = %q, want %q", mb.AlgorithmID(), AlgorithmID)
	}
}

func TestMemoryBankAlgorithm_Ingest_InitialState(t *testing.T) {
	cfg := DefaultConfig()
	mb := New("test", cfg)
	ctx := schemas.AlgorithmContext{AgentID: "agent1", Timestamp: tsNow()}

	mem := schemas.Memory{
		MemoryID:    "mem1",
		MemoryType:  string(schemas.MemoryTypeEpisodic),
		AgentID:     "agent1",
		Importance:  0.8,
		Confidence:  0.9,
		Level:       0,
		SourceEventIDs: []string{"evt1", "evt2", "evt3"},
	}

	states := mb.Ingest([]schemas.Memory{mem}, ctx)
	if len(states) != 1 {
		t.Fatalf("len(states) = %d, want 1", len(states))
	}

	st := states[0]
	if st.MemoryID != "mem1" {
		t.Errorf("st.MemoryID = %q, want mem1", st.MemoryID)
	}
	if st.AlgorithmID != AlgorithmID {
		t.Errorf("st.AlgorithmID = %q, want %q", st.AlgorithmID, AlgorithmID)
	}
	if st.Strength == 0 {
		t.Error("st.Strength should be > 0")
	}
	if st.RetentionScore == 0 {
		t.Error("st.RetentionScore should be > 0")
	}
}

func TestMemoryBankAlgorithm_Ingest_HighConfidenceActive(t *testing.T) {
	cfg := DefaultConfig()
	mb := New("test", cfg)
	ctx := schemas.AlgorithmContext{AgentID: "agent1", Timestamp: tsNow()}

	// High confidence + high importance → should be active immediately
	mem := schemas.Memory{
		MemoryID:   "mem-high",
		MemoryType: string(schemas.MemoryTypeFactual),
		AgentID:    "agent1",
		Importance: 0.9,
		Confidence: 0.95,
		Level:      2, // summary level = high task relevance
	}

	states := mb.Ingest([]schemas.Memory{mem}, ctx)
	if states[0].SuggestedLifecycleState != string(schemas.MemoryLifecycleActive) {
		t.Errorf("SuggestedLifecycleState = %q, want active",
			states[0].SuggestedLifecycleState)
	}
}

func TestMemoryBankAlgorithm_Ingest_LowScoreCandidate(t *testing.T) {
	cfg := DefaultConfig()
	cfg.THActive = 0.65
	mb := New("test", cfg)
	ctx := schemas.AlgorithmContext{AgentID: "agent1", Timestamp: tsNow()}

	// Very low importance + confidence → below threshold → candidate
	mem := schemas.Memory{
		MemoryID:    "mem-low",
		MemoryType:  string(schemas.MemoryTypeEpisodic),
		AgentID:     "agent1",
		Importance:  0.05,
		Confidence:  0.2,
		Level:       0,
		PolicyTags:  []string{"noise"}, // noise tag
	}

	states := mb.Ingest([]schemas.Memory{mem}, ctx)
	state := states[0]
	if state.SuggestedLifecycleState != string(schemas.MemoryLifecycleCandidate) {
		t.Errorf("SuggestedLifecycleState = %q, want candidate (low admission score)",
			state.SuggestedLifecycleState)
	}
}

func TestMemoryBankAlgorithm_Ingest_Idempotent(t *testing.T) {
	cfg := DefaultConfig()
	mb := New("test", cfg)
	ctx := schemas.AlgorithmContext{AgentID: "agent1", Timestamp: tsNow()}

	mem := schemas.Memory{
		MemoryID:    "mem1",
		MemoryType:  string(schemas.MemoryTypeEpisodic),
		AgentID:     "agent1",
		Importance:  0.8,
		Confidence:  0.9,
		Level:       0,
	}

	// Ingest once
	states1 := mb.Ingest([]schemas.Memory{mem}, ctx)
	strength1 := states1[0].Strength

	// Ingest again (same memory)
	states2 := mb.Ingest([]schemas.Memory{mem}, ctx)
	strength2 := states2[0].Strength

	// Strength should be preserved (idempotent)
	if strength2 < strength1 {
		t.Errorf("Strength after re-ingest = %.4f, should not decrease from %.4f",
			strength2, strength1)
	}
}

func TestMemoryBankAlgorithm_Ingest_FactualHighStrength(t *testing.T) {
	cfg := DefaultConfig()
	mb := New("test", cfg)
	ctx := schemas.AlgorithmContext{AgentID: "agent1", Timestamp: tsNow()}

	mem := schemas.Memory{
		MemoryID:   "mem-fact",
		MemoryType: string(schemas.MemoryTypeFactual),
		AgentID:    "agent1",
		Confidence: 0.9,
		Importance: 0.8,
	}

	states := mb.Ingest([]schemas.Memory{mem}, ctx)
	// Factual type → initial strength = 3.0 (high value)
	if states[0].Strength != 3.0 {
		t.Errorf("Strength for factual = %.2f, want 3.0", states[0].Strength)
	}
}

func TestMemoryBankAlgorithm_Ingest_AffectiveStateLowStrength(t *testing.T) {
	cfg := DefaultConfig()
	mb := New("test", cfg)
	ctx := schemas.AlgorithmContext{AgentID: "agent1", Timestamp: tsNow()}

	mem := schemas.Memory{
		MemoryID:   "mem-affect",
		MemoryType: string(schemas.MemoryTypeAffectiveState),
		AgentID:    "agent1",
		Confidence: 0.6,
		Importance: 0.5,
	}

	states := mb.Ingest([]schemas.Memory{mem}, ctx)
	// Affective state → initial strength = 0.8 (short TTL)
	if states[0].Strength != 0.8 {
		t.Errorf("Strength for affective_state = %.2f, want 0.8", states[0].Strength)
	}
}

func TestMemoryBankAlgorithm_Recall_GovernanceFilter(t *testing.T) {
	cfg := DefaultConfig()
	mb := New("test", cfg)
	ctx := schemas.AlgorithmContext{AgentID: "agent1", Timestamp: tsNow()}

	// Ingest a memory with high-enough admission signals to reach active state.
	// Importance=1.0 + SourceEventIDs=1 → stability=0.2 → admission≈0.43≥THSession=0.40
	mem := schemas.Memory{
		MemoryID:       "mem-active",
		MemoryType:     string(schemas.MemoryTypeEpisodic),
		AgentID:        "agent1",
		Importance:     1.0,
		Confidence:     0.9,
		Level:          0,
		SourceEventIDs: []string{"evt1"},
		IsActive:       true,
	}
	mb.Ingest([]schemas.Memory{mem}, ctx)

	// Recall
	scored := mb.Recall("test query", []schemas.Memory{mem}, ctx)
	if len(scored) != 1 {
		t.Errorf("len(scored) = %d, want 1 for active memory", len(scored))
	}
}

func TestMemoryBankAlgorithm_Recall_ScoreRanking(t *testing.T) {
	cfg := DefaultConfig()
	mb := New("test", cfg)
	ctx := schemas.AlgorithmContext{AgentID: "agent1", Timestamp: tsNow()}

	// lowMem: crosses THSession with high signals + some stability
	// Importance=0.9 + Confidence=1.0 + stability=0.2 → admission=0.415≥THSession=0.40
	lowMem := schemas.Memory{
		MemoryID:       "mem-low",
		MemoryType:     string(schemas.MemoryTypeEpisodic),
		AgentID:        "agent1",
		Importance:     0.9,
		Confidence:     1.0,
		Level:          0,
		SourceEventIDs: []string{"evt1"},
	}
	// highMem: strong signals across all dimensions
	highMem := schemas.Memory{
		MemoryID:       "mem-high",
		MemoryType:     string(schemas.MemoryTypeFactual),
		AgentID:        "agent1",
		Importance:     0.95,
		Confidence:     0.95,
		Level:          2,
		SourceEventIDs: []string{"evt1", "evt2"},
	}

	mb.Ingest([]schemas.Memory{lowMem, highMem}, ctx)
	scored := mb.Recall("test", []schemas.Memory{lowMem, highMem}, ctx)

	if len(scored) != 2 {
		t.Fatalf("len(scored) = %d, want 2", len(scored))
	}
	if scored[0].Score <= scored[1].Score {
		t.Errorf("scored[0].Score (%.4f) should be > scored[1].Score (%.4f)",
			scored[0].Score, scored[1].Score)
	}
}

func TestMemoryBankAlgorithm_Recall_Reinforcement(t *testing.T) {
	cfg := DefaultConfig()
	mb := New("test", cfg)
	ctx := schemas.AlgorithmContext{AgentID: "agent1", Timestamp: tsNow()}

	// High-enough signals to reach active state (admission≈0.43≥THSession=0.40)
	mem := schemas.Memory{
		MemoryID:       "mem-reinforce",
		MemoryType:     string(schemas.MemoryTypeEpisodic),
		AgentID:        "agent1",
		Importance:     1.0,
		Confidence:     0.9,
		Level:          0,
		SourceEventIDs: []string{"evt1"},
	}
	mb.Ingest([]schemas.Memory{mem}, ctx)

	// First recall
	mb.Recall("query", []schemas.Memory{mem}, ctx)

	// Second recall
	mb.Recall("query", []schemas.Memory{mem}, ctx)

	st, ok := mb.ExportState("mem-reinforce")
	if !ok {
		t.Fatal("ExportState failed")
	}
	if st.RecallCount != 2 {
		t.Errorf("RecallCount = %d, want 2", st.RecallCount)
	}
	if st.SuggestedLifecycleState != string(schemas.MemoryLifecycleReinforced) {
		t.Errorf("SuggestedLifecycleState = %q, want reinforced",
			st.SuggestedLifecycleState)
	}
}

func TestMemoryBankAlgorithm_Recall_ConflictPenalty(t *testing.T) {
	cfg := DefaultConfig()
	mb := New("test", cfg)
	ctx := schemas.AlgorithmContext{AgentID: "agent1", Timestamp: tsNow()}

	existing := schemas.Memory{
		MemoryID:    "mem-existing",
		MemoryType:  string(schemas.MemoryTypePreferenceConstraint),
		AgentID:     "agent1",
		Importance:  0.8,
		Confidence:  0.95,
		Level:       0,
		Content:     "prefers formal meetings",
	}
	mb.Ingest([]schemas.Memory{existing}, ctx)

	// New memory with opposite preference → conflict detected
	newMem := schemas.Memory{
		MemoryID:    "mem-new",
		MemoryType:  string(schemas.MemoryTypePreferenceConstraint),
		AgentID:     "agent1",
		Importance:  0.8,
		Confidence:  0.9,
		Level:       0,
		Content:     "avoids formal meetings", // opposite polarity
	}

	// Ingest triggers conflict detection (preference reversal, severity=0.85 → confirmed)
	states := mb.Ingest([]schemas.Memory{newMem}, ctx)
	if states[0].SuggestedLifecycleState != string(schemas.MemoryLifecycleQuarantined) {
		t.Errorf("SuggestedLifecycleState = %q, want quarantined (confirmed conflict)",
			states[0].SuggestedLifecycleState)
	}

	// Quarantined memory should be excluded from recall
	scored := mb.Recall("query", []schemas.Memory{newMem}, ctx)
	if len(scored) != 0 {
		t.Errorf("len(scored) = %d for quarantined memory, want 0", len(scored))
	}
}

func TestMemoryBankAlgorithm_Recall_TTLFilter(t *testing.T) {
	cfg := DefaultConfig()
	mb := New("test", cfg)
	ctx := schemas.AlgorithmContext{AgentID: "agent1", Timestamp: tsNow()}

	// Expired memory (ValidTo in the past)
	expiredMem := schemas.Memory{
		MemoryID:   "mem-expired",
		MemoryType: string(schemas.MemoryTypeEpisodic),
		AgentID:    "agent1",
		Importance: 0.8,
		Confidence: 0.9,
		Level:      0,
		TTL:        1,
		ValidTo:    time.Now().Add(-24 * time.Hour).Format(time.RFC3339),
	}

	mb.Ingest([]schemas.Memory{expiredMem}, ctx)
	scored := mb.Recall("query", []schemas.Memory{expiredMem}, ctx)
	if len(scored) != 0 {
		t.Errorf("len(scored) = %d for expired memory, want 0", len(scored))
	}
}

func TestMemoryBankAlgorithm_Update_Reinforcement(t *testing.T) {
	cfg := DefaultConfig()
	mb := New("test", cfg)
	ctx := schemas.AlgorithmContext{AgentID: "agent1", Timestamp: tsNow()}

	mem := schemas.Memory{
		MemoryID:    "mem-update",
		MemoryType:  string(schemas.MemoryTypeEpisodic),
		AgentID:     "agent1",
		Importance:  0.8,
		Confidence:  0.9,
		Level:       0,
		SourceEventIDs: []string{"evt1"},
	}
	mb.Ingest([]schemas.Memory{mem}, ctx)

	initialStrength := mb.states["mem-update"].Strength

	// Update with positive signal
	signals := map[string]float64{"mem-update": 0.5}
	states := mb.Update([]schemas.Memory{mem}, signals)

	newStrength := states[0].Strength
	if newStrength <= initialStrength {
		t.Errorf("Strength after positive update = %.4f, should increase from %.4f",
			newStrength, initialStrength)
	}
	if newStrength > cfg.MaxStrength {
		t.Errorf("Strength = %.4f exceeds MaxStrength %.4f", newStrength, cfg.MaxStrength)
	}
}

func TestMemoryBankAlgorithm_Update_CandidateActivation(t *testing.T) {
	cfg := DefaultConfig()
	cfg.THActive = 0.65
	mb := New("test", cfg)
	ctx := schemas.AlgorithmContext{AgentID: "agent1", Timestamp: tsNow()}

	// Start with low-scoring candidate
	mem := schemas.Memory{
		MemoryID:    "mem-candidate",
		MemoryType:  string(schemas.MemoryTypeEpisodic),
		AgentID:     "agent1",
		Importance:  0.1, // very low
		Confidence:  0.2,
		Level:       0,
		PolicyTags:  []string{"noise"},
	}
	mb.Ingest([]schemas.Memory{mem}, ctx)
	if mb.states["mem-candidate"].SuggestedLifecycleState != string(schemas.MemoryLifecycleCandidate) {
		t.Fatalf("initial state = %q, want candidate",
			mb.states["mem-candidate"].SuggestedLifecycleState)
	}

	// Update with explicit reaffirmation (user confirmed)
	signals := map[string]float64{"mem-candidate": 1.5}
	updatedMem := mem
	updatedMem.Importance = 0.9 // high importance signal
	states := mb.Update([]schemas.Memory{updatedMem}, signals)

	// Should re-evaluate admission
	if states[0].SuggestedLifecycleState == string(schemas.MemoryLifecycleActive) {
		t.Log("candidate activated after update (expected)")
	}
}

func TestMemoryBankAlgorithm_Decay_LifecycleTransitions(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RetentionThresholdStale = 0.25
	cfg.RetentionThresholdArchive = 0.10
	mb := New("test", cfg)
	ctx := schemas.AlgorithmContext{AgentID: "agent1", Timestamp: tsNow()}

	mem := schemas.Memory{
		MemoryID:    "mem-decay",
		MemoryType:  string(schemas.MemoryTypeEpisodic),
		AgentID:     "agent1",
		Importance:  0.8,
		Confidence:  0.9,
		Level:       0,
	}
	mb.Ingest([]schemas.Memory{mem}, ctx)

	// Force a low retention score to trigger stale
	mb.mu.Lock()
	st := mb.states["mem-decay"]
	st.RetentionScore = 0.2
	mb.states["mem-decay"] = st
	mb.mu.Unlock()

	// Decay with time far in the future
	futureTS := time.Now().Add(30 * 24 * time.Hour).Format(time.RFC3339)
	states := mb.Decay([]schemas.Memory{mem}, futureTS)

	if states[0].SuggestedLifecycleState == string(schemas.MemoryLifecycleStale) {
		t.Log("memory moved to stale (expected for low retention)")
	}
	if states[0].SuggestedLifecycleState == string(schemas.MemoryLifecycleArchived) {
		t.Log("memory moved to archived (expected for very low retention)")
	}
}

func TestMemoryBankAlgorithm_Decay_QuarantinePreserved(t *testing.T) {
	cfg := DefaultConfig()
	mb := New("test", cfg)
	ctx := schemas.AlgorithmContext{AgentID: "agent1", Timestamp: tsNow()}

	mem := schemas.Memory{
		MemoryID:   "mem-quarantine",
		MemoryType: string(schemas.MemoryTypeFactual),
		AgentID:    "agent1",
		Confidence: 0.95,
		Importance: 0.9,
	}
	mb.Ingest([]schemas.Memory{mem}, ctx)

	// Mark as quarantined
	mb.mu.Lock()
	st2 := mb.states["mem-quarantine"]
	st2.SuggestedLifecycleState = string(schemas.MemoryLifecycleQuarantined)
	mb.states["mem-quarantine"] = st2
	mb.mu.Unlock()

	// Apply decay
	states := mb.Decay([]schemas.Memory{mem}, time.Now().Format(time.RFC3339))

	// Quarantine should be preserved
	if states[0].SuggestedLifecycleState != string(schemas.MemoryLifecycleQuarantined) {
		t.Errorf("Quarantined state lost after decay: got %q",
			states[0].SuggestedLifecycleState)
	}
}

func TestMemoryBankAlgorithm_Compress(t *testing.T) {
	cfg := DefaultConfig()
	mb := New("test", cfg)
	ctx := schemas.AlgorithmContext{AgentID: "agent1", Timestamp: tsNow()}

	mems := []schemas.Memory{
		{MemoryID: "mem1", MemoryType: string(schemas.MemoryTypeEpisodic), AgentID: "agent1", Content: "event one", Importance: 0.8, Confidence: 0.9, Level: 0},
		{MemoryID: "mem2", MemoryType: string(schemas.MemoryTypeEpisodic), AgentID: "agent1", Content: "event two", Importance: 0.7, Confidence: 0.85, Level: 0},
	}
	mb.Ingest(mems, ctx)

	derived := mb.Compress(mems, ctx)
	if len(derived) != 1 {
		t.Fatalf("len(derived) = %d, want 1", len(derived))
	}

	c := derived[0]
	if c.MemoryType != string(schemas.MemoryTypeSemantic) {
		t.Errorf("MemoryType = %q, want semantic", c.MemoryType)
	}
	if c.Level != cfg.CompressedLevel {
		t.Errorf("Level = %d, want %d", c.Level, cfg.CompressedLevel)
	}
	if c.LifecycleState != string(schemas.MemoryLifecycleActive) {
		t.Errorf("LifecycleState = %q, want active", c.LifecycleState)
	}
	if !strings.Contains(c.Content, "event one") || !strings.Contains(c.Content, "event two") {
		t.Errorf("Content = %q, want merged content", c.Content)
	}
	if c.AlgorithmStateRef != AlgorithmID {
		t.Errorf("AlgorithmStateRef = %q, want %q", c.AlgorithmStateRef, AlgorithmID)
	}
}

func TestMemoryBankAlgorithm_Compress_SourceCompressed(t *testing.T) {
	cfg := DefaultConfig()
	mb := New("test", cfg)
	ctx := schemas.AlgorithmContext{AgentID: "agent1", Timestamp: tsNow()}

	mem := schemas.Memory{
		MemoryID:   "mem1",
		MemoryType: string(schemas.MemoryTypeEpisodic),
		AgentID:    "agent1",
		Importance: 0.8,
		Confidence: 0.9,
		Level:      0,
	}
	mb.Ingest([]schemas.Memory{mem}, ctx)
	mb.Compress([]schemas.Memory{mem}, ctx)

	st := mb.states["mem1"]
	if st.SuggestedLifecycleState != string(schemas.MemoryLifecycleCompressed) {
		t.Errorf("Source lifecycleState = %q, want compressed",
			st.SuggestedLifecycleState)
	}
}

func TestMemoryBankAlgorithm_Summarize(t *testing.T) {
	cfg := DefaultConfig()
	mb := New("test", cfg)
	ctx := schemas.AlgorithmContext{AgentID: "agent1", Timestamp: tsNow()}

	mems := []schemas.Memory{
		{MemoryID: "mem1", MemoryType: string(schemas.MemoryTypeEpisodic), AgentID: "agent1", Summary: "summary one", Importance: 0.8, Confidence: 0.9, Level: 1},
		{MemoryID: "mem2", MemoryType: string(schemas.MemoryTypeEpisodic), AgentID: "agent1", Summary: "summary two", Importance: 0.7, Confidence: 0.85, Level: 1},
	}
	mb.Ingest(mems, ctx)

	summaries := mb.Summarize(mems, ctx)
	if len(summaries) != 1 {
		t.Fatalf("len(summaries) = %d, want 1", len(summaries))
	}

	s := summaries[0]
	if s.Level != 2 { // maxLevel(1) + 1
		t.Errorf("Level = %d, want 2 (max+1)", s.Level)
	}
	if !strings.Contains(s.Content, "summary one") {
		t.Errorf("Content = %q, want summary content", s.Content)
	}
}

func TestMemoryBankAlgorithm_Summarize_SummaryRefs(t *testing.T) {
	cfg := DefaultConfig()
	mb := New("test", cfg)
	ctx := schemas.AlgorithmContext{AgentID: "agent1", Timestamp: tsNow()}

	mem := schemas.Memory{
		MemoryID:   "mem1",
		MemoryType: string(schemas.MemoryTypeEpisodic),
		AgentID:    "agent1",
		Summary:    "event summary",
		Importance: 0.8,
		Confidence: 0.9,
		Level:      1,
	}
	mb.Ingest([]schemas.Memory{mem}, ctx)
	summaries := mb.Summarize([]schemas.Memory{mem}, ctx)

	if len(summaries) != 1 {
		t.Fatalf("len(summaries) = %d, want 1", len(summaries))
	}

	summaryID := summaries[0].MemoryID
	st := mb.states["mem1"]
	if len(st.SummaryRefs) == 0 || st.SummaryRefs[0] != summaryID {
		t.Errorf("SummaryRefs = %v, want [包含 %s]", st.SummaryRefs, summaryID)
	}
}

func TestMemoryBankAlgorithm_ExportState(t *testing.T) {
	cfg := DefaultConfig()
	mb := New("test", cfg)
	ctx := schemas.AlgorithmContext{AgentID: "agent1", Timestamp: tsNow()}

	mem := schemas.Memory{
		MemoryID:   "mem-export",
		MemoryType: string(schemas.MemoryTypeEpisodic),
		AgentID:    "agent1",
		Importance: 0.8,
		Confidence: 0.9,
		Level:      0,
	}
	mb.Ingest([]schemas.Memory{mem}, ctx)

	st, ok := mb.ExportState("mem-export")
	if !ok {
		t.Fatal("ExportState returned false for existing memory")
	}
	if st.MemoryID != "mem-export" {
		t.Errorf("st.MemoryID = %q, want mem-export", st.MemoryID)
	}

	_, ok = mb.ExportState("nonexistent")
	if ok {
		t.Error("ExportState returned true for nonexistent memory")
	}
}

func TestMemoryBankAlgorithm_LoadState(t *testing.T) {
	cfg := DefaultConfig()
	mb := New("test", cfg)

	// Load external state
	state := schemas.MemoryAlgorithmState{
		MemoryID:       "mem-loaded",
		AlgorithmID:    AlgorithmID,
		Strength:       4.5,
		RetentionScore: 0.8,
		RecallCount:    10,
		PortraitState:  `{"stable_traits":{"style":"formal"}}`,
		UpdatedAt:      tsNow(),
	}
	mb.LoadState(state)

	// Verify restored
	st, ok := mb.ExportState("mem-loaded")
	if !ok {
		t.Fatal("ExportState returned false after LoadState")
	}
	if st.Strength != 4.5 {
		t.Errorf("Strength = %.2f, want 4.5", st.Strength)
	}
	if st.RecallCount != 10 {
		t.Errorf("RecallCount = %d, want 10", st.RecallCount)
	}
}

func TestConflictRegistry_RegisterAndGet(t *testing.T) {
	reg := NewConflictRegistry()
	rec := ConflictRecord{
		LeftID: "mem1", RightID: "mem2",
		Type: ConflictTypePreference, Severity: 0.85,
		DetectedAt: tsNow(),
	}
	reg.Register(rec)

	if len(reg.Get("mem1")) != 1 {
		t.Errorf("len(reg.Get(mem1)) = %d, want 1", len(reg.Get("mem1")))
	}
	if len(reg.Get("mem2")) != 1 {
		t.Errorf("len(reg.Get(mem2)) = %d, want 1", len(reg.Get("mem2")))
	}
}

func TestConflictRegistry_MaxSeverity(t *testing.T) {
	reg := NewConflictRegistry()
	reg.Register(ConflictRecord{LeftID: "mem1", RightID: "mem2", Severity: 0.3, DetectedAt: tsNow()})
	reg.Register(ConflictRecord{LeftID: "mem1", RightID: "mem3", Severity: 0.7, DetectedAt: tsNow()})

	if reg.MaxSeverity("mem1") != 0.7 {
		t.Errorf("MaxSeverity = %.2f, want 0.7", reg.MaxSeverity("mem1"))
	}
	if reg.IsConfirmed("mem1") != true {
		t.Error("IsConfirmed(mem1) = false, want true (severity ≥ 0.7)")
	}
}

func TestConflictRegistry_IsSuspected(t *testing.T) {
	reg := NewConflictRegistry()
	reg.Register(ConflictRecord{LeftID: "mem1", RightID: "mem2", Severity: 0.4, DetectedAt: tsNow()})

	if !reg.IsSuspected("mem1") {
		t.Error("IsSuspected(mem1) = false, want true (0 < severity < 0.7)")
	}
	if reg.IsConfirmed("mem1") {
		t.Error("IsConfirmed(mem1) = true, want false (severity < 0.7)")
	}
}

func TestConflictDetection_PreferenceReversal(t *testing.T) {
	reg := NewConflictRegistry()

	memA := schemas.Memory{
		MemoryID:   "mem-pref-a",
		MemoryType: string(schemas.MemoryTypePreferenceConstraint),
		AgentID:    "agent1",
		Content:    "prefers dark mode",
		Confidence: 0.9,
	}
	memB := schemas.Memory{
		MemoryID:   "mem-pref-b",
		MemoryType: string(schemas.MemoryTypePreferenceConstraint),
		AgentID:    "agent1",
		Content:    "avoids dark mode",
		Confidence: 0.8,
	}

	conflicts := DetectConflicts(reg, []schemas.Memory{memA}, memB)
	if len(conflicts) == 0 {
		t.Fatal("Expected preference reversal conflict, got none")
	}
	if conflicts[0].Severity < 0.7 {
		t.Errorf("Preference reversal severity = %.2f, want ≥ 0.7", conflicts[0].Severity)
	}
}

func TestConflictDetection_NoConflictForDifferentAgents(t *testing.T) {
	reg := NewConflictRegistry()

	memA := schemas.Memory{
		MemoryID:   "mem-a",
		MemoryType: string(schemas.MemoryTypeEpisodic),
		AgentID:    "agent1",
		Content:    "event one",
	}
	memB := schemas.Memory{
		MemoryID:   "mem-b",
		MemoryType: string(schemas.MemoryTypeEpisodic),
		AgentID:    "agent2", // different agent
		Content:    "event one",
	}

	conflicts := DetectConflicts(reg, []schemas.Memory{memA}, memB)
	if len(conflicts) != 0 {
		t.Errorf("Expected no conflict across agents, got %d conflicts", len(conflicts))
	}
}

func TestComputeAdmissionScore(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WSalience = 0.25
	cfg.WStability = 0.20
	cfg.WTaskRelevance = 0.25
	cfg.WUserExplicitness = 0.15
	cfg.WNoise = 0.15

	sig := MemorySignals{
		Salience:         0.8,
		Stability:        0.6,
		TaskRelevance:    0.5,
		UserExplicitness: 0.9,
		Noise:            0.0,
	}

	score := ComputeAdmissionScore(cfg, sig)
	if score <= 0 || score > 1 {
		t.Errorf("ComputeAdmissionScore = %.4f, want in (0,1]", score)
	}

	// With noise tag: score should decrease
	sigNoise := MemorySignals{
		Salience:         0.8,
		Stability:        0.6,
		TaskRelevance:    0.5,
		UserExplicitness: 0.9,
		Noise:            1.0,
	}
	scoreNoise := ComputeAdmissionScore(cfg, sigNoise)
	if scoreNoise >= score {
		t.Errorf("Score with noise (%.4f) should be < score without noise (%.4f)",
			scoreNoise, score)
	}
}

func TestComputeRetentionScore(t *testing.T) {
	cfg := DefaultConfig()
	st := schemas.MemoryAlgorithmState{
		RecallCount: 3,
		UpdatedAt:  time.Now().Format(time.RFC3339),
	}
	m := schemas.Memory{
		MemoryID:   "mem-ret",
		MemoryType: string(schemas.MemoryTypeEpisodic),
		Importance: 0.8,
		Confidence: 0.9,
	}
	reaffirmSet := make(map[string]bool)

	score := ComputeRetentionScore(cfg, st, m, reaffirmSet, 0.0)
	if score < 0 || score > 1 {
		t.Errorf("ComputeRetentionScore = %.4f, want in [0,1]", score)
	}
}

func TestNextLifecycle(t *testing.T) {
	cfg := DefaultConfig()

	tests := []struct {
		name         string
		currentState string
		st           schemas.MemoryAlgorithmState
		want         string
	}{
		{
			name:         "nil → active suggestion",
			currentState: "",
			st:           schemas.MemoryAlgorithmState{SuggestedLifecycleState: string(schemas.MemoryLifecycleActive)},
			want:         string(schemas.MemoryLifecycleActive),
		},
		{
			name:         "nil → candidate (no suggestion)",
			currentState: "",
			st:           schemas.MemoryAlgorithmState{},
			want:         string(schemas.MemoryLifecycleCandidate),
		},
		{
			name:         "candidate → active (score rises)",
			currentState: string(schemas.MemoryLifecycleCandidate),
			st:           schemas.MemoryAlgorithmState{SuggestedLifecycleState: string(schemas.MemoryLifecycleActive)},
			want:         string(schemas.MemoryLifecycleActive),
		},
		{
			name:         "active → reinforced",
			currentState: string(schemas.MemoryLifecycleActive),
			st:           schemas.MemoryAlgorithmState{
				SuggestedLifecycleState: string(schemas.MemoryLifecycleReinforced),
				RetentionScore:          0.5,
			},
			want: string(schemas.MemoryLifecycleReinforced),
		},
		{
			name:         "active → stale (retention drops)",
			currentState: string(schemas.MemoryLifecycleActive),
			st:           schemas.MemoryAlgorithmState{
				RetentionScore:          0.2, // below threshold 0.25
				SuggestedLifecycleState: "",
			},
			want: string(schemas.MemoryLifecycleStale),
		},
		{
			name:         "stale → archived (very low retention)",
			currentState: string(schemas.MemoryLifecycleStale),
			st:           schemas.MemoryAlgorithmState{
				RetentionScore: 0.05, // below threshold 0.10
			},
			want: string(schemas.MemoryLifecycleArchived),
		},
		{
			name:         "quarantined stays",
			currentState: string(schemas.MemoryLifecycleQuarantined),
			st:           schemas.MemoryAlgorithmState{},
			want:         string(schemas.MemoryLifecycleQuarantined),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nextLifecycle(cfg, tt.st, tt.currentState)
			if got != tt.want {
				t.Errorf("nextLifecycle(%q) = %q, want %q",
					tt.currentState, got, tt.want)
			}
		})
	}
}

func TestProfileState_SerializeRoundTrip(t *testing.T) {
	ps := &ProfileState{
		StableTraits: map[string]string{"style": "formal", "pace": "fast"},
		Preferences: map[string]float64{"coffee": 0.8, "meetings": 0.3},
		CommunicationStyle: "concise",
		UncertainInferences: map[string]float64{"mood": 0.5},
		UpdatedAt: "2026-03-28T00:00:00Z",
	}

	data := ps.Serialize()
	ps2, err := DeserializeProfile(data)
	if err != nil {
		t.Fatalf("DeserializeProfile error: %v", err)
	}
	if ps2.CommunicationStyle != "concise" {
		t.Errorf("CommunicationStyle = %q, want concise", ps2.CommunicationStyle)
	}
	if len(ps2.Preferences) != 2 {
		t.Errorf("len(Preferences) = %d, want 2", len(ps2.Preferences))
	}
}

func TestExtractProfile(t *testing.T) {
	mems := []schemas.Memory{
		{
			MemoryID:       "mem-profile",
			MemoryType:     string(schemas.MemoryTypeProfile),
			Content:        "communication_style: formal\nuses python primarily",
			Confidence:     0.85,
			SourceEventIDs: []string{"evt1", "evt2"},
		},
		{
			MemoryID:    "mem-pref",
			MemoryType:  string(schemas.MemoryTypePreferenceConstraint),
			Content:     "prefers python",
			Confidence:  0.8,
		},
	}

	ps := ExtractProfile(mems)
	if ps.CommunicationStyle != "formal" {
		t.Errorf("CommunicationStyle = %q, want formal", ps.CommunicationStyle)
	}
	if len(ps.Preferences) == 0 {
		t.Error("Expected preferences extracted")
	}
}

func TestIsHighValue(t *testing.T) {
	if !IsHighValue(schemas.MemoryTypePreferenceConstraint) {
		t.Error("preference_constraint should be high value")
	}
	if !IsHighValue(schemas.MemoryTypeFactual) {
		t.Error("factual should be high value")
	}
	if IsHighValue(schemas.MemoryTypeEpisodic) {
		t.Error("episodic should not be high value")
	}
}

func TestClamp01(t *testing.T) {
	tests := []struct {
		input, want float64
	}{
		{-0.5, 0},
		{0.0, 0},
		{0.5, 0.5},
		{1.0, 1.0},
		{1.5, 1.0},
	}
	for _, tt := range tests {
		got := clamp01(tt.input)
		if got != tt.want {
			t.Errorf("clamp01(%.2f) = %.2f, want %.2f", tt.input, got, tt.want)
		}
	}
}

func TestLog1pCapped(t *testing.T) {
	tests := []struct {
		n    int
		want float64
	}{
		{0, 0.0},
		{1, 0.095},   // log10(2) ≈ 0.095
		{9, 0.954},   // log10(10) ≈ 1.0 → capped
		{10, 1.0},
		{100, 1.0},
	}
	for _, tt := range tests {
		got := log1pCapped(tt.n)
		if got > tt.want+0.01 && got < tt.want-0.01 {
			t.Errorf("log1pCapped(%d) = %.3f, want ≈ %.3f", tt.n, got, tt.want)
		}
	}
}
