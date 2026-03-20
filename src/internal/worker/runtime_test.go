package worker

import (
	"testing"

	"andb/src/internal/coordinator"
	"andb/src/internal/dataplane"
	"andb/src/internal/eventbackbone"
	"andb/src/internal/evidence"
	"andb/src/internal/materialization"
	"andb/src/internal/schemas"
	"andb/src/internal/semantic"
	"andb/src/internal/storage"
	"andb/src/internal/worker/nodes"
)

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
	r := CreateRuntime(wal, bus, plane, coord, policy, planner, materializer, preCompute, assembler, nodeManager, store)

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
		QueryText:  "hello",
		QueryScope: "w1",
		TopK:       5,
		SessionID:  "s1",
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
