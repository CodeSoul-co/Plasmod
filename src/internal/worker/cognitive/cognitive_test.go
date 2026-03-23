package cognitive

import (
	"testing"

	"andb/src/internal/schemas"
	"andb/src/internal/storage"
)

// ─── AlgorithmDispatchWorker ──────────────────────────────────────────────────

// stubAlgorithm is a no-op MemoryManagementAlgorithm for testing.
type stubAlgorithm struct{}

func (s *stubAlgorithm) AlgorithmID() string { return "stub_algo_v1" }

func (s *stubAlgorithm) Ingest(memories []schemas.Memory, ctx schemas.AlgorithmContext) []schemas.MemoryAlgorithmState {
	out := make([]schemas.MemoryAlgorithmState, 0, len(memories))
	for _, m := range memories {
		out = append(out, schemas.MemoryAlgorithmState{
			MemoryID:    m.MemoryID,
			AlgorithmID: s.AlgorithmID(),
			Strength:    0.9,
			UpdatedAt:   ctx.Timestamp,
		})
	}
	return out
}

func (s *stubAlgorithm) Update(memories []schemas.Memory, signals map[string]float64) []schemas.MemoryAlgorithmState {
	return nil
}

func (s *stubAlgorithm) Recall(query string, candidates []schemas.Memory, ctx schemas.AlgorithmContext) []schemas.ScoredMemory {
	out := make([]schemas.ScoredMemory, 0, len(candidates))
	for _, m := range candidates {
		out = append(out, schemas.ScoredMemory{Memory: m, Score: 1.0, Signal: "stub"})
	}
	return out
}

func (s *stubAlgorithm) Compress(memories []schemas.Memory, ctx schemas.AlgorithmContext) []schemas.Memory {
	return nil
}

func (s *stubAlgorithm) Decay(memories []schemas.Memory, nowTS string) []schemas.MemoryAlgorithmState {
	out := make([]schemas.MemoryAlgorithmState, 0, len(memories))
	for _, m := range memories {
		out = append(out, schemas.MemoryAlgorithmState{
			MemoryID:                m.MemoryID,
			AlgorithmID:             s.AlgorithmID(),
			RetentionScore:          0.05,
			SuggestedLifecycleState: string(schemas.MemoryLifecycleDecayed), // algorithm decides
			UpdatedAt:               nowTS,
		})
	}
	return out
}

func (s *stubAlgorithm) Summarize(memories []schemas.Memory, ctx schemas.AlgorithmContext) []schemas.Memory {
	return nil
}

func (s *stubAlgorithm) ExportState(memoryID string) (schemas.MemoryAlgorithmState, bool) {
	return schemas.MemoryAlgorithmState{}, false
}

func (s *stubAlgorithm) LoadState(state schemas.MemoryAlgorithmState) {}

func TestAlgorithmDispatchWorker_Ingest_PersistsState(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	store.Objects().PutMemory(schemas.Memory{
		MemoryID: "mem_a1", AgentID: "agent1", Content: "test", IsActive: true,
	})

	w := CreateAlgorithmDispatchWorker("adw-1", &stubAlgorithm{},
		store.Objects(), store.AlgorithmStates(), store.Audits())

	out, err := w.Run(schemas.AlgorithmDispatchInput{
		Operation: "ingest",
		MemoryIDs: []string{"mem_a1"},
		AgentID:   "agent1",
	})
	if err != nil {
		t.Fatalf("Run ingest failed: %v", err)
	}
	result := out.(schemas.AlgorithmDispatchOutput)
	if result.UpdatedCount != 1 {
		t.Errorf("UpdatedCount: want 1, got %d", result.UpdatedCount)
	}

	st, ok := store.AlgorithmStates().GetAlgorithmState("mem_a1", "stub_algo_v1")
	if !ok {
		t.Fatal("expected MemoryAlgorithmState to be stored")
	}
	if st.Strength != 0.9 {
		t.Errorf("Strength: want 0.9, got %f", st.Strength)
	}

	// AlgorithmStateRef should be updated on the memory
	mem, _ := store.Objects().GetMemory("mem_a1")
	if mem.AlgorithmStateRef != "stub_algo_v1" {
		t.Errorf("AlgorithmStateRef: want stub_algo_v1, got %q", mem.AlgorithmStateRef)
	}

	// AuditRecord should have been emitted
	audits := store.Audits().GetAudits("mem_a1")
	if len(audits) == 0 {
		t.Error("expected AuditRecord after ingest")
	}
}

func TestAlgorithmDispatchWorker_Decay_SetsLifecycleDecayed(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	store.Objects().PutMemory(schemas.Memory{
		MemoryID: "mem_d1", IsActive: true,
	})

	w := CreateAlgorithmDispatchWorker("adw-2", &stubAlgorithm{},
		store.Objects(), store.AlgorithmStates(), store.Audits())

	_, err := w.Run(schemas.AlgorithmDispatchInput{
		Operation: "decay",
		MemoryIDs: []string{"mem_d1"},
		NowTS:     "2026-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("Run decay failed: %v", err)
	}

	mem, _ := store.Objects().GetMemory("mem_d1")
	if mem.LifecycleState != string(schemas.MemoryLifecycleDecayed) {
		t.Errorf("LifecycleState: want %q, got %q", schemas.MemoryLifecycleDecayed, mem.LifecycleState)
	}
}

func TestAlgorithmDispatchWorker_Recall_ReturnsScoredRefs(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	store.Objects().PutMemory(schemas.Memory{MemoryID: "mem_r1", Content: "foo"})
	store.Objects().PutMemory(schemas.Memory{MemoryID: "mem_r2", Content: "bar"})

	w := CreateAlgorithmDispatchWorker("adw-3", &stubAlgorithm{},
		store.Objects(), store.AlgorithmStates(), store.Audits())

	out, err := w.Run(schemas.AlgorithmDispatchInput{
		Operation: "recall",
		MemoryIDs: []string{"mem_r1", "mem_r2"},
		Query:     "foo",
	})
	if err != nil {
		t.Fatalf("Run recall failed: %v", err)
	}
	result := out.(schemas.AlgorithmDispatchOutput)
	if len(result.ScoredRefs) != 2 {
		t.Errorf("ScoredRefs: want 2, got %d", len(result.ScoredRefs))
	}
}

func TestAlgorithmDispatchWorker_UnknownOperation_ReturnsError(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	w := CreateAlgorithmDispatchWorker("adw-4", &stubAlgorithm{},
		store.Objects(), store.AlgorithmStates(), store.Audits())

	_, err := w.Run(schemas.AlgorithmDispatchInput{
		Operation: "unknown_op",
		MemoryIDs: []string{},
	})
	if err == nil {
		t.Error("expected error for unknown operation")
	}
}
