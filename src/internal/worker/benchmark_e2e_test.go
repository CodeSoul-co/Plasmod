package worker

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"andb/src/internal/coordinator"
	"andb/src/internal/dataplane"
	"andb/src/internal/eventbackbone"
	"andb/src/internal/evidence"
	"andb/src/internal/materialization"
	"andb/src/internal/schemas"
	"andb/src/internal/semantic"
	"andb/src/internal/storage"
	"andb/src/internal/worker/chain"
	baseline "andb/src/internal/worker/cognitive/baseline"
	"andb/src/internal/worker/coordination"
	"andb/src/internal/worker/nodes"
)

func buildColdQueryRuntime(t testing.TB) (*Runtime, *storage.TieredObjectStore, *storage.MemoryRuntimeStorage) {
	t.Helper()

	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	wal := eventbackbone.NewInMemoryWAL(bus, clock)
	policy := semantic.NewPolicyEngine()
	planner := semantic.NewDefaultQueryPlanner()
	materializer := materialization.NewService()
	assembler := evidence.NewAssembler()
	store := storage.NewMemoryRuntimeStorage()
	cold := storage.NewInMemoryColdStore()
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

	derivLog := eventbackbone.NewDerivationLog(clock, bus)
	nodeManager := nodes.CreateManager()
	nodeManager.RegisterData(nodes.CreateInMemoryDataNode("data-1", store.Segments()))
	nodeManager.RegisterIndex(nodes.CreateInMemoryIndexNode("index-1", store.Indexes()))
	nodeManager.RegisterQuery(nodes.CreateInMemoryQueryNode("query-1", plane))
	nodeManager.RegisterProofTrace(coordination.CreateInMemoryProofTraceWorker("proof-cold-bench", store.Edges(), derivLog))
	nodeManager.RegisterIndexBuild(nil)

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
		wal, bus, plane, coord, policy, planner, materializer, preCompute,
		assembler, evCache, derivLog, nil, nodeManager, store, tieredObjs,
	)
	return r, tieredObjs, store
}

func seedArchivedColdMemories(t testing.TB, tieredObjs *storage.TieredObjectStore, store *storage.MemoryRuntimeStorage, totalN, targetN int, content string) map[string]struct{} {
	t.Helper()
	targetIDs := make(map[string]struct{}, targetN)
	for i := 0; i < totalN; i++ {
		id := fmt.Sprintf("mem_rt_cold_%05d", i)
		mem := schemas.Memory{
			MemoryID:   id,
			AgentID:    "agent-cold-bench",
			SessionID:  "session-cold-bench",
			Content:    content,
			MemoryType: string(schemas.MemoryTypeEpisodic),
			Version:    int64(i + 1),
			IsActive:   true,
		}
		if i < targetN {
			mem.Content = content + " target"
			targetIDs[id] = struct{}{}
		}
		tieredObjs.PutMemory(mem, 0.8)
		tieredObjs.ArchiveMemory(id)
		store.Objects().DeleteMemory(id)
	}
	return targetIDs
}

// BenchmarkQueryChain_E2E measures end-to-end latency and QPS for the full
// QueryChain pipeline including:
//   - TieredDataPlane.Search (lexical + vector)
//   - ObjectStore metadata fetch
//   - SafetyFilter governance rules
//   - RRF reranking
//   - ProofTrace BFS
//   - Subgraph expansion
//
// Compare with BenchmarkHNSW_DirectSearch in dataplane/benchmark_test.go
func BenchmarkQueryChain_E2E(b *testing.B) {
	// Bootstrap runtime components
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

	derivLog := eventbackbone.NewDerivationLog(clock, bus)
	nodeManager := nodes.CreateManager()
	nodeManager.RegisterData(nodes.CreateInMemoryDataNode("data-1", store.Segments()))
	nodeManager.RegisterIndex(nodes.CreateInMemoryIndexNode("index-1", store.Indexes()))
	nodeManager.RegisterQuery(nodes.CreateInMemoryQueryNode("query-1", plane))
	nodeManager.RegisterMemoryExtraction(nil)
	nodeManager.RegisterGraphRelation(nil)
	nodeManager.RegisterProofTrace(coordination.CreateInMemoryProofTraceWorker("proof-bench", store.Edges(), derivLog))
	nodeManager.RegisterIndexBuild(nil)

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
		wal, bus, plane, coord, policy, planner, materializer, preCompute,
		assembler, evCache, derivLog, nil, nodeManager, store, tieredObjs,
	)

	// Ingest test data
	numDocs := 1000
	for i := 0; i < numDocs; i++ {
		r.SubmitIngest(schemas.Event{
			EventID:     fmt.Sprintf("evt_%d", i),
			TenantID:    "tenant-bench",
			WorkspaceID: "ws-bench",
			AgentID:     "agent-bench",
			SessionID:   "session-bench",
			EventType:   "user_message",
			Source:      "user",
			Importance:  0.5 + float64(i%5)*0.1,
			Payload:     map[string]any{"text": fmt.Sprintf("Document %d about machine learning and neural networks", i)},
		})
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		resp := r.ExecuteQuery(schemas.QueryRequest{
			QueryText:   "machine learning neural networks",
			QueryScope:  "workspace",
			WorkspaceID: "ws-bench",
			AgentID:     "agent-bench",
			SessionID:   "session-bench",
			TopK:        10,
			ObjectTypes: []string{"memory"},
		})
		_ = resp
	}
}

// TestQueryChain_E2E_Latency measures and reports detailed latency breakdown
// for the QueryChain pipeline.
func TestQueryChain_E2E_Latency(t *testing.T) {
	// Bootstrap runtime components
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

	derivLog := eventbackbone.NewDerivationLog(clock, bus)
	nodeManager := nodes.CreateManager()
	nodeManager.RegisterData(nodes.CreateInMemoryDataNode("data-1", store.Segments()))
	nodeManager.RegisterIndex(nodes.CreateInMemoryIndexNode("index-1", store.Indexes()))
	nodeManager.RegisterQuery(nodes.CreateInMemoryQueryNode("query-1", plane))
	nodeManager.RegisterMemoryExtraction(baseline.CreateInMemoryMemoryExtractionWorker("extract-1", store.Objects(), derivLog))
	nodeManager.RegisterGraphRelation(nil)
	nodeManager.RegisterProofTrace(coordination.CreateInMemoryProofTraceWorker("proof-latency", store.Edges(), derivLog))
	nodeManager.RegisterIndexBuild(nil)

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
		wal, bus, plane, coord, policy, planner, materializer, preCompute,
		assembler, evCache, derivLog, nil, nodeManager, store, tieredObjs,
	)

	// Ingest test data
	numDocs := 1000
	t.Logf("Ingesting %d documents...", numDocs)
	ingestStart := time.Now()
	for i := 0; i < numDocs; i++ {
		r.SubmitIngest(schemas.Event{
			EventID:     fmt.Sprintf("evt_%d", i),
			TenantID:    "tenant-latency",
			WorkspaceID: "ws-latency",
			AgentID:     "agent-latency",
			SessionID:   "session-latency",
			EventType:   "user_message",
			Source:      "user",
			Importance:  0.5 + float64(i%5)*0.1,
			Payload:     map[string]any{"text": fmt.Sprintf("Document %d about machine learning and neural networks", i)},
		})
	}
	ingestTime := time.Since(ingestStart)
	t.Logf("Ingest time: %v (%.1f docs/sec)", ingestTime, float64(numDocs)/ingestTime.Seconds())

	// Warm up
	for i := 0; i < 10; i++ {
		r.ExecuteQuery(schemas.QueryRequest{
			QueryText:   "machine learning",
			QueryScope:  "workspace",
			WorkspaceID: "ws-latency",
			AgentID:     "agent-latency",
			SessionID:   "session-latency",
			TopK:        10,
			ObjectTypes: []string{"memory"},
		})
	}

	// Measure latency
	numQueries := 100
	var totalLatency time.Duration
	var minLatency, maxLatency time.Duration = time.Hour, 0

	t.Logf("Running %d queries...", numQueries)
	for i := 0; i < numQueries; i++ {
		start := time.Now()
		resp := r.ExecuteQuery(schemas.QueryRequest{
			QueryText:   "machine learning neural networks",
			QueryScope:  "workspace",
			WorkspaceID: "ws-latency",
			AgentID:     "agent-latency",
			SessionID:   "session-latency",
			TopK:        10,
			ObjectTypes: []string{"memory"},
		})
		latency := time.Since(start)
		totalLatency += latency

		if latency < minLatency {
			minLatency = latency
		}
		if latency > maxLatency {
			maxLatency = latency
		}

		// Log first query details
		if i == 0 {
			t.Logf("First query response:")
			t.Logf("  Objects returned: %d", len(resp.Objects))
			t.Logf("  ProofTrace steps: %d", len(resp.ProofTrace))
			t.Logf("  AppliedFilters: %d", len(resp.AppliedFilters))
			if len(resp.ProofTrace) > 0 {
				t.Logf("  ProofTrace: %v", resp.ProofTrace[:min(3, len(resp.ProofTrace))])
			}
		}
	}

	avgLatency := totalLatency / time.Duration(numQueries)
	qps := float64(numQueries) / totalLatency.Seconds()

	t.Logf("")
	t.Logf("=== QueryChain E2E Benchmark Results ===")
	t.Logf("Documents: %d", numDocs)
	t.Logf("Queries: %d", numQueries)
	t.Logf("Avg latency: %.3f ms", float64(avgLatency.Microseconds())/1000)
	t.Logf("Min latency: %.3f ms", float64(minLatency.Microseconds())/1000)
	t.Logf("Max latency: %.3f ms", float64(maxLatency.Microseconds())/1000)
	t.Logf("QPS: %.1f", qps)
	t.Logf("")
	t.Logf("Pipeline stages:")
	t.Logf("  1. TieredDataPlane.Search (lexical + vector)")
	t.Logf("  2. ObjectStore metadata fetch")
	t.Logf("  3. SafetyFilter governance rules")
	t.Logf("  4. RRF reranking")
	t.Logf("  5. ProofTrace BFS")
	t.Logf("  6. Subgraph expansion")
}

func TestRuntime_IncludeCold_10KArchivedCorrectnessAndLatency(t *testing.T) {
	if testing.Short() {
		t.Skip("skip 10K archived runtime validation in short mode")
	}

	r, tieredObjs, store := buildColdQueryRuntime(t)
	targetIDs := seedArchivedColdMemories(t, tieredObjs, store, 10000, 100, "runtime cold benchmark payload")

	const queries = 50
	latencies := make([]time.Duration, 0, queries)

	var firstResp schemas.QueryResponse
	for i := 0; i < queries; i++ {
		start := time.Now()
		resp := r.ExecuteQuery(schemas.QueryRequest{
			QueryText:   "runtime cold benchmark payload target",
			QueryScope:  "session",
			SessionID:   "session-cold-bench",
			AgentID:     "agent-cold-bench",
			TopK:        10,
			ObjectTypes: []string{"memory"},
			IncludeCold: true,
		})
		latencies = append(latencies, time.Since(start))
		if i == 0 {
			firstResp = resp
		}
	}

	if len(firstResp.Objects) != 10 {
		t.Fatalf("expected first response topK=10, got %d", len(firstResp.Objects))
	}
	for i, id := range firstResp.Objects {
		if _, ok := targetIDs[id]; !ok {
			t.Fatalf("unexpected non-target result at rank %d: %s", i, id)
		}
	}

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p50 := latencies[len(latencies)/2]
	p95 := latencies[(len(latencies)*95)/100]
	p99 := latencies[(len(latencies)*99)/100]

	t.Logf("Runtime include_cold 10K archived: p50=%.3fms p95=%.3fms p99=%.3fms first_mode=%v first_objects=%d",
		float64(p50.Microseconds())/1000.0,
		float64(p95.Microseconds())/1000.0,
		float64(p99.Microseconds())/1000.0,
		firstResp.ChainTraces.Main,
		len(firstResp.Objects),
	)
}

func BenchmarkRuntime_ExecuteQuery_IncludeCold_10KArchived(b *testing.B) {
	r, tieredObjs, store := buildColdQueryRuntime(b)
	_ = seedArchivedColdMemories(b, tieredObjs, store, 10000, 100, "runtime cold benchmark payload")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		resp := r.ExecuteQuery(schemas.QueryRequest{
			QueryText:   "runtime cold benchmark payload target",
			QueryScope:  "session",
			SessionID:   "session-cold-bench",
			AgentID:     "agent-cold-bench",
			TopK:        10,
			ObjectTypes: []string{"memory"},
			IncludeCold: true,
		})
		if len(resp.Objects) != 10 {
			b.Fatalf("expected topK=10, got %d", len(resp.Objects))
		}
		b.ReportMetric(float64(time.Since(start).Microseconds())/1000.0, "ms/op-observed")
	}
}

// TestQueryChain_ProofTrace_Stages verifies and logs the stages in ProofTrace
func TestQueryChain_ProofTrace_Stages(t *testing.T) {
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	store := storage.NewMemoryRuntimeStorage()
	derivLog := eventbackbone.NewDerivationLog(clock, bus)

	nodeManager := nodes.CreateManager()
	nodeManager.RegisterProofTrace(coordination.CreateInMemoryProofTraceWorker("proof-stages", store.Edges(), derivLog))

	qc := chain.CreateQueryChain(nodeManager)

	// Seed edges for BFS traversal
	edges := []schemas.Edge{
		{EdgeID: "e1", SrcObjectID: "obj_A", DstObjectID: "obj_B", EdgeType: string(schemas.EdgeTypeCausedBy), Weight: 0.9},
		{EdgeID: "e2", SrcObjectID: "obj_B", DstObjectID: "obj_C", EdgeType: string(schemas.EdgeTypeDerivedFrom), Weight: 0.8},
		{EdgeID: "e3", SrcObjectID: "obj_C", DstObjectID: "obj_D", EdgeType: string(schemas.EdgeTypeBelongsToSession), Weight: 0.7},
	}
	for _, e := range edges {
		store.Edges().PutEdge(e)
	}

	// Add derivation log entry
	derivLog.Append("evt_source", "event", "obj_A", "memory", "extraction")

	chainOut, result := qc.Run(chain.QueryChainInput{
		ObjectIDs:   []string{"obj_A"},
		MaxDepth:    4,
		ObjectStore: store.Objects(),
		EdgeStore:   store.Edges(),
	})

	if !result.OK {
		t.Fatalf("QueryChain.Run failed: %s", result.Error)
	}

	t.Logf("=== ProofTrace Stages ===")
	for i, step := range chainOut.ProofTrace {
		t.Logf("  [%d] %s", i, step.Description)
	}
	t.Logf("")
	t.Logf("Total ProofTrace steps: %d", len(chainOut.ProofTrace))
}
