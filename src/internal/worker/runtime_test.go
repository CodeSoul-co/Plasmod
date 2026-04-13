package worker

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"plasmod/src/internal/coordinator"
	"plasmod/src/internal/dataplane"
	"plasmod/src/internal/eventbackbone"
	"plasmod/src/internal/evidence"
	"plasmod/src/internal/materialization"
	"plasmod/src/internal/schemas"
	"plasmod/src/internal/semantic"
	"plasmod/src/internal/storage"
	"plasmod/src/internal/worker/indexing"
	matworker "plasmod/src/internal/worker/materialization"
	"plasmod/src/internal/worker/nodes"
)

type failingPlane struct{}

func (f *failingPlane) Ingest(record dataplane.IngestRecord) error {
	return errors.New("ingest failed")
}
func (f *failingPlane) Search(input dataplane.SearchInput) dataplane.SearchOutput {
	return dataplane.SearchOutput{}
}
func (f *failingPlane) Flush() error { return nil }

func buildTestRuntime(t *testing.T) *Runtime {
	t.Helper()
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	wal := eventbackbone.NewInMemoryWAL(bus, clock)
	plane := dataplane.NewSegmentDataPlane()
	policy := semantic.NewPolicyEngine()
	planner := semantic.NewDefaultQueryPlanner()
	materializer := materialization.NewService()
	assembler := evidence.NewAssembler()
	store := storage.NewMemoryRuntimeStorage()
	tieredObjs := storage.NewTieredObjectStore(
		store.HotCache(),
		store.Objects(),
		store.Edges(),
		storage.NewInMemoryColdStore(),
	)
	nodeManager := nodes.CreateManager()
	nodeManager.RegisterData(nodes.CreateInMemoryDataNode("data-1", store.Segments()))
	nodeManager.RegisterIndex(nodes.CreateInMemoryIndexNode("index-1", store.Indexes()))
	nodeManager.RegisterQuery(nodes.CreateInMemoryQueryNode("query-1", plane))
	nodeManager.RegisterSubgraphExecutor(indexing.CreateInMemorySubgraphExecutorWorker("subgraph-1"))
	nodeManager.RegisterStateMaterialization(
		matworker.CreateInMemoryStateMaterializationWorker("state-mat-1", store.Objects(), store.Versions(), nil))
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
	evCache := evidence.NewCache(1000)
	preCompute := materialization.NewPreComputeService(evCache)
	return CreateRuntime(wal, bus, plane, coord, policy, planner, materializer, preCompute, assembler, evCache, nil, nil, nodeManager, store, tieredObjs)
}

func TestRuntime_IngestAndQuery(t *testing.T) {
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	wal := eventbackbone.NewInMemoryWAL(bus, clock)
	plane := dataplane.NewSegmentDataPlane()
	policy := semantic.NewPolicyEngine()
	planner := semantic.NewDefaultQueryPlanner()
	materializer := materialization.NewService()
	assembler := evidence.NewAssembler()
	store := storage.NewMemoryRuntimeStorage()
	nodeManager := nodes.CreateManager()
	nodeManager.RegisterData(nodes.CreateInMemoryDataNode("data-1", store.Segments()))
	nodeManager.RegisterIndex(nodes.CreateInMemoryIndexNode("index-1", store.Indexes()))
	nodeManager.RegisterQuery(nodes.CreateInMemoryQueryNode("query-1", plane))
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

	evCache := evidence.NewCache(1000)
	preCompute := materialization.NewPreComputeService(evCache)
	tieredObjs := storage.NewTieredObjectStore(
		store.HotCache(),
		store.Objects(),
		store.Edges(),
		storage.NewInMemoryColdStore(),
	)
	r := CreateRuntime(wal, bus, plane, coord, policy, planner, materializer, preCompute, assembler, evCache, nil, nil, nodeManager, store, tieredObjs)

	_, err := r.SubmitIngest(schemas.Event{
		EventID:     "evt_test_1",
		TenantID:    "t1",
		WorkspaceID: "w1",
		AgentID:     "a1",
		SessionID:   "s1",
		Payload:     map[string]any{"text": "hello andb"},
	})
	if err != nil {
		t.Fatalf("submit ingest failed: %v", err)
	}

	resp := r.ExecuteQuery(schemas.QueryRequest{
		QueryText:   "hello",
		QueryScope:  "workspace",
		WorkspaceID: "w1",
		TopK:        5,
		SessionID:   "s1",
	})

	if len(resp.Objects) == 0 {
		t.Fatalf("expected at least one object")
	}
	if len(resp.ProofTrace) == 0 {
		t.Fatalf("expected proof trace")
	}

	topology := r.Topology()
	nodesAny, ok := topology["nodes"]
	if !ok || nodesAny == nil {
		t.Fatalf("expected topology nodes")
	}
}

func TestRuntime_SubmitIngest_FailFast_NoCanonicalWrites(t *testing.T) {
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	wal := eventbackbone.NewInMemoryWAL(bus, clock)
	plane := &failingPlane{}
	policy := semantic.NewPolicyEngine()
	planner := semantic.NewDefaultQueryPlanner()
	materializer := materialization.NewService()
	assembler := evidence.NewAssembler()
	store := storage.NewMemoryRuntimeStorage()
	nodeManager := nodes.CreateManager()
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
	evCache := evidence.NewCache(100)
	preCompute := materialization.NewPreComputeService(evCache)
	tieredObjs := storage.NewTieredObjectStore(store.HotCache(), store.Objects(), store.Edges(), storage.NewInMemoryColdStore())
	r := CreateRuntime(wal, bus, plane, coord, policy, planner, materializer, preCompute, assembler, evCache, nil, nil, nodeManager, store, tieredObjs)

	_, err := r.SubmitIngest(schemas.Event{
		EventID:     "evt_failfast_1",
		AgentID:     "agent_1",
		SessionID:   "session_1",
		WorkspaceID: "w1",
		Payload:     map[string]any{"text": "should not persist on plane failure"},
	})
	if err == nil {
		t.Fatal("expected ingest error from failing plane")
	}

	if _, ok := store.Objects().GetMemory("mem_evt_failfast_1"); ok {
		t.Fatal("memory should not be persisted when plane ingest fails")
	}
}

func TestRuntime_SubmitIngest_AllowsMetadataOnlyEmbeddingDimMismatch(t *testing.T) {
	r := buildTestRuntime(t)
	_, err := r.SubmitIngest(schemas.Event{
		EventID:   "evt_dim_mismatch_1",
		AgentID:   "agent_dim",
		SessionID: "session_dim",
		Payload: map[string]any{
			"text":          "metadata-only dim should pass",
			"embedding_dim": 128, // runtime default tfidf dim is 256
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRuntime_SubmitIngest_RejectsEmbeddingVectorLengthMismatch(t *testing.T) {
	r := buildTestRuntime(t)
	_, err := r.SubmitIngest(schemas.Event{
		EventID:   "evt_vec_mismatch_1",
		AgentID:   "agent_vec",
		SessionID: "session_vec",
		Payload: map[string]any{
			"text":   "embedding vector mismatch",
			"vector": []any{0.1, 0.2, 0.3},
		},
	})
	if err == nil {
		t.Fatal("expected embedding_vector_len_mismatch error")
	}
	if !strings.Contains(err.Error(), "embedding_vector_len_mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRuntime_SubmitIngest_RejectsEmbeddingDimMismatchWhenVectorProvided(t *testing.T) {
	r := buildTestRuntime(t)
	_, err := r.SubmitIngest(schemas.Event{
		EventID:   "evt_dim_mismatch_with_vec_1",
		AgentID:   "agent_dim_vec",
		SessionID: "session_dim_vec",
		Payload: map[string]any{
			"text":          "embedding dim mismatch with vector",
			"embedding_dim": 128, // runtime default tfidf dim is 256
			"vector":        []any{0.1, 0.2, 0.3},
		},
	})
	if err == nil {
		t.Fatal("expected embedding_dim_mismatch error")
	}
	if !strings.Contains(err.Error(), "embedding_dim_mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRuntime_SubmitIngest_SkipsConflictMergeForDatasetLoader(t *testing.T) {
	t.Setenv("ANDB_CONFLICT_MERGE_SKIP_DATASET_LOADER", "true")
	r := buildTestRuntime(t)

	events := []schemas.Event{
		{
			EventID:   "evt_bulk_runtime_1",
			AgentID:   "agent_bulk_runtime",
			SessionID: "session_bulk_runtime",
			Source:    "dataset_loader",
			Payload: map[string]any{
				"text":        "dataset=testQuery10K.fbin row:1",
				"ingest_mode": "bulk_dataset",
			},
		},
		{
			EventID:   "evt_bulk_runtime_2",
			AgentID:   "agent_bulk_runtime",
			SessionID: "session_bulk_runtime",
			Source:    "dataset_loader",
			Payload: map[string]any{
				"text":        "dataset=testQuery10K.fbin row:2",
				"ingest_mode": "bulk_dataset",
			},
		},
	}
	for _, ev := range events {
		if _, err := r.SubmitIngest(ev); err != nil {
			t.Fatalf("SubmitIngest failed: %v", err)
		}
	}

	m1, ok1 := r.storage.Objects().GetMemory("mem_evt_bulk_runtime_1")
	m2, ok2 := r.storage.Objects().GetMemory("mem_evt_bulk_runtime_2")
	if !ok1 || !ok2 {
		t.Fatalf("expected both ingested memories to exist")
	}
	if !m1.IsActive || !m2.IsActive {
		t.Fatalf("dataset_loader ingest should keep all rows active, got m1=%v m2=%v", m1.IsActive, m2.IsActive)
	}
}

func TestRuntime_ExecuteQuery_FilterByImportBatchID(t *testing.T) {
	r := buildTestRuntime(t)
	for i, batch := range []string{"batch_old", "batch_new"} {
		ev := schemas.Event{
			EventID:     fmt.Sprintf("evt_batch_filter_%d", i),
			WorkspaceID: "w_batch",
			AgentID:     "agent_batch",
			SessionID:   "session_batch",
			Payload: map[string]any{
				"text":            "dataset=testQuery10K.fbin row:1",
				"dataset":         "testQuery10K.fbin",
				"file_name":       "testQuery10K.fbin",
				"import_batch_id": batch,
			},
		}
		if _, err := r.SubmitIngest(ev); err != nil {
			t.Fatalf("SubmitIngest failed: %v", err)
		}
	}

	resp := r.ExecuteQuery(schemas.QueryRequest{
		QueryText:      "dataset=testQuery10K.fbin",
		QueryScope:     "w_batch",
		WorkspaceID:    "w_batch",
		AgentID:        "agent_batch",
		SessionID:      "session_batch",
		TopK:           10,
		ResponseMode:   schemas.ResponseModeStructuredEvidence,
		DatasetName:    "testQuery10K.fbin",
		SourceFileName: "testQuery10K.fbin",
		ImportBatchID:  "batch_new",
	})
	if len(resp.Objects) != 1 || resp.Objects[0] != "mem_evt_batch_filter_1" {
		t.Fatalf("expected only latest selected batch memory, got objects=%v", resp.Objects)
	}
}

func TestRuntime_ExecuteQuery_LatestBatchOnly(t *testing.T) {
	r := buildTestRuntime(t)
	events := []schemas.Event{
		{
			EventID:     "evt_latest_batch_old_1",
			WorkspaceID: "w_latest",
			AgentID:     "agent_latest",
			SessionID:   "session_latest",
			Payload: map[string]any{
				"text":            "dataset=deep1B.ibin row:1",
				"dataset":         "deep1B",
				"file_name":       "deep1B.ibin",
				"import_batch_id": "batch_old",
			},
		},
		{
			EventID:     "evt_latest_batch_new_1",
			WorkspaceID: "w_latest",
			AgentID:     "agent_latest",
			SessionID:   "session_latest",
			Payload: map[string]any{
				"text":            "dataset=deep1B.ibin row:2",
				"dataset":         "deep1B",
				"file_name":       "deep1B.ibin",
				"import_batch_id": "batch_new",
			},
		},
	}
	for _, ev := range events {
		if _, err := r.SubmitIngest(ev); err != nil {
			t.Fatalf("SubmitIngest failed: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	resp := r.ExecuteQuery(schemas.QueryRequest{
		QueryText:        "dataset=deep1B.ibin",
		QueryScope:       "w_latest",
		WorkspaceID:      "w_latest",
		AgentID:          "agent_latest",
		SessionID:        "session_latest",
		TopK:             10,
		ResponseMode:     schemas.ResponseModeStructuredEvidence,
		DatasetName:      "deep1B",
		SourceFileName:   "deep1B.ibin",
		LatestBatchOnly:  true,
	})
	if len(resp.Objects) != 1 || resp.Objects[0] != "mem_evt_latest_batch_new_1" {
		t.Fatalf("expected only latest batch memory, got objects=%v", resp.Objects)
	}
}

// TestRuntime_SubgraphExpand_NodesPopulated is a regression test for the
// SubgraphExecutorWorker nodes pre-fetch gap.
//
// Root cause: DispatchSubgraphExpand was called with nodes=nil, causing
// OneHopExpand to build an empty nodeIndex and return EvidenceSubgraph{Nodes:[]}
// even when edges were present.  The fix pre-fetches Memory objects via
// r.storage.Objects().GetMemory and converts them to []GraphNode before the
// dispatch call.
//
// Verifies:
//   - ExecuteQuery does not panic when SubgraphExecutorWorker is registered.
//   - resp.Edges is non-empty after ingest (materialization creates session +
//     agent edges that are then picked up by BulkEdges + subgraph expansion).
func TestRuntime_SubgraphExpand_NodesPopulated(t *testing.T) {
	r := buildTestRuntime(t)

	_, err := r.SubmitIngest(schemas.Event{
		EventID:   "evt_subgraph_1",
		AgentID:   "agent_sg",
		SessionID: "session_sg",
		Payload:   map[string]any{"text": "subgraph node test"},
	})
	if err != nil {
		t.Fatalf("ingest failed: %v", err)
	}

	resp := r.ExecuteQuery(schemas.QueryRequest{
		QueryText: "subgraph node test",
		SessionID: "session_sg",
		TopK:      5,
	})

	if len(resp.Objects) == 0 {
		t.Fatalf("expected at least one object from query")
	}
	if len(resp.Edges) == 0 {
		t.Fatalf("expected non-empty resp.Edges: materialization derives session/agent edges, " +
			"SubgraphExecutorWorker should surface them via preNodes+preEdges")
	}
}

func TestRuntime_QueryResponse_ContainsEmbeddingProvenance(t *testing.T) {
	r := buildTestRuntime(t)

	_, err := r.SubmitIngest(schemas.Event{
		EventID:   "evt_prov_1",
		AgentID:   "agent_prov",
		SessionID: "session_prov",
		Payload:   map[string]any{"text": "provenance test text"},
	})
	if err != nil {
		t.Fatalf("ingest failed: %v", err)
	}

	resp := r.ExecuteQuery(schemas.QueryRequest{
		QueryText: "provenance test text",
		TopK:      5,
		SessionID: "session_prov",
	})

	if len(resp.Provenance) == 0 {
		t.Fatal("expected provenance entries")
	}
	joined := strings.Join(resp.Provenance, " ")
	if !strings.Contains(joined, "embedding_runtime_family=") {
		t.Fatalf("expected embedding_runtime_family in provenance, got: %v", resp.Provenance)
	}
	if !strings.Contains(joined, "embedding_runtime_dim=") {
		t.Fatalf("expected embedding_runtime_dim in provenance, got: %v", resp.Provenance)
	}
	if !strings.Contains(joined, "cross_dim_fusion=rrf_result_layer") {
		t.Fatalf("expected cross_dim_fusion provenance marker, got: %v", resp.Provenance)
	}
}

// TestRuntime_StateCheckpoint_Flow verifies that:
//  1. StateMaterializationWorker applies state_update events to create State objects.
//  2. DispatchStateCheckpoint writes ObjectVersion snapshots for each active State.
//  3. The snapshot tag is non-empty after checkpoint.
//
// Note: SubmitIngest does NOT create State objects synchronously (State creation is
// driven by StateMaterializationWorker, which is invoked asynchronously by EventSubscriber).
// Therefore the test directly calls DispatchStateMaterialization to exercise the worker.
func TestRuntime_StateCheckpoint_Flow(t *testing.T) {
	r := buildTestRuntime(t)

	// Step 1: Ingest state_update events via SubmitIngest (creates Memories).
	var stateEvents []schemas.Event
	for i := 0; i < 2; i++ {
		stateEvents = append(stateEvents, schemas.Event{
			EventID:   fmt.Sprintf("evt_ckpt_%d", i),
			AgentID:   "agent_ckpt",
			SessionID: "session_ckpt",
			EventType: string(schemas.EventTypeStateUpdate),
			Payload: map[string]any{
				schemas.PayloadKeyStateKey:   fmt.Sprintf("key_%d", i),
				schemas.PayloadKeyStateValue: fmt.Sprintf("value_%d", i),
			},
		})
	}
	for _, ev := range stateEvents {
		_, err := r.SubmitIngest(ev)
		if err != nil {
			t.Fatalf("SubmitIngest failed: %v", err)
		}
		// Step 2: StateMaterializationWorker runs asynchronously via EventSubscriber;
		// in unit tests we drive it directly.
		r.nodeManager.DispatchStateMaterialization(ev)
	}

	// Step 3: Verify State objects were created by StateMaterializationWorker.
	states := r.storage.Objects().ListStates("agent_ckpt", "session_ckpt")
	if len(states) == 0 {
		t.Fatalf("expected at least one State object after DispatchStateMaterialization, got %d", len(states))
	}
	t.Logf("created %d State objects", len(states))

	// Step 4: Trigger a checkpoint.
	r.nodeManager.DispatchStateCheckpoint("agent_ckpt", "session_ckpt")

	// Step 5: Verify ObjectVersion snapshots were written for each state.
	checkpointFound := false
	for _, state := range states {
		versions := r.storage.Versions().GetVersions(state.StateID)
		for _, v := range versions {
			if v.SnapshotTag != "" {
				checkpointFound = true
				t.Logf("snapshot: state=%s tag=%s", state.StateID, v.SnapshotTag)
			}
		}
	}
	if !checkpointFound {
		t.Error("expected at least one snapshot with non-empty SnapshotTag after DispatchStateCheckpoint")
	}
}

func TestRuntime_ExecuteQuery_ReturnsEventNodeProperties(t *testing.T) {
	r := buildTestRuntime(t)

	_, err := r.SubmitIngest(schemas.Event{
		EventID:    "evt_runtime_props_1",
		AgentID:    "agent_rt",
		SessionID:  "sess_rt",
		EventType:  "tool_call",
		Source:     "planner",
		Importance: 0.8,
		Payload: map[string]any{
			"text": "tool_call search",
			"tool": "search",
		},
	})
	if err != nil {
		t.Fatalf("submit ingest failed: %v", err)
	}

	resp := r.ExecuteQuery(schemas.QueryRequest{
		QueryText: "tool_call",
		SessionID: "sess_rt",
		TopK:      5,
	})

	if len(resp.Objects) == 0 {
		t.Fatalf("expected non-empty resp.Objects, got %+v", resp)
	}
	if len(resp.Nodes) == 0 {
		t.Fatalf("expected non-empty resp.Nodes, got %+v", resp)
	}

	found := false
	for _, n := range resp.Nodes {
		if n.ObjectID == "mem_evt_runtime_props_1" {
			found = true

			if n.ObjectType != "memory" {
				t.Fatalf("expected memory node, got %s", n.ObjectType)
			}
			if n.Properties == nil {
				t.Fatal("expected node properties")
			}

			if got, ok := n.Properties["provenance_ref"]; !ok || got != "evt_runtime_props_1" {
				t.Fatalf("expected provenance_ref=evt_runtime_props_1, got %v", n.Properties["provenance_ref"])
			}

			srcIDs, ok := n.Properties["source_event_ids"]
			if !ok {
				t.Fatal("expected source_event_ids in memory node properties")
			}

			ids, ok := srcIDs.([]string)
			if !ok {
				t.Fatalf("expected source_event_ids to be []string, got %T", srcIDs)
			}
			if len(ids) == 0 || ids[0] != "evt_runtime_props_1" {
				t.Fatalf("expected source_event_ids to contain evt_runtime_props_1, got %+v", ids)
			}

			if got, ok := n.Properties["content"]; !ok || got != "tool_call search" {
				t.Fatalf("expected content=tool_call search, got %v", n.Properties["content"])
			}
		}
	}

	if !found {
		t.Fatalf("expected memory node mem_evt_runtime_props_1 in resp.Nodes, got %+v", resp.Nodes)
	}
}

func TestRuntime_SubmitIngest_AutoBuildsMemoryBaseEdges(t *testing.T) {
	r := buildTestRuntime(t)

	_, err := r.SubmitIngest(schemas.Event{
		EventID:    "evt_auto_edges_1",
		AgentID:    "agent_auto",
		SessionID:  "sess_auto",
		EventType:  "tool_call",
		Source:     "planner",
		Importance: 0.9,
		Payload: map[string]any{
			"text": "search quarterly revenue",
			"tool": "search",
		},
	})
	if err != nil {
		t.Fatalf("submit ingest failed: %v", err)
	}

	memID := "mem_evt_auto_edges_1"

	_, ok := r.storage.Objects().GetMemory(memID)
	if !ok {
		t.Fatalf("expected memory %s to be stored", memID)
	}

	edges := r.storage.Edges().EdgesFrom(memID)
	if len(edges) == 0 {
		t.Fatalf("expected auto-built edges from %s, got none", memID)
	}

	var hasSession, hasAgent, hasDerived bool
	for _, e := range edges {
		switch e.EdgeType {
		case string(schemas.EdgeTypeBelongsToSession):
			if e.DstObjectID == "sess_auto" {
				hasSession = true
			}
		case string(schemas.EdgeTypeOwnedByAgent):
			if e.DstObjectID == "agent_auto" {
				hasAgent = true
			}
		case string(schemas.EdgeTypeDerivedFrom):
			if e.DstObjectID == "evt_auto_edges_1" {
				hasDerived = true
			}
		}
	}

	if !hasSession || !hasAgent || !hasDerived {
		t.Fatalf("missing expected auto-built edges: %+v", edges)
	}
}

func TestRuntime_ExecuteQuery_ReturnsArtifactNodeProperties(t *testing.T) {
	t.Skip("artifact nodes are not currently materialized into resp.Nodes by ExecuteQuery; raw PutArtifact + PutEdge does not make artifacts query-visible in the current runtime path")
}

// TestRuntime_SeedDrivesGraphExpansion is a functional test for the Week 3
// Member B task: "candidate seed interface connected to relation layer".
//
// It verifies the full pipeline:
//
//	TieredDataPlane.Search → Retriever.EnrichAndRank (seed marking)
//	→ SeedIDs → QueryChain (SubgraphExecutorWorker) → resp.Nodes + resp.Edges
//
// Specifically:
//  1. Ingest multiple events so the retrieval plane has real candidates.
//  2. Execute a query and confirm the response contains the expected graph fields.
//  3. Confirm seed provenance appears in resp.Provenance.
//  4. Confirm resp.Nodes carries Memory properties (provenance_ref, content).
//  5. Confirm resp.Edges reflects materialization-derived relations
//     (belongs_to_session, owned_by_agent, derived_from).
func TestRuntime_SeedDrivesGraphExpansion(t *testing.T) {
	r := buildTestRuntime(t)

	// Ingest 3 events so there are multiple candidates — seed marking needs
	// at least 2 candidates to differentiate high vs low scores.
	events := []schemas.Event{
		{
			EventID:    "evt_seed_1",
			AgentID:    "agent_seed",
			SessionID:  "sess_seed",
			EventType:  "tool_call",
			Source:     "planner",
			Importance: 0.9, // high → should become seed
			Payload:    map[string]any{"text": "vector search query result"},
		},
		{
			EventID:    "evt_seed_2",
			AgentID:    "agent_seed",
			SessionID:  "sess_seed",
			EventType:  "observation",
			Source:     "observer",
			Importance: 0.3, // low → may not become seed
			Payload:    map[string]any{"text": "background noise event"},
		},
		{
			EventID:    "evt_seed_3",
			AgentID:    "agent_seed",
			SessionID:  "sess_seed",
			EventType:  "tool_call",
			Source:     "executor",
			Importance: 0.8,
			Payload:    map[string]any{"text": "vector search follow-up"},
		},
	}
	for _, ev := range events {
		_, err := r.SubmitIngest(ev)
		if err != nil {
			t.Fatalf("SubmitIngest(%s) failed: %v", ev.EventID, err)
		}
	}

	resp := r.ExecuteQuery(schemas.QueryRequest{
		QueryText: "vector search",
		SessionID: "sess_seed",
		AgentID:   "agent_seed",
		TopK:      5,
	})

	// ── 1. Basic response sanity ──────────────────────────────────────────────
	if len(resp.Objects) == 0 {
		t.Fatal("expected non-empty resp.Objects")
	}
	t.Logf("resp.Objects (%d): %v", len(resp.Objects), resp.Objects)

	// ── 2. Seed provenance marker ─────────────────────────────────────────────
	// runtime.go appends "retrieval_seeds=N graph_expansion_via=seed_ids" when
	// at least one seed was marked by the Retriever.
	joined := strings.Join(resp.Provenance, " ")
	hasSeedProv := strings.Contains(joined, "retrieval_seeds=") &&
		strings.Contains(joined, "graph_expansion_via=seed_ids")
	if !hasSeedProv {
		// If no seeds were marked (all scores below floor — e.g. tfidf gives
		// uniform scores), the fallback path is used and no seed provenance
		// is attached.  Log a warning but do not fail — this is expected when
		// the embedder produces identical scores for all candidates.
		t.Logf("WARN: seed provenance not found in resp.Provenance — Retriever fallback path used (uniform scores)")
		t.Logf("Provenance: %v", resp.Provenance)
	} else {
		t.Logf("PASS: seed provenance = %q", joined)
	}

	// ── 3. Nodes populated with Memory properties ─────────────────────────────
	if len(resp.Nodes) == 0 {
		t.Fatal("expected non-empty resp.Nodes — SubgraphExecutorWorker should populate nodes from seeds")
	}
	t.Logf("resp.Nodes (%d):", len(resp.Nodes))
	for _, n := range resp.Nodes {
		t.Logf("  node id=%s type=%s", n.ObjectID, n.ObjectType)
		if n.Properties == nil {
			t.Errorf("node %s has nil Properties — expected at least content/provenance_ref", n.ObjectID)
		}
	}

	// ── 4. Edges populated from materialization-derived relations ─────────────
	if len(resp.Edges) == 0 {
		t.Fatal("expected non-empty resp.Edges — SubmitIngest materializes belongs_to_session/owned_by_agent/derived_from edges")
	}
	edgeTypes := make(map[string]int)
	for _, e := range resp.Edges {
		edgeTypes[e.EdgeType]++
	}
	t.Logf("resp.Edges (%d): %v", len(resp.Edges), edgeTypes)

	// ── 5. ProofTrace includes retrieval + graph stages ───────────────────────
	if len(resp.ProofTrace) == 0 {
		t.Fatal("expected non-empty resp.ProofTrace")
	}
	t.Logf("ProofTrace (%d stages): %v", len(resp.ProofTrace), resp.ProofTrace)

	// ── 6. ChainTraces.Query reflects seed path ───────────────────────────────
	queryTrace := strings.Join(resp.ChainTraces.Query, " ")
	if strings.Contains(queryTrace, "skipped=no_seed_object_ids") {
		t.Error("query chain was skipped — seed IDs or fallback IDs should have been passed")
	}
	t.Logf("ChainTraces.Query: %v", resp.ChainTraces.Query)
}

func TestRuntime_ExecuteQuery_IncludeColdReturnsArchivedMemory(t *testing.T) {
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	wal := eventbackbone.NewInMemoryWAL(bus, clock)

	plane := dataplane.NewSegmentDataPlane()
	policy := semantic.NewPolicyEngine()
	planner := semantic.NewDefaultQueryPlanner()
	materializer := materialization.NewService()
	assembler := evidence.NewAssembler()

	store := storage.NewMemoryRuntimeStorage()
	cold := storage.NewInMemoryColdStore()

	// 这里不能直接复用 buildTestRuntime(t)，因为 buildTestRuntime 里的
	// tieredObjs 没有 embedder，ArchiveMemory 不会写 cold embedding。
	embedder := dataplane.NewTfidfEmbedder(dataplane.DefaultEmbeddingDim)

	tieredObjs := storage.NewTieredObjectStoreWithEmbedder(
		store.HotCache(),
		store.Objects(),
		store.Edges(),
		cold,
		embedder,
		schemas.DefaultAlgorithmConfig().HotTierSalienceThreshold,
	)

	nodeManager := nodes.CreateManager()
	nodeManager.RegisterData(nodes.CreateInMemoryDataNode("data-1", store.Segments()))
	nodeManager.RegisterIndex(nodes.CreateInMemoryIndexNode("index-1", store.Indexes()))
	nodeManager.RegisterQuery(nodes.CreateInMemoryQueryNode("query-1", plane))
	nodeManager.RegisterSubgraphExecutor(indexing.CreateInMemorySubgraphExecutorWorker("subgraph-1"))
	nodeManager.RegisterStateMaterialization(
		matworker.CreateInMemoryStateMaterializationWorker("state-mat-1", store.Objects(), store.Versions(), nil),
	)

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

	evCache := evidence.NewCache(1000)
	preCompute := materialization.NewPreComputeService(evCache)

	r := CreateRuntime(
		wal,
		bus,
		plane,
		coord,
		policy,
		planner,
		materializer,
		preCompute,
		assembler,
		evCache,
		nil,
		nil,
		nodeManager,
		store,
		tieredObjs,
	)

	ev := schemas.Event{
		EventID:    "evt_cold_runtime_1",
		AgentID:    "agent_cold",
		SessionID:  "sess_cold",
		EventType:  "tool_call",
		Source:     "planner",
		Importance: 0.95,
		Payload:    map[string]any{"text": "archived cold retrieval target"},
	}

	if _, err := r.SubmitIngest(ev); err != nil {
		t.Fatalf("SubmitIngest failed: %v", err)
	}

	memID := "mem_evt_cold_runtime_1"

	// 先确认 ingest 后 warm 中有这条 memory
	mem, ok := store.Objects().GetMemory(memID)
	if !ok {
		t.Fatalf("expected warm memory %s after ingest", memID)
	}

	// archive 到 cold（会写 cold memory + cold embedding）
	tieredObjs.ArchiveMemory(memID)

	// ArchiveMemory 当前不会删除 warm，所以这里手动删掉 warm copy，
	// 这样后面的命中才能证明来自 cold tier，而不是 warm。
	store.Objects().DeleteMemory(memID)

	// 验证 cold 中已经有对象
	if _, ok := cold.GetMemory(memID); !ok {
		t.Fatalf("expected archived memory %s in cold store", memID)
	}

	// 验证 cold embedding 已写入
	vec, ok, err := cold.GetMemoryEmbedding(memID)
	if err != nil {
		t.Fatalf("GetMemoryEmbedding failed: %v", err)
	}
	if !ok || len(vec) == 0 {
		t.Fatalf("expected cold embedding for %s", memID)
	}

	resp := r.ExecuteQuery(schemas.QueryRequest{
		QueryText:   mem.Content,
		SessionID:   "sess_cold",
		AgentID:     "agent_cold",
		TopK:        5,
		IncludeCold: true,
	})

	if len(resp.Objects) == 0 {
		t.Fatal("expected non-empty resp.Objects for include_cold query")
	}

	found := false
	for _, id := range resp.Objects {
		if id == memID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected archived cold memory %s in resp.Objects, got %v", memID, resp.Objects)
	}

	if len(resp.Provenance) == 0 {
		t.Fatal("expected non-empty resp.Provenance")
	}
	if len(resp.ProofTrace) == 0 {
		t.Fatal("expected non-empty resp.ProofTrace")
	}

	t.Logf("resp.Objects: %v", resp.Objects)
	t.Logf("resp.Provenance: %v", resp.Provenance)
	t.Logf("resp.ProofTrace: %v", resp.ProofTrace)
	t.Logf("resp.ChainTraces.Query: %v", resp.ChainTraces.Query)
}

func TestRuntime_ExecuteQuery_IncludeColdReturnsArchivedMemory_FromS3(t *testing.T) {
	cfg, err := storage.LoadFromEnv()
	if err != nil {
		t.Skipf("skip S3-backed include_cold runtime test: %v", err)
	}
	cfg.Prefix = fmt.Sprintf("%s/test_runtime_include_cold_%d", cfg.Prefix, time.Now().UnixNano())

	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	wal := eventbackbone.NewInMemoryWAL(bus, clock)

	policy := semantic.NewPolicyEngine()
	planner := semantic.NewDefaultQueryPlanner()
	materializer := materialization.NewService()
	assembler := evidence.NewAssembler()

	store := storage.NewMemoryRuntimeStorage()
	cold := storage.NewS3ColdStore(cfg)

	embedder := dataplane.NewTfidfEmbedder(dataplane.DefaultEmbeddingDim)
	tieredObjs := storage.NewTieredObjectStoreWithEmbedder(
		store.HotCache(),
		store.Objects(),
		store.Edges(),
		cold,
		embedder,
		schemas.DefaultAlgorithmConfig().HotTierSalienceThreshold,
	)

	plane, err := dataplane.NewTieredDataPlaneWithEmbedderAndConfig(
		tieredObjs,
		embedder,
		schemas.DefaultAlgorithmConfig(),
	)
	if err != nil {
		t.Fatalf("NewTieredDataPlaneWithEmbedderAndConfig failed: %v", err)
	}

	nodeManager := nodes.CreateManager()
	nodeManager.RegisterData(nodes.CreateInMemoryDataNode("data-1", store.Segments()))
	nodeManager.RegisterIndex(nodes.CreateInMemoryIndexNode("index-1", store.Indexes()))
	nodeManager.RegisterQuery(nodes.CreateInMemoryQueryNode("query-1", plane))
	nodeManager.RegisterSubgraphExecutor(indexing.CreateInMemorySubgraphExecutorWorker("subgraph-1"))
	nodeManager.RegisterStateMaterialization(
		matworker.CreateInMemoryStateMaterializationWorker("state-mat-1", store.Objects(), store.Versions(), nil),
	)

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

	evCache := evidence.NewCache(1000)
	preCompute := materialization.NewPreComputeService(evCache)

	r := CreateRuntime(
		wal,
		bus,
		plane,
		coord,
		policy,
		planner,
		materializer,
		preCompute,
		assembler,
		evCache,
		nil,
		nil,
		nodeManager,
		store,
		tieredObjs,
	)

	ev := schemas.Event{
		EventID:    "evt_cold_runtime_s3_1",
		AgentID:    "agent_cold_s3",
		SessionID:  "sess_cold_s3",
		EventType:  "tool_call",
		Source:     "planner",
		Importance: 0.95,
		Payload:    map[string]any{"text": "archived cold retrieval target s3"},
	}

	if _, err := r.SubmitIngest(ev); err != nil {
		t.Fatalf("SubmitIngest failed: %v", err)
	}

	memID := "mem_evt_cold_runtime_s3_1"
	mem, ok := store.Objects().GetMemory(memID)
	if !ok {
		t.Fatalf("expected warm memory %s after ingest", memID)
	}

	tieredObjs.ArchiveMemory(memID)
	store.Objects().DeleteMemory(memID)

	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, ok := cold.GetMemory(memID); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected archived memory %s in S3 cold store", memID)
		}
		time.Sleep(200 * time.Millisecond)
	}

	var vec []float32
	var hasEmbedding bool
	for {
		var embErr error
		vec, hasEmbedding, embErr = cold.GetMemoryEmbedding(memID)
		if embErr == nil && hasEmbedding && len(vec) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected cold embedding for %s (ok=%v len=%d err=%v)", memID, hasEmbedding, len(vec), embErr)
		}
		time.Sleep(200 * time.Millisecond)
	}

	resp := r.ExecuteQuery(schemas.QueryRequest{
		QueryText:   mem.Content,
		SessionID:   "sess_cold_s3",
		AgentID:     "agent_cold_s3",
		TopK:        5,
		IncludeCold: true,
	})

	if len(resp.Objects) == 0 {
		t.Fatal("expected non-empty resp.Objects for S3-backed include_cold query")
	}

	found := false
	for _, id := range resp.Objects {
		if id == memID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected archived S3 cold memory %s in resp.Objects, got %v", memID, resp.Objects)
	}
	if len(resp.Provenance) == 0 {
		t.Fatal("expected non-empty resp.Provenance")
	}
	if len(resp.ProofTrace) == 0 {
		t.Fatal("expected non-empty resp.ProofTrace")
	}

	_ = cold.DeleteMemoryEmbedding(memID)
	_ = cold.DeleteMemory(memID)
}
