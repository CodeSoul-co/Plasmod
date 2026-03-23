package cognitive

import (
	"testing"
	"time"

	"andb/src/internal/eventbackbone"
	"andb/src/internal/schemas"
	"andb/src/internal/storage"
)

// ─── MemoryExtractionWorker ──────────────────────────────────────────────────

func TestMemoryExtractionWorker_Extract_StoresMemory(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	w := CreateInMemoryMemoryExtractionWorker("test-extract", store.Objects())

	err := w.Extract("evt1", "agent1", "sess1", "hello world")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	mem, ok := store.Objects().GetMemory(schemas.IDPrefixMemory + "evt1")
	if !ok {
		t.Fatal("expected memory to be stored")
	}
	if mem.AgentID != "agent1" || mem.SessionID != "sess1" {
		t.Errorf("wrong agent/session: got %q/%q", mem.AgentID, mem.SessionID)
	}
	if mem.Content != "hello world" {
		t.Errorf("wrong content: %q", mem.Content)
	}
	if mem.Level != 0 {
		t.Errorf("expected level 0, got %d", mem.Level)
	}
	if !mem.IsActive {
		t.Error("expected IsActive=true")
	}
}

func TestMemoryExtractionWorker_Run_TypedDispatch(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	w := CreateInMemoryMemoryExtractionWorker("test-extract", store.Objects())

	out, err := w.Run(schemas.MemoryExtractionInput{
		EventID:   "evt2",
		AgentID:   "agent2",
		SessionID: "sess2",
		Content:   "typed input",
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	result, ok := out.(schemas.MemoryExtractionOutput)
	if !ok {
		t.Fatalf("unexpected output type %T", out)
	}
	if result.MemoryID == "" {
		t.Error("expected non-empty MemoryID")
	}
}

func TestMemoryExtractionWorker_Run_WrongInputType(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	w := CreateInMemoryMemoryExtractionWorker("test", store.Objects())
	_, err := w.Run(schemas.IngestInput{})
	if err == nil {
		t.Error("expected error for wrong input type")
	}
}

// ─── MemoryConsolidationWorker ───────────────────────────────────────────────

func TestMemoryConsolidationWorker_Consolidate_NoOp(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	w := CreateInMemoryMemoryConsolidationWorker("test-consol", store.Objects())

	// No memories stored — should succeed without error.
	if err := w.Consolidate("agent1", "sess1"); err != nil {
		t.Fatalf("Consolidate failed: %v", err)
	}
}

func TestMemoryConsolidationWorker_Consolidate_ProducesLevel1(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	w := CreateInMemoryMemoryConsolidationWorker("test-consol", store.Objects())

	// Seed three level-0 memories for the same agent+session.
	for i := range 3 {
		store.Objects().PutMemory(schemas.Memory{
			MemoryID:  schemas.IDPrefixMemory + "e" + string(rune('0'+i)),
			AgentID:   "agent1",
			SessionID: "sess1",
			Level:     0,
			IsActive:  true,
			Content:   "content",
		})
	}

	if err := w.Consolidate("agent1", "sess1"); err != nil {
		t.Fatalf("Consolidate failed: %v", err)
	}
}

// ─── SummarizationWorker ─────────────────────────────────────────────────────

func TestSummarizationWorker_Summarize_NoOp(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	w := CreateInMemorySummarizationWorker("test-sum", store.Objects())

	if err := w.Summarize("agent1", "sess1", 1); err != nil {
		t.Fatalf("Summarize failed: %v", err)
	}
}

// ─── ReflectionPolicyWorker ──────────────────────────────────────────────────

func TestReflectionPolicyWorker_Reflect_NoPolicy(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	policyLog := eventbackbone.NewPolicyDecisionLog(clock, bus)
	w := CreateInMemoryReflectionPolicyWorker("test-reflect", store.Objects(), store.Policies(), policyLog)

	// Memory exists but no policy — should be a no-op.
	store.Objects().PutMemory(schemas.Memory{
		MemoryID: "mem_evt1",
		IsActive: true,
	})
	if err := w.Reflect("mem_evt1", "memory"); err != nil {
		t.Fatalf("Reflect failed: %v", err)
	}
	mem, _ := store.Objects().GetMemory("mem_evt1")
	if !mem.IsActive {
		t.Error("expected memory to remain active with no policy")
	}
}

func TestReflectionPolicyWorker_Reflect_TTLExpiry(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	policyLog := eventbackbone.NewPolicyDecisionLog(clock, bus)
	w := CreateInMemoryReflectionPolicyWorker("test-reflect", store.Objects(), store.Policies(), policyLog)

	// Memory created 10 seconds ago, TTL = 1 second.
	past := time.Now().Add(-10 * time.Second).UTC().Format(time.RFC3339)
	store.Objects().PutMemory(schemas.Memory{
		MemoryID:  "mem_ttl",
		IsActive:  true,
		ValidFrom: past,
	})
	store.Policies().AppendPolicy(schemas.PolicyRecord{
		PolicyID: "pol1",
		ObjectID: "mem_ttl",
		TTL:      1, // 1 second
	})

	if err := w.Reflect("mem_ttl", "memory"); err != nil {
		t.Fatalf("Reflect failed: %v", err)
	}
	mem, _ := store.Objects().GetMemory("mem_ttl")
	if mem.IsActive {
		t.Error("expected memory to be deactivated by TTL expiry")
	}
}

func TestReflectionPolicyWorker_Reflect_NonMemoryType(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	policyLog := eventbackbone.NewPolicyDecisionLog(clock, bus)
	w := CreateInMemoryReflectionPolicyWorker("test-reflect", store.Objects(), store.Policies(), policyLog)

	// Non-memory objectType should be a no-op.
	if err := w.Reflect("obj1", "artifact"); err != nil {
		t.Fatalf("Reflect failed: %v", err)
	}
}
