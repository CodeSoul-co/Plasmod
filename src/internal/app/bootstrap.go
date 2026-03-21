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

// BuildServer constructs the HTTP server and a cleanup function that must be
// invoked on shutdown to close Badger when on-disk stores are enabled.
func BuildServer() (*http.Server, func() error, error) {
	addr := os.Getenv("ANDB_HTTP_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}

	// ── Event Backbone ───────────────────────────────────────────────────────
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	wal := eventbackbone.NewInMemoryWAL(bus, clock)
	watermark := eventbackbone.NewWatermarkPublisher(clock, bus)
	derivLog := eventbackbone.NewDerivationLog(clock, bus)
	policyDecLog := eventbackbone.NewPolicyDecisionLog(clock, bus)

	// ── Storage Layer (memory / Badger / hybrid — see STORAGE_BACKEND.md) ────
	bundle, err := storage.BuildRuntimeFromEnv()
	if err != nil {
		return nil, nil, err
	}
	store := bundle.RuntimeStorage
	storageCfg := bundle.Config
	cleanup := bundle.Close

	// ── Semantic Layer ───────────────────────────────────────────────────────
	objectModel := semantic.NewObjectModelRegistry()
	policyEngine := semantic.NewPolicyEngine()
	planner := semantic.NewDefaultQueryPlanner()

	// ── Materialization & Evidence ───────────────────────────────────────────
	evCache := evidence.NewCache(10000)
	materializer := materialization.NewService()
	preCompute := materialization.NewPreComputeService(evCache)
	assembler := evidence.NewCachedAssembler(evCache).WithEdgeStore(store.Edges())

	// ── Data Plane (Tiered: hot → warm → cold) ──────────────────────────────────
	plane := dataplane.NewTieredDataPlane()

	// ── Coordinator Hub ──────────────────────────────────────────────────────
	coord := coordinator.NewCoordinatorHub(
		coordinator.NewSchemaCoordinator(objectModel),
		coordinator.NewObjectCoordinator(store.Objects(), store.Versions()),
		coordinator.NewPolicyCoordinator(policyEngine, store.Policies()),
		coordinator.NewVersionCoordinator(clock, store.Versions()),
		coordinator.NewWorkerScheduler(),
		coordinator.NewMemoryCoordinator(store.Objects()),
		coordinator.NewIndexCoordinator(store.Segments(), store.Indexes()),
		coordinator.NewShardCoordinator(8),
		coordinator.NewQueryCoordinator(planner, policyEngine),
	)

	// ── Module Registry ──────────────────────────────────────────────────────
	coord.Registry.Register("dataplane", plane)
	coord.Registry.Register("policy_engine", policyEngine)
	coord.Registry.Register("query_planner", planner)
	coord.Registry.Register("materializer", materializer)
	coord.Registry.Register("evidence_assembler", assembler)
	coord.Registry.Register("wal", wal)
	coord.Registry.Register("watermark", watermark)
	coord.Registry.Register("derivation_log", derivLog)
	coord.Registry.Register("policy_decision_log", policyDecLog)
	coord.Registry.Register("runtime_storage", store)
	coord.Registry.Register("storage_config", storageCfg)

	// ── Worker Node Manager ──────────────────────────────────────────────────
	nodeManager := nodes.NewManager()
	// Hot tier: dedicated data/index nodes wired to the tiered plane's warm layer
	nodeManager.RegisterData(nodes.NewInMemoryDataNode("data-hot", store.Segments()))
	nodeManager.RegisterIndex(nodes.NewInMemoryIndexNode("index-hot", store.Indexes()))
	nodeManager.RegisterQuery(nodes.NewInMemoryQueryNode("query-1", plane))
	nodeManager.RegisterMemoryExtraction(nodes.NewInMemoryMemoryExtractionWorker("mem-extract-1", store.Objects()))
	nodeManager.RegisterMemoryConsolidation(nodes.NewInMemoryMemoryConsolidationWorker("mem-consolidate-1", store.Objects()))
	nodeManager.RegisterGraphRelation(nodes.NewInMemoryGraphRelationWorker("graph-1", store.Edges()))
	nodeManager.RegisterProofTrace(nodes.NewInMemoryProofTraceWorker("proof-1", store.Edges()))
	coord.Registry.Register("node_manager", nodeManager)

	// ── Module Registry: evidence + pre-compute services ──────────────────────
	coord.Registry.Register("evidence_cache", evCache)
	coord.Registry.Register("pre_compute", preCompute)

	// ── Runtime ──────────────────────────────────────────────────────────────
	runtime := worker.NewRuntime(wal, bus, plane, coord, policyEngine, planner, materializer, preCompute, assembler, nodeManager, store)
	runtime.RegisterDefaults()

	// ── HTTP Gateway ─────────────────────────────────────────────────────────
	gateway := access.NewGateway(coord, runtime, store, storageCfg)
	mux := http.NewServeMux()
	gateway.RegisterRoutes(mux)

	return &http.Server{Addr: addr, Handler: mux}, cleanup, nil
}
