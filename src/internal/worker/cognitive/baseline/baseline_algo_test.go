package baseline

import (
	"fmt"
	"testing"
	"time"

	"plasmod/src/internal/schemas"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

func makeMemory(id string, confidence, importance float64) schemas.Memory {
	return schemas.Memory{
		MemoryID:       id,
		AgentID:        "agent1",
		SessionID:      "sess1",
		Content:        fmt.Sprintf("content of %s", id),
		Summary:        fmt.Sprintf("summary of %s", id),
		Confidence:     confidence,
		Importance:     importance,
		IsActive:       true,
		LifecycleState: string(schemas.MemoryLifecycleActive),
		ValidFrom:      time.Now().UTC().Format(time.RFC3339),
		Level:          0,
		Version:        1,
	}
}

func ctx() schemas.AlgorithmContext {
	return schemas.AlgorithmContext{
		AgentID:   "agent1",
		SessionID: "sess1",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
}

// ─── AlgorithmID ──────────────────────────────────────────────────────────────

func TestBaselineAlgorithm_AlgorithmID(t *testing.T) {
	a := NewDefault()
	if a.AlgorithmID() != AlgorithmID {
		t.Errorf("want %q, got %q", AlgorithmID, a.AlgorithmID())
	}
}

// ─── Ingest ───────────────────────────────────────────────────────────────────

func TestBaseline_Ingest_InitialisesState(t *testing.T) {
	a := NewDefault()
	mems := []schemas.Memory{makeMemory("m1", 0.9, 0.8), makeMemory("m2", 0.7, 0.6)}
	states := a.Ingest(mems, ctx())
	if len(states) != 2 {
		t.Fatalf("want 2 states, got %d", len(states))
	}
	for _, st := range states {
		if st.Strength != a.cfg.InitialStrength {
			t.Errorf("Strength: want %f, got %f", a.cfg.InitialStrength, st.Strength)
		}
		if st.AlgorithmID != AlgorithmID {
			t.Errorf("AlgorithmID: want %q, got %q", AlgorithmID, st.AlgorithmID)
		}
	}
}

func TestBaseline_Ingest_Idempotent(t *testing.T) {
	a := NewDefault()
	m := makeMemory("m1", 0.9, 0.8)
	a.Ingest([]schemas.Memory{m}, ctx())
	st, _ := a.ExportState("m1")
	st.Strength = 3.0
	a.LoadState(st)
	a.Ingest([]schemas.Memory{m}, ctx())
	st2, ok := a.ExportState("m1")
	if !ok {
		t.Fatal("state not found after re-ingest")
	}
	if st2.Strength != 3.0 {
		t.Errorf("Ingest should be idempotent; Strength changed to %f", st2.Strength)
	}
}

// ─── Update ───────────────────────────────────────────────────────────────────

func TestBaseline_Update_AppliesSignal(t *testing.T) {
	a := NewDefault()
	m := makeMemory("m1", 0.9, 0.8)
	a.Ingest([]schemas.Memory{m}, ctx())
	states := a.Update([]schemas.Memory{m}, map[string]float64{"m1": 2.0})
	want := a.cfg.InitialStrength * 2.0
	if states[0].Strength != want {
		t.Errorf("Strength after Update(×2): want %f, got %f", want, states[0].Strength)
	}
}

func TestBaseline_Update_CapsAtMaxStrength(t *testing.T) {
	a := NewDefault()
	m := makeMemory("m1", 0.9, 0.8)
	a.Ingest([]schemas.Memory{m}, ctx())
	a.Update([]schemas.Memory{m}, map[string]float64{"m1": 100.0})
	st, _ := a.ExportState("m1")
	if st.Strength > a.cfg.MaxStrength {
		t.Errorf("Strength %f exceeds MaxStrength %f", st.Strength, a.cfg.MaxStrength)
	}
}

func TestBaseline_Update_SuggestsDecayedWhenBelowThreshold(t *testing.T) {
	cfg := DefaultConfig()
	cfg.InitialStrength = 0.05
	a := New(cfg)
	m := makeMemory("m1", 0.9, 0.8)
	a.Ingest([]schemas.Memory{m}, ctx())
	states := a.Update([]schemas.Memory{m}, nil)
	if states[0].SuggestedLifecycleState != string(schemas.MemoryLifecycleDecayed) {
		t.Errorf("want SuggestedLifecycleState=decayed, got %q", states[0].SuggestedLifecycleState)
	}
}

// ─── Recall ───────────────────────────────────────────────────────────────────

func TestBaseline_Recall_ReturnsSortedByScore(t *testing.T) {
	a := NewDefault()
	low := makeMemory("low", 0.5, 0.3)
	high := makeMemory("high", 0.9, 0.9)
	a.Ingest([]schemas.Memory{low, high}, ctx())
	scored := a.Recall("query", []schemas.Memory{low, high}, ctx())
	if len(scored) != 2 {
		t.Fatalf("want 2 scored, got %d", len(scored))
	}
	if scored[0].Memory.MemoryID != "high" {
		t.Errorf("expected high-score memory first, got %q", scored[0].Memory.MemoryID)
	}
	if scored[0].Score < scored[1].Score {
		t.Error("results not sorted descending")
	}
}

func TestBaseline_Recall_BoostsStrength(t *testing.T) {
	a := NewDefault()
	m := makeMemory("m1", 0.9, 0.8)
	a.Ingest([]schemas.Memory{m}, ctx())
	before, _ := a.ExportState("m1")
	a.Recall("q", []schemas.Memory{m}, ctx())
	after, _ := a.ExportState("m1")
	if after.Strength <= before.Strength {
		t.Errorf("Strength should increase after recall: before=%f after=%f", before.Strength, after.Strength)
	}
	if after.RecallCount != before.RecallCount+1 {
		t.Errorf("RecallCount: want %d, got %d", before.RecallCount+1, after.RecallCount)
	}
}

// ─── Compress ─────────────────────────────────────────────────────────────────

func TestBaseline_Compress_ProducesOneDerivedMemory(t *testing.T) {
	a := NewDefault()
	mems := []schemas.Memory{makeMemory("m1", 0.8, 0.7), makeMemory("m2", 0.7, 0.5)}
	derived := a.Compress(mems, ctx())
	if len(derived) != 1 {
		t.Fatalf("want 1 derived memory, got %d", len(derived))
	}
	d := derived[0]
	if d.Level != a.cfg.CompressedLevel {
		t.Errorf("Level: want %d, got %d", a.cfg.CompressedLevel, d.Level)
	}
	if d.LifecycleState != string(schemas.MemoryLifecycleActive) {
		t.Errorf("LifecycleState: want active, got %q", d.LifecycleState)
	}
}

func TestBaseline_Compress_EmptyInput_ReturnsNil(t *testing.T) {
	a := NewDefault()
	if result := a.Compress(nil, ctx()); result != nil {
		t.Errorf("expected nil for empty input, got %v", result)
	}
}

// ─── Decay ────────────────────────────────────────────────────────────────────

func TestBaseline_Decay_ReducesStrength(t *testing.T) {
	a := NewDefault()
	m := makeMemory("m1", 0.9, 0.8)
	past := time.Now().UTC().Add(-10 * 24 * time.Hour).Format(time.RFC3339)
	a.LoadState(schemas.MemoryAlgorithmState{
		MemoryID: "m1", AlgorithmID: AlgorithmID, Strength: 1.0, RetentionScore: 1.0, UpdatedAt: past,
	})
	states := a.Decay([]schemas.Memory{m}, time.Now().UTC().Format(time.RFC3339))
	if states[0].Strength >= 1.0 {
		t.Errorf("Strength should decrease after decay: got %f", states[0].Strength)
	}
}

func TestBaseline_Decay_SuggestsDecayedLifecycle(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DecayRate = 10.0
	a := New(cfg)
	m := makeMemory("m1", 0.9, 0.8)
	past := time.Now().UTC().Add(-30 * 24 * time.Hour).Format(time.RFC3339)
	a.LoadState(schemas.MemoryAlgorithmState{
		MemoryID: "m1", AlgorithmID: AlgorithmID, Strength: 1.0, UpdatedAt: past,
	})
	states := a.Decay([]schemas.Memory{m}, time.Now().UTC().Format(time.RFC3339))
	if states[0].SuggestedLifecycleState != string(schemas.MemoryLifecycleDecayed) {
		t.Errorf("want SuggestedLifecycleState=decayed, got %q", states[0].SuggestedLifecycleState)
	}
}

func TestBaseline_Decay_HealthyMemory_NoLifecycleSuggestion(t *testing.T) {
	a := NewDefault()
	m := makeMemory("m1", 0.9, 0.8)
	a.Ingest([]schemas.Memory{m}, ctx())
	states := a.Decay([]schemas.Memory{m}, time.Now().UTC().Format(time.RFC3339))
	if states[0].SuggestedLifecycleState != "" {
		t.Errorf("healthy memory should not suggest lifecycle change, got %q", states[0].SuggestedLifecycleState)
	}
}

// ─── Summarize ────────────────────────────────────────────────────────────────

func TestBaseline_Summarize_ProducesHigherLevel(t *testing.T) {
	a := NewDefault()
	mems := []schemas.Memory{makeMemory("m1", 0.8, 0.7), makeMemory("m2", 0.7, 0.5)}
	summaries := a.Summarize(mems, ctx())
	if len(summaries) != 1 {
		t.Fatalf("want 1 summary, got %d", len(summaries))
	}
	s := summaries[0]
	if s.Level != 1 {
		t.Errorf("Level: want 1 (maxLevel 0 + 1), got %d", s.Level)
	}
	if s.MemoryType != string(schemas.MemoryTypeSemantic) {
		t.Errorf("MemoryType: want semantic, got %q", s.MemoryType)
	}
}

func TestBaseline_Summarize_UsesSummaryField(t *testing.T) {
	a := NewDefault()
	m := makeMemory("m1", 0.9, 0.8)
	m.Summary = "explicit summary text"
	summaries := a.Summarize([]schemas.Memory{m}, ctx())
	if !containsStr(summaries[0].Content, "explicit summary text") {
		t.Errorf("Summarize should prefer Summary field: got %q", summaries[0].Content)
	}
}

func TestBaseline_Summarize_EmptyInput_ReturnsNil(t *testing.T) {
	a := NewDefault()
	if result := a.Summarize(nil, ctx()); result != nil {
		t.Errorf("expected nil for empty input, got %v", result)
	}
}

// ─── ExportState / LoadState ─────────────────────────────────────────────────

func TestBaseline_ExportState_ReturnsFalseForUnknown(t *testing.T) {
	a := NewDefault()
	_, ok := a.ExportState("nonexistent")
	if ok {
		t.Error("expected false for unknown memory ID")
	}
}

func TestBaseline_LoadState_RestoresState(t *testing.T) {
	a := NewDefault()
	st := schemas.MemoryAlgorithmState{
		MemoryID: "m_restored", AlgorithmID: AlgorithmID, Strength: 3.7,
		RetentionScore: 0.8, RecallCount: 5, UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	a.LoadState(st)
	got, ok := a.ExportState("m_restored")
	if !ok {
		t.Fatal("state not found after LoadState")
	}
	if got.Strength != 3.7 {
		t.Errorf("Strength: want 3.7, got %f", got.Strength)
	}
	if got.RecallCount != 5 {
		t.Errorf("RecallCount: want 5, got %d", got.RecallCount)
	}
}

// ─── helper ──────────────────────────────────────────────────────────────────

func containsStr(s, sub string) bool {
	if len(sub) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
