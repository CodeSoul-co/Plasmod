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
	"andb/src/internal/worker/indexing"
	"andb/src/internal/worker/nodes"
)

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
	tieredObjs := storage.NewTieredObjectStore(store.HotCache(), store.Objects(), storage.NewInMemoryColdStore())
	nodeManager := nodes.CreateManager()
	nodeManager.RegisterData(nodes.CreateInMemoryDataNode("data-1", store.Segments()))
	nodeManager.RegisterIndex(nodes.CreateInMemoryIndexNode("index-1", store.Indexes()))
	nodeManager.RegisterQuery(nodes.CreateInMemoryQueryNode("query-1", plane))
	nodeManager.RegisterSubgraphExecutor(indexing.CreateInMemorySubgraphExecutorWorker("subgraph-1"))
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
	tieredObjs := storage.NewTieredObjectStore(store.HotCache(), store.Objects(), storage.NewInMemoryColdStore())
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
		QueryText:   "subgraph node test",
		WorkspaceID: "default",
		SessionID:   "session_sg",
		TopK:        5,
		ObjectTypes: []string{string(schemas.ObjectTypeMemory)},
	})

	if len(resp.Objects) == 0 {
		t.Fatalf("expected at least one object from query")
	}
	if len(resp.Edges) == 0 {
		t.Fatalf("expected non-empty resp.Edges: materialization derives session/agent edges, " +
			"SubgraphExecutorWorker should surface them via preNodes+preEdges")
	}
}

func TestRuntime_Query_TenantObjectTypeTopK_Combination(t *testing.T) {
	r := buildTestRuntime(t)

	events := []schemas.Event{
		{
			EventID:     "evt_combo_t1_a",
			TenantID:    "tenant_a",
			WorkspaceID: "w_combo",
			AgentID:     "agent_combo",
			SessionID:   "s_combo",
			Payload:     map[string]any{"text": "combo query alpha"},
		},
		{
			EventID:     "evt_combo_t1_b",
			TenantID:    "tenant_a",
			WorkspaceID: "w_combo",
			AgentID:     "agent_combo",
			SessionID:   "s_combo",
			Payload:     map[string]any{"text": "combo query beta"},
		},
		{
			EventID:     "evt_combo_t2_x",
			TenantID:    "tenant_b",
			WorkspaceID: "w_combo",
			AgentID:     "agent_combo",
			SessionID:   "s_combo",
			Payload:     map[string]any{"text": "combo query gamma"},
		},
	}
	for _, ev := range events {
		if _, err := r.SubmitIngest(ev); err != nil {
			t.Fatalf("ingest failed for %s: %v", ev.EventID, err)
		}
	}

	resp := r.ExecuteQuery(schemas.QueryRequest{
		QueryText:   "combo query",
		TenantID:    "tenant_a",
		WorkspaceID: "w_combo",
		SessionID:   "s_combo",
		TopK:        2,
		ObjectTypes: []string{string(schemas.ObjectTypeMemory)},
	})

	if len(resp.Objects) == 0 {
		t.Fatalf("expected non-empty query results")
	}
	if len(resp.Objects) > 2 {
		t.Fatalf("expected top_k cap 2, got %d objects: %v", len(resp.Objects), resp.Objects)
	}
	for _, id := range resp.Objects {
		if !strings.HasPrefix(id, schemas.IDPrefixMemory) {
			t.Fatalf("expected memory-only results, got object id %q", id)
		}
		if strings.Contains(id, "evt_combo_t2_x") {
			t.Fatalf("expected tenant filter to exclude tenant_b object %q", id)
		}
	}
}

func TestRuntime_Query_StateOnly_AfterToolCallIngest(t *testing.T) {
	r := buildTestRuntime(t)

	_, err := r.SubmitIngest(schemas.Event{
		EventID:     "evt_state_toolcall_1",
		EventType:   "tool_call",
		TenantID:    "tenant_state",
		WorkspaceID: "w_state",
		AgentID:     "agent_state",
		SessionID:   "s_state",
		Payload: map[string]any{
			"text":      "tool call state query",
			"state_key": "k1",
			"state_val": "v1",
		},
	})
	if err != nil {
		t.Fatalf("ingest failed: %v", err)
	}

	resp := r.ExecuteQuery(schemas.QueryRequest{
		QueryText:   "tool call state query",
		TenantID:    "tenant_state",
		WorkspaceID: "w_state",
		AgentID:     "agent_state",
		SessionID:   "s_state",
		TopK:        5,
		ObjectTypes: []string{string(schemas.ObjectTypeState)},
	})

	if len(resp.Objects) == 0 {
		t.Fatalf("expected state-only query to return at least one object")
	}
	for _, id := range resp.Objects {
		if !strings.HasPrefix(id, schemas.IDPrefixState) {
			t.Fatalf("expected only state_* objects, got %q", id)
		}
	}
}
