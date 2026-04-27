package app

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"plasmod/src/internal/access"
	"plasmod/src/internal/config"
	"plasmod/src/internal/coordinator"
	"plasmod/src/internal/dataplane"
	"plasmod/src/internal/dataplane/embedding"
	"plasmod/src/internal/eventbackbone"
	"plasmod/src/internal/evidence"
	"plasmod/src/internal/materialization"
	"plasmod/src/internal/schemas"
	"plasmod/src/internal/semantic"
	"plasmod/src/internal/storage"
	"plasmod/src/internal/transport"
	"plasmod/src/internal/worker"
	cognitive "plasmod/src/internal/worker/cognitive"
	baseline "plasmod/src/internal/worker/cognitive/baseline"
	"plasmod/src/internal/worker/cognitive/memorybank"
	"plasmod/src/internal/worker/coordination"
	"plasmod/src/internal/worker/indexing"
	"plasmod/src/internal/worker/ingestion"
	matworker "plasmod/src/internal/worker/materialization"
	"plasmod/src/internal/worker/nodes"
)

// BuildServer constructs and wires all Plasmod server components.
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
	addr := os.Getenv("PLASMOD_HTTP_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}

	// ── Vector-only mode (Baseline 1) ────────────────────────────────────────
	// When PLASMOD_VECTOR_ONLY_MODE=true, disable graph expansion, policy
	// enforcement, and provenance tracking to create a pure vector-search baseline.
	vectorOnlyMode := os.Getenv("PLASMOD_VECTOR_ONLY_MODE") == "true"
	if vectorOnlyMode {
		log.Printf("[bootstrap] VECTOR-ONLY MODE enabled (baseline: no graph/policy/provenance)")
	}

	// ── Event Backbone ───────────────────────────────────────────────────────
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	watermark := eventbackbone.NewWatermarkPublisher(clock, bus)

	// ── Storage Layer (memory / Badger / hybrid — see STORAGE_BACKEND.md) ────
	// BuildRuntimeFromEnv selects the backend based on PLASMOD_STORAGE env var.
	// Default mode is "memory" (all stores in-process).  Set PLASMOD_STORAGE=disk
	// for Badger-backed persistent storage under PLASMOD_DATA_DIR.
	bundle, err := storage.BuildRuntimeFromEnv()
	if err != nil {
		return nil, nil, err
	}
	store := bundle.RuntimeStorage
	storageCfg := bundle.Config
	var wal eventbackbone.WAL
	if storageCfg != nil && storageCfg.WALPersistence {
		wal = eventbackbone.NewFileWAL(filepath.Join(storageCfg.DataDir, "wal.log"), bus, clock)
		log.Printf("[bootstrap] wal: file-backed (%s)", filepath.Join(storageCfg.DataDir, "wal.log"))
	} else {
		wal = eventbackbone.NewInMemoryWAL(bus, clock)
		log.Printf("[bootstrap] wal: in-memory mode")
	}
	derivStore := eventbackbone.NewFileDerivationStore(filepath.Join(storageCfg.DataDir, "derivation.log"))
	derivLog := eventbackbone.NewDerivationLogWithStore(clock, bus, derivStore)
	policyDecLog := eventbackbone.NewPolicyDecisionLog(clock, bus)
	if storageCfg.BadgerEnabled {
		log.Printf("[bootstrap] storage: Badger enabled (mode=%s, dir=%s)", storageCfg.Mode, storageCfg.DataDir)
	} else {
		log.Printf("[bootstrap] storage: in-memory mode")
	}

	// ── Algorithm Config — all tunable worker parameters ─────────────────────
	// Defaults are in schemas.DefaultAlgorithmConfig().  Environment variables
	// override specific fields when set:
	//   PLASMOD_EVIDENCE_CACHE_SIZE   (default 10000)
	//   PLASMOD_MAX_PROOF_DEPTH       (default 8)
	//   PLASMOD_HOT_TIER_THRESHOLD    (default 0.5)
	algoCfg, err := config.LoadSharedAlgorithmConfig()
	if err != nil {
		log.Printf("[bootstrap] shared algorithm config load failed, using defaults: %v", err)
		algoCfg = schemas.DefaultAlgorithmConfig()
	}

	// ── Cold-tier selection: S3 if env vars present, otherwise in-memory sim ──
	// Set S3_ENDPOINT, S3_ACCESS_KEY, S3_SECRET_KEY, S3_BUCKET to enable S3.
	var coldStore storage.ColdObjectStore
	if s3Cfg, loadErr := storage.LoadFromEnv(); loadErr == nil {
		coldStore = storage.NewS3ColdStoreWithAlgorithmConfig(s3Cfg, algoCfg)
		log.Printf("[bootstrap] cold store: S3 endpoint=%s bucket=%s prefix=%s",
			s3Cfg.Endpoint, s3Cfg.Bucket, s3Cfg.Prefix)
	} else {
		coldStore = storage.NewInMemoryColdStore()
		log.Printf("[bootstrap] cold store: in-memory simulation (S3 not configured: %v)", loadErr)
	}
	tieredObjects := storage.NewTieredObjectStoreWithThreshold(
		store.HotCache(),
		store.Objects(),
		store.Edges(),
		coldStore,
		algoCfg.HotTierSalienceThreshold,
	)

	// ── Semantic Layer ───────────────────────────────────────────────────────
	objectModel := semantic.NewObjectModelRegistry()
	policyEngine := semantic.NewPolicyEngine()
	planner := semantic.NewDefaultQueryPlanner()

	if sz := os.Getenv("PLASMOD_EVIDENCE_CACHE_SIZE"); sz != "" {
		if n, parseErr := strconv.Atoi(sz); parseErr == nil && n > 0 {
			algoCfg.EvidenceCacheSize = n
		}
	}
	if d := os.Getenv("PLASMOD_MAX_PROOF_DEPTH"); d != "" {
		if n, parseErr := strconv.Atoi(d); parseErr == nil && n > 0 {
			algoCfg.MaxProofDepth = n
		}
	}
	if t := os.Getenv("PLASMOD_HOT_TIER_THRESHOLD"); t != "" {
		if f, parseErr := strconv.ParseFloat(t, 64); parseErr == nil && f > 0 {
			algoCfg.HotTierSalienceThreshold = f
		}
	}

	// ── Materialization & Evidence ───────────────────────────────────────────
	evCache := evidence.NewCache(algoCfg.EvidenceCacheSize)
	materializer := materialization.NewService()
	preCompute := materialization.NewPreComputeServiceWithConfig(evCache, algoCfg)
	assembler := evidence.NewCachedAssembler(evCache).
		WithEdgeStore(store.Edges()).
		WithVersionStore(store.Versions()).
		WithObjectStore(store.Objects()).
		WithPolicyStore(store.Policies())

	// ── Data Plane (Tiered: hot → warm → cold, hybrid vector search) ──────────────
	// TieredDataPlane uses TieredObjectStore for cold queries and cold writes.
	// Warm tier performs hybrid search: lexical (segmentstore.Index) + vector (CGO Knowhere/HNSW)
	// via an EmbeddingGenerator + RRF fusion.
	//
	// Embedder selection (set PLASMOD_EMBEDDER):
	//   tfidf  (default)  — pure-Go word-hashed TF-IDF, no external dependency
	//   openai           — OpenAI-compatible HTTP API (Ollama, local server, Azure OpenAI)
	//   zhipuai          — ZhipuAI / 智谱AI (api-key auth, OpenAI-compatible schema)
	//   cohere           — Cohere /v2/embed API
	//
	// When PLASMOD_EMBEDDER is "openai" or "zhipuai", also set:
	//   PLASMOD_EMBEDDER_BASE_URL   (defaults per provider)
	//   PLASMOD_EMBEDDER_MODEL      (e.g. text-embedding-3-small, embedding-3)
	//   PLASMOD_EMBEDDER_API_KEY
	//   PLASMOD_EMBEDDER_DIM        (expected vector dimension; 0 = skip probe)
	//   PLASMOD_EMBEDDER_TIMEOUT    (per-request timeout in seconds; default 30)
	//   PLASMOD_EMBEDDER_BATCH_SIZE (inputs per HTTP request; default 100)
	// Optional:
	//   PLASMOD_EMBEDDING_FAMILY    (override family label used in segment metadata)
	var embedder embedding.Generator
	var embedderDim int
	var embedderErr error
	embedderType := os.Getenv("PLASMOD_EMBEDDER")
	if embedderType == "" {
		embedderType = "tfidf"
	}
	switch embedderType {
	case "openai", "zhipuai":
		baseURL := os.Getenv("PLASMOD_EMBEDDER_BASE_URL")
		model := os.Getenv("PLASMOD_EMBEDDER_MODEL")
		apiKey := os.Getenv("PLASMOD_EMBEDDER_API_KEY")
		if dimStr := os.Getenv("PLASMOD_EMBEDDER_DIM"); dimStr != "" {
			if n, parseErr := strconv.Atoi(dimStr); parseErr == nil {
				embedderDim = n
			}
		}
		timeoutSec := 30
		if ts := os.Getenv("PLASMOD_EMBEDDER_TIMEOUT"); ts != "" {
			if n, parseErr := strconv.Atoi(ts); parseErr == nil {
				timeoutSec = n
			}
		}
		batchSize := 100
		if bs := os.Getenv("PLASMOD_EMBEDDER_BATCH_SIZE"); bs != "" {
			if n, parseErr := strconv.Atoi(bs); parseErr == nil {
				batchSize = n
			}
		}
		cfg := embedding.OpenAIConfig{
			BaseURL:   baseURL,
			Model:     model,
			APIKey:    apiKey,
			Timeout:   time.Duration(timeoutSec) * time.Second,
			BatchSize: batchSize,
		}
		ctx := context.Background()
		embedder, embedderErr = embedding.NewOpenAI(ctx, cfg, embedderDim)
		if embedderErr != nil {
			return nil, nil, fmt.Errorf("failed to initialize %s embedder: %w", embedderType, embedderErr)
		}
		log.Printf("[bootstrap] embedder: %s model=%s dim=%d", embedderType, model, embedderDim)
	case "cohere":
		model := os.Getenv("PLASMOD_EMBEDDER_MODEL")
		if model == "" {
			model = "embed-english-v3.0"
		}
		if dimStr := os.Getenv("PLASMOD_EMBEDDER_DIM"); dimStr != "" {
			if n, parseErr := strconv.Atoi(dimStr); parseErr == nil {
				embedderDim = n
			}
		}
		if embedderDim <= 0 {
			return nil, nil, fmt.Errorf("PLASMOD_EMBEDDER_DIM is required for Cohere (e.g. 1024)")
		}
		apiKey := os.Getenv("PLASMOD_EMBEDDER_API_KEY")
		embedder, embedderErr = embedding.NewCohere(context.Background(), apiKey, model, embedderDim)
		if embedderErr != nil {
			return nil, nil, fmt.Errorf("failed to initialize Cohere embedder: %w", embedderErr)
		}
		log.Printf("[bootstrap] embedder: cohere model=%s dim=%d", model, embedderDim)
	case "huggingface":
		model := os.Getenv("PLASMOD_EMBEDDER_MODEL")
		if model == "" {
			model = "sentence-transformers/all-MiniLM-L6-v2"
		}
		if dimStr := os.Getenv("PLASMOD_EMBEDDER_DIM"); dimStr != "" {
			if n, parseErr := strconv.Atoi(dimStr); parseErr == nil {
				embedderDim = n
			}
		}
		apiKey := os.Getenv("PLASMOD_EMBEDDER_API_KEY")
		timeoutSec := 60
		if ts := os.Getenv("PLASMOD_EMBEDDER_TIMEOUT"); ts != "" {
			if n, parseErr := strconv.Atoi(ts); parseErr == nil {
				timeoutSec = n
			}
		}
		hfEmbedder, hfErr := embedding.NewHuggingFace(context.Background(), embedding.HuggingFaceConfig{
			Model:        model,
			APIKey:       apiKey,
			Timeout:      time.Duration(timeoutSec) * time.Second,
			WaitForModel: true,
		}, embedderDim)
		if hfErr != nil {
			return nil, nil, fmt.Errorf("failed to initialize HuggingFace embedder: %w", hfErr)
		}
		embedder = hfEmbedder
		if embedderDim <= 0 {
			embedderDim = hfEmbedder.Dim()
		}
		log.Printf("[bootstrap] embedder: huggingface model=%s dim=%d", model, embedderDim)
	case "vertexai":
		if dimStr := os.Getenv("PLASMOD_EMBEDDER_DIM"); dimStr != "" {
			if n, parseErr := strconv.Atoi(dimStr); parseErr == nil {
				embedderDim = n
			}
		}
		vaEmbedder, vaErr := embedding.NewVertexAIFromEnv(context.Background(), embedderDim)
		if vaErr != nil {
			return nil, nil, fmt.Errorf("failed to initialize VertexAI embedder: %w", vaErr)
		}
		embedder = vaEmbedder
		if embedderDim <= 0 {
			embedderDim = vaEmbedder.Dim()
		}
		log.Printf("[bootstrap] embedder: vertexai project=%s dim=%d",
			os.Getenv("GOOGLE_CLOUD_PROJECT"), embedderDim)
	case "onnx":
		if dimStr := os.Getenv("PLASMOD_EMBEDDER_DIM"); dimStr != "" {
			if n, parseErr := strconv.Atoi(dimStr); parseErr == nil && n > 0 {
				embedderDim = n
			}
		}
		if embedderDim <= 0 {
			embedderDim = 384 // all-MiniLM-L6-v2
		}
		onnxEmbedder, onnxErr := embedding.NewOnnxFromEnv(context.Background(), embedderDim)
		if onnxErr != nil {
			return nil, nil, fmt.Errorf("failed to initialize ONNX embedder: %w", onnxErr)
		}
		embedder = onnxEmbedder
		log.Printf("[bootstrap] embedder: onnx model=%s dim=%d",
			os.Getenv("PLASMOD_EMBEDDER_MODEL_PATH"), embedderDim)
	case "tensorrt":
		if dimStr := os.Getenv("PLASMOD_EMBEDDER_DIM"); dimStr != "" {
			if n, parseErr := strconv.Atoi(dimStr); parseErr == nil {
				embedderDim = n
			}
		}
		trtEmbedder, trtErr := embedding.NewTensorRTFromEnv(context.Background(), embedderDim)
		if trtErr != nil {
			return nil, nil, fmt.Errorf("failed to initialize TensorRT embedder: %w", trtErr)
		}
		embedder = trtEmbedder
		if embedderDim <= 0 {
			embedderDim = trtEmbedder.Dim()
		}
		log.Printf("[bootstrap] embedder: tensorrt engine=%s dim=%d",
			os.Getenv("PLASMOD_EMBEDDER_MODEL_PATH"), embedderDim)
	default:
		embedder = embedding.NewTfidf(dataplane.DefaultEmbeddingDim)
		embedderDim = dataplane.DefaultEmbeddingDim
		log.Printf("[bootstrap] embedder: tfidf (pure-Go, dim=%d)", embedderDim)
	}
	// Wire embedder into tieredObjects so ArchiveMemory writes cold-tier embeddings.
	// tieredObjects was constructed before the embedder was known; SetEmbedder patches it in.
	tieredObjects.SetEmbedder(embedder)

	plane, err := dataplane.NewTieredDataPlaneWithEmbedderAndConfig(tieredObjects, embedder, algoCfg)
	if err != nil {
		return nil, nil, err
	}
	embeddingFamily := storage.ResolveEmbeddingFamily(nil)
	log.Printf("[bootstrap] data plane: hybrid search enabled (provider=%s dim=%d)",
		embedder.Provider(), embedderDim)
	log.Printf("[bootstrap] embedding family: %s", embeddingFamily)

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
	nodeManager.RegisterMemoryExtraction(baseline.CreateInMemoryMemoryExtractionWorker("mem-extract-1", store.Objects(), derivLog))
	nodeManager.RegisterMemoryConsolidation(baseline.CreateInMemoryMemoryConsolidationWorker("mem-consolidate-1", store.Objects(), derivLog))
	nodeManager.RegisterGraphRelation(indexing.CreateInMemoryGraphRelationWorker("graph-1", store.Edges()))
	nodeManager.RegisterProofTrace(coordination.CreateInMemoryProofTraceWorker("proof-1", store.Edges(), derivLog))
	nodeManager.RegisterReflectionPolicy(baseline.CreateInMemoryReflectionPolicyWorker(
		"reflect-1",
		store.Objects(),
		store.Policies(),
		policyDecLog,
	).WithTieredObjects(tieredObjects))
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
		store.Edges(),
		store.Versions(),
		derivLog,
	))
	nodeManager.RegisterStateMaterialization(matworker.CreateInMemoryStateMaterializationWorker(
		"state-mat-1",
		store.Objects(),
		store.Versions(),
		derivLog,
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
	nodeManager.RegisterSummarization(baseline.CreateInMemorySummarizationWorker("summarize-1", store.Objects(), derivLog))

	// ── Algorithm Dispatch worker ─────────────────────────────────────────────
	// Bridges MemoryManagementAlgorithm plugins into the cognitive pipeline.
	// Active algorithm is selected by PLASMOD_ACTIVE_ALGORITHM (memorybank|zep|baseline).
	// Note: zep profile currently reuses the MemoryBank engine with zep config root.
	activeAlgo := strings.ToLower(strings.TrimSpace(os.Getenv("PLASMOD_ACTIVE_ALGORITHM")))
	if activeAlgo == "" {
		activeAlgo = "memorybank"
	}
	activeAlgoID := "memorybank_v1"
	var activeAlgoImpl schemas.MemoryManagementAlgorithm
	switch activeAlgo {
	case "baseline":
		activeAlgoID = "baseline_v1"
		activeAlgoImpl = baseline.NewDefault()
	case "zep":
		activeAlgoID = "zep_v1"
		activeAlgoImpl = memorybank.NewDefault(activeAlgoID)
	default:
		activeAlgo = "memorybank"
		activeAlgoID = "memorybank_v1"
		activeAlgoImpl = memorybank.NewDefault(activeAlgoID)
	}
	log.Printf("[bootstrap] active algorithm profile: %s (%s)", activeAlgo, activeAlgoID)

	nodeManager.RegisterAlgorithmDispatch(cognitive.CreateAlgorithmDispatchWorker(
		"algo-dispatch-1",
		activeAlgoImpl,
		store.Objects(),
		store.AlgorithmStates(),
		store.Audits(),
	))
	// Baseline algorithm registered as a second dispatch worker (fallback / comparison).
	nodeManager.RegisterAlgorithmDispatch(cognitive.CreateAlgorithmDispatchWorker(
		"algo-dispatch-baseline",
		baseline.NewDefault(),
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
	runtime.VectorOnlyMode = vectorOnlyMode
	runtime.RegisterDefaults()
	if err := runtime.AdminWarmPrebuild(); err != nil {
		log.Printf("[bootstrap] warm prebuild skipped: %v", err)
	} else {
		log.Printf("[bootstrap] warm prebuild: segment=warm.default")
	}

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
	runtime.StartFlushLoop(ctx)
	coord.Registry.Register("event_subscriber", subscriber)

	// ── Execution Orchestrator ─────────────────────────────────────────────────
	// The orchestrator provides priority-aware task dispatch across the 4 flow
	// chains.  concurrency=4, queueCap=256 per priority level.
	orch := worker.CreateOrchestrator(nodeManager, 4, 256)
	go orch.Run(ctx)
	coord.Registry.Register("orchestrator", orch)

	// ── HTTP Gateway ─────────────────────────────────────────────────────────
	gateway := access.NewGateway(coord, runtime, store, storageCfg, bundle)
	gatewayMux := http.NewServeMux()
	gateway.RegisterRoutes(gatewayMux)
	wrapped := access.WrapVisibility(access.WrapAdminAuth(gatewayMux))

	// Internal high-throughput transport: binary batch ingest/query and
	// SSE WAL streaming.  Mounted on the root mux so the visibility wrapper
	// (which buffers full responses) does not interfere with streaming or
	// binary payloads.
	mux := http.NewServeMux()
	transport.NewServer(runtime, wal, bus).RegisterRoutes(mux)
	mux.Handle("/", wrapped)
	handler := mux

	// shutdown bundles context cancellation (subscriber/orchestrator) and
	// Badger close (storage cleanup) into one cleanup function.
	shutdown := func() error {
		cancel()
		return bundle.Close()
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadTimeout:       30 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}
	return srv, shutdown, nil
}
