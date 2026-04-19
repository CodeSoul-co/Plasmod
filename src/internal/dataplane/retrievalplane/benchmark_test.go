//go:build retrieval
// +build retrieval

// benchmark_test.go — Layer-1 benchmark: direct retrievalplane (Group 3).
//
// Measures the overhead of the Knowhere CGO bridge in isolation,
// with zero HTTP, zero embedder, zero object-model overhead.
// This is the "vector-only" baseline for comparison.
//
// Run from repository root:
//   go test -tags retrieval -bench=. -benchtime=10s \
//     -ldflags="-L$PWD/cpp/build -landb_retrieval -Wl,-rpath,$PWD/cpp/build" \
//     ./src/internal/dataplane/retrievalplane/ -run=^$ -benchmem
//
// Or use the Makefile target:
//   make bench-layer1

package retrievalplane

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// ── Benchmark parameters ────────────────────────────────────────────────────────

const (
	benchDim        = 128
	benchNb         = 100_000 // indexed vectors
	benchNq         = 10_000  // queries per benchmark iteration
	benchTopK       = 10
	benchHNSWM      = 16
	benchSeed  int64 = 20260419
)

// benchNormalise makes a unit vector (IP ≡ cosine after normalisation).
func benchNormalise(v []float32) {
	var sum float32
	for _, x := range v {
		sum += x * x
	}
	norm := float32(math.Sqrt(float64(sum)))
	if norm == 0 {
		return
	}
	for i := range v {
		v[i] /= norm
	}
}

// generateVectors creates nb random normalised vectors of dimension dim.
func generateVectors(nb, dim int, seed int64) []float32 {
	r := rand.New(rand.NewSource(seed))
	vecs := make([]float32, nb*dim)
	for i := range vecs {
		vecs[i] = float32(r.Float64())
	}
// batch-normalise
	for i := 0; i < nb; i++ {
		benchNormalise(vecs[i*dim : (i+1)*dim])
	}
	return vecs
}

// ── Benchmark: Build ───────────────────────────────────────────────────────────

func BenchmarkHNSWBuild(b *testing.B) {
	const segID = "bench.segment.0001"
	vecs := generateVectors(benchNb, benchDim, benchSeed)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		sr := GlobalSegmentRetriever
		_ = sr.BuildSegment(segID, vecs, benchNb, benchDim) // nolint:errcheck
		sr.UnloadSegment(segID)                               // nolint:errcheck
	}
}

// ── Benchmark: Single-query latency ────────────────────────────────────────────

func BenchmarkHNSSearchLatency(b *testing.B) {
	const segID = "bench.segment.0002"
	vecs   := generateVectors(benchNb, benchDim, benchSeed)
	sr     := GlobalSegmentRetriever
	_ = sr.BuildSegment(segID, vecs, benchNb, benchDim) // nolint:errcheck
	defer sr.UnloadSegment(segID)                        // nolint:errcheck

	queries := generateVectors(benchNq, benchDim, benchSeed+1)

	b.ResetTimer()
	b.ReportAllocs()

	// Warm up
	for i := 0; i < 100; i++ {
		sr.Search(segID, queries[i*benchDim:(i+1)*benchDim], 1, benchTopK) // nolint:errcheck
	}

	b.StopTimer()
	runtime.GC()
	b.StartTimer()

	for i := 0; i < b.N; i++ {
		q := queries[(i%benchNq)*benchDim : (i%benchNq+1)*benchDim]
		sr.Search(segID, q, 1, benchTopK) // nolint:errcheck
	}
}

// ── Benchmark: Single-threaded throughput ──────────────────────────────────────

func BenchmarkHNSWSearchThroughputSerial(b *testing.B) {
	const segID = "bench.segment.0003"
	vecs   := generateVectors(benchNb, benchDim, benchSeed)
	sr     := GlobalSegmentRetriever
	_ = sr.BuildSegment(segID, vecs, benchNb, benchDim) // nolint:errcheck
	defer sr.UnloadSegment(segID)                        // nolint:errcheck

	queries := generateVectors(benchNq, benchDim, benchSeed+1)

	// Warm up
	for i := 0; i < 100; i++ {
		sr.Search(segID, queries[i*benchDim:(i+1)*benchDim], 1, benchTopK) // nolint:errcheck
	}

	b.ResetTimer()
	b.ReportAllocs()

	n := b.N * benchNq
	t0 := time.Now()
	for i := 0; i < n; i++ {
		q := queries[(i%benchNq)*benchDim : (i%benchNq+1)*benchDim]
		sr.Search(segID, q, 1, benchTopK) // nolint:errcheck
	}
	elapsed := time.Since(t0)

	b.ReportMetric(float64(n)/elapsed.Seconds(), "queries/s")
	b.ReportMetric(float64(elapsed.Milliseconds())/float64(n), "ms/op")
}

// ── Benchmark: Parallel throughput ─────────────────────────────────────────────

func BenchmarkHNSSearchThroughputParallel(b *testing.B) {
	const segID = "bench.segment.0004"
	vecs   := generateVectors(benchNb, benchDim, benchSeed)
	sr     := GlobalSegmentRetriever
	_ = sr.BuildSegment(segID, vecs, benchNb, benchDim) // nolint:errcheck
	defer sr.UnloadSegment(segID)                        // nolint:errcheck

	queries := generateVectors(benchNq, benchDim, benchSeed+1)

	// Warm up
	for i := 0; i < 100; i++ {
		sr.Search(segID, queries[i*benchDim:(i+1)*benchDim], 1, benchTopK) // nolint:errcheck
	}

	b.ResetTimer()
	b.ReportAllocs()

	var total int64

	b.RunParallel(func(pb *testing.PB) {
		local := 0
		for pb.Next() {
			i := int(atomic.AddInt64(&total, 1)-1) % benchNq
			q := queries[i*benchDim : (i+1)*benchDim]
			sr.Search(segID, q, 1, benchTopK) // nolint:errcheck
			local++
		}
		_ = local
	})
}

// ── Benchmark: Multi-segment (simulates hot/warm multi-tenant) ─────────────────

func BenchmarkHNSSearchMultiSegment(b *testing.B) {
	const nSegments = 10
	vecs := generateVectors(benchNb, benchDim, benchSeed)
	sr   := GlobalSegmentRetriever

	segIDs := make([]string, nSegments)
	for i := 0; i < nSegments; i++ {
		segIDs[i] = fmt.Sprintf("bench.segment.%04d", i)
		_ = sr.BuildSegment(segIDs[i], vecs, benchNb, benchDim) // nolint:errcheck
	}
	defer func() {
		for i := 0; i < nSegments; i++ {
			sr.UnloadSegment(segIDs[i]) // nolint:errcheck
		}
	}()

	queries := generateVectors(benchNq, benchDim, benchSeed+1)

	// Warm up
	for i := 0; i < 100; i++ {
		seg := segIDs[i%nSegments]
		sr.Search(seg, queries[i*benchDim:(i+1)*benchDim], 1, benchTopK) // nolint:errcheck
	}

	b.ResetTimer()
	b.ReportAllocs()

	var total int64
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			i := int(atomic.AddInt64(&total, 1))
			seg := segIDs[i % nSegments]
			q := queries[(i*7)%benchNq*benchDim : ((i*7)%benchNq+1)*benchDim]
			sr.Search(seg, q, 1, benchTopK) // nolint:errcheck
		}
	})
}

// ── Benchmark: Search with allowList filter ───────────────────────────────────

func BenchmarkHNSSearchWithFilter(b *testing.B) {
	const segID = "bench.segment.0005"
	vecs   := generateVectors(benchNb, benchDim, benchSeed)
	sr     := GlobalSegmentRetriever
	_ = sr.BuildSegment(segID, vecs, benchNb, benchDim) // nolint:errcheck
	defer sr.UnloadSegment(segID)                         // nolint:errcheck

	queries := generateVectors(benchNq, benchDim, benchSeed+1)
	// allowList: allow every 3rd vector (bitmask)
	allowList := make([]byte, (benchNb+7)/8)
	for i := 0; i < benchNb; i += 3 {
		allowList[i/8] |= 1 << (i % 8)
	}

	// Warm up
	for i := 0; i < 100; i++ {
		sr.SearchWithFilter(segID, queries[i*benchDim:(i+1)*benchDim], 1, benchTopK, allowList) // nolint:errcheck
	}

	b.ResetTimer()
	b.ReportAllocs()

	var total int64
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			i := int(atomic.AddInt64(&total, 1)) % benchNq
			q := queries[i*benchDim : (i+1)*benchDim]
			sr.SearchWithFilter(segID, q, 1, benchTopK, allowList) // nolint:errcheck
		}
	})
}

// ── Validation: recall vs brute force ─────────────────────────────────────────

func TestHNSWRecall(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping recall test in -short mode")
	}

	const segID = "recall.segment.0001"
	const nb = 10_000
	const nq = 1_000
	const topk = 10

	vecs := generateVectors(nb, benchDim, benchSeed)
	sr := GlobalSegmentRetriever
	if err := sr.BuildSegment(segID, vecs, nb, benchDim); err != nil {
		t.Fatalf("BuildSegment: %v", err)
	}
	defer sr.UnloadSegment(segID) // nolint:errcheck

	queries := generateVectors(nq, benchDim, benchSeed+1)

	// Brute-force ground truth
	fmt.Println("[Recall] Computing brute-force ground truth…")
	gtIDs := make([]int64, nq*topk)
	for qi := 0; qi < nq; qi++ {
		q := queries[qi*benchDim : (qi+1)*benchDim]
		// Compute IP distances manually
		type pair struct{ id int; sim float32 }
		pairs := make([]pair, nb)
		for vi := 0; vi < nb; vi++ {
			var dot float32
			for d := 0; d < benchDim; d++ {
				dot += q[d] * vecs[vi*benchDim+d]
			}
			pairs[vi] = pair{vi, dot}
		}
		// Sort by descending similarity (partial sort for topk)
		for i := 0; i < topk; i++ {
			maxIdx := i
			for j := i + 1; j < nb; j++ {
				if pairs[j].sim > pairs[maxIdx].sim {
					maxIdx = j
				}
			}
			gtIDs[qi*topk+i] = int64(pairs[maxIdx].id)
			pairs[maxIdx].sim = pairs[i].sim
		}
	}

	// HNSW search
	fmt.Println("[Recall] Running HNSW search…")
	resIDs := make([]int64, nq*topk)
	for qi := 0; qi < nq; qi++ {
		q := queries[qi*benchDim : (qi+1)*benchDim]
		ids, _, err := sr.Search(segID, q, 1, topk)
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		copy(resIDs[qi*topk:qi*topk+len(ids)], ids)
	}

	// Recall@10
	hits := 0
	for i := 0; i < nq*topk; i++ {
		if resIDs[i] == gtIDs[i] {
			hits++
		}
	}
	recall := float64(hits) / float64(nq*topk)
	fmt.Printf("[Recall] HNSW@10 recall vs brute-force: %.4f (%d/%d hits)\n", recall, hits, nq*topk)

	if recall < 0.95 {
		t.Errorf("Recall too low: %.4f", recall)
	}
}

// ── Main: standalone run ────────────────────────────────────────────────────────

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
