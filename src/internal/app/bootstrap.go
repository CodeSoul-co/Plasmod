package app

import (
	"context"
	"log"
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
	baseline "andb/src/internal/worker/cognitive/baseline"
	"andb/src/internal/worker/coordination"
	"andb/src/internal/worker/indexing"
	"andb/src/internal/worker/ingestion"
	matworker "andb/src/internal/worker/materialization"
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

	// ── Cold-tier selection: S3 if env vars present, otherwise in-memory sim ──
	// Set S3_ENDPOINT, S3_ACCESS_KEY, S3_SECRET_KEY, S3_BUCKET to enable S3.
	var coldStore storage.ColdObjectStore
	if s3Cfg, err := storage.LoadFromEnv(); err == nil {
		coldStore = storage.NewS3ColdStore(s3Cfg)
		log.Printf("[bootstrap] cold store: S3 endpoint=%s bucket=%s prefix=%s",
			s3Cfg.Endpoint, s3Cfg.Bucket, s3Cfg.Prefix)
	} else {
		coldStore = storage.NewInMemoryColdStore()
		log.Printf("[bootstrap] cold store: in-memory simulation (S3 not configured: %v)", err)
	}
	tieredObjects := storage.NewTieredObjectStore(store.HotCache(), store.Objects(), coldStore)

	// ── Semantic Layer ───────────────────────────────────────────────────────
	objectModel := semantic.NewObjectModelRegistry()
	policyEngine := semantic.NewPolicyEngine()
	planner := semantic.NewDefaultQueryPlanner()

	// ── Materialization & Evidence ───────────────────────────────────────────
	evCache := evidence.NewCache(10000)
	materializer := materialization.NewService()
	preCompute := materialization.NewPreComputeService(evCache)
	assembler := evidence.NewCachedAssembler(evCache).
		WithEdgeStore(store.Edges()).
		WithVersionStore(store.Versions()).
		WithObjectStore(store.Objects()).
		WithPolicyStore(store.Policies())

	// ── Data Plane (Tiered: hot → warm → cold) ──────────────────────────────────
	// TieredDataPlane now uses TieredObjectStore for cold queries and cold writes.
	// This connects the cold tier (S3 or in-memory) into the active query path.
	plane := dataplane.NewTieredDataPlane(tieredObjects)

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
	coord.Registry.Register("tiered_objects", tieredObjects)

	// ── Worker Node Manager ──────────────────────────────────────────────────
	nodeManager := nodes.CreateManager()
	// Hot tier: dedicated data/index nodes wired to the tiered plane's warm layer
	nodeManager.RegisterData(nodes.CreateInMemoryDataNode("data-hot", store.Segments()))
	nodeManager.RegisterIndex(nodes.CreateInMemoryIndexNode("index-hot", store.Indexes()))
	nodeManager.RegisterQuery(nodes.CreateInMemoryQueryNode("query-1", plane))
	nodeManager.RegisterMemoryExtraction(baseline.CreateInMemoryMemoryExtractionWorker("mem-extract-1", store.Objects()))
	nodeManager.RegisterMemoryConsolidation(baseline.CreateInMemoryMemoryConsolidationWorker("mem-consolidate-1", store.Objects()))
	nodeManager.RegisterGraphRelation(indexing.CreateInMemoryGraphRelationWorker("graph-1", store.Edges()))
	nodeManager.RegisterProofTrace(coordination.CreateInMemoryProofTraceWorker("proof-1", store.Edges(), derivLog))
	nodeManager.RegisterReflectionPolicy(baseline.CreateInMemoryReflectionPolicyWorker(
		"reflect-1",
		store.Objects(),
		store.Policies(),
		policyDecLog,
	))
	nodeManager.RegisterConflictMerge(coordination.CreateInMemoryConflictMergeWorker(
		"conflict-merge-1",
		store.Objects(),
		store.Edges(),
	))

	// ── Ingestion & Materialization workers ───────────────────────────────────
	nodeManager.RegisterIngest(ingestion.CreateInMemoryIngestWorker("ingest-1"))
	nodeManager.RegisterObjectMaterialization(matworker.CreateInMemoryObjectMaterializationWorker(
		"obj-mat-1",
		store.Objects(),
		store.Versions(),
	))
	nodeManager.RegisterStateMaterialization(matworker.CreateInMemoryStateMaterializationWorker(
		"state-mat-1",
		store.Objects(),
		store.Versions(),
	))
	nodeManager.RegisterToolTrace(matworker.CreateInMemoryToolTraceWorker("tool-trace-1", store.Objects(), derivLog))

	// ── Index & Retrieval workers ─────────────────────────────────────────────
	nodeManager.RegisterIndexBuild(indexing.CreateInMemoryIndexBuildWorker(
		"idx-build-1",
		store.Segments(),
		store.Indexes(),
	))
	nodeManager.RegisterSubgraphExecutor(indexing.CreateInMemorySubgraphExecutorWorker("subgraph-1"))

	// ── Multi-Agent Coordination workers ─────────────────────────────────────
	nodeManager.RegisterCommunication(coordination.CreateInMemoryCommunicationWorker("comm-1", store.Objects()))
	nodeManager.RegisterMicroBatch(coordination.CreateInMemoryMicroBatchScheduler("micro-batch-1", 64))

	// ── Cognitive Compression workers ─────────────────────────────────────────
	nodeManager.RegisterSummarization(baseline.CreateInMemorySummarizationWorker("summarize-1", store.Objects()))

	coord.Registry.Register("node_manager", nodeManager)

	// ── Module Registry: evidence + pre-compute services ──────────────────────
	coord.Registry.Register("evidence_cache", evCache)
	coord.Registry.Register("pre_compute", preCompute)

	// ── Runtime ──────────────────────────────────────────────────────────────
	runtime := worker.CreateRuntime(wal, bus, plane, coord, policyEngine, planner, materializer, preCompute, assembler, nodeManager, store, tieredObjects)
	coord.Registry.Register("ingest_worker", runtime.IngestWorker())
	runtime.RegisterDefaults()

	// ── Async Worker Subscriber ───────────────────────────────────────────────
	// The subscriber polls the WAL every 200 ms and drives governance workers
	// (ReflectionPolicy, ConflictMerge, MemoryConsolidation) asynchronously.
	// It is tied to the process lifetime via context.Background(); for graceful
	// shutdown, replace with a cancellable context from the caller.
	subscriber := worker.CreateEventSubscriber(wal, nodeManager)
	runtime.StartSubscriber(context.Background(), subscriber)
	coord.Registry.Register("event_subscriber", subscriber)

	// ── Execution Orchestrator ─────────────────────────────────────────────────
	// The orchestrator provides priority-aware task dispatch across the 4 flow
	// chains.  concurrency=4, queueCap=256 per priority level.
	orch := worker.CreateOrchestrator(nodeManager, 4, 256)
	go orch.Run(context.Background())
	coord.Registry.Register("orchestrator", orch)

	// ── HTTP Gateway ─────────────────────────────────────────────────────────
	gateway := access.NewGateway(coord, runtime, store, storageCfg)
	mux := http.NewServeMux()
	gateway.RegisterRoutes(mux)

	return &http.Server{Addr: addr, Handler: mux}, cleanup, nil
}
