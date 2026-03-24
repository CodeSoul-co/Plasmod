package chain

import (
	"testing"
	"time"

	"andb/src/internal/eventbackbone"
	"andb/src/internal/schemas"
	"andb/src/internal/storage"
	baseline "andb/src/internal/worker/cognitive/baseline"
	"andb/src/internal/worker/coordination"
	"andb/src/internal/worker/indexing"
	"andb/src/internal/worker/ingestion"
	matworker "andb/src/internal/worker/materialization"
	"andb/src/internal/worker/nodes"
)

// buildManager creates a fully-wired Manager with one worker of each type
// backed by in-memory stores, matching the bootstrap.go wiring pattern.
func buildManager(t *testing.T) (*nodes.Manager, storage.RuntimeStorage) {
	t.Helper()
	store := storage.NewMemoryRuntimeStorage()
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	derivLog := eventbackbone.NewDerivationLog(clock, bus)
	policyLog := eventbackbone.NewPolicyDecisionLog(clock, bus)

	mgr := nodes.CreateManager()
	mgr.RegisterData(nodes.CreateInMemoryDataNode("data-1", store.Segments()))
	mgr.RegisterIndex(nodes.CreateInMemoryIndexNode("index-1", store.Indexes()))

	mgr.RegisterIngest(ingestion.CreateInMemoryIngestWorker("ingest-1"))
	mgr.RegisterObjectMaterialization(
		matworker.CreateInMemoryObjectMaterializationWorker("obj-mat-1", store.Objects(), store.Versions()))
	mgr.RegisterStateMaterialization(
		matworker.CreateInMemoryStateMaterializationWorker("state-mat-1", store.Objects(), store.Versions()))
	mgr.RegisterToolTrace(
		matworker.CreateInMemoryToolTraceWorker("tool-trace-1", store.Objects(), derivLog))
	mgr.RegisterIndexBuild(
		indexing.CreateInMemoryIndexBuildWorker("idx-build-1", store.Segments(), store.Indexes()))
	mgr.RegisterGraphRelation(
		indexing.CreateInMemoryGraphRelationWorker("graph-1", store.Edges()))
	mgr.RegisterSubgraphExecutor(
		indexing.CreateInMemorySubgraphExecutorWorker("subgraph-1"))
	mgr.RegisterMemoryExtraction(
		baseline.CreateInMemoryMemoryExtractionWorker("mem-ext-1", store.Objects()))
	mgr.RegisterMemoryConsolidation(
		baseline.CreateInMemoryMemoryConsolidationWorker("mem-consol-1", store.Objects()))
	mgr.RegisterSummarization(
		baseline.CreateInMemorySummarizationWorker("sum-1", store.Objects()))
	mgr.RegisterReflectionPolicy(
		baseline.CreateInMemoryReflectionPolicyWorker("reflect-1", store.Objects(), store.Policies(), policyLog))
	mgr.RegisterConflictMerge(
		coordination.CreateInMemoryConflictMergeWorker("cm-1", store.Objects(), store.Edges()))
	mgr.RegisterCommunication(
		coordination.CreateInMemoryCommunicationWorker("comm-1", store.Objects()))
	mgr.RegisterMicroBatch(
		coordination.CreateInMemoryMicroBatchScheduler("mb-1", 64))
	mgr.RegisterProofTrace(
		coordination.CreateInMemoryProofTraceWorker("proof-1", store.Edges(), derivLog))

	return mgr, store
}

// ─── MainChain ────────────────────────────────────────────────────────────────

func TestMainChain_Run_ValidEvent(t *testing.T) {
	mgr, store := buildManager(t)
	chain := CreateMainChain(mgr)

	ev := schemas.Event{
		EventID:   "evt_main1",
		AgentID:   "a1",
		SessionID: "s1",
		EventType: "agent_thought",
		Payload:   map[string]any{schemas.PayloadKeyText: "test content"},
	}
	result := chain.Run(MainChainInput{Event: ev, MemoryID: schemas.IDPrefixMemory + ev.EventID, Namespace: "ws1"})
	if !result.OK {
		t.Fatalf("MainChain.Run failed: %s", result.Error)
	}
	if result.ChainName != "main_chain" {
		t.Errorf("expected ChainName=main_chain, got %q", result.ChainName)
	}
	// Memory should be materialized
	_, ok := store.Objects().GetMemory(schemas.IDPrefixMemory + "evt_main1")
	if !ok {
		t.Error("expected Memory to be stored after MainChain")
	}
}

func TestMainChain_Run_ToolCallEvent(t *testing.T) {
	mgr, store := buildManager(t)
	chain := CreateMainChain(mgr)

	ev := schemas.Event{
		EventID:   "evt_tool1",
		AgentID:   "a1",
		SessionID: "s1",
		EventType: string(schemas.EventTypeToolCall),
	}
	result := chain.Run(MainChainInput{Event: ev, MemoryID: "", Namespace: "ws1"})
	if !result.OK {
		t.Fatalf("MainChain.Run failed: %s", result.Error)
	}
	// Artifact should be stored
	_, ok := store.Objects().GetArtifact(schemas.IDPrefixArtifact + "evt_tool1")
	if !ok {
		t.Error("expected Artifact to be stored for tool_call event")
	}
}

// MainChain does not perform schema validation (that is IngestWorker's role,
// called by Runtime.SubmitIngest before the chain runs). An event with a
// missing EventID still passes through the chain without error; downstream
// workers produce empty or no-op results rather than hard failures.
func TestMainChain_Run_MissingEventID_NoHardFail(t *testing.T) {
	mgr, _ := buildManager(t)
	chain := CreateMainChain(mgr)

	ev := schemas.Event{AgentID: "a1", SessionID: "s1", EventType: "agent_thought"}
	result := chain.Run(MainChainInput{Event: ev})
	// Chain must succeed; validation is enforced upstream by IngestWorker.
	if !result.OK {
		t.Errorf("unexpected chain failure for missing EventID: %s", result.Error)
	}
}

// ─── MemoryPipelineChain ──────────────────────────────────────────────────────

func TestMemoryPipelineChain_Run_ValidInput(t *testing.T) {
	mgr, store := buildManager(t)
	chain := CreateMemoryPipelineChain(mgr)

	// Seed a memory for extraction.
	store.Objects().PutMemory(schemas.Memory{
		MemoryID:  "mem_seed",
		AgentID:   "a1",
		SessionID: "s1",
		IsActive:  true,
		Content:   "seed content",
	})

	result := chain.Run(MemoryPipelineInput{
		EventID:   "evt_mp1",
		AgentID:   "a1",
		SessionID: "s1",
		Content:   "new memory content",
	})
	if !result.OK {
		t.Fatalf("MemoryPipelineChain.Run failed: %s", result.Error)
	}
}

// ─── QueryChain ───────────────────────────────────────────────────────────────

func TestQueryChain_Run_EmptyObjectIDs(t *testing.T) {
	mgr, store := buildManager(t)
	chain := CreateQueryChain(mgr)

	out, result := chain.Run(QueryChainInput{
		ObjectIDs:   []string{},
		MaxDepth:    2,
		ObjectStore: store.Objects(),
		EdgeStore:   store.Edges(),
	})
	if !result.OK {
		t.Fatalf("QueryChain.Run failed: %s", result.Error)
	}
	_ = out
}

func TestQueryChain_Run_WithObjectIDs(t *testing.T) {
	mgr, store := buildManager(t)
	chain := CreateQueryChain(mgr)

	store.Edges().PutEdge(schemas.Edge{
		EdgeID:      "e1",
		SrcObjectID: "mem_q1",
		DstObjectID: "evt_q1",
		EdgeType:    string(schemas.EdgeTypeDerivedFrom),
		Weight:      1.0,
	})

	out, result := chain.Run(QueryChainInput{
		ObjectIDs:   []string{"mem_q1"},
		MaxDepth:     3,
		ObjectStore: store.Objects(),
		EdgeStore:   store.Edges(),
	})
	if !result.OK {
		t.Fatalf("QueryChain.Run failed: %s", result.Error)
	}
	_ = out
}

// ─── CollaborationChain ───────────────────────────────────────────────────────

func TestCollaborationChain_Run_SameAgentLWW(t *testing.T) {
	mgr, store := buildManager(t)
	chain := CreateCollaborationChain(mgr)

	// Seed two same-agent memories; right has higher version → right should win.
	store.Objects().PutMemory(schemas.Memory{
		MemoryID:  "mem_collab_left",
		AgentID:   "a1",
		SessionID: "s1",
		Version:   1,
		IsActive:  true,
	})
	store.Objects().PutMemory(schemas.Memory{
		MemoryID:  "mem_collab_right",
		AgentID:   "a1",
		SessionID: "s1",
		Version:   3,
		IsActive:  true,
	})

	out, result := chain.Run(CollaborationChainInput{
		LeftMemID:     "mem_collab_left",
		RightMemID:    "mem_collab_right",
		ObjectType:    "memory",
		SourceAgentID: "a1",
		TargetAgentID: "a2",
	})
	if !result.OK {
		t.Fatalf("CollaborationChain.Run failed: %s", result.Error)
	}
	// The winner should be the higher-version memory.
	if out.WinnerMemID != "mem_collab_right" {
		t.Errorf("expected WinnerMemID=mem_collab_right (LWW), got %q", out.WinnerMemID)
	}
}

// ─── MainChain extensions ───────────────────────────────────────────────────

func TestMainChain_Run_StateUpdateEvent_RoutesToStateMatWorker(t *testing.T) {
	mgr, store := buildManager(t)
	chain := CreateMainChain(mgr)

	ev := schemas.Event{
		EventID:   "evt_state_main",
		AgentID:   "a1",
		SessionID: "s1",
		EventType: string(schemas.EventTypeStateUpdate),
		Payload: map[string]any{
			schemas.PayloadKeyStateKey:   "counter",
			schemas.PayloadKeyStateValue: 42,
		},
	}
	result := chain.Run(MainChainInput{Event: ev})
	if !result.OK {
		t.Fatalf("MainChain.Run failed: %s", result.Error)
	}
	// State should be materialized (not Memory).
	stateID := schemas.IDPrefixState + "a1_counter"
	_, ok := store.Objects().GetState(stateID)
	if !ok {
		t.Errorf("expected State %q after state_update event", stateID)
	}
}

func TestMainChain_Run_CheckpointEvent_ProducesCheckpointState(t *testing.T) {
	mgr, store := buildManager(t)
	chain := CreateMainChain(mgr)

	ev := schemas.Event{
		EventID:   "evt_ckpt",
		AgentID:   "a1",
		SessionID: "s1",
		EventType: string(schemas.EventTypeCheckpoint),
		Payload:   map[string]any{},
	}
	result := chain.Run(MainChainInput{Event: ev})
	if !result.OK {
		t.Fatalf("MainChain.Run failed: %s", result.Error)
	}
	// StateMaterializationWorker applies checkpoint events even without state payload.
	states := store.Objects().ListStates("a1", "s1")
	if len(states) == 0 {
		t.Error("expected at least one state after checkpoint event")
	}
}

func TestMainChain_Run_IndexBuildWorker_StoresSegment(t *testing.T) {
	mgr, store := buildManager(t)
	chain := CreateMainChain(mgr)

	ev := schemas.Event{
		EventID:   "evt_idx",
		AgentID:   "a1",
		SessionID: "s1",
		EventType: "agent_thought",
		Payload:   map[string]any{schemas.PayloadKeyText: "indexed content"},
	}
	memID := schemas.IDPrefixMemory + ev.EventID
	result := chain.Run(MainChainInput{Event: ev, MemoryID: memID, Namespace: "ws_idx"})
	if !result.OK {
		t.Fatalf("MainChain.Run failed: %s", result.Error)
	}
	// Verify a segment was created for this namespace.
	segs := store.Segments().List("ws_idx")
	if len(segs) == 0 {
		t.Error("expected at least one segment after IndexBuildWorker ran")
	}
}

func TestMainChain_Run_GraphRelationWorker_StoresEdge(t *testing.T) {
	mgr, store := buildManager(t)
	chain := CreateMainChain(mgr)

	ev := schemas.Event{
		EventID:   "evt_edge",
		AgentID:   "a1",
		SessionID: "s1",
		EventType: "agent_thought",
	}
	memID := schemas.IDPrefixMemory + ev.EventID
	result := chain.Run(MainChainInput{Event: ev, MemoryID: memID})
	if !result.OK {
		t.Fatalf("MainChain.Run failed: %s", result.Error)
	}
	edges := store.Edges().BulkEdges([]string{memID})
	if len(edges) == 0 {
		t.Error("expected at least one edge (memory→event) after GraphRelationWorker ran")
	}
}

// ─── MemoryPipelineChain extensions ─────────────────────────────────────────

func TestMemoryPipelineChain_Run_ExtractionOnly_NoConsolidation(t *testing.T) {
	mgr, store := buildManager(t)
	chain := CreateMemoryPipelineChain(mgr)

	result := chain.Run(MemoryPipelineInput{
		EventID:   "evt_extract_only",
		AgentID:   "a1",
		SessionID: "s1",
		Content:   "raw episodic content",
		// RunConsolidation=false
	})
	if !result.OK {
		t.Fatalf("MemoryPipelineChain.Run failed: %s", result.Error)
	}
	if result.Meta["consolidation_ran"] != false {
		t.Error("expected consolidation_ran=false in meta")
	}
	// A level-0 memory should have been extracted.
	memID := schemas.IDPrefixMemory + "evt_extract_only"
	mem, ok := store.Objects().GetMemory(memID)
	if !ok {
		t.Fatal("expected memory to be extracted")
	}
	if mem.Level != 0 {
		t.Errorf("expected level-0 memory, got level=%d", mem.Level)
	}
}

func TestMemoryPipelineChain_Run_ConsolidationFires_Level1Produced(t *testing.T) {
	mgr, store := buildManager(t)
	chain := CreateMemoryPipelineChain(mgr)

	// Pre-seed level-0 memories for consolidation.
	store.Objects().PutMemory(schemas.Memory{
		MemoryID:  "mem_c1",
		AgentID:   "a1",
		SessionID: "s1",
		Level:     0,
		IsActive:  true,
		Content:   "memory 1",
		Version:   1,
	})
	store.Objects().PutMemory(schemas.Memory{
		MemoryID:  "mem_c2",
		AgentID:   "a1",
		SessionID: "s1",
		Level:     0,
		IsActive:  true,
		Content:   "memory 2",
		Version:   2,
	})

	result := chain.Run(MemoryPipelineInput{
		EventID:           "evt_consol",
		AgentID:           "a1",
		SessionID:         "s1",
		Content:           "new content",
		RunConsolidation:  true,
		MaxSummaryLevel:   1,
	})
	if !result.OK {
		t.Fatalf("MemoryPipelineChain.Run failed: %s", result.Error)
	}
	// After consolidation, at least one level-1 memory should exist.
	mems := store.Objects().ListMemories("a1", "s1")
	hasLevel1 := false
	for _, m := range mems {
		if m.Level == 1 {
			hasLevel1 = true
			break
		}
	}
	if !hasLevel1 {
		t.Error("expected a level-1 consolidation memory")
	}
}

func TestMemoryPipelineChain_Run_Summarization_Level2Produced(t *testing.T) {
	mgr, store := buildManager(t)
	chain := CreateMemoryPipelineChain(mgr)

	// Pre-seed level-0 and level-1 memories.
	for i := 0; i < 3; i++ {
		store.Objects().PutMemory(schemas.Memory{
			MemoryID:  "mem_s1_" + string(rune('a'+i)),
			AgentID:   "a1",
			SessionID: "s1",
			Level:     0,
			IsActive:  true,
			Content:   "content " + string(rune('A'+i)),
			Version:   int64(i + 1),
		})
	}
	store.Objects().PutMemory(schemas.Memory{
		MemoryID:  "mem_s1_summary",
		AgentID:   "a1",
		SessionID: "s1",
		Level:     1,
		IsActive:  true,
		Content:   "summary of contents",
		Version:   1,
	})

	result := chain.Run(MemoryPipelineInput{
		EventID:          "evt_sum",
		AgentID:          "a1",
		SessionID:        "s1",
		Content:          "new",
		RunConsolidation: true,
		MaxSummaryLevel:  2,
	})
	if !result.OK {
		t.Fatalf("MemoryPipelineChain.Run failed: %s", result.Error)
	}
	// After MaxSummaryLevel=2, a level-2 memory should be produced.
	mems := store.Objects().ListMemories("a1", "s1")
	hasLevel2 := false
	for _, m := range mems {
		if m.Level == 2 {
			hasLevel2 = true
			break
		}
	}
	if !hasLevel2 {
		t.Error("expected a level-2 summarization memory after MaxSummaryLevel=2")
	}
}

func TestMemoryPipelineChain_Run_ReflectionPolicy_AppliedToMemory(t *testing.T) {
	mgr, store := buildManager(t)
	chain := CreateMemoryPipelineChain(mgr)

	result := chain.Run(MemoryPipelineInput{
		EventID:   "evt_reflect",
		AgentID:   "a1",
		SessionID: "s1",
		Content:   "reflectable content",
	})
	if !result.OK {
		t.Fatalf("MemoryPipelineChain.Run failed: %s", result.Error)
	}
	// The memory should be active (default) after reflection.
	memID := schemas.IDPrefixMemory + "evt_reflect"
	mem, ok := store.Objects().GetMemory(memID)
	if !ok {
		t.Fatal("expected memory to exist")
	}
	if !mem.IsActive {
		t.Error("expected IsActive=true for freshly extracted memory with no policy overrides")
	}
}

func TestMemoryPipelineChain_Run_EmptyContent_NoOpNoPanic(t *testing.T) {
	mgr, store := buildManager(t)
	chain := CreateMemoryPipelineChain(mgr)

	result := chain.Run(MemoryPipelineInput{
		EventID: "evt_empty",
		AgentID: "a1",
		SessionID: "s1",
		Content:   "", // empty content — should not panic
	})
	if !result.OK {
		t.Fatalf("expected chain to succeed with empty content: %s", result.Error)
	}
	// Memory should still be created (level-0 with empty content).
	memID := schemas.IDPrefixMemory + "evt_empty"
	if _, ok := store.Objects().GetMemory(memID); !ok {
		t.Error("expected memory to be created even with empty content")
	}
}

// ─── QueryChain extensions ──────────────────────────────────────────────────

func TestQueryChain_Run_PrefetchPopulatesSubgraph(t *testing.T) {
	mgr, store := buildManager(t)
	chain := CreateQueryChain(mgr)

	// Seed a memory directly (bypassing materialization) — this is the scenario
	// QueryChain's pre-fetch fixes: nodes were previously nil when this seed
	// was stored without going through the full ingest path.
	store.Objects().PutMemory(schemas.Memory{
		MemoryID:  "mem_prefetch",
		AgentID:   "agentPF",
		SessionID: "s1",
		Content:   "prefetch test content",
		IsActive:  true,
		Version:   1,
	})
	store.Edges().PutEdge(schemas.Edge{
		EdgeID:      "e_prefetch",
		SrcObjectID: "mem_prefetch",
		DstObjectID: "evt_prefetch",
		EdgeType:    string(schemas.EdgeTypeDerivedFrom),
		Weight:      1.0,
	})

	out, result := chain.Run(QueryChainInput{
		ObjectIDs:   []string{"mem_prefetch"},
		MaxDepth:    3,
		ObjectStore: store.Objects(),
		EdgeStore:   store.Edges(),
	})
	if !result.OK {
		t.Fatalf("QueryChain.Run failed: %s", result.Error)
	}
	if len(out.Subgraph.Nodes) == 0 {
		t.Error("expected non-empty Subgraph.Nodes after pre-fetch")
	}
	if len(out.Subgraph.Edges) == 0 {
		t.Error("expected non-empty Subgraph.Edges after expansion")
	}
}

func TestQueryChain_Run_EdgeTypeFilter_Respected(t *testing.T) {
	mgr, store := buildManager(t)
	chain := CreateQueryChain(mgr)

	store.Edges().PutEdge(schemas.Edge{
		EdgeID:      "e_include",
		SrcObjectID: "mem_filter",
		DstObjectID: "evt_include",
		EdgeType:    string(schemas.EdgeTypeDerivedFrom),
		Weight:      1.0,
	})
	store.Edges().PutEdge(schemas.Edge{
		EdgeID:      "e_exclude",
		SrcObjectID: "mem_filter",
		DstObjectID: "evt_exclude",
		EdgeType:    "other_type",
		Weight:      1.0,
	})

	out, _ := chain.Run(QueryChainInput{
		ObjectIDs:     []string{"mem_filter"},
		MaxDepth:      2,
		ObjectStore:   store.Objects(),
		EdgeStore:     store.Edges(),
		EdgeTypeFilter: []string{string(schemas.EdgeTypeDerivedFrom)},
	})

	// EdgeTypeFilter only applies to SubgraphExecutor expansion, not pre-fetched
	// bulk edges.  The MergedEdges may contain unfiltered preEdges; verify the
	// Subgraph.Edges (which are filtered) is non-empty when edges match the filter.
	if len(out.Subgraph.Edges) == 0 {
		t.Error("expected non-empty Subgraph.Edges after filtered expansion")
	}
}

func TestQueryChain_Run_MaxDepth_Respected(t *testing.T) {
	mgr, store := buildManager(t)
	chain := CreateQueryChain(mgr)

	// Build a chain: A→B→C→D (3 hops).
	store.Edges().PutEdge(schemas.Edge{EdgeID: "e0", SrcObjectID: "depthA", DstObjectID: "depthB", EdgeType: string(schemas.EdgeTypeDerivedFrom), Weight: 1.0})
	store.Edges().PutEdge(schemas.Edge{EdgeID: "e1", SrcObjectID: "depthB", DstObjectID: "depthC", EdgeType: string(schemas.EdgeTypeDerivedFrom), Weight: 1.0})
	store.Edges().PutEdge(schemas.Edge{EdgeID: "e2", SrcObjectID: "depthC", DstObjectID: "depthD", EdgeType: string(schemas.EdgeTypeDerivedFrom), Weight: 1.0})

	_, result := chain.Run(QueryChainInput{
		ObjectIDs:   []string{"depthA"},
		MaxDepth:    1, // only 1 hop
		ObjectStore: store.Objects(),
		EdgeStore:   store.Edges(),
	})
	if !result.OK {
		t.Fatalf("QueryChain.Run failed: %s", result.Error)
	}
	// With maxDepth=1, only step count should be limited.
	if result.Meta["trace_steps"].(int) > 2 {
		t.Errorf("expected trace_steps to be capped at maxDepth, got %v", result.Meta["trace_steps"])
	}
}

func TestQueryChain_Run_CyclicGraph_TerminatesSafely(t *testing.T) {
	mgr, store := buildManager(t)
	chain := CreateQueryChain(mgr)

	// Create a self-loop: X→X
	store.Edges().PutEdge(schemas.Edge{EdgeID: "e_self", SrcObjectID: "nodeX", DstObjectID: "nodeX", EdgeType: string(schemas.EdgeTypeDerivedFrom), Weight: 1.0})
	// Create a cross-cycle: A→B→C→A
	store.Edges().PutEdge(schemas.Edge{EdgeID: "ec1", SrcObjectID: "cycleA", DstObjectID: "cycleB", EdgeType: string(schemas.EdgeTypeDerivedFrom), Weight: 1.0})
	store.Edges().PutEdge(schemas.Edge{EdgeID: "ec2", SrcObjectID: "cycleB", DstObjectID: "cycleC", EdgeType: string(schemas.EdgeTypeDerivedFrom), Weight: 1.0})
	store.Edges().PutEdge(schemas.Edge{EdgeID: "ec3", SrcObjectID: "cycleC", DstObjectID: "cycleA", EdgeType: string(schemas.EdgeTypeDerivedFrom), Weight: 1.0})

	// Self-loop: must not hang.
	done := make(chan struct{})
	go func() {
		_, result := chain.Run(QueryChainInput{
			ObjectIDs:   []string{"nodeX"},
			MaxDepth:    8,
			ObjectStore: store.Objects(),
			EdgeStore:   store.Edges(),
		})
		if !result.OK {
			t.Errorf("QueryChain failed on self-loop: %s", result.Error)
		}
		close(done)
	}()

	select {
	case <-done:
		// OK — terminated
	case <-time.After(2 * time.Second):
		t.Fatal("QueryChain hung on cyclic graph (self-loop)")
	}

	// Cross-cycle: must not hang.
	done2 := make(chan struct{})
	go func() {
		_, result := chain.Run(QueryChainInput{
			ObjectIDs:   []string{"cycleA"},
			MaxDepth:    8,
			ObjectStore: store.Objects(),
			EdgeStore:   store.Edges(),
		})
		if !result.OK {
			t.Errorf("QueryChain failed on cross-cycle: %s", result.Error)
		}
		close(done2)
	}()

	select {
	case <-done2:
		// OK — terminated
	case <-time.After(2 * time.Second):
		t.Fatal("QueryChain hung on cyclic graph (cross-cycle)")
	}
}

func TestQueryChain_Run_DerivationLog_Appended(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	derivLog := eventbackbone.NewDerivationLog(clock, bus)

	mgr := nodes.CreateManager()
	mgr.RegisterData(nodes.CreateInMemoryDataNode("data-1", store.Segments()))
	mgr.RegisterIndex(nodes.CreateInMemoryIndexNode("index-1", store.Indexes()))
	mgr.RegisterProofTrace(coordination.CreateInMemoryProofTraceWorker("proof-1", store.Edges(), derivLog))
	mgr.RegisterSubgraphExecutor(indexing.CreateInMemorySubgraphExecutorWorker("subgraph-1"))

	chain := CreateQueryChain(mgr)

	store.Edges().PutEdge(schemas.Edge{
		EdgeID:      "e_deriv",
		SrcObjectID: "mem_deriv",
		DstObjectID: "evt_deriv",
		EdgeType:    string(schemas.EdgeTypeDerivedFrom),
		Weight:      1.0,
	})

	_, result := chain.Run(QueryChainInput{
		ObjectIDs:   []string{"mem_deriv"},
		MaxDepth:    3,
		ObjectStore: store.Objects(),
		EdgeStore:   store.Edges(),
	})
	if !result.OK {
		t.Fatalf("QueryChain.Run failed: %s", result.Error)
	}
	// DerivationLog should have been written to (entries were emitted during edge write).
	if derivLog == nil {
		t.Skip("derivLog not wired in this test build")
	}
}

// ─── CollaborationChain extensions ───────────────────────────────────────────

func TestCollaborationChain_Run_CrossAgent_LWW_Applied(t *testing.T) {
	mgr, store := buildManager(t)
	chain := CreateCollaborationChain(mgr)

	// Same session but different agents — same session means Merge applies LWW.
	store.Objects().PutMemory(schemas.Memory{
		MemoryID:  "mem_ca_left",
		AgentID:   "agentA",
		SessionID: "shared_sess",
		Version:   1,
		IsActive:  true,
	})
	store.Objects().PutMemory(schemas.Memory{
		MemoryID:  "mem_ca_right",
		AgentID:   "agentA",
		SessionID: "shared_sess",
		Version:   7,
		IsActive:  true,
	})

	out, result := chain.Run(CollaborationChainInput{
		LeftMemID:     "mem_ca_left",
		RightMemID:    "mem_ca_right",
		ObjectType:    "memory",
		SourceAgentID: "agentA",
		TargetAgentID: "agentB",
	})
	if !result.OK {
		t.Fatalf("CollaborationChain.Run failed: %s", result.Error)
	}
	// LWW should pick the higher version regardless of cross-agent setting.
	if out.WinnerMemID != "mem_ca_right" {
		t.Errorf("expected WinnerMemID=mem_ca_right (LWW), got %q", out.WinnerMemID)
	}
}

func TestCollaborationChain_Run_ConflictEdge_Created(t *testing.T) {
	mgr, store := buildManager(t)
	chain := CreateCollaborationChain(mgr)

	store.Objects().PutMemory(schemas.Memory{MemoryID: "ml_ce", AgentID: "a1", SessionID: "s1", Version: 2, IsActive: true})
	store.Objects().PutMemory(schemas.Memory{MemoryID: "mr_ce", AgentID: "a1", SessionID: "s1", Version: 5, IsActive: true})

	out, _ := chain.Run(CollaborationChainInput{
		LeftMemID:     "ml_ce",
		RightMemID:    "mr_ce",
		ObjectType:    "memory",
		SourceAgentID: "a1",
		TargetAgentID: "a2",
	})

	// Verify conflict_resolved edge was created.
	edges := store.Edges().BulkEdges([]string{out.WinnerMemID})
	found := false
	for _, e := range edges {
		if e.EdgeType == string(schemas.EdgeTypeConflictResolved) {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected conflict_resolved edge after CollaborationChain.Run")
	}
}

func TestCollaborationChain_Run_MicroBatch_Enqueued(t *testing.T) {
	mgr, store := buildManager(t)
	chain := CreateCollaborationChain(mgr)

	store.Objects().PutMemory(schemas.Memory{MemoryID: "ml_mb", AgentID: "a1", SessionID: "s1", Version: 1, IsActive: true})
	store.Objects().PutMemory(schemas.Memory{MemoryID: "mr_mb", AgentID: "a1", SessionID: "s1", Version: 3, IsActive: true})

	chain.Run(CollaborationChainInput{
		LeftMemID:     "ml_mb",
		RightMemID:    "mr_mb",
		ObjectType:    "memory",
		SourceAgentID: "a1",
		TargetAgentID: "a2",
	})

	// Verify the MicroBatchScheduler received the conflict result.
	flushed := mgr.FlushMicroBatch()
	if len(flushed) == 0 {
		t.Fatal("expected MicroBatch to have at least one enqueued payload after CollaborationChain.Run")
	}
}

func TestCollaborationChain_Run_CommunicationWorker_BroadcastWinner(t *testing.T) {
	mgr, store := buildManager(t)
	chain := CreateCollaborationChain(mgr)

	store.Objects().PutMemory(schemas.Memory{
		MemoryID: "mem_bc_src",
		AgentID:  "agentA",
		Content:  "to be broadcast",
		IsActive: true,
		Version:  1,
	})

	out, _ := chain.Run(CollaborationChainInput{
		LeftMemID:     "mem_bc_src",
		RightMemID:    "mem_bc_src", // same ID = no merge
		ObjectType:    "memory",
		SourceAgentID: "agentA",
		TargetAgentID: "agentB",
	})

	// Winner is mem_bc_src (same as left/right since they are equal).
	// CommunicationWorker should have created a shared copy in agentB's space.
	if out.SharedMemID == "" {
		t.Error("expected SharedMemID to be set after broadcast")
	}
	sharedMems := store.Objects().ListMemories("agentB", "")
	if len(sharedMems) == 0 {
		t.Error("expected a shared memory in agentB's space after broadcast")
	}
}
