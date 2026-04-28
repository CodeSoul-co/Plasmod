//go:build retrieval
// +build retrieval

// cmd/benchmark/main.go — Plasmod retrieval benchmark binary.
//
// Modes:
//   faiss           — G1: FAISS HNSW via CGO, batch+serial search
//   knowhere-build   — G2: Knowhere HNSW via CGO, batch+serial search (OpenMP parallel)
//   vector-only      — G3: GlobalSegmentRetriever.Search via CGO, batch+serial search
//   http-query      — G4: HTTP batch query
//
// Batch vs Serial:
//   batch_* metrics: single batch_search call (nq queries together, OpenMP parallel)
//   serial_* metrics: nq individual calls (each nq=1, HnswFastSearchFloat hot path)
//   build_ms: HNSW index construction time (separate)
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"time"

	"plasmod/retrievalplane"
)

var (
	mode            = flag.String("mode", "faiss", "faiss|faiss-single|knowhere-build|knowhere-single|knowhere-raw|vector-only|vector-only-raw|http-query|http-query-raw")
	dataset         = flag.String("dataset", "", "Path to .fbin test data (used when indexed/query not specified separately)")
	indexedDataset  = flag.String("indexed-dataset", "", "Path to .fbin containing the indexed vectors (HNSW build corpus)")
	queryDataset    = flag.String("query-dataset", "", "Path to .fbin containing the query vectors")
	groundtruthPath = flag.String("groundtruth", "", "Path to .ibin ground truth (int64, reshape [nq][topk])")
	limit           = flag.Int("limit", 10000, "Max vectors to load (single-dataset mode only)")
	nquery          = flag.Int("queries", 1000, "Number of queries")
	topk            = flag.Int("topk", 10, "Top-k results")
	segmentID       = flag.String("segment", "bench.layer1", "Warm segment ID")
	serverURL       = flag.String("server-url", "http://127.0.0.1:8080", "Plasmod server URL for http-query mode")
	concurrency     = flag.Int("concurrency", 16, "Concurrency for http-query mode")
	batchSize       = flag.Int("batch-size", 100, "Batch size for http-query mode (queries per HTTP request)")
	indexedCount    = flag.Int("indexed-count", 0, "Number of vectors to index (0=all loaded). Keeps indexed/query sets disjoint for correct recall.")
)

type BenchResult struct {
	Mode       string   `json:"mode"`
	NIndexed   int      `json:"n_indexed"`
	NQueries   int      `json:"n_queries"`
	TopK       int      `json:"topk"`
	Dim        int      `json:"dim"`
	BuildMs   float64  `json:"build_ms"`
	BatchMs   float64  `json:"batch_ms"`
	BatchQPS  float64  `json:"batch_qps"`
	SerialMs  float64  `json:"serial_ms"`
	SerialQPS float64  `json:"serial_qps"`
	MeanMs    float64  `json:"mean_ms"`
	P50Ms     float64  `json:"p50_ms"`
	P95Ms     float64  `json:"p95_ms"`
	P99Ms     float64  `json:"p99_ms"`
	Errors    int      `json:"errors"`
	// IntIDs holds integer indices for the last query batch (used for recall calculation).
	IntIDs []int64 `json:"int_ids,omitempty"`
	// Recall holds the recall vs ground truth (0-1 float).
	Recall float64 `json:"recall,omitempty"`
	// GroundTruthPath records which ground truth file was used.
	GroundTruthPath string `json:"groundtruth_path,omitempty"`
}

func main() {
	os.Setenv("KMP_DUPLICATE_LIB_OK", "TRUE")

	flag.Parse()
	switch *mode {
	case "faiss":
		runFAISS()
	case "faiss-single":
		runFAISSSingle()
	case "vector-only":
		runVectorOnly()
	case "knowhere-build":
		runKnowhereBuild()
	case "knowhere-single":
		runKnowhereSingle()
	case "knowhere-raw":
		runKnowhereRaw()
	case "vector-only-raw":
		runVectorOnlyRaw()
	case "http-query":
		runHTTPQuery()
	case "http-query-raw":
		runHTTPQueryRaw()
	default:
		fmt.Fprintf(os.Stderr, "unknown mode: %s\n", *mode)
		os.Exit(1)
	}
}

// loadFbin reads a float32 .fbin file with an 8-byte little-endian header: n (uint32), dim (uint32).
func loadFbin(path string, limitN int) (vecs []float32, n int, dim int, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, 0, err
	}
	if len(data) < 8 {
		return nil, 0, 0, fmt.Errorf("file too short for header")
	}
	n = int(binary.LittleEndian.Uint32(data[0:4]))
	dim = int(binary.LittleEndian.Uint32(data[4:8]))
	if n <= 0 || dim <= 0 {
		return nil, 0, 0, fmt.Errorf("invalid header: n=%d dim=%d", n, dim)
	}
	rest := len(data) - 8
	expected := n * dim * 4
	if rest < expected {
		availableN := rest / (dim * 4)
		if availableN == 0 {
			return nil, 0, 0, fmt.Errorf("insufficient data: have %d bytes, need %d", rest, expected)
		}
		n = availableN
	}
	if limitN > 0 && n > limitN {
		n = limitN
	}
	vecs = make([]float32, n*dim)
	offset := 8
	for i := 0; i < n*dim; i++ {
		vecs[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[offset:]))
		offset += 4
	}
	return vecs, n, dim, nil
}

// percentile returns the p-th percentile of sorted data.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// finishResult fills in Recall and GroundTruthPath and emits JSON.
func finishResult(r BenchResult, intIDs []int64, nq int) BenchResult {
	if *groundtruthPath != "" {
		gtIDs, err := loadGroundtruth(*groundtruthPath, nq, r.TopK)
		if err == nil {
			r.Recall = computeRecall(intIDs, gtIDs, nq, r.TopK)
		}
	}
	r.GroundTruthPath = *groundtruthPath
	json.NewEncoder(os.Stdout).Encode(r)
	return r
}

// loadGroundtruth reads an integer binary file (.ibin) with header [nq(uint32)][topk(uint32)]
// and returns a [nq][topk] array of int64 IDs.
func loadGroundtruth(path string, nq int, topk int) ([]int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < 8 {
		return nil, fmt.Errorf("ground truth file too short for header")
	}
	gtNQ := int(binary.LittleEndian.Uint32(data[0:4]))
	gtK := int(binary.LittleEndian.Uint32(data[4:8]))
	if gtNQ < nq {
		return nil, fmt.Errorf("ground truth has fewer queries (%d) than requested (%d)", gtNQ, nq)
	}
	expected := gtNQ * gtK * 8
	if len(data)-8 < expected {
		return nil, fmt.Errorf("ground truth file truncated: have %d bytes, need %d", len(data)-8, expected)
	}
	// Slice for first nq*topk int64s
	ids := make([]int64, nq*topk)
	offset := 8
	for i := 0; i < nq*topk; i++ {
		ids[i] = int64(binary.LittleEndian.Uint64(data[offset:]))
		offset += 8
	}
	return ids, nil
}

// computeRecall compares gotIDs (flattened [nq][topk]) against gtIDs (flattened [nq][topk]).
func computeRecall(gotIDs []int64, gtIDs []int64, nq int, topk int) float64 {
	if len(gotIDs) < nq*topk || len(gtIDs) < nq*topk {
		return 0
	}
	hits := 0
	for i := 0; i < nq; i++ {
		gtSet := make(map[int64]bool)
		for j := 0; j < topk; j++ {
			gtSet[gtIDs[i*topk+j]] = true
		}
		for j := 0; j < topk; j++ {
			if gtSet[gotIDs[i*topk+j]] {
				hits++
			}
		}
	}
	return float64(hits) / float64(nq*topk)
}

// getDatasets returns (indexedVecs, queryVecs, indexedN, queryN, dim) for both
// two-file and single-file modes. indexedVecs and queryVecs are flat float32 arrays.
func getDatasets() (indexedVecs, queryVecs []float32, indexedN, queryN, dim int, err error) {
	if *indexedDataset != "" && *queryDataset != "" {
		// Two-file mode: explicit corpus + query files.
		// Load only up to *indexedCount vectors from the indexed file.
		idxVecs, _, iDim, e := loadFbin(*indexedDataset, *indexedCount)
		if e != nil {
			err = fmt.Errorf("indexed-dataset: %v", e)
			return
		}
		// Load up to *nquery vectors from the query file.
		qVecs, _, qDim, e := loadFbin(*queryDataset, *nquery)
		if e != nil {
			err = fmt.Errorf("query-dataset: %v", e)
			return
		}
		if iDim != qDim {
			err = fmt.Errorf("dimension mismatch: indexed=%d query=%d", iDim, qDim)
			return
		}
		indexedVecs = idxVecs
		queryVecs = qVecs
		indexedN = len(idxVecs) / iDim
		queryN = len(qVecs) / qDim
		dim = iDim
		return
	}
	// Single-file mode: split limit into indexed + queries
	vecs, n, d, e := loadFbin(*dataset, *limit)
	if e != nil {
		err = e
		return
	}
	dim = d
	nIndexed := *indexedCount
	if nIndexed == 0 || nIndexed > n {
		nIndexed = n
	}
	nq := *nquery
	if nq > n {
		nq = n
	}
	indexedN = nIndexed
	queryN = nq
	indexedVecs = vecs[:nIndexed*dim]
	qstart := n - nq
	if qstart < 0 {
		qstart = 0
	}
	// Need at least nIndexed indexed vectors before the queries
	if qstart < nIndexed {
		qstart = nIndexed
	}
	queryVecs = make([]float32, nq*dim)
	for i := 0; i < nq; i++ {
		copy(queryVecs[i*dim:(i+1)*dim], vecs[(qstart+i)*dim:(qstart+i)*dim+dim])
	}
	return
}

// ── shared measurement helpers ──────────────────────────────────────────────────

func measureSearch(segID string, flatQueries []float32, nq, dim, topk int) (
	batchMs, serialMs, serialQPS float64,
	p50, p95, p99, mean float64,
	intIDs []int64, errors int,
) {
	// Warm-up
	_, _, _ = retrievalplane.GlobalSegmentRetriever.Search(segID, flatQueries[:dim], 1, topk)

	// ── Batch search: single call with nq>1 → OpenMP parallel path ──
	tBatch := time.Now()
	intIDs, _, err := retrievalplane.GlobalSegmentRetriever.Search(segID, flatQueries, nq, topk)
	batchMs = time.Since(tBatch).Seconds() * 1000
	if err != nil {
		errors = 1
	}

	// ── Serial search: nq individual calls (nq=1 each → HnswFastSearchFloat hot path) ──
	latencies := make([]float64, nq)
	for i := 0; i < nq; i++ {
		start := time.Now()
		_, _, _ = retrievalplane.GlobalSegmentRetriever.Search(segID, flatQueries[i*dim:(i+1)*dim], 1, topk)
		latencies[i] = time.Since(start).Seconds() * 1000
	}
	serialMs = 0
	for _, l := range latencies {
		serialMs += l
	}
	serialQPS = float64(nq) / (serialMs / 1000.0)

	sortedL := make([]float64, len(latencies))
	copy(sortedL, latencies)
	sort.Float64s(sortedL)
	p50 = percentile(sortedL, 0.50)
	p95 = percentile(sortedL, 0.95)
	p99 = percentile(sortedL, 0.99)
	mean = 0
	for _, l := range latencies {
		mean += l
	}
	if nq > 0 {
		mean /= float64(nq)
	}
	return
}

// ── G1: FAISS HNSW via CGO ───────────────────────────────────────────────────
func runFAISS() {
	idxVecs, qVecs, nIndexed, nq, dim, err := getDatasets()
	if err != nil {
		fmt.Fprintf(os.Stderr, "getDatasets: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "[G1 FAISS] indexed=%d queries=%d dim=%d\n", nIndexed, nq, dim)

	// Normalize for inner-product consistency with Knowhere (IP metric).
	norms := make([]float64, nIndexed)
	for i := 0; i < nIndexed; i++ {
		s := 0.0
		for j := 0; j < dim; j++ {
			v := float64(idxVecs[i*dim+j])
			s += v * v
		}
		norms[i] = math.Sqrt(s)
		if norms[i] == 0 {
			norms[i] = 1
		}
	}
	normIdxVecs := make([]float32, nIndexed*dim)
	for i := 0; i < nIndexed; i++ {
		for j := 0; j < dim; j++ {
			normIdxVecs[i*dim+j] = float32(float64(idxVecs[i*dim+j]) / norms[i])
		}
	}

	// Normalize query vectors too.
	normQVecs := make([]float32, nq*dim)
	for i := 0; i < nq; i++ {
		s := 0.0
		for j := 0; j < dim; j++ {
			v := float64(qVecs[i*dim+j])
			s += v * v
		}
		inv := 1.0 / math.Sqrt(s)
		if math.IsInf(inv, 0) || inv == 0 {
			inv = 1.0
		}
		for j := 0; j < dim; j++ {
			normQVecs[i*dim+j] = float32(float64(qVecs[i*dim+j]) * inv)
		}
	}

	// Build FAISS index from first nIndexed normalized vectors.
	faissIdx := retrievalplane.NewFaissHNSW()
	defer faissIdx.Close()
	t0 := time.Now()
	if err := faissIdx.Build(normIdxVecs, nIndexed, dim, 16, 256); err != nil {
		fmt.Fprintf(os.Stderr, "FAISS BuildSegment: %v\n", err)
		os.Exit(1)
	}
	buildMs := time.Since(t0).Seconds() * 1000

	// Warm-up
	_, _, _ = faissIdx.Search(normQVecs[:dim], 1, *topk)

	// ── Batch search: single call (FAISS parallel internally) ──
	tBatch := time.Now()
	intIDs, _, err := faissIdx.Search(normQVecs, nq, *topk)
	batchMs := time.Since(tBatch).Seconds() * 1000
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAISS batch search: %v\n", err)
		os.Exit(1)
	}

	// ── Serial search: nq individual calls ──
	latencies := make([]float64, nq)
	for i := 0; i < nq; i++ {
		start := time.Now()
		_, _, _ = faissIdx.Search(normQVecs[i*dim:(i+1)*dim], 1, *topk)
		latencies[i] = time.Since(start).Seconds() * 1000
	}
	serialMs := 0.0
	for _, l := range latencies {
		serialMs += l
	}
	serialQPS := float64(nq) / (serialMs / 1000.0)

	sortedL := make([]float64, len(latencies))
	copy(sortedL, latencies)
	sort.Float64s(sortedL)
	p50 := percentile(sortedL, 0.50)
	p95 := percentile(sortedL, 0.95)
	p99 := percentile(sortedL, 0.99)
	mean := 0.0
	for _, l := range latencies {
		mean += l
	}
	if nq > 0 {
		mean /= float64(nq)
	}

	result := BenchResult{
		Mode:      "G1_FAISS",
		NIndexed:  nIndexed,
		NQueries:  nq,
		TopK:      *topk,
		Dim:       dim,
		BuildMs:   buildMs,
		BatchMs:   batchMs,
		BatchQPS:  float64(nq) / (batchMs / 1000.0),
		SerialMs:  serialMs,
		SerialQPS: serialQPS,
		MeanMs:    mean,
		P50Ms:     p50,
		P95Ms:     p95,
		P99Ms:     p99,
		Errors:     0,
		IntIDs:    intIDs,
	}
	finishResult(result, intIDs, nq)
}

// ── G1-single: FAISS HNSW — serial-per-query path (nq=1 loop) ─────────────────
func runFAISSSingle() {
	idxVecs, qVecs, nIndexed, nq, dim, err := getDatasets()
	if err != nil {
		fmt.Fprintf(os.Stderr, "getDatasets: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "[G1-single FAISS] indexed=%d queries=%d dim=%d\n", nIndexed, nq, dim)

	// Normalize indexed vectors for FAISS inner-product search.
	norms := make([]float64, nIndexed)
	for i := 0; i < nIndexed; i++ {
		s := 0.0
		for j := 0; j < dim; j++ {
			v := float64(idxVecs[i*dim+j])
			s += v * v
		}
		norms[i] = math.Sqrt(s)
		if norms[i] == 0 {
			norms[i] = 1
		}
	}
	normIdxVecs := make([]float32, nIndexed*dim)
	for i := 0; i < nIndexed; i++ {
		for j := 0; j < dim; j++ {
			normIdxVecs[i*dim+j] = float32(float64(idxVecs[i*dim+j]) / norms[i])
		}
	}

	// Normalize query vectors.
	normQVecs := make([]float32, nq*dim)
	for i := 0; i < nq; i++ {
		s := 0.0
		for j := 0; j < dim; j++ {
			v := float64(qVecs[i*dim+j])
			s += v * v
		}
		inv := 1.0 / math.Sqrt(s)
		if math.IsInf(inv, 0) || inv == 0 {
			inv = 1.0
		}
		for j := 0; j < dim; j++ {
			normQVecs[i*dim+j] = float32(float64(qVecs[i*dim+j]) * inv)
		}
	}

	faissIdx := retrievalplane.NewFaissHNSW()
	defer faissIdx.Close()
	t0 := time.Now()
	if err := faissIdx.Build(normIdxVecs, nIndexed, dim, 16, 256); err != nil {
		fmt.Fprintf(os.Stderr, "FAISS BuildSegment: %v\n", err)
		os.Exit(1)
	}
	buildMs := time.Since(t0).Seconds() * 1000

	// Warm-up
	_, _, _ = faissIdx.Search(normQVecs[:dim], 1, *topk)

	// Serial path: nq individual nq=1 calls (FAISS HnswFastSearchFloat hot path)
	latencies := make([]float64, nq)
	for i := 0; i < nq; i++ {
		start := time.Now()
		_, _, _ = faissIdx.Search(normQVecs[i*dim:(i+1)*dim], 1, *topk)
		latencies[i] = time.Since(start).Seconds() * 1000
	}
	serialMs := 0.0
	for _, l := range latencies {
		serialMs += l
	}
	serialQPS := float64(nq) / (serialMs / 1000.0)

	sortedL := make([]float64, len(latencies))
	copy(sortedL, latencies)
	sort.Float64s(sortedL)
	p50 := percentile(sortedL, 0.50)
	p95 := percentile(sortedL, 0.95)
	p99 := percentile(sortedL, 0.99)
	mean := 0.0
	for _, l := range latencies {
		mean += l
	}
	if nq > 0 {
		mean /= float64(nq)
	}

	// Collect int IDs for recall reference
	intIDs := make([]int64, 0, nq*(*topk))
	for i := 0; i < nq; i++ {
		ids, _, _ := faissIdx.Search(normQVecs[i*dim:(i+1)*dim], 1, *topk)
		intIDs = append(intIDs, ids...)
	}

	r := BenchResult{
		Mode:      "G1_FAISS_single",
		NIndexed:  nIndexed,
		NQueries:  nq,
		TopK:      *topk,
		Dim:       dim,
		BuildMs:   buildMs,
		BatchMs:   serialMs,
		BatchQPS:  serialQPS,
		SerialMs:  serialMs,
		SerialQPS: serialQPS,
		MeanMs:    mean,
		P50Ms:     p50,
		P95Ms:     p95,
		P99Ms:     p99,
		Errors:    0,
		IntIDs:    intIDs,
	}
	finishResult(r, intIDs, nq)
}

// ── G2: Knowhere via CGO (OpenMP parallel batch search) ────────────────────
func runKnowhereBuild() {
	idxVecs, qVecs, nIndexed, nq, dim, err := getDatasets()
	if err != nil {
		fmt.Fprintf(os.Stderr, "getDatasets: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "[G2 Knowhere] indexed=%d queries=%d dim=%d\n", nIndexed, nq, dim)

	segID := *segmentID
	_ = retrievalplane.GlobalSegmentRetriever.UnloadSegment(segID)
	t0 := time.Now()
	if err := retrievalplane.GlobalSegmentRetriever.BuildSegment(segID, idxVecs, nIndexed, dim); err != nil {
		fmt.Fprintf(os.Stderr, "BuildSegment: %v\n", err)
		os.Exit(1)
	}
	buildMs := time.Since(t0).Seconds() * 1000

	batchMs, serialMs, serialQPS, p50, p95, p99, mean, intIDs, errors := measureSearch(segID, qVecs, nq, dim, *topk)

	result := BenchResult{
		Mode:      "G2_Knowhere_ctypes",
		NIndexed:  nIndexed,
		NQueries:  nq,
		TopK:      *topk,
		Dim:       dim,
		BuildMs:   buildMs,
		BatchMs:   batchMs,
		BatchQPS:  float64(nq) / (batchMs / 1000.0),
		SerialMs:  serialMs,
		SerialQPS: serialQPS,
		MeanMs:    mean,
		P50Ms:     p50,
		P95Ms:     p95,
		P99Ms:     p99,
		Errors:    errors,
		IntIDs:    intIDs,
	}
	finishResult(result, intIDs, nq)
}

// ── G2-single: Knowhere via CGO — serial-per-query path (loop nq=1) ───────────
func runKnowhereSingle() {
	idxVecs, qVecs, nIndexed, nq, dim, err := getDatasets()
	if err != nil {
		fmt.Fprintf(os.Stderr, "getDatasets: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "[G2-single Knowhere] indexed=%d queries=%d dim=%d\n", nIndexed, nq, dim)

	segID := *segmentID
	t0 := time.Now()
	if err := retrievalplane.GlobalSegmentRetriever.BuildSegment(segID, idxVecs, nIndexed, dim); err != nil {
		fmt.Fprintf(os.Stderr, "BuildSegment: %v\n", err)
		os.Exit(1)
	}
	buildMs := time.Since(t0).Seconds() * 1000

	// Serial search: nq individual nq=1 calls (HnswFastSearchFloat hot path)
	latencies := make([]float64, nq)
	for i := 0; i < nq; i++ {
		start := time.Now()
		_, _, _ = retrievalplane.GlobalSegmentRetriever.Search(segID, qVecs[i*dim:(i+1)*dim], 1, *topk)
		latencies[i] = time.Since(start).Seconds() * 1000
	}
	serialMs := 0.0
	for _, l := range latencies {
		serialMs += l
	}
	serialQPS := float64(nq) / (serialMs / 1000.0)

	sortedL := make([]float64, len(latencies))
	copy(sortedL, latencies)
	sort.Float64s(sortedL)
	p50 := percentile(sortedL, 0.50)
	p95 := percentile(sortedL, 0.95)
	p99 := percentile(sortedL, 0.99)
	mean := 0.0
	for _, l := range latencies {
		mean += l
	}
	if nq > 0 {
		mean /= float64(nq)
	}

	// Collect int IDs for recall reference
	intIDs := make([]int64, 0, nq*(*topk))
	for i := 0; i < nq; i++ {
		ids, _, _ := retrievalplane.GlobalSegmentRetriever.Search(segID, qVecs[i*dim:(i+1)*dim], 1, *topk)
		intIDs = append(intIDs, ids...)
	}

	result := BenchResult{
		Mode:      "G2_Knowhere_single",
		NIndexed:  nIndexed,
		NQueries:  nq,
		TopK:      *topk,
		Dim:       dim,
		BuildMs:   buildMs,
		BatchMs:   serialMs,
		BatchQPS:  serialQPS,
		SerialMs:  serialMs,
		SerialQPS: serialQPS,
		MeanMs:    mean,
		P50Ms:     p50,
		P95Ms:     p95,
		P99Ms:     p99,
		Errors:    0,
		IntIDs:    intIDs,
	}
	finishResult(result, intIDs, nq)
}

// ── G2-raw: Knowhere via CGO — standard Knowhere batch (NO plugin reorder) ────
func runKnowhereRaw() {
	idxVecs, qVecs, nIndexed, nq, dim, err := getDatasets()
	if err != nil {
		fmt.Fprintf(os.Stderr, "getDatasets: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "[G2-raw Knowhere] indexed=%d queries=%d dim=%d\n", nIndexed, nq, dim)

	segID := *segmentID
	_ = retrievalplane.GlobalSegmentRetriever.UnloadSegment(segID)
	t0 := time.Now()
	if err := retrievalplane.GlobalSegmentRetriever.BuildSegment(segID, idxVecs, nIndexed, dim); err != nil {
		fmt.Fprintf(os.Stderr, "BuildSegment: %v\n", err)
		os.Exit(1)
	}
	buildMs := time.Since(t0).Seconds() * 1000

	// Warm-up
	_, _, _ = retrievalplane.GlobalSegmentRetriever.SearchRaw(segID, qVecs[:dim], 1, *topk)

	// Batch search via SearchRaw (standard Knowhere, no plugin reorder)
	tBatch := time.Now()
	intIDs, _, err := retrievalplane.GlobalSegmentRetriever.SearchRaw(segID, qVecs, nq, *topk)
	batchMs := time.Since(tBatch).Seconds() * 1000
	if err != nil {
		fmt.Fprintf(os.Stderr, "SearchRaw batch: %v\n", err)
		os.Exit(1)
	}

	// Serial: nq individual SearchRaw calls
	latencies := make([]float64, nq)
	for i := 0; i < nq; i++ {
		start := time.Now()
		_, _, _ = retrievalplane.GlobalSegmentRetriever.SearchRaw(segID, qVecs[i*dim:(i+1)*dim], 1, *topk)
		latencies[i] = time.Since(start).Seconds() * 1000
	}
	serialMs := 0.0
	for _, l := range latencies {
		serialMs += l
	}
	serialQPS := float64(nq) / (serialMs / 1000.0)

	sortedL := make([]float64, len(latencies))
	copy(sortedL, latencies)
	sort.Float64s(sortedL)
	p50 := percentile(sortedL, 0.50)
	p95 := percentile(sortedL, 0.95)
	p99 := percentile(sortedL, 0.99)
	mean := 0.0
	for _, l := range latencies {
		mean += l
	}
	if nq > 0 {
		mean /= float64(nq)
	}

	result := BenchResult{
		Mode:      "G2_Knowhere_raw",
		NIndexed:  nIndexed,
		NQueries:  nq,
		TopK:      *topk,
		Dim:       dim,
		BuildMs:   buildMs,
		BatchMs:   batchMs,
		BatchQPS:  float64(nq) / (batchMs / 1000.0),
		SerialMs:  serialMs,
		SerialQPS: serialQPS,
		MeanMs:    mean,
		P50Ms:     p50,
		P95Ms:     p95,
		P99Ms:     p99,
		Errors:    0,
		IntIDs:    intIDs,
	}
	finishResult(result, intIDs, nq)
}

// ── G3: GlobalSegmentRetriever.Search via CGO ─────────────────────────────────
func runVectorOnly() {
	idxVecs, qVecs, nIndexed, nq, dim, err := getDatasets()
	if err != nil {
		fmt.Fprintf(os.Stderr, "getDatasets: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "[G3 Plasmod] indexed=%d queries=%d dim=%d\n", nIndexed, nq, dim)

	segID := *segmentID
	_ = retrievalplane.GlobalSegmentRetriever.UnloadSegment(segID)
	t0 := time.Now()
	if err := retrievalplane.GlobalSegmentRetriever.BuildSegment(segID, idxVecs, nIndexed, dim); err != nil {
		fmt.Fprintf(os.Stderr, "BuildSegment: %v\n", err)
		os.Exit(1)
	}
	buildMs := time.Since(t0).Seconds() * 1000

	batchMs, serialMs, serialQPS, p50, p95, p99, mean, intIDs, errors := measureSearch(segID, qVecs, nq, dim, *topk)

	result := BenchResult{
		Mode:      "G3_Plasmod_cgo",
		NIndexed:  nIndexed,
		NQueries:  nq,
		TopK:      *topk,
		Dim:       dim,
		BuildMs:   buildMs,
		BatchMs:   batchMs,
		BatchQPS:  float64(nq) / (batchMs / 1000.0),
		SerialMs:  serialMs,
		SerialQPS: serialQPS,
		MeanMs:    mean,
		P50Ms:     p50,
		P95Ms:     p95,
		P99Ms:     p99,
		Errors:    errors,
		IntIDs:    intIDs,
	}
	finishResult(result, intIDs, nq)
}

// ── G3-raw: Plasmod Bridge — standard Knowhere batch (no plugin reorder) ──────
func runVectorOnlyRaw() {
	idxVecs, qVecs, nIndexed, nq, dim, err := getDatasets()
	if err != nil {
		fmt.Fprintf(os.Stderr, "getDatasets: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "[G3-raw Plasmod] indexed=%d queries=%d dim=%d\n", nIndexed, nq, dim)

	segID := *segmentID
	_ = retrievalplane.GlobalSegmentRetriever.UnloadSegment(segID)
	t0 := time.Now()
	if err := retrievalplane.GlobalSegmentRetriever.BuildSegment(segID, idxVecs, nIndexed, dim); err != nil {
		fmt.Fprintf(os.Stderr, "BuildSegment: %v\n", err)
		os.Exit(1)
	}
	buildMs := time.Since(t0).Seconds() * 1000

	// Warm-up
	_, _, _ = retrievalplane.GlobalSegmentRetriever.SearchRaw(segID, qVecs[:dim], 1, *topk)

	// Batch search via SearchRaw (standard Knowhere, no plugin reorder)
	tBatch := time.Now()
	intIDs, _, err := retrievalplane.GlobalSegmentRetriever.SearchRaw(segID, qVecs, nq, *topk)
	batchMs := time.Since(tBatch).Seconds() * 1000
	if err != nil {
		fmt.Fprintf(os.Stderr, "SearchRaw batch: %v\n", err)
		os.Exit(1)
	}

	// Serial: nq individual SearchRaw calls (nq=1 each, Knowhere Index::Search)
	latencies := make([]float64, nq)
	for i := 0; i < nq; i++ {
		start := time.Now()
		_, _, _ = retrievalplane.GlobalSegmentRetriever.SearchRaw(segID, qVecs[i*dim:(i+1)*dim], 1, *topk)
		latencies[i] = time.Since(start).Seconds() * 1000
	}
	serialMs := 0.0
	for _, l := range latencies {
		serialMs += l
	}
	serialQPS := float64(nq) / (serialMs / 1000.0)

	sortedL := make([]float64, len(latencies))
	copy(sortedL, latencies)
	sort.Float64s(sortedL)
	p50 := percentile(sortedL, 0.50)
	p95 := percentile(sortedL, 0.95)
	p99 := percentile(sortedL, 0.99)
	mean := 0.0
	for _, l := range latencies {
		mean += l
	}
	if nq > 0 {
		mean /= float64(nq)
	}

	result := BenchResult{
		Mode:      "G3_Plasmod_raw",
		NIndexed:  nIndexed,
		NQueries:  nq,
		TopK:      *topk,
		Dim:       dim,
		BuildMs:   buildMs,
		BatchMs:   batchMs,
		BatchQPS:  float64(nq) / (batchMs / 1000.0),
		SerialMs:  serialMs,
		SerialQPS: serialQPS,
		MeanMs:    mean,
		P50Ms:     p50,
		P95Ms:     p95,
		P99Ms:     p99,
		Errors:    0,
		IntIDs:    intIDs,
	}
	finishResult(result, intIDs, nq)
}

// ── G4: HTTP batch query ───────────────────────────────────────────────────
func runHTTPQuery() {
	idxVecs, qVecs, nIndexed, nq, dim, err := getDatasets()
	if err != nil {
		fmt.Fprintf(os.Stderr, "getDatasets: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "[G4 HTTP] indexed=%d queries=%d dim=%d\n", nIndexed, nq, dim)

	segID := *segmentID
	ingestURL := *serverURL + "/v1/internal/rpc/ingest_batch"
	unloadURL := *serverURL + "/v1/internal/rpc/unload_segment"

	// Evict any stale segment so the next ingest triggers a fresh HNSW build.
	fmt.Fprintf(os.Stderr, "[G4 http] unloading stale segment=%s\n", segID)
	unloadReq, _ := http.NewRequest("POST", unloadURL, bytes.NewBufferString(
		fmt.Sprintf(`{"segment_id":%q}`, segID)))
	unloadReq.Header.Set("Content-Type", "application/json")
	unloadResp, unloadErr := (&http.Client{Timeout: 10 * time.Second}).Do(unloadReq)
	if unloadErr == nil {
		unloadResp.Body.Close()
	} // ignore errors (segment may not exist yet)

	fmt.Fprintf(os.Stderr, "[G4 http] ingesting %d indexed vectors into segment=%s\n", nIndexed, segID)

	var buf bytes.Buffer
	buf.Write([]byte("PLIB"))
	buf.WriteByte(2) // wire version 2
	binary.Write(&buf, binary.LittleEndian, uint16(len(segID)))
	buf.WriteString(segID)
	binary.Write(&buf, binary.LittleEndian, uint32(nIndexed))
	binary.Write(&buf, binary.LittleEndian, uint32(dim))
	for i := 0; i < nIndexed; i++ {
		for j := 0; j < dim; j++ {
			binary.Write(&buf, binary.LittleEndian, idxVecs[i*dim+j])
		}
	}
	for i := 0; i < nIndexed; i++ {
		id := fmt.Sprintf("bench-g4-%06d", i)
		binary.Write(&buf, binary.LittleEndian, uint16(len(id)))
		buf.WriteString(id)
	}

	t0 := time.Now()
	req, _ := http.NewRequest("POST", ingestURL, bytes.NewReader(buf.Bytes()))
	req.Header.Set("Content-Type", "application/octet-stream")
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest request: %v\n", err)
		os.Exit(1)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		fmt.Fprintf(os.Stderr, "ingest HTTP %d: %s\n", resp.StatusCode, string(body))
		os.Exit(1)
	}
	ingestMs := time.Since(t0).Seconds() * 1000
	fmt.Fprintf(os.Stderr, "[G4 http] ingest done (%.1f ms)\n", ingestMs)

	batchSz := *batchSize
	if batchSz <= 0 {
		batchSz = 100
	}
	httpClient := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        batchSz,
			MaxIdleConnsPerHost: batchSz,
			IdleConnTimeout:     120 * time.Second,
			DisableKeepAlives:   false,
		},
		Timeout: 30 * time.Second,
	}

	baseURL := *serverURL
	latencies := make([]float64, 0, nq)
	allIntIDs := make([]int64, 0, nq*(*topk))
	errors := 0

	batchStart := time.Now()
	for batchStartIdx := 0; batchStartIdx < nq; batchStartIdx += batchSz {
		batchEnd := batchStartIdx + batchSz
		if batchEnd > nq {
			batchEnd = nq
		}
		batchNQ := batchEnd - batchStartIdx

		var qbuf bytes.Buffer
		qbuf.Write([]byte("PLQB"))
		qbuf.WriteByte(1)
		binary.Write(&qbuf, binary.LittleEndian, uint16(len(segID)))
		qbuf.WriteString(segID)
		binary.Write(&qbuf, binary.LittleEndian, uint32(*topk))
		binary.Write(&qbuf, binary.LittleEndian, uint32(batchNQ))
		binary.Write(&qbuf, binary.LittleEndian, uint32(dim))
		for i := batchStartIdx; i < batchEnd; i++ {
			for j := 0; j < dim; j++ {
				binary.Write(&qbuf, binary.LittleEndian, qVecs[i*dim+j])
			}
		}

		qStart := time.Now()
		qreq, _ := http.NewRequest("POST", baseURL+"/v1/internal/rpc/query_warm_batch", bytes.NewReader(qbuf.Bytes()))
		qreq.Header.Set("Content-Type", "application/octet-stream")
		qresp, err := httpClient.Do(qreq)
		qElapsed := time.Since(qStart).Seconds() * 1000

		if err != nil || qresp.StatusCode != http.StatusOK {
			if err == nil {
				qresp.Body.Close()
			}
			errors += batchNQ
			for i := 0; i < batchNQ; i++ {
				latencies = append(latencies, qElapsed)
			}
			continue
		}

		body, _ := io.ReadAll(qresp.Body)
		qresp.Body.Close()

		if len(body) >= 8 {
			respNQ := int(binary.LittleEndian.Uint32(body[0:4]))
			respTopK := int(binary.LittleEndian.Uint32(body[4:8]))
			for i := 0; i < respNQ*respTopK; i++ {
				id := int64(binary.LittleEndian.Uint64(body[8+i*8 : 8+i*8+8]))
				allIntIDs = append(allIntIDs, id)
			}
		}

		perQuery := qElapsed / float64(batchNQ)
		for i := 0; i < batchNQ; i++ {
			latencies = append(latencies, perQuery)
		}
	}

	batchMs := time.Since(batchStart).Seconds() * 1000
	serialMs := 0.0
	for _, l := range latencies {
		serialMs += l
	}
	serialQPS := float64(nq) / (serialMs / 1000.0)

	sortedL := make([]float64, len(latencies))
	copy(sortedL, latencies)
	sort.Float64s(sortedL)
	p50 := percentile(sortedL, 0.50)
	p95 := percentile(sortedL, 0.95)
	p99 := percentile(sortedL, 0.99)
	mean := 0.0
	for _, l := range latencies {
		mean += l
	}
	if len(latencies) > 0 {
		mean /= float64(len(latencies))
	}

	result := BenchResult{
		Mode:      "G4_HTTP_batch",
		NIndexed:  nIndexed,
		NQueries:  nq,
		TopK:      *topk,
		Dim:       dim,
		BuildMs:   ingestMs,
		BatchMs:   batchMs,
		BatchQPS:  float64(nq) / (batchMs / 1000.0),
		SerialMs:  serialMs,
		SerialQPS: serialQPS,
		MeanMs:    mean,
		P50Ms:     p50,
		P95Ms:     p95,
		P99Ms:     p99,
		Errors:    errors,
		IntIDs:    allIntIDs,
	}
	finishResult(result, allIntIDs, nq)
}

// ── G4-raw: Plasmod HTTP E2E — standard Knowhere batch (no plugin reorder) ─────
func runHTTPQueryRaw() {
	idxVecs, qVecs, nIndexed, nq, dim, err := getDatasets()
	if err != nil {
		fmt.Fprintf(os.Stderr, "getDatasets: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "[G4-raw HTTP] indexed=%d queries=%d dim=%d\n", nIndexed, nq, dim)

	segID := *segmentID
	ingestURL := *serverURL + "/v1/internal/rpc/ingest_batch"
	unloadURL := *serverURL + "/v1/internal/rpc/unload_segment"

	fmt.Fprintf(os.Stderr, "[G4-raw http] unloading stale segment=%s\n", segID)
	unloadReq, _ := http.NewRequest("POST", unloadURL, bytes.NewBufferString(
		fmt.Sprintf(`{"segment_id":%q}`, segID)))
	unloadReq.Header.Set("Content-Type", "application/json")
	unloadResp, _ := (&http.Client{Timeout: 10 * time.Second}).Do(unloadReq)
	if unloadResp != nil {
		unloadResp.Body.Close()
	}

	fmt.Fprintf(os.Stderr, "[G4-raw http] ingesting %d indexed vectors into segment=%s\n", nIndexed, segID)

	var buf bytes.Buffer
	buf.Write([]byte("PLIB"))
	buf.WriteByte(2)
	binary.Write(&buf, binary.LittleEndian, uint16(len(segID)))
	buf.WriteString(segID)
	binary.Write(&buf, binary.LittleEndian, uint32(nIndexed))
	binary.Write(&buf, binary.LittleEndian, uint32(dim))
	for i := 0; i < nIndexed; i++ {
		for j := 0; j < dim; j++ {
			binary.Write(&buf, binary.LittleEndian, idxVecs[i*dim+j])
		}
	}
	for i := 0; i < nIndexed; i++ {
		id := fmt.Sprintf("bench-g4r-%06d", i)
		binary.Write(&buf, binary.LittleEndian, uint16(len(id)))
		buf.WriteString(id)
	}

	t0 := time.Now()
	req, _ := http.NewRequest("POST", ingestURL, bytes.NewReader(buf.Bytes()))
	req.Header.Set("Content-Type", "application/octet-stream")
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest request: %v\n", err)
		os.Exit(1)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		fmt.Fprintf(os.Stderr, "ingest HTTP %d: %s\n", resp.StatusCode, string(body))
		os.Exit(1)
	}
	ingestMs := time.Since(t0).Seconds() * 1000
	fmt.Fprintf(os.Stderr, "[G4-raw http] ingest done (%.1f ms)\n", ingestMs)

	// Register the warm segment with object IDs
	registerURL := *serverURL + "/v1/internal/rpc/register_warm"
	regIDs := make([]string, nIndexed)
	for i := 0; i < nIndexed; i++ {
		regIDs[i] = fmt.Sprintf("bench-g4r-%06d", i)
	}
	regBody, _ := json.Marshal(map[string]any{"segment_id": segID, "object_ids": regIDs})
	regReq, _ := http.NewRequest("POST", registerURL, bytes.NewReader(regBody))
	regReq.Header.Set("Content-Type", "application/json")
	regResp, _ := client.Do(regReq)
	if regResp != nil {
		regResp.Body.Close()
	}

	// SearchWarmSegmentBatch uses SearchRaw internally
	batchSz := *batchSize
	if batchSz <= 0 {
		batchSz = 1000
	}
	httpClient := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        batchSz,
			MaxIdleConnsPerHost: batchSz,
			IdleConnTimeout:     120 * time.Second,
			DisableKeepAlives:   false,
		},
		Timeout: 30 * time.Second,
	}

	// ── Batch query via SearchWarmSegmentBatchRaw (SearchRaw path) ─────────────
	batchStart := time.Now()
	allIntIDs := make([]int64, 0, nq*(*topk))
	for batchStartIdx := 0; batchStartIdx < nq; batchStartIdx += batchSz {
		batchEnd := batchStartIdx + batchSz
		if batchEnd > nq {
			batchEnd = nq
		}
		batchNQ := batchEnd - batchStartIdx

		var qbuf bytes.Buffer
		qbuf.Write([]byte("PLQB"))
		qbuf.WriteByte(1)
		binary.Write(&qbuf, binary.LittleEndian, uint16(len(segID)))
		qbuf.WriteString(segID)
		binary.Write(&qbuf, binary.LittleEndian, uint32(*topk))
		binary.Write(&qbuf, binary.LittleEndian, uint32(batchNQ))
		binary.Write(&qbuf, binary.LittleEndian, uint32(dim))
		for i := batchStartIdx; i < batchEnd; i++ {
			for j := 0; j < dim; j++ {
				binary.Write(&qbuf, binary.LittleEndian, qVecs[i*dim+j])
			}
		}

		qStart := time.Now()
		qreq, _ := http.NewRequest("POST",
			*serverURL+"/v1/internal/rpc/query_warm_batch_raw",
			bytes.NewReader(qbuf.Bytes()))
		qreq.Header.Set("Content-Type", "application/octet-stream")
		qresp, err := httpClient.Do(qreq)
		qElapsed := time.Since(qStart).Seconds() * 1000

		if err != nil || qresp.StatusCode != http.StatusOK {
			if qresp != nil {
				qresp.Body.Close()
			}
			continue
		}

		body, _ := io.ReadAll(qresp.Body)
		qresp.Body.Close()

		if len(body) >= 8 {
			for i := 0; i < batchNQ*(*topk); i++ {
				id := int64(binary.LittleEndian.Uint64(body[8+i*8 : 8+i*8+8]))
				allIntIDs = append(allIntIDs, id)
			}
		}
		_ = qElapsed
	}

	batchMs := time.Since(batchStart).Seconds() * 1000

	// Serial: nq individual calls (for S-QPS reference, not a real HTTP path)
	serialMs := 0.0
	for i := 0; i < nq; i++ {
		start := time.Now()
		var qbuf bytes.Buffer
		qbuf.Write([]byte("PLQB"))
		qbuf.WriteByte(1)
		binary.Write(&qbuf, binary.LittleEndian, uint16(len(segID)))
		qbuf.WriteString(segID)
		binary.Write(&qbuf, binary.LittleEndian, uint32(*topk))
		binary.Write(&qbuf, binary.LittleEndian, uint32(1))
		binary.Write(&qbuf, binary.LittleEndian, uint32(dim))
		for j := 0; j < dim; j++ {
			binary.Write(&qbuf, binary.LittleEndian, qVecs[i*dim+j])
		}
		qreq, _ := http.NewRequest("POST",
			*serverURL+"/v1/internal/rpc/query_warm_batch",
			bytes.NewReader(qbuf.Bytes()))
		qreq.Header.Set("Content-Type", "application/octet-stream")
		qresp, _ := httpClient.Do(qreq)
		if qresp != nil {
			qresp.Body.Close()
		}
		serialMs += time.Since(start).Seconds() * 1000
	}
	serialQPS := float64(nq) / (serialMs / 1000.0)

	latencies := make([]float64, nq)
	for i := 0; i < nq; i++ {
		latencies[i] = serialMs / float64(nq)
	}
	sortedL := make([]float64, len(latencies))
	copy(sortedL, latencies)
	sort.Float64s(sortedL)
	p50 := percentile(sortedL, 0.50)
	p95 := percentile(sortedL, 0.95)
	p99 := percentile(sortedL, 0.99)
	mean := 0.0
	for _, l := range latencies {
		mean += l
	}
	if nq > 0 {
		mean /= float64(nq)
	}

	result := BenchResult{
		Mode:      "G4_HTTP_raw",
		NIndexed:  nIndexed,
		NQueries:  nq,
		TopK:      *topk,
		Dim:       dim,
		BuildMs:   ingestMs,
		BatchMs:   batchMs,
		BatchQPS:  float64(nq) / (batchMs / 1000.0),
		SerialMs:  serialMs,
		SerialQPS: serialQPS,
		MeanMs:    mean,
		P50Ms:     p50,
		P95Ms:     p95,
		P99Ms:     p99,
		Errors:    0,
		IntIDs:    allIntIDs,
	}
	finishResult(result, allIntIDs, nq)
}
