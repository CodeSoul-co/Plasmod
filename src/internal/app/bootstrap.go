package app

import (
	"net/http"
	"os"

	"andb/src/internal/access"
	"andb/src/internal/coordinator"
	"andb/src/internal/dataplane"
	"andb/src/internal/eventbackbone"
	"andb/src/internal/evidence"
	"andb/src/internal/materialization"
	"andb/src/internal/semantic"
	"andb/src/internal/storage"
	"andb/src/internal/worker"
	"andb/src/internal/worker/nodes"
)

func BuildServer() (*http.Server, error) {
	addr := os.Getenv("ANDB_HTTP_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}

	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	wal := eventbackbone.NewInMemoryWAL(bus, clock)

	plane := dataplane.NewSegmentDataPlane()
	objectModel := semantic.NewObjectModelRegistry()
	policyEngine := semantic.NewPolicyEngine()
	planner := semantic.NewDefaultQueryPlanner()
	materializer := materialization.NewService()
	assembler := evidence.NewAssembler()
	store := storage.NewMemoryRuntimeStorage()
	nodeManager := nodes.NewManager()
	nodeManager.RegisterData(nodes.NewInMemoryDataNode("data-1", store.Segments()))
	nodeManager.RegisterIndex(nodes.NewInMemoryIndexNode("index-1", store.Indexes()))
	nodeManager.RegisterQuery(nodes.NewInMemoryQueryNode("query-1", plane))

	coord := coordinator.NewCoordinatorHub(
		coordinator.NewSchemaCoordinator(objectModel),
		coordinator.NewObjectCoordinator(),
		coordinator.NewPolicyCoordinator(policyEngine),
		coordinator.NewVersionCoordinator(clock),
		coordinator.NewWorkerScheduler(),
	)
	coord.Registry.Register("dataplane", plane)
	coord.Registry.Register("policy_engine", policyEngine)
	coord.Registry.Register("query_planner", planner)
	coord.Registry.Register("materializer", materializer)
	coord.Registry.Register("evidence_assembler", assembler)
	coord.Registry.Register("wal", wal)
	coord.Registry.Register("node_manager", nodeManager)
	coord.Registry.Register("runtime_storage", store)

	runtime := worker.NewRuntime(wal, bus, plane, coord, policyEngine, planner, materializer, assembler, nodeManager, store)
	runtime.RegisterDefaults()

	gateway := access.NewGateway(coord, runtime)
	mux := http.NewServeMux()
	gateway.RegisterRoutes(mux)

	return &http.Server{Addr: addr, Handler: mux}, nil
}
