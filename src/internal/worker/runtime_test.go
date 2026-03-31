package worker

import (
	"errors"
	"fmt"
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
	"andb/src/internal/worker/indexing"
	matworker "andb/src/internal/worker/materialization"
	"andb/src/internal/worker/nodes"
)

type failingPlane struct{}

func (f *failingPlane) Ingest(record dataplane.IngestRecord) error { return errors.New("ingest failed") }
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

func TestRuntime_SubmitIngest_RejectsEmbeddingDimMismatch(t *testing.T) {
	r := buildTestRuntime(t)
	_, err := r.SubmitIngest(schemas.Event{
		EventID:   "evt_dim_mismatch_1",
		AgentID:   "agent_dim",
		SessionID: "session_dim",
		Payload: map[string]any{
			"text":          "embedding dim mismatch",
			"embedding_dim": 128, // runtime default tfidf dim is 256
		},
	})
	if err == nil {
		t.Fatal("expected embedding_dim_mismatch error")
	}
	if !strings.Contains(err.Error(), "embedding_dim_mismatch") {
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
