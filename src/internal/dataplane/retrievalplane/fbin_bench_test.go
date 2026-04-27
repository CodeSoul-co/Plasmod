//go:build retrieval
// +build retrieval

// fbin_bench_test.go — End-to-end benchmark of our project's Knowhere HNSW
// against the same dataset & methodology used in the user's Mac experiment:
//
//   * data    : TEST/testQuery10K.fbin (10000 × dim=100)
//   * base    : the queries themselves (self-query)
//   * metric  : L2 (Euclidean)
//   * topK    : 10
//   * GT      : brute-force exact L2 search
//   * report  : build_s, QPS, p50/p95/p99 (ms), recall@10
//
// HNSW params are taken from the C++ side defaults (M=16, efC=256,
// efSearch=max(topK*2,64)=64) — our bridge does not currently expose
// custom M/efC. The user's Mac ran knowhere with M=32, efC=200, which
// will affect comparability; the differences are noted in the output.
//
// Run with:
//   go test -tags retrieval -run TestBenchFbinKnowhere -v -timeout 30m \
//       ./src/internal/dataplane/retrievalplane/

package retrievalplane

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"
)

// loadFbin reads a Microsoft-format .fbin file:
//
//	uint32 n, uint32 d, then n*d float32 in row-major order.
func loadFbin(path string) ([]float32, int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, err
	}
	defer f.Close()
	var hdr [8]byte
	if _, err := f.Read(hdr[:]); err != nil {
		return nil, 0, 0, err
	}
	n := int(binary.LittleEndian.Uint32(hdr[0:4]))
	d := int(binary.LittleEndian.Uint32(hdr[4:8]))
	buf := make([]float32, n*d)
	for i := 0; i < n*d; i++ {
		var b [4]byte
		if _, err := f.Read(b[:]); err != nil {
			return nil, 0, 0, err
		}
		buf[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[:]))
	}
	return buf, n, d, nil
}

// bruteForceL2TopK computes the exact top-K L2 nearest neighbors for each
// query against base. Result layout: gt[q*topK..q*topK+topK].
func bruteForceL2TopK(base, query []float32, nb, nq, dim, topK int) []int64 {
	gt := make([]int64, nq*topK)
	type pair struct {
		dist float32
		idx  int64
	}
	// Parallelize over queries.
	nworkers := runtime.NumCPU()
	if nworkers > 16 {
		nworkers = 16
	}
	type job struct{ start, end int }
	jobs := make(chan job, nworkers*2)
	done := make(chan struct{}, nworkers)
	for w := 0; w < nworkers; w++ {
		go func() {
			heap := make([]pair, 0, topK+1)
			for j := range jobs {
				for q := j.start; q < j.end; q++ {
					qv := query[q*dim : (q+1)*dim]
					heap = heap[:0]
					for b := 0; b < nb; b++ {
						bv := base[b*dim : (b+1)*dim]
						var s float32
						for k := 0; k < dim; k++ {
							d := qv[k] - bv[k]
							s += d * d
						}
						if len(heap) < topK {
							heap = append(heap, pair{s, int64(b)})
							// Sort descending so heap[0] is the worst.
							sort.Slice(heap, func(a, c int) bool { return heap[a].dist > heap[c].dist })
						} else if s < heap[0].dist {
							heap[0] = pair{s, int64(b)}
							sort.Slice(heap, func(a, c int) bool { return heap[a].dist > heap[c].dist })
						}
					}
					// Heap currently descending; flip to ascending for output.
					sort.Slice(heap, func(a, c int) bool { return heap[a].dist < heap[c].dist })
					for k := 0; k < topK; k++ {
						gt[q*topK+k] = heap[k].idx
					}
				}
			}
			done <- struct{}{}
		}()
	}
	chunk := (nq + nworkers - 1) / nworkers
	for s := 0; s < nq; s += chunk {
		e := s + chunk
		if e > nq {
			e = nq
		}
		jobs <- job{s, e}
	}
	close(jobs)
	for w := 0; w < nworkers; w++ {
		<-done
	}
	return gt
}

func percentile(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	cp := make([]float64, len(xs))
	copy(cp, xs)
	sort.Float64s(cp)
	idx := int(math.Ceil(p/100.0*float64(len(cp)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(cp) {
		idx = len(cp) - 1
	}
	return cp[idx]
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var s float64
	for _, v := range xs {
		s += v
	}
	return s / float64(len(xs))
}

// TestBenchFbinKnowhere runs the project's Knowhere HNSW benchmark on
// TEST/testQuery10K.fbin (self-query) and writes JSON metrics next to it.
func TestBenchFbinKnowhere(t *testing.T) {
	const topK = 10

	// Locate TEST/testQuery10K.fbin from the workspace root. We walk up from
	// the test working dir (which is the package dir) until we find TEST/.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := wd
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(root, "TEST", "testQuery10K.fbin")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatalf("could not locate TEST/testQuery10K.fbin from %s", wd)
		}
		root = parent
	}
	fbinPath := filepath.Join(root, "TEST", "testQuery10K.fbin")
	t.Logf("data: %s", fbinPath)

	vecs, n, dim, err := loadFbin(fbinPath)
	if err != nil {
		t.Fatalf("loadFbin: %v", err)
	}
	t.Logf("loaded %d vectors of dim %d (%d float32 total)", n, dim, n*dim)

	// Self-query: base == queries == vecs.
	base := vecs
	queries := vecs
	nb := n
	nq := n

	// ── Compute ground truth (brute-force exact L2 top-K).  ──────────────
	t.Logf("computing brute-force L2 ground truth (nq=%d × nb=%d, topK=%d)...", nq, nb, topK)
	t0 := time.Now()
	gt := bruteForceL2TopK(base, queries, nb, nq, dim, topK)
	t.Logf("ground truth done in %.2fs", time.Since(t0).Seconds())

	// ── Build our project's Knowhere HNSW (L2). ──────────────────────────
	r, err := NewRetrieverWithMetric(dim, 0, 0, 0, "L2")
	if err != nil {
		t.Fatalf("NewRetrieverWithMetric: %v", err)
	}
	defer r.Close()

	t.Logf("building HNSW index (n=%d, dim=%d, metric=L2, defaults M=16/efC=256)...", nb, dim)
	t0 = time.Now()
	if err := r.Build(base, nb); err != nil {
		t.Fatalf("Build: %v", err)
	}
	buildSec := time.Since(t0).Seconds()
	t.Logf("build done in %.3fs", buildSec)

	// ── Search loop. ─────────────────────────────────────────────────────
	// Warm-up.
	for i := 0; i < 32 && i < nq; i++ {
		_, _, _ = r.Search(queries[i*dim:(i+1)*dim], topK, nil)
	}

	latUS := make([]float64, nq)
	hits := 0
	t.Logf("running %d queries (topK=%d)...", nq, topK)
	t0 = time.Now()
	for q := 0; q < nq; q++ {
		qv := queries[q*dim : (q+1)*dim]
		ts := time.Now()
		ids, _, err := r.Search(qv, topK, nil)
		latUS[q] = float64(time.Since(ts).Microseconds())
		if err != nil {
			t.Fatalf("Search q=%d: %v", q, err)
		}
		// Recall@10 = |found ∩ gt| / topK.
		gtRow := gt[q*topK : (q+1)*topK]
		gtSet := make(map[int64]struct{}, topK)
		for _, id := range gtRow {
			gtSet[id] = struct{}{}
		}
		for _, id := range ids {
			if _, ok := gtSet[id]; ok {
				hits++
			}
		}
	}
	wallSec := time.Since(t0).Seconds()

	latMS := make([]float64, nq)
	for i, v := range latUS {
		latMS[i] = v / 1000.0
	}
	qps := float64(nq) / wallSec
	p50 := percentile(latMS, 50)
	p95 := percentile(latMS, 95)
	p99 := percentile(latMS, 99)
	avgMS := mean(latMS)
	recall := float64(hits) / float64(nq*topK)

	metrics := map[string]any{
		"engine":  "plasmod (project) Knowhere HNSW via CGO",
		"dataset": "TEST/testQuery10K.fbin (self-query)",
		"params": map[string]any{
			"dim":           dim,
			"M":             16,
			"efConstruction": 256,
			"efSearch":      64,
			"topK":          topK,
			"nb":            nb,
			"nq":            nq,
			"metric":        "L2",
		},
		"build_s":       round3(buildSec),
		"qps":           round1(qps),
		"wall_s":        round3(wallSec),
		"p50_ms":        round4(p50),
		"p95_ms":        round4(p95),
		"p99_ms":        round4(p99),
		"mean_ms":       round4(avgMS),
		"recall_at_10":  round4(recall),
	}
	bs, _ := json.MarshalIndent(metrics, "", "  ")
	out := filepath.Join(root, "TEST", "metrics_plasmod_knowhere.json")
	_ = os.WriteFile(out, bs, 0644)

	fmt.Println("\n========== plasmod Knowhere HNSW benchmark ==========")
	fmt.Printf("dataset      : TEST/testQuery10K.fbin (n=%d, dim=%d, self-query)\n", nb, dim)
	fmt.Printf("params       : M=16  efC=256  efS=64  topK=%d  metric=L2 (defaults)\n", topK)
	fmt.Printf("build_s      : %.3f\n", buildSec)
	fmt.Printf("QPS          : %.1f\n", qps)
	fmt.Printf("p50_ms       : %.4f\n", p50)
	fmt.Printf("p95_ms       : %.4f\n", p95)
	fmt.Printf("p99_ms       : %.4f\n", p99)
	fmt.Printf("mean_ms      : %.4f\n", avgMS)
	fmt.Printf("recall@10    : %.4f\n", recall)
	fmt.Printf("output JSON  : %s\n", out)
	fmt.Println("=====================================================")
}

// TestBenchFbinKnowhereBatch tests the SAME index in batch-search mode
// (single Search call with nq=10000), which is how the Python Knowhere
// reference benchmark on Mac is most likely measured. This isolates
// "per-query CGO+JSON overhead" vs "core HNSW search throughput".
func TestBenchFbinKnowhereBatch(t *testing.T) {
	const topK = 10

	wd, _ := os.Getwd()
	root := wd
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(root, "TEST", "testQuery10K.fbin")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatalf("could not locate TEST/testQuery10K.fbin")
		}
		root = parent
	}
	fbinPath := filepath.Join(root, "TEST", "testQuery10K.fbin")
	vecs, n, dim, err := loadFbin(fbinPath)
	if err != nil {
		t.Fatalf("loadFbin: %v", err)
	}
	t.Logf("loaded %d × %d", n, dim)

	// Use SegmentRetriever which DOES expose nq>1 in its C API.
	segID := "bench.knowhere.batch.fbin10k"
	defer GlobalSegmentRetriever.UnloadSegment(segID)

	t.Log("building segment HNSW...")
	t0 := time.Now()
	if err := GlobalSegmentRetriever.BuildSegment(segID, vecs, n, dim); err != nil {
		t.Fatalf("BuildSegment: %v", err)
	}
	buildSec := time.Since(t0).Seconds()
	t.Logf("build: %.3fs", buildSec)

	// Brute-force GT (L2). Reuses the same routine from the loop test.
	t.Log("computing brute-force GT...")
	gt := bruteForceL2TopK(vecs, vecs, n, n, dim, topK)

	// Warm-up.
	_, _, _ = GlobalSegmentRetriever.Search(segID, vecs[:32*dim], 32, topK)

	// Single batch call: nq = n.
	t.Logf("running 1 batch Search call with nq=%d...", n)
	t0 = time.Now()
	ids, _, err := GlobalSegmentRetriever.Search(segID, vecs, n, topK)
	wallSec := time.Since(t0).Seconds()
	if err != nil {
		t.Fatalf("Search batch: %v", err)
	}

	hits := 0
	for q := 0; q < n; q++ {
		gtRow := gt[q*topK : (q+1)*topK]
		gtSet := make(map[int64]struct{}, topK)
		for _, id := range gtRow {
			gtSet[id] = struct{}{}
		}
		for k := 0; k < topK; k++ {
			if _, ok := gtSet[ids[q*topK+k]]; ok {
				hits++
			}
		}
	}
	recall := float64(hits) / float64(n*topK)
	qps := float64(n) / wallSec
	avgPerQueryMS := (wallSec / float64(n)) * 1000.0

	fmt.Println("\n========== plasmod Knowhere HNSW (BATCH) =============")
	fmt.Printf("dataset      : TEST/testQuery10K.fbin (n=%d, dim=%d, self-query)\n", n, dim)
	fmt.Printf("build_s      : %.3f\n", buildSec)
	fmt.Printf("batch wall_s : %.3f  (single Search call, nq=%d, topK=%d)\n", wallSec, n, topK)
	fmt.Printf("QPS (batch)  : %.1f\n", qps)
	fmt.Printf("avg / query  : %.4f ms  (= wall_s / nq)\n", avgPerQueryMS)
	fmt.Printf("recall@10    : %.4f\n", recall)
	fmt.Println("=====================================================")

	out := filepath.Join(root, "TEST", "metrics_plasmod_knowhere_batch.json")
	bs, _ := json.MarshalIndent(map[string]any{
		"engine":           "plasmod (project) Knowhere HNSW BATCH via CGO",
		"dataset":          "TEST/testQuery10K.fbin (self-query)",
		"build_s":          round3(buildSec),
		"batch_wall_s":     round3(wallSec),
		"qps_batch":        round1(qps),
		"avg_per_query_ms": round4(avgPerQueryMS),
		"recall_at_10":     round4(recall),
		"params": map[string]any{
			"nb": n, "nq": n, "dim": dim, "topK": topK, "metric": "L2 (segment default)",
		},
	}, "", "  ")
	_ = os.WriteFile(out, bs, 0644)
	fmt.Printf("output JSON  : %s\n", out)
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
func round3(v float64) float64 { return math.Round(v*1000) / 1000 }
func round4(v float64) float64 { return math.Round(v*10000) / 10000 }

// locateFbin walks up from the package dir until it finds TEST/testQuery10K.fbin
// and returns (workspaceRoot, fbinPath).  Fatal on failure.
func locateFbin(t *testing.T) (string, string) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := wd
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(root, "TEST", "testQuery10K.fbin")); err == nil {
			return root, filepath.Join(root, "TEST", "testQuery10K.fbin")
		}
		parent := filepath.Dir(root)
		if parent == root {
			break
		}
		root = parent
	}
	t.Fatalf("could not locate TEST/testQuery10K.fbin from %s", wd)
	return "", ""
}

// TestBenchFbinIVFLoop runs IVF_FLAT on the same fbin dataset using the
// single-query loop methodology (10000 × Search(nq=1)). Reports QPS,
// p50/p95/p99, recall@10. IVF-specific tuning (nlist, nprobe) uses the
// C++ defaults (nlist=128, nprobe=8).
func TestBenchFbinIVFLoop(t *testing.T) {
	const topK = 10
	root, fbinPath := locateFbin(t)
	t.Logf("data: %s", fbinPath)

	vecs, n, dim, err := loadFbin(fbinPath)
	if err != nil {
		t.Fatalf("loadFbin: %v", err)
	}
	t.Logf("loaded %d × %d", n, dim)

	r, err := NewRetrieverWithIndexType(dim, "IVF_FLAT", "L2")
	if err != nil {
		t.Skipf("IVF_FLAT not available in this build: %v", err)
	}
	defer r.Close()

	t.Logf("building IVF_FLAT (n=%d, dim=%d, nlist=128 default)...", n, dim)
	t0 := time.Now()
	if err := r.Build(vecs, n); err != nil {
		t.Fatalf("Build: %v", err)
	}
	buildSec := time.Since(t0).Seconds()
	t.Logf("build: %.3fs", buildSec)

	t.Log("computing brute-force GT...")
	gt := bruteForceL2TopK(vecs, vecs, n, n, dim, topK)

	// Warm-up.
	for i := 0; i < 32; i++ {
		_, _, _ = r.Search(vecs[i*dim:(i+1)*dim], topK, nil)
	}

	latMS := make([]float64, n)
	hits := 0
	t.Logf("running %d single-query searches (topK=%d, nprobe=8 default)...", n, topK)
	t0 = time.Now()
	for q := 0; q < n; q++ {
		qv := vecs[q*dim : (q+1)*dim]
		ts := time.Now()
		ids, _, err := r.Search(qv, topK, nil)
		latMS[q] = float64(time.Since(ts).Nanoseconds()) / 1e6
		if err != nil {
			t.Fatalf("Search q=%d: %v", q, err)
		}
		gtRow := gt[q*topK : (q+1)*topK]
		gtSet := make(map[int64]struct{}, topK)
		for _, id := range gtRow {
			gtSet[id] = struct{}{}
		}
		k := topK
		if len(ids) < k {
			k = len(ids)
		}
		for i := 0; i < k; i++ {
			if _, ok := gtSet[ids[i]]; ok {
				hits++
			}
		}
	}
	wallSec := time.Since(t0).Seconds()

	qps := float64(n) / wallSec
	recall := float64(hits) / float64(n*topK)

	fmt.Println("\n========== plasmod IVF_FLAT (LOOP) =================")
	fmt.Printf("dataset      : TEST/testQuery10K.fbin (n=%d, dim=%d, self-query)\n", n, dim)
	fmt.Printf("params       : nlist=128  nprobe=8  topK=%d  metric=L2 (C++ defaults)\n", topK)
	fmt.Printf("build_s      : %.3f\n", buildSec)
	fmt.Printf("QPS          : %.1f\n", qps)
	fmt.Printf("p50_ms       : %.4f\n", percentile(latMS, 50))
	fmt.Printf("p95_ms       : %.4f\n", percentile(latMS, 95))
	fmt.Printf("p99_ms       : %.4f\n", percentile(latMS, 99))
	fmt.Printf("mean_ms      : %.4f\n", mean(latMS))
	fmt.Printf("recall@10    : %.4f\n", recall)
	fmt.Println("=====================================================")

	out := filepath.Join(root, "TEST", "metrics_plasmod_ivf_loop.json")
	bs, _ := json.MarshalIndent(map[string]any{
		"engine":       "plasmod IVF_FLAT (loop) via CGO",
		"dataset":      "TEST/testQuery10K.fbin (self-query)",
		"build_s":      round3(buildSec),
		"wall_s":       round3(wallSec),
		"qps":          round1(qps),
		"p50_ms":       round4(percentile(latMS, 50)),
		"p95_ms":       round4(percentile(latMS, 95)),
		"p99_ms":       round4(percentile(latMS, 99)),
		"mean_ms":      round4(mean(latMS)),
		"recall_at_10": round4(recall),
		"params": map[string]any{
			"index": "IVF_FLAT", "nlist": 128, "nprobe": 8,
			"nb": n, "nq": n, "dim": dim, "topK": topK, "metric": "L2",
		},
	}, "", "  ")
	_ = os.WriteFile(out, bs, 0644)
	fmt.Printf("output JSON  : %s\n", out)
}

