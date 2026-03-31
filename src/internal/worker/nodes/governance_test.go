package nodes_test

import (
	"testing"
	"time"

	"andb/src/internal/eventbackbone"
	"andb/src/internal/schemas"
	"andb/src/internal/storage"
	baseline "andb/src/internal/worker/cognitive/baseline"
	"andb/src/internal/worker/coordination"
	"andb/src/internal/worker/indexing"
	"andb/src/internal/worker/nodes"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func newTestStorage() storage.RuntimeStorage {
	return storage.NewMemoryRuntimeStorage()
}

func seedMemory(store storage.ObjectStore, id, agentID, sessionID string, version int64, active bool) {
	store.PutMemory(schemas.Memory{
		MemoryID:   id,
		MemoryType: "episodic",
		AgentID:    agentID,
		SessionID:  sessionID,
		Content:    "test content " + id,
		Level:      0,
		IsActive:   active,
		Version:    version,
		ValidFrom:  time.Now().UTC().Format(time.RFC3339),
		Confidence: 1.0,
		Importance: 0.8,
	})
}

// ── ReflectionPolicyWorker ────────────────────────────────────────────────────

func TestReflectionPolicyWorker_QuarantineDeactivates(t *testing.T) {
	st := newTestStorage()
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	plog := eventbackbone.NewPolicyDecisionLog(clock, bus)

	seedMemory(st.Objects(), "mem_q1", "agent1", "sess1", 1, true)
	st.Policies().AppendPolicy(schemas.PolicyRecord{
		PolicyID:       "pol_quarantine",
		ObjectID:       "mem_q1",
		ObjectType:     "memory",
		QuarantineFlag: true,
		PolicyReason:   "test quarantine",
	})

	w := baseline.CreateInMemoryReflectionPolicyWorker("reflect-test", st.Objects(), st.Policies(), plog)
	if err := w.Reflect("mem_q1", "memory"); err != nil {
		t.Fatalf("Reflect: unexpected error: %v", err)
	}

	mem, ok := st.Objects().GetMemory("mem_q1")
	if !ok {
		t.Fatal("memory not found after Reflect")
	}
	if mem.IsActive {
		t.Error("quarantined memory should be inactive")
	}
}

func TestReflectionPolicyWorker_TTLExpiry(t *testing.T) {
	st := newTestStorage()
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	plog := eventbackbone.NewPolicyDecisionLog(clock, bus)

	// Seed a memory with a ValidFrom far in the past.
	st.Objects().PutMemory(schemas.Memory{
		MemoryID:   "mem_ttl",
		AgentID:    "agent1",
		SessionID:  "sess1",
		IsActive:   true,
		Version:    1,
		Confidence: 1.0,
		ValidFrom:  time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339),
	})
	// TTL = 60 seconds, already expired (memory is 2h old).
	st.Policies().AppendPolicy(schemas.PolicyRecord{
		PolicyID: "pol_ttl",
		ObjectID: "mem_ttl",
		TTL:      60,
	})

	w := baseline.CreateInMemoryReflectionPolicyWorker("reflect-ttl", st.Objects(), st.Policies(), plog)
	if err := w.Reflect("mem_ttl", "memory"); err != nil {
		t.Fatalf("Reflect: unexpected error: %v", err)
	}

	mem, _ := st.Objects().GetMemory("mem_ttl")
	if mem.IsActive {
		t.Error("memory past TTL should be inactive")
	}
}

func TestReflectionPolicyWorker_ConfidenceOverride(t *testing.T) {
	st := newTestStorage()
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	plog := eventbackbone.NewPolicyDecisionLog(clock, bus)

	seedMemory(st.Objects(), "mem_conf", "a", "s", 1, true)
	st.Policies().AppendPolicy(schemas.PolicyRecord{
		PolicyID:           "pol_conf",
		ObjectID:           "mem_conf",
		ConfidenceOverride: 0.4,
	})

	w := baseline.CreateInMemoryReflectionPolicyWorker("reflect-conf", st.Objects(), st.Policies(), plog)
	_ = w.Reflect("mem_conf", "memory")

	mem, _ := st.Objects().GetMemory("mem_conf")
	if mem.Confidence != 0.4 {
		t.Errorf("confidence override: want 0.4, got %.2f", mem.Confidence)
	}
}

func TestReflectionPolicyWorker_SalienceDecay(t *testing.T) {
	st := newTestStorage()
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	plog := eventbackbone.NewPolicyDecisionLog(clock, bus)

	seedMemory(st.Objects(), "mem_sal", "a", "s", 1, true)
	st.Policies().AppendPolicy(schemas.PolicyRecord{
		PolicyID:       "pol_sal",
		ObjectID:       "mem_sal",
		SalienceWeight: 0.5,
		DecayFn:        "linear",
	})

	w := baseline.CreateInMemoryReflectionPolicyWorker("reflect-sal", st.Objects(), st.Policies(), plog)
	_ = w.Reflect("mem_sal", "memory")

	mem, _ := st.Objects().GetMemory("mem_sal")
	if mem.Importance >= 0.8 {
		t.Errorf("importance should decay; want < 0.8, got %.4f", mem.Importance)
	}
}

func TestReflectionPolicyWorker_NoPolicies_NoOp(t *testing.T) {
	st := newTestStorage()
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	plog := eventbackbone.NewPolicyDecisionLog(clock, bus)

	seedMemory(st.Objects(), "mem_nop", "a", "s", 1, true)

	w := baseline.CreateInMemoryReflectionPolicyWorker("reflect-nop", st.Objects(), st.Policies(), plog)
	if err := w.Reflect("mem_nop", "memory"); err != nil {
		t.Fatalf("expected no-op, got error: %v", err)
	}
	mem, _ := st.Objects().GetMemory("mem_nop")
	if !mem.IsActive {
		t.Error("memory without policies should stay active")
	}
}

func TestReflectionPolicyWorker_UnknownObject_NoError(t *testing.T) {
	st := newTestStorage()
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	plog := eventbackbone.NewPolicyDecisionLog(clock, bus)

	w := baseline.CreateInMemoryReflectionPolicyWorker("reflect-miss", st.Objects(), st.Policies(), plog)
	if err := w.Reflect("nonexistent", "memory"); err != nil {
		t.Fatalf("missing object should be a no-op, got: %v", err)
	}
}

// ── ConflictMergeWorker ───────────────────────────────────────────────────────

func TestConflictMergeWorker_LastWriterWins(t *testing.T) {
	st := newTestStorage()
	seedMemory(st.Objects(), "mem_left", "agent1", "sess1", 2, true)
	seedMemory(st.Objects(), "mem_right", "agent1", "sess1", 5, true)

	w := coordination.CreateInMemoryConflictMergeWorker("conflict-test", st.Objects(), st.Edges())
	if err := w.Merge("mem_left", "mem_right", "memory"); err != nil {
		t.Fatalf("Merge error: %v", err)
	}

	left, _ := st.Objects().GetMemory("mem_left")
	right, _ := st.Objects().GetMemory("mem_right")

	// right has higher version → should survive
	if !right.IsActive {
		t.Error("higher-version memory (right) should remain active")
	}
	if left.IsActive {
		t.Error("lower-version memory (left) should be deactivated")
	}
}

func TestConflictMergeWorker_ConflictEdgeCreated(t *testing.T) {
	st := newTestStorage()
	seedMemory(st.Objects(), "mem_w", "a", "s", 10, true)
	seedMemory(st.Objects(), "mem_l", "a", "s", 3, true)

	w := coordination.CreateInMemoryConflictMergeWorker("conflict-edge", st.Objects(), st.Edges())
	_ = w.Merge("mem_w", "mem_l", "memory")

	edges := st.Edges().ListEdges()
	found := false
	for _, e := range edges {
		if e.EdgeType == "conflict_resolved" {
			found = true
			break
		}
	}
	if !found {
		t.Error("conflict_resolved edge should be created after merge")
	}
}

func TestConflictMergeWorker_DifferentAgents_NoOp(t *testing.T) {
	st := newTestStorage()
	seedMemory(st.Objects(), "mem_a", "agent1", "sess1", 1, true)
	seedMemory(st.Objects(), "mem_b", "agent2", "sess1", 5, true)

	w := coordination.CreateInMemoryConflictMergeWorker("conflict-diff", st.Objects(), st.Edges())
	_ = w.Merge("mem_a", "mem_b", "memory")

	a, _ := st.Objects().GetMemory("mem_a")
	b, _ := st.Objects().GetMemory("mem_b")
	if !a.IsActive || !b.IsActive {
		t.Error("memories from different agents should not be merged")
	}
}

func TestConflictMergeWorker_NonMemoryType_NoOp(t *testing.T) {
	st := newTestStorage()
	w := coordination.CreateInMemoryConflictMergeWorker("conflict-type", st.Objects(), st.Edges())
	if err := w.Merge("x", "y", "artifact"); err != nil {
		t.Fatalf("non-memory type should be a no-op, got: %v", err)
	}
}

func TestConflictMergeWorker_SameID_NoOp(t *testing.T) {
	st := newTestStorage()
	seedMemory(st.Objects(), "mem_same", "a", "s", 1, true)
	w := coordination.CreateInMemoryConflictMergeWorker("conflict-same", st.Objects(), st.Edges())
	if err := w.Merge("mem_same", "mem_same", "memory"); err != nil {
		t.Fatalf("same-ID merge should be a no-op, got: %v", err)
	}
}

// ── Manager dispatch integration ─────────────────────────────────────────────

func TestManager_DispatchReflectionPolicy(t *testing.T) {
	st := newTestStorage()
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	plog := eventbackbone.NewPolicyDecisionLog(clock, bus)

	seedMemory(st.Objects(), "mem_r", "a", "s", 1, true)
	st.Policies().AppendPolicy(schemas.PolicyRecord{
		PolicyID:       "p1",
		ObjectID:       "mem_r",
		QuarantineFlag: true,
	})

	m := nodes.CreateManager()
	m.RegisterReflectionPolicy(baseline.CreateInMemoryReflectionPolicyWorker("rp-1", st.Objects(), st.Policies(), plog))
	m.DispatchReflectionPolicy("mem_r", "memory")

	mem, _ := st.Objects().GetMemory("mem_r")
	if mem.IsActive {
		t.Error("DispatchReflectionPolicy: quarantined memory should be inactive")
	}
}

func TestManager_DispatchConflictMerge(t *testing.T) {
	st := newTestStorage()
	seedMemory(st.Objects(), "mem_new", "a", "s", 7, true)
	seedMemory(st.Objects(), "mem_old", "a", "s", 2, true)

	m := nodes.CreateManager()
	m.RegisterConflictMerge(coordination.CreateInMemoryConflictMergeWorker("cm-1", st.Objects(), st.Edges()))
	m.DispatchConflictMerge("mem_new", "mem_old", "memory")

	old, _ := st.Objects().GetMemory("mem_old")
	if old.IsActive {
		t.Error("DispatchConflictMerge: lower-version memory should be inactive")
	}
}

func TestManager_DispatchMemoryConsolidation(t *testing.T) {
	st := newTestStorage()
	seedMemory(st.Objects(), "mem_c1", "agentC", "sessC", 1, true)
	seedMemory(st.Objects(), "mem_c2", "agentC", "sessC", 2, true)

	m := nodes.CreateManager()
	m.RegisterMemoryConsolidation(baseline.CreateInMemoryMemoryConsolidationWorker("consol-1", st.Objects(), nil))
	m.DispatchMemoryConsolidation("agentC", "sessC")

	// Consolidation should produce a level-1 summary memory.
	mems := st.Objects().ListMemories("agentC", "sessC")
	hasLevel1 := false
	for _, mem := range mems {
		if mem.Level == 1 {
			hasLevel1 = true
		}
	}
	if !hasLevel1 {
		t.Error("DispatchMemoryConsolidation: expected a level-1 summary memory")
	}
}

func TestManager_DispatchGraphRelation(t *testing.T) {
	st := newTestStorage()
	m := nodes.CreateManager()
	m.RegisterGraphRelation(indexing.CreateInMemoryGraphRelationWorker("gr-1", st.Edges()))

	m.DispatchGraphRelation("mem_src", "memory", "sess_dst", "session", "belongs_to_session", 1.0)

	edges := st.Edges().ListEdges()
	if len(edges) == 0 {
		t.Error("DispatchGraphRelation: expected at least one edge")
	}
}
