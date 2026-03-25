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
	cognitive "andb/src/internal/worker/cognitive"
	"andb/src/internal/worker/coordination"
	"andb/src/internal/worker/indexing"
	"andb/src/internal/worker/ingestion"
	matworker "andb/src/internal/worker/materialization"
	"andb/src/internal/worker/nodes"
)

// BuildServer constructs and wires all ANDB server components.
// Returns the HTTP server, a cleanup function, and any build error.
// The cleanup function must be called when the server is shutting down;
// it cancels the background worker contexts (EventSubscriber, Orchestrator).
//
// Usage:
//
//	srv, cleanup, err := app.BuildServer()
//	if err != nil { ... }
//	defer cleanup()
//	if err := srv.ListenAndServe(); err != nil { ... }
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
	// BuildRuntimeFromEnv selects the backend based on ANDB_STORAGE env var.
	// Default mode is "memory" (all stores in-process).  Set ANDB_STORAGE=disk
	// for Badger-backed persistent storage under ANDB_DATA_DIR.
	bundle, err := storage.BuildRuntimeFromEnv()
	if err != nil {
		return nil, nil, err
	}
	store := bundle.RuntimeStorage
	storageCfg := bundle.Config
	if storageCfg.BadgerEnabled {
		log.Printf("[bootstrap] storage: Badger enabled (mode=%s, dir=%s)", storageCfg.Mode, storageCfg.DataDir)
	} else {
		log.Printf("[bootstrap] storage: in-memory mode")
	}

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

	// ── Data Plane (Tiered: hot → warm → cold, hybrid vector search) ──────────────
	// TieredDataPlane uses TieredObjectStore for cold queries and cold writes.
	// Warm tier performs hybrid search: lexical (segmentstore.Index) + vector (CGO Knowhere/HNSW)
	// via TF-IDF embedder + RRF fusion.  Gracefully degrades to lexical-only when
	// CGO library is unavailable (auto-detected in VectorStore).
	embedder := dataplane.NewTfidfEmbedder(dataplane.DefaultEmbeddingDim)
	plane, err := dataplane.NewTieredDataPlaneWithEmbedder(tieredObjects, embedder)
	if err != nil {
		return nil, nil, err
	}
	log.Printf("[bootstrap] data plane: hybrid search enabled (dim=%d, TF-IDF embedder)", dataplane.DefaultEmbeddingDim)

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

	// ── Algorithm Dispatch worker ─────────────────────────────────────────────
	// Bridges MemoryManagementAlgorithm plugins into the cognitive pipeline.
	// Uses a no-op default when no custom algorithm is configured.
	nodeManager.RegisterAlgorithmDispatch(cognitive.CreateAlgorithmDispatchWorker(
		"algo-dispatch-1",
		cognitive.NewDefaultAlgorithm(),
		store.Objects(),
		store.AlgorithmStates(),
		store.Audits(),
	))

	coord.Registry.Register("node_manager", nodeManager)

	// ── Module Registry: evidence + pre-compute services ──────────────────────
	coord.Registry.Register("evidence_cache", evCache)
	coord.Registry.Register("pre_compute", preCompute)

	// ── Runtime ──────────────────────────────────────────────────────────────
	runtime := worker.CreateRuntime(wal, bus, plane, coord, policyEngine, planner, materializer, preCompute, assembler, evCache, derivLog, policyDecLog, nodeManager, store, tieredObjects)
	runtime.RegisterDefaults()

	// ── QueryChain (post-retrieval reasoning: ProofTrace + SubgraphExpand) ───
	// Created internally by Runtime; exposed here for discoverability.
	// Internally calls ProofTraceWorker (BFS multi-hop) and SubgraphExecutorWorker
	// (1-hop graph expansion), wiring in the CGO Knowhere/HNSW vector search
	// results produced by the DataPlane via TieredDataPlane.Search.
	coord.Registry.Register("query_chain", runtime.QueryChain())

	// ── Async Worker Subscriber ───────────────────────────────────────────────
	// The subscriber polls the WAL every 200 ms and drives governance workers
	// (ReflectionPolicy, ConflictMerge, MemoryConsolidation) asynchronously.
	// Uses a cancellable context so goroutines can be cleanly stopped on shutdown.
	ctx, cancel := context.WithCancel(context.Background())
	subscriber := worker.CreateEventSubscriber(wal, nodeManager)
	runtime.StartSubscriber(ctx, subscriber)
	coord.Registry.Register("event_subscriber", subscriber)

	// ── Execution Orchestrator ─────────────────────────────────────────────────
	// The orchestrator provides priority-aware task dispatch across the 4 flow
	// chains.  concurrency=4, queueCap=256 per priority level.
	orch := worker.CreateOrchestrator(nodeManager, 4, 256)
	go orch.Run(ctx)
	coord.Registry.Register("orchestrator", orch)

	// ── HTTP Gateway ─────────────────────────────────────────────────────────
	gateway := access.NewGateway(coord, runtime, store, storageCfg)
	mux := http.NewServeMux()
	gateway.RegisterRoutes(mux)

	// shutdown bundles context cancellation (subscriber/orchestrator) and
	// Badger close (storage cleanup) into one cleanup function.
	shutdown := func() error {
		cancel()
		return bundle.Close()
	}
	return &http.Server{Addr: addr, Handler: mux}, shutdown, nil
}
