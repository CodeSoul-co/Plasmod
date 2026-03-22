package coordination

import (
	"strings"
	"testing"

	"andb/src/internal/eventbackbone"
	"andb/src/internal/schemas"
	"andb/src/internal/storage"
)

// ─── ConflictMergeWorker ─────────────────────────────────────────────────────

func TestConflictMergeWorker_Merge_LWW_RightWins(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	w := CreateInMemoryConflictMergeWorker("test-cm", store.Objects(), store.Edges())

	store.Objects().PutMemory(schemas.Memory{
		MemoryID:  "mem_left",
		AgentID:   "a1",
		SessionID: "s1",
		Version:   1,
		IsActive:  true,
	})
	store.Objects().PutMemory(schemas.Memory{
		MemoryID:  "mem_right",
		AgentID:   "a1",
		SessionID: "s1",
		Version:   2,
		IsActive:  true,
	})

	if err := w.Merge("mem_left", "mem_right", "memory"); err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	left, _ := store.Objects().GetMemory("mem_left")
	right, _ := store.Objects().GetMemory("mem_right")
	if left.IsActive {
		t.Error("lower-version memory should be deactivated")
	}
	if !right.IsActive {
		t.Error("higher-version memory should remain active")
	}
}

func TestConflictMergeWorker_Merge_LWW_LeftWins(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	w := CreateInMemoryConflictMergeWorker("test-cm", store.Objects(), store.Edges())

	store.Objects().PutMemory(schemas.Memory{
		MemoryID:  "mem_left2",
		AgentID:   "a1",
		SessionID: "s1",
		Version:   5,
		IsActive:  true,
	})
	store.Objects().PutMemory(schemas.Memory{
		MemoryID:  "mem_right2",
		AgentID:   "a1",
		SessionID: "s1",
		Version:   3,
		IsActive:  true,
	})

	if err := w.Merge("mem_left2", "mem_right2", "memory"); err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	left, _ := store.Objects().GetMemory("mem_left2")
	right, _ := store.Objects().GetMemory("mem_right2")
	if !left.IsActive {
		t.Error("higher-version left memory should remain active")
	}
	if right.IsActive {
		t.Error("lower-version right memory should be deactivated")
	}
}

func TestConflictMergeWorker_Merge_CrossAgent_NoOp(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	w := CreateInMemoryConflictMergeWorker("test-cm", store.Objects(), store.Edges())

	store.Objects().PutMemory(schemas.Memory{
		MemoryID:  "mem_a1",
		AgentID:   "agentA",
		SessionID: "s1",
		IsActive:  true,
	})
	store.Objects().PutMemory(schemas.Memory{
		MemoryID:  "mem_a2",
		AgentID:   "agentB", // different agent → merge is a no-op
		SessionID: "s1",
		IsActive:  true,
	})

	if err := w.Merge("mem_a1", "mem_a2", "memory"); err != nil {
		t.Fatalf("Merge failed: %v", err)
	}
	m1, _ := store.Objects().GetMemory("mem_a1")
	m2, _ := store.Objects().GetMemory("mem_a2")
	if !m1.IsActive || !m2.IsActive {
		t.Error("cross-agent merge should be a no-op, both memories should remain active")
	}
}

func TestConflictMergeWorker_Merge_CreatesConflictEdge(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	w := CreateInMemoryConflictMergeWorker("test-cm", store.Objects(), store.Edges())

	store.Objects().PutMemory(schemas.Memory{MemoryID: "ml", AgentID: "a1", SessionID: "s1", Version: 1, IsActive: true})
	store.Objects().PutMemory(schemas.Memory{MemoryID: "mr", AgentID: "a1", SessionID: "s1", Version: 2, IsActive: true})

	_ = w.Merge("ml", "mr", "memory")

	edges := store.Edges().BulkEdges([]string{"mr"})
	found := false
	for _, e := range edges {
		if e.EdgeType == string(schemas.EdgeTypeConflictResolved) {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected a conflict_resolved edge after merge")
	}
}

func TestConflictMergeWorker_Run_ReturnsWinner(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	w := CreateInMemoryConflictMergeWorker("test-cm", store.Objects(), store.Edges())

	store.Objects().PutMemory(schemas.Memory{MemoryID: "wl", AgentID: "a1", SessionID: "s1", Version: 1, IsActive: true})
	store.Objects().PutMemory(schemas.Memory{MemoryID: "wr", AgentID: "a1", SessionID: "s1", Version: 3, IsActive: true})

	out, err := w.Run(schemas.ConflictMergeInput{LeftID: "wl", RightID: "wr", ObjectType: "memory"})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	result, ok := out.(schemas.ConflictMergeOutput)
	if !ok {
		t.Fatalf("unexpected output type %T", out)
	}
	if result.WinnerID != "wr" {
		t.Errorf("expected winner=wr (higher version), got %q", result.WinnerID)
	}
	if !result.Resolved {
		t.Error("expected Resolved=true")
	}
}

// ─── CommunicationWorker ─────────────────────────────────────────────────────

func TestCommunicationWorker_Broadcast_CopiesMemory(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	w := CreateInMemoryCommunicationWorker("test-comm", store.Objects())

	store.Objects().PutMemory(schemas.Memory{
		MemoryID: "mem_src",
		AgentID:  "agentA",
		Content:  "shared knowledge",
		IsActive: true,
	})

	if err := w.Broadcast("agentA", "agentB", "mem_src"); err != nil {
		t.Fatalf("Broadcast failed: %v", err)
	}

	// Expect a copy stored for agentB.
	mems := store.Objects().ListMemories("agentB", "")
	if len(mems) == 0 {
		t.Error("expected a memory copy in target agent's space")
	}
}

func TestCommunicationWorker_Broadcast_MissingMemory_NoError(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	w := CreateInMemoryCommunicationWorker("test-comm", store.Objects())

	// Broadcasting a non-existent memory should not error.
	if err := w.Broadcast("a1", "a2", "mem_nonexistent"); err != nil {
		t.Fatalf("expected no error for missing memory, got: %v", err)
	}
}

// ─── MicroBatchScheduler ─────────────────────────────────────────────────────

func TestMicroBatchScheduler_EnqueueAndFlush(t *testing.T) {
	w := CreateInMemoryMicroBatchScheduler("test-mb", 64)

	w.Enqueue("q1", map[string]string{"winner_id": "mem1"})
	w.Enqueue("q2", map[string]string{"winner_id": "mem2"})

	flushed := w.Flush()
	if len(flushed) != 2 {
		t.Errorf("expected 2 flushed items, got %d", len(flushed))
	}

	// Second flush should be empty.
	flushed2 := w.Flush()
	if len(flushed2) != 0 {
		t.Errorf("expected empty flush after drain, got %d", len(flushed2))
	}
}

func TestMicroBatchScheduler_FlushEmptyQueue(t *testing.T) {
	w := CreateInMemoryMicroBatchScheduler("test-mb", 64)
	flushed := w.Flush()
	if flushed == nil {
		t.Error("Flush should return non-nil empty slice")
	}
}

// ─── ProofTraceWorker ────────────────────────────────────────────────────────

func TestProofTraceWorker_AssembleTrace_SingleHop(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	derivLog := eventbackbone.NewDerivationLog(clock, bus)
	w := CreateInMemoryProofTraceWorker("test-proof", store.Edges(), derivLog)

	store.Edges().PutEdge(schemas.Edge{
		EdgeID:      "e1",
		SrcObjectID: "mem_A",
		SrcType:     "memory",
		EdgeType:    string(schemas.EdgeTypeDerivedFrom),
		DstObjectID: "evt_A",
		DstType:     "event",
		Weight:      1.0,
	})

	trace := w.AssembleTrace([]string{"mem_A"}, 3)
	if len(trace) == 0 {
		t.Error("expected at least one trace step")
	}
	found := false
	for _, step := range trace {
		if strings.Contains(step, "mem_A") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected trace to contain 'mem_A', got: %v", trace)
	}
}

func TestProofTraceWorker_AssembleTrace_MaxDepth(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	w := CreateInMemoryProofTraceWorker("test-proof", store.Edges(), nil)

	// Build a linear chain: A→B→C→D (4 hops)
	for i, pair := range [][2]string{{"A", "B"}, {"B", "C"}, {"C", "D"}, {"D", "E"}} {
		store.Edges().PutEdge(schemas.Edge{
			EdgeID:      "e" + string(rune('0'+i)),
			SrcObjectID: pair[0],
			DstObjectID: pair[1],
			EdgeType:    string(schemas.EdgeTypeDerivedFrom),
			Weight:      1.0,
		})
	}

	// maxDepth=2 should stop after 2 hops, not traverse the whole chain.
	trace := w.AssembleTrace([]string{"A"}, 2)
	for _, step := range trace {
		if strings.Contains(step, "D") || strings.Contains(step, "E") {
			t.Errorf("trace exceeded maxDepth=2: %v", trace)
		}
	}
}

func TestProofTraceWorker_AssembleTrace_CyclicGraph_Terminates(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	w := CreateInMemoryProofTraceWorker("test-proof", store.Edges(), nil)

	// Create a cycle: X→Y→Z→X
	store.Edges().PutEdge(schemas.Edge{EdgeID: "c1", SrcObjectID: "X", DstObjectID: "Y", EdgeType: "cycle", Weight: 1.0})
	store.Edges().PutEdge(schemas.Edge{EdgeID: "c2", SrcObjectID: "Y", DstObjectID: "Z", EdgeType: "cycle", Weight: 1.0})
	store.Edges().PutEdge(schemas.Edge{EdgeID: "c3", SrcObjectID: "Z", DstObjectID: "X", EdgeType: "cycle", Weight: 1.0})

	// Must terminate (BFS seen-set prevents revisit).
	done := make(chan struct{})
	go func() {
		_ = w.AssembleTrace([]string{"X"}, 8)
		close(done)
	}()
	select {
	case <-done:
	default:
		// If we get here synchronously, all is fine.
	}
	<-done
}

func TestProofTraceWorker_Run_TypedDispatch(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	w := CreateInMemoryProofTraceWorker("test-proof", store.Edges(), nil)

	out, err := w.Run(schemas.ProofTraceInput{ObjectIDs: []string{"obj1"}, MaxDepth: 2})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if _, ok := out.(schemas.ProofTraceOutput); !ok {
		t.Fatalf("unexpected output type %T", out)
	}
}
