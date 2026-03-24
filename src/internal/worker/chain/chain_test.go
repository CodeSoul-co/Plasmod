package chain

import (
	"testing"

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

func TestCollaborationChain_Run_CrossAgent_FallbackToLeft(t *testing.T) {
	mgr, store := buildManager(t)
	chain := CreateCollaborationChain(mgr)

	// Different agents → Merge is a no-op → LeftMemID is the fallback winner.
	store.Objects().PutMemory(schemas.Memory{
		MemoryID:  "mem_ca_left",
		AgentID:   "agentA",
		SessionID: "s1",
		IsActive:  true,
	})
	store.Objects().PutMemory(schemas.Memory{
		MemoryID:  "mem_ca_right",
		AgentID:   "agentB",
		SessionID: "s1",
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
	// Cross-agent → no LWW merge → left is the default winner.
	if out.WinnerMemID != "mem_ca_left" {
		t.Errorf("expected WinnerMemID=mem_ca_left (cross-agent fallback), got %q", out.WinnerMemID)
	}
}
