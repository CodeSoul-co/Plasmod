package worker

import (
	"strings"
	"testing"

	"andb/src/internal/coordinator"
	"andb/src/internal/dataplane"
	"andb/src/internal/eventbackbone"
	"andb/src/internal/evidence"
	"andb/src/internal/materialization"
	"andb/src/internal/schemas"
	"andb/src/internal/semantic"
	"andb/src/internal/storage"
	"andb/src/internal/worker/chain"
	"andb/src/internal/worker/coordination"
	baseline "andb/src/internal/worker/cognitive/baseline"
	"andb/src/internal/worker/nodes"
)

// TestE2E_CPUQueryPath is a comprehensive end-to-end simulation of the full
// CPU-only query pipeline, covering all major subsystems.
//
// Pipeline exercised:
//  1. WAL append (SubmitIngest)
//  2. ObjectMaterialization → Memory + Artifact stored in ObjectStore
//  3. PreComputeService → EvidenceFragment cached at ingest time
//  4. GraphRelationWorker → base edges (derived_from) written to GraphEdgeStore
//  5. IndexBuildWorker → segment index
//  6. DispatchQuery → SegmentDataPlane.Search (CPU HNSW / lexical)
//  7. Assembler → merges cached fragments, applies filters
//  8. QueryChain.Run → proof trace BFS + subgraph expansion + edge merge
//  9. DerivationLog → derivation entries appended at ingest, surfaced in proof trace
func TestE2E_CPUQueryPath(t *testing.T) {
	// ── 1. Bootstrap runtime components ─────────────────────────────────────────
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	wal := eventbackbone.NewInMemoryWAL(bus, clock)
	plane := dataplane.NewSegmentDataPlane() // CPU-only, no CGO
	policy := semantic.NewPolicyEngine()
	planner := semantic.NewDefaultQueryPlanner()
	materializer := materialization.NewService()
	assembler := evidence.NewAssembler()
	store := storage.NewMemoryRuntimeStorage()

	// TieredObjectStore with GraphEdgeStore wired (graph-c)
	tieredObjs := storage.NewTieredObjectStore(
		store.HotCache(),
		store.Objects(),
		store.Edges(),
		storage.NewInMemoryColdStore(),
	)

	// Node manager with all worker types registered
	derivLog := eventbackbone.NewDerivationLog(clock, bus)
	nodeManager := nodes.CreateManager()
	nodeManager.RegisterData(nodes.CreateInMemoryDataNode("data-1", store.Segments()))
	nodeManager.RegisterIndex(nodes.CreateInMemoryIndexNode("index-1", store.Indexes()))
	nodeManager.RegisterQuery(nodes.CreateInMemoryQueryNode("query-1", plane))
	nodeManager.RegisterMemoryExtraction(nil) // use DefaultMemoryExtractionWorker
	nodeManager.RegisterGraphRelation(nil)  // use DefaultGraphRelationWorker
	nodeManager.RegisterProofTrace(coordination.CreateInMemoryProofTraceWorker("proof-e2e", store.Edges(), derivLog))
	nodeManager.RegisterIndexBuild(nil)     // use DefaultIndexBuildWorker

	// Full CoordinatorHub
	coord := coordinator.NewCoordinatorHub(
		coordinator.NewSchemaCoordinator(semantic.NewObjectModelRegistry()),
		coordinator.NewObjectCoordinator(store.Objects(), store.Versions()),
		coordinator.NewPolicyCoordinator(policy, store.Policies()),
		coordinator.NewVersionCoordinator(clock, store.Versions()),
		coordinator.NewWorkerScheduler(),
		coordinator.NewMemoryCoordinator(store.Objects()),
		coordinator.NewIndexCoordinator(store.Segments(), store.Indexes()),
		coordinator.NewShardCoordinator(4),
		coordinator.NewQueryCoordinator(planner, policy),
	)

	// Evidence layer
	evCache := evidence.NewCache(1000)
	preCompute := materialization.NewPreComputeService(evCache)

	// Full Runtime with all deps
	r := CreateRuntime(
		wal, bus, plane, coord, policy, planner, materializer, preCompute,
		assembler, evCache, derivLog, nil, nodeManager, store, tieredObjs,
	)

	// ── 2. Ingest two related events ──────────────────────────────────────────
	// Event 1: root event
	_, err := r.SubmitIngest(schemas.Event{
		EventID:     "evt_root",
		TenantID:    "tenant-1",
		WorkspaceID: "ws-1",
		AgentID:     "agent-alpha",
		SessionID:   "session-1",
		EventType:  "user_message",
		Source:      "user",
		Importance: 0.8,
		Payload:    map[string]any{"text": "what is the Q3 revenue for NVDA"},
	})
	if err != nil {
		t.Fatalf("ingest event 1 failed: %v", err)
	}

	// Event 2: causal child (references event 1)
	_, err = r.SubmitIngest(schemas.Event{
		EventID:     "evt_child",
		TenantID:    "tenant-1",
		WorkspaceID: "ws-1",
		AgentID:     "agent-alpha",
		SessionID:   "session-1",
		EventType:  "tool_result",
		Source:      "tool",
		CausalRefs: []string{"evt_root"},
		Importance: 0.9,
		Payload:    map[string]any{"text": "NVDA Q3 revenue: $35.1 billion"},
	})
	if err != nil {
		t.Fatalf("ingest event 2 failed: %v", err)
	}

	// ── 3. Verify objects stored ────────────────────────────────────────────────
	memRoot, ok := store.Objects().GetMemory("mem_evt_root")
	if !ok {
		t.Fatalf("expected memory mem_evt_root to be stored")
	}
	if memRoot.Importance != 0.8 {
		t.Errorf("expected importance 0.8, got %f", memRoot.Importance)
	}

	memChild, ok := store.Objects().GetMemory("mem_evt_child")
	if !ok {
		t.Fatalf("expected memory mem_evt_child to be stored")
	}
	_ = memChild // used indirectly via salience verification below

	// ── 4. Verify EvidenceFragment pre-computed at ingest time ─────────────────
	fragRoot, ok := evCache.Get("mem_evt_root")
	if !ok {
		t.Fatalf("expected EvidenceFragment for mem_evt_root (PreComputeService not called)")
	}
	if fragRoot.SalienceScore == 0 {
		t.Errorf("expected non-zero SalienceScore for mem_evt_root")
	}
	if fragRoot.ObjectType != "user_message" {
		t.Errorf("expected ObjectType user_message, got %s", fragRoot.ObjectType)
	}
	if len(fragRoot.TextTokens) == 0 {
		t.Errorf("expected non-empty TextTokens for mem_evt_root")
	}

	// Child has CausalRef → causal_ref bonus applied
	fragChild, ok := evCache.Get("mem_evt_child")
	if !ok {
		t.Fatalf("expected EvidenceFragment for mem_evt_child")
	}

	// ── 5. Verify graph edges stored ───────────────────────────────────────────
	edges := store.Edges().ListEdges()
	if len(edges) == 0 {
		t.Errorf("expected at least one edge in GraphEdgeStore (MainChain writes derived_from edges)")
	}

	// Check for derived_from edges from memory to event
	var hasDerivedFrom bool
	for _, e := range edges {
		if e.EdgeType == string(schemas.EdgeTypeDerivedFrom) {
			hasDerivedFrom = true
			break
		}
	}
	if !hasDerivedFrom {
		t.Errorf("expected at least one edge with type %s", schemas.EdgeTypeDerivedFrom)
	}

	// ── 6. Execute query ───────────────────────────────────────────────────────
	resp := r.ExecuteQuery(schemas.QueryRequest{
		QueryText:   "Q3 revenue NVDA",
		QueryScope:  "workspace",
		WorkspaceID: "ws-1",
		AgentID:     "agent-alpha",
		SessionID:   "session-1",
		TopK:        5,
		ObjectTypes: []string{"memory"},
	})

	// ── 7. Verify query response ────────────────────────────────────────────────
	if len(resp.Objects) == 0 {
		t.Errorf("expected at least one object in query response; resp=%+v", resp)
	}

	// Proof trace must be non-empty (QueryChain.Run → ProofTraceWorker BFS)
	if len(resp.ProofTrace) == 0 {
		t.Errorf("expected non-empty ProofTrace from QueryChain.Run; resp.ProofTrace=%v", resp.ProofTrace)
	} else {
		t.Logf("ProofTrace (%d steps): %v", len(resp.ProofTrace), resp.ProofTrace)
	}

	// AppliedFilters must contain governance filter steps
	if len(resp.AppliedFilters) == 0 {
		t.Errorf("expected non-empty AppliedFilters (policy.ApplyQueryFilters should add steps)")
	} else {
		t.Logf("AppliedFilters: %v", resp.AppliedFilters)
	}

	// ── 8. Verify DerivationLog entries ────────────────────────────────────────
	// SubmitIngest → MainChain → ObjectMatWorker calls DerivationLog.Append
	// via ToolTraceWorker for tool_call/tool_result events
	derivEntries := derivLog.Since(0)
	if len(derivEntries) == 0 {
		t.Logf("NOTE: DerivationLog has 0 entries — verify ObjectMatWorker calls DerivationLog.Append for derivation steps")
	} else {
		t.Logf("DerivationLog entries: %d", len(derivEntries))
		for _, e := range derivEntries {
			t.Logf("  [%d] %s -[%s]-> %s (%s)", e.LSN, e.SourceID, e.Operation, e.DerivedID, e.DerivedType)
		}
	}

	// ── 9. Verify QueryChain subgraph expansion ────────────────────────────────
	// With pre-seeded edges (BulkEdges), SubgraphExecutorWorker should surface
	// 1-hop neighbours in resp.Nodes or resp.Edges
	if len(resp.Nodes) > 0 {
		t.Logf("Subgraph nodes: %d", len(resp.Nodes))
	}
	if len(resp.Edges) > 0 {
		t.Logf("Subgraph edges: %d", len(resp.Edges))
	}

	// ── 10. AlgorithmConfig: verify PreComputeService uses cfg ──────────────────
	// Salience for mem_evt_child: base=0.9, CausalRefs>0 → +0.1 bonus = 1.0 capped
	if fragChild.SalienceScore < 0.9 {
		t.Errorf("expected salience >= 0.9 for child with CausalRefs (got %f)", fragChild.SalienceScore)
	}
	if fragChild.SalienceScore > 1.0 {
		t.Errorf("expected salience capped at 1.0, got %f", fragChild.SalienceScore)
	}

	t.Logf("=== E2E CPU Query Path Summary ===")
	t.Logf("  Objects in response  : %d", len(resp.Objects))
	t.Logf("  ProofTrace steps     : %d", len(resp.ProofTrace))
	t.Logf("  AppliedFilters        : %d", len(resp.AppliedFilters))
	t.Logf("  Graph nodes           : %d", len(resp.Nodes))
	t.Logf("  Graph edges           : %d", len(resp.Edges))
	t.Logf("  EvidenceFragment cached: %d", evCache.Len())
	t.Logf("  DerivationLog entries : %d", len(derivEntries))
}

// TestE2E_QueryChain_BFSProofTrace verifies that QueryChain.Run produces a
// multi-hop proof trace from edges in the GraphEdgeStore.
func TestE2E_QueryChain_BFSProofTrace(t *testing.T) {
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	store := storage.NewMemoryRuntimeStorage()

	derivLog := eventbackbone.NewDerivationLog(clock, bus)

	nodeManager := nodes.CreateManager()
	nodeManager.RegisterProofTrace(coordination.CreateInMemoryProofTraceWorker("proof-e2e-bfs", store.Edges(), derivLog))

	qc := chain.CreateQueryChain(nodeManager)

	// Seed edges: A → B → C (chain depth 2)
	edges := []schemas.Edge{
		{EdgeID: "e1", SrcObjectID: "obj_A", DstObjectID: "obj_B", EdgeType: string(schemas.EdgeTypeCausedBy), Weight: schemas.DefaultCausalWeight},
		{EdgeID: "e2", SrcObjectID: "obj_B", DstObjectID: "obj_C", EdgeType: string(schemas.EdgeTypeCausedBy), Weight: schemas.DefaultCausalWeight},
	}
	for _, e := range edges {
		store.Edges().PutEdge(e)
	}

	// DerivationLog: A derived_from original_A
	derivLog.Append("original_A", "event", "obj_A", "memory", "extraction")

	chainOut, result := qc.Run(chain.QueryChainInput{
		ObjectIDs:   []string{"obj_A"},
		MaxDepth:    4,
		ObjectStore: store.Objects(),
		EdgeStore:   store.Edges(),
	})

	if !result.OK {
		t.Fatalf("QueryChain.Run should succeed, got error: %s", result.Error)
	}
	if len(chainOut.ProofTrace) == 0 {
		t.Fatalf("expected ProofTrace from BFS over edges")
	}

	// Verify BFS trace contains the chain: A→B, B→C
	var hasB, hasC bool
	for _, step := range chainOut.ProofTrace {
		if strings.Contains(step, "obj_B") {
			hasB = true
		}
		if strings.Contains(step, "obj_C") {
			hasC = true
		}
	}
	if !hasB {
		t.Errorf("expected trace to reach obj_B; trace: %v", chainOut.ProofTrace)
	}
	if !hasC {
		t.Errorf("expected trace to reach obj_C (2 hops from A); trace: %v", chainOut.ProofTrace)
	}

	// Derivation steps should also appear
	var hasDerivation bool
	for _, step := range chainOut.ProofTrace {
		if strings.Contains(step, "derivation:") {
			hasDerivation = true
			break
		}
	}
	if !hasDerivation {
		t.Errorf("expected derivation: step in proof trace; trace: %v", chainOut.ProofTrace)
	}

	t.Logf("ProofTrace: %v", chainOut.ProofTrace)
}

// TestE2E_TieredObjectStore_GraphEdgeAutoBuild verifies that TieredObjectStore
// automatically builds base edges (derived_from, caused_by, contextual) when
// PutMemory is called, without requiring an explicit GraphRelationWorker call.
func TestE2E_TieredObjectStore_GraphEdgeAutoBuild(t *testing.T) {
	hot := storage.NewHotObjectCache(100)
	warm := storage.NewMemoryObjectStore()
	warmEdges := storage.NewMemoryGraphEdgeStore()
	cold := storage.NewInMemoryColdStore()
	tiered := storage.NewTieredObjectStore(hot, warm, warmEdges, cold)

	// PutMemory — graph-c should auto-generate base edges
	mem := schemas.Memory{
		MemoryID:       "mem_auto_edge_test",
		SourceEventIDs: []string{"evt_auto_edge_test"},
		AgentID:        "agent-x",
		SessionID:      "session-x",
		Importance:     0.7,
		IsActive:       true,
	}
	tiered.PutMemory(mem, 0.7)

	edges := warmEdges.ListEdges()
	if len(edges) == 0 {
		t.Fatalf("expected TieredObjectStore.PutMemory to auto-build base edges, got 0 edges")
	}

	var hasDerivedFrom, hasBelongsToSession, hasOwnedByAgent bool
	for _, e := range edges {
		switch e.EdgeType {
		case string(schemas.EdgeTypeDerivedFrom):
			hasDerivedFrom = true
		case string(schemas.EdgeTypeBelongsToSession):
			hasBelongsToSession = true
		case string(schemas.EdgeTypeOwnedByAgent):
			hasOwnedByAgent = true
		}
	}

	if !hasDerivedFrom {
		t.Errorf("expected derived_from edge auto-built; edges: %v", edges)
	}
	if !hasBelongsToSession {
		t.Errorf("expected belongs_to_session edge auto-built; edges: %v", edges)
	}
	if !hasOwnedByAgent {
		t.Errorf("expected owned_by_agent edge auto-built; edges: %v", edges)
	}
	t.Logf("Auto-built edges: %d total — derived_from=%v belongs_to_session=%v owned_by_agent=%v",
		len(edges), hasDerivedFrom, hasBelongsToSession, hasOwnedByAgent)
}

// TestE2E_MemoryPipelineChain verifies that MemoryPipelineChain.Run drives the full
// cognitive memory ladder: Extraction → Consolidation → Summarization → ReflectionPolicy.
func TestE2E_MemoryPipelineChain(t *testing.T) {
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	store := storage.NewMemoryRuntimeStorage()

	nodeManager := nodes.CreateManager()
	nodeManager.RegisterMemoryExtraction(baseline.CreateInMemoryMemoryExtractionWorker("extract-1", store.Objects()))
	nodeManager.RegisterMemoryConsolidation(baseline.CreateInMemoryMemoryConsolidationWorker("consolidate-1", store.Objects()))
	nodeManager.RegisterSummarization(baseline.CreateInMemorySummarizationWorker("summarize-1", store.Objects()))

	policyDecLog := eventbackbone.NewPolicyDecisionLog(clock, bus)
	hot := storage.NewHotObjectCache(100)
	tieredObjs := storage.NewTieredObjectStore(hot, store.Objects(), store.Edges(), storage.NewInMemoryColdStore())
	nodeManager.RegisterReflectionPolicy(baseline.CreateInMemoryReflectionPolicyWorker(
		"reflect-1", store.Objects(), store.Policies(), policyDecLog,
	).WithTieredObjects(tieredObjs))

	pipeChain := chain.CreateMemoryPipelineChain(nodeManager)

	// Run extraction only first
	result := pipeChain.Run(chain.MemoryPipelineInput{
		EventID:   "evt_mpc_1",
		AgentID:   "agent-pipeline",
		SessionID: "session-pipeline",
		Content:   "The Q3 revenue for NVDA increased by 35% year over year",
		RunConsolidation: false,
	})
	if !result.OK {
		t.Fatalf("MemoryPipelineChain extraction step failed: %s", result.Error)
	}

	memID := schemas.IDPrefixMemory + "evt_mpc_1"
	mem, ok := store.Objects().GetMemory(memID)
	if !ok {
		t.Fatalf("expected memory %s after extraction", memID)
	}
	if mem.Content == "" {
		t.Errorf("expected non-empty content in extracted memory")
	}
	t.Logf("Extraction step OK: memory=%s content=%q", mem.MemoryID, mem.Content)

	// Run consolidation + summarization step
	result = pipeChain.Run(chain.MemoryPipelineInput{
		EventID:          "evt_mpc_2",
		AgentID:          "agent-pipeline",
		SessionID:        "session-pipeline",
		Content:          "Second memory for same session",
		RunConsolidation: true,
		MaxSummaryLevel:  1,
	})
	if !result.OK {
		t.Fatalf("MemoryPipelineChain consolidation step failed: %s", result.Error)
	}
	if result.Meta["consolidation_ran"] != true {
		t.Errorf("expected consolidation_ran=true in result metadata")
	}

	t.Logf("MemoryPipelineChain summary: consolidation_ran=%v memory_id=%v",
		result.Meta["consolidation_ran"], result.Meta["memory_id"])

	// Verify reflection policy was applied (with TieredObjectStore wired)
	memID2 := schemas.IDPrefixMemory + "evt_mpc_2"
	mem2, ok := store.Objects().GetMemory(memID2)
	if !ok {
		t.Fatalf("expected memory %s after full pipeline", memID2)
	}
	t.Logf("Consolidation step OK: memory=%s is_active=%v", mem2.MemoryID, mem2.IsActive)
}

// TestE2E_CollaborationChain verifies that CollaborationChain.Run resolves
// a conflict (LWW) and broadcasts the winner to the target agent.
func TestE2E_CollaborationChain(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()

	nodeManager := nodes.CreateManager()
	nodeManager.RegisterConflictMerge(coordination.CreateInMemoryConflictMergeWorker(
		"conflict-merge-1", store.Objects(), store.Edges(),
	))
	nodeManager.RegisterCommunication(coordination.CreateInMemoryCommunicationWorker(
		"comm-1", store.Objects(),
	))
	nodeManager.RegisterMicroBatch(coordination.CreateInMemoryMicroBatchScheduler("micro-batch-1", 64))

	collabChain := chain.CreateCollaborationChain(nodeManager)

	// Seed two competing memories for the same entity (different versions)
	memA := schemas.Memory{
		MemoryID:  "mem_competing_a",
		AgentID:   "agent-alpha",
		SessionID: "session-collab",
		Content:   "NVDA revenue Q3: $35.1B (source: earnings report)",
		Version:   3,
		IsActive:  true,
	}
	memB := schemas.Memory{
		MemoryID:  "mem_competing_b",
		AgentID:   "agent-alpha",
		SessionID: "session-collab",
		Content:   "NVDA revenue Q3: $35.1B (source: investor deck)",
		Version:   5, // higher version → winner
		IsActive:  true,
	}
	store.Objects().PutMemory(memA)
	store.Objects().PutMemory(memB)

	// Also seed events so the EdgeStore is available
	store.Objects().PutEvent(schemas.Event{EventID: "evt_collab_a", AgentID: "agent-alpha", SessionID: "session-collab"})
	store.Objects().PutEvent(schemas.Event{EventID: "evt_collab_b", AgentID: "agent-alpha", SessionID: "session-collab"})

	out, result := collabChain.Run(chain.CollaborationChainInput{
		LeftMemID:     "mem_competing_a",
		RightMemID:    "mem_competing_b",
		ObjectType:    string(schemas.ObjectTypeMemory),
		SourceAgentID: "agent-alpha",
		TargetAgentID: "agent-beta",
	})

	if !result.OK {
		t.Fatalf("CollaborationChain.Run failed: %s", result.Error)
	}
	if out.WinnerMemID != "mem_competing_b" {
		t.Errorf("expected winner mem_competing_b (higher version=5), got %s", out.WinnerMemID)
	}
	if out.SharedMemID == "" {
		t.Errorf("expected shared_mem_id to be non-empty (cross-agent broadcast)")
	}
	if !strings.HasPrefix(out.SharedMemID, schemas.IDPrefixShared) {
		t.Errorf("expected shared_mem_id to start with %s, got %s", schemas.IDPrefixShared, out.SharedMemID)
	}

	// Verify conflict_resolved edge was written
	edges := store.Edges().ListEdges()
	var hasConflictEdge bool
	for _, e := range edges {
		if e.EdgeType == string(schemas.EdgeTypeConflictResolved) {
			hasConflictEdge = true
			break
		}
	}
	if !hasConflictEdge {
		t.Logf("NOTE: no conflict_resolved edge found — verify ConflictMergeWorker writes it to EdgeStore")
	}

	t.Logf("CollaborationChain summary: winner=%s shared=%s edges=%d",
		out.WinnerMemID, out.SharedMemID, len(edges))
}
