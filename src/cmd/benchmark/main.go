//go:build retrieval
// +build retrieval

// cmd/benchmark/main.go — Plasmod retrieval benchmark binary.
//
// Modes:
//   knowhere-build  — load vectors, build Knowhere HNSW via cgo, run batch+serial search, return int_ids for recall
//   vector-only      — call GlobalSegmentRetriever.Search, return int IDs for recall
//   http-query       — batch query via HTTP /query_warm_batch, return int IDs for recall
//
// G1 (FAISS) is handled by the Python benchmark script.
// This binary handles G2 (Knowhere via cgo), G3 (Plasmod direct Go call), and G4 (HTTP batch).
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
	mode         = flag.String("mode", "vector-only", "vector-only|knowhere-build|http-query")
	dataset      = flag.String("dataset", "", "Path to .fbin test data")
	limit        = flag.Int("limit", 10000, "Max vectors to load")
	nquery       = flag.Int("queries", 1000, "Number of queries")
	topk         = flag.Int("topk", 10, "Top-k results")
	segmentID    = flag.String("segment", "bench.layer1", "Warm segment ID")
	serverURL    = flag.String("server-url", "http://127.0.0.1:8080", "Plasmod server URL for http-query mode")
	concurrency  = flag.Int("concurrency", 16, "Concurrency for http-query mode")
	batchSize    = flag.Int("batch-size", 100, "Batch size for http-query mode (queries per HTTP request)")
	indexedCount = flag.Int("indexed-count", 0, "Number of vectors to index (0=all loaded). Used to keep indexed/query sets disjoint for correct recall.")
)

type BenchResult struct {
	Mode          string   `json:"mode"`
	NIndexed      int      `json:"n_indexed"`
	NQueries      int      `json:"n_queries"`
	TopK          int      `json:"topk"`
	Dim           int      `json:"dim"`
	BuildMs       float64  `json:"build_ms"`
	WallMs        float64  `json:"wall_ms"`
	QPS           float64  `json:"qps"`
	MeanMs        float64  `json:"mean_ms"`
	P50Ms         float64  `json:"p50_ms"`
	P95Ms         float64  `json:"p95_ms"`
	P99Ms         float64  `json:"p99_ms"`
	Errors        int      `json:"errors"`
	RetrieverMode string   `json:"retriever_mode"`
	// IntIDs holds integer indices for the last query batch (used for recall calculation).
	// Flat row-major: [q0_ids...][q1_ids...]...
	IntIDs []int64 `json:"int_ids,omitempty"`
}

func main() {
	// KMP_DUPLICATE_LIB_OK: both Python ctypes (G2) and this Go binary load libomp.
	// Setting this before any CGO call prevents the duplicate-runtime SIGABRT.
	os.Setenv("KMP_DUPLICATE_LIB_OK", "TRUE")

	flag.Parse()
	switch *mode {
	case "vector-only":
		runVectorOnly()
	case "knowhere-build":
		runKnowhereBuild()
	case "http-query":
		runHTTPQuery()
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

// runVectorOnly measures GlobalSegmentRetriever.Search (G3) with precomputed vectors.
// Returns integer indices so the Python caller can compute recall vs G1/G2 ground truth.
func runVectorOnly() {
	if *dataset == "" {
		fmt.Fprintf(os.Stderr, "--dataset required for vector-only mode\n")
		os.Exit(1)
	}

	vecs, n, dim, err := loadFbin(*dataset, *limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load fbin: %v\n", err)
		os.Exit(1)
	}
	dim = len(vecs) / n
	fmt.Fprintf(os.Stderr, "[G3] loaded %d vecs dim=%d from %s\n", n, dim, *dataset)

	// Determine how many vectors to index (disjoint from query set for correct recall).
	nIndexed := *indexedCount
	if nIndexed == 0 {
		nIndexed = n
	}
	if nIndexed > n {
		nIndexed = n
	}
	nq := *nquery
	if nq > n {
		nq = n
	}

	// Build segment from only the indexed vectors (first nIndexed).
	segID := *segmentID
	t0 := time.Now()
	if err := retrievalplane.GlobalSegmentRetriever.BuildSegment(segID, vecs, nIndexed, dim); err != nil {
		fmt.Fprintf(os.Stderr, "BuildSegment: %v\n", err)
		os.Exit(1)
	}
	buildMs := time.Since(t0).Seconds() * 1000

	// Query vectors: use the LAST nq vectors from the full dataset.
	// This keeps indexed (first nIndexed) and query (last nq) disjoint.
	qstart := n - nq
	if qstart < 0 {
		qstart = 0
	}
	flatQueries := make([]float32, nq*dim)
	for i := 0; i < nq; i++ {
		copy(flatQueries[i*dim:(i+1)*dim], vecs[(qstart+i)*dim:(qstart+i)*dim+dim])
	}

	// Warm-up
	_, _, _ = retrievalplane.GlobalSegmentRetriever.Search(segID, flatQueries[:dim], 1, *topk)

	// Batch search: nq queries in one call (triggers OpenMP parallel path when nq>1)
	intIDs, _, err := retrievalplane.GlobalSegmentRetriever.Search(segID, flatQueries, nq, *topk)
	wallMs := time.Since(t0).Seconds() * 1000
	elapsedS := wallMs / 1000.0

	errors := 0
	if err != nil {
		errors = 1
	}

	// Per-query latencies via serial calls (for p50/p95/p99 comparison with G1/G2)
	latencies := make([]float64, nq)
	for i := 0; i < nq; i++ {
		start := time.Now()
		_, _, _ = retrievalplane.GlobalSegmentRetriever.Search(segID, flatQueries[i*dim:(i+1)*dim], 1, *topk)
		latencies[i] = time.Since(start).Seconds() * 1000
	}

	sorted := make([]float64, len(latencies))
	copy(sorted, latencies)
	sort.Float64s(sorted)
	p50 := percentile(sorted, 0.50)
	p95 := percentile(sorted, 0.95)
	p99 := percentile(sorted, 0.99)
	mean := 0.0
	for _, l := range latencies {
		mean += l
	}
	if nq > 0 {
		mean /= float64(nq)
	}

	result := BenchResult{
		Mode:          "vector-only",
		NIndexed:      nIndexed,
		NQueries:      nq,
		TopK:          *topk,
		Dim:           dim,
		BuildMs:       buildMs,
		WallMs:        wallMs,
		QPS:           float64(nq) / elapsedS,
		MeanMs:        mean,
		P50Ms:         p50,
		P95Ms:         p95,
		P99Ms:         p99,
		Errors:        errors,
		RetrieverMode: retrievalplane.Version(),
		IntIDs:        intIDs,
	}
	json.NewEncoder(os.Stdout).Encode(result)
}

// runKnowhereBuild builds the segment, runs batch + serial search, and returns
// int_ids so the Python caller can compute recall vs G1 ground truth.
// This exercises the optimised OpenMP parallel path (nq>1 → #pragma omp for).
func runKnowhereBuild() {
	if *dataset == "" {
		fmt.Fprintf(os.Stderr, "--dataset required\n")
		os.Exit(1)
	}
	vecs, n, dim, err := loadFbin(*dataset, *limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		os.Exit(1)
	}
	dim = len(vecs) / n
	fmt.Fprintf(os.Stderr, "[G2 knowhere] loaded %d vecs dim=%d from %s\n", n, dim, *dataset)

	// Determine how many vectors to index (disjoint from query set for correct recall).
	nIndexed := *indexedCount
	if nIndexed == 0 {
		nIndexed = n
	}
	if nIndexed > n {
		nIndexed = n
	}
	nq := *nquery
	if nq > n {
		nq = n
	}

	segID := *segmentID
	t0 := time.Now()
	// Build index from only the first nIndexed vectors (disjoint from query set).
	if err := retrievalplane.GlobalSegmentRetriever.BuildSegment(segID, vecs, nIndexed, dim); err != nil {
		fmt.Fprintf(os.Stderr, "BuildSegment: %v\n", err)
		os.Exit(1)
	}
	buildMs := time.Since(t0).Seconds() * 1000

	// Query vectors: use the LAST nq vectors from the full dataset.
	qstart := n - nq
	if qstart < 0 {
		qstart = 0
	}
	flatQueries := make([]float32, nq*dim)
	for i := 0; i < nq; i++ {
		copy(flatQueries[i*dim:(i+1)*dim], vecs[(qstart+i)*dim:(qstart+i)*dim+dim])
	}

	// Warm-up
	_, _, _ = retrievalplane.GlobalSegmentRetriever.Search(segID, flatQueries[:dim], 1, *topk)

	// ── Batch search: nq>1 triggers OpenMP parallel path in segment_index.cpp ──
	intIDs, _, _ := retrievalplane.GlobalSegmentRetriever.Search(segID, flatQueries, nq, *topk)
	wallMs := time.Since(t0).Seconds() * 1000
	elapsedS := wallMs / 1000.0

	// ── Serial search for per-query latency metrics ──────────────────────────
	latencies := make([]float64, nq)
	for i := 0; i < nq; i++ {
		start := time.Now()
		_, _, _ = retrievalplane.GlobalSegmentRetriever.Search(segID, flatQueries[i*dim:(i+1)*dim], 1, *topk)
		latencies[i] = time.Since(start).Seconds() * 1000
	}
	sorted := make([]float64, len(latencies))
	copy(sorted, latencies)
	sort.Float64s(sorted)
	p50 := percentile(sorted, 0.50)
	p95 := percentile(sorted, 0.95)
	p99 := percentile(sorted, 0.99)
	mean := 0.0
	for _, l := range latencies {
		mean += l
	}
	if nq > 0 {
		mean /= float64(nq)
	}

	result := BenchResult{
		Mode:          "knowhere-build",
		NIndexed:      nIndexed,
		NQueries:      nq,
		TopK:          *topk,
		Dim:           dim,
		BuildMs:       buildMs,
		WallMs:        wallMs,
		QPS:           float64(nq) / elapsedS,
		MeanMs:        mean,
		P50Ms:         p50,
		P95Ms:         p95,
		P99Ms:         p99,
		Errors:        0,
		RetrieverMode: retrievalplane.Version(),
		IntIDs:        intIDs,
	}
	json.NewEncoder(os.Stdout).Encode(result)
}

// runHTTPQuery measures end-to-end Plasmod HTTP endpoint using Go net/http with
// batch query support.  This is G4: HTTP stack with batch search.
//
// Uses POST /v1/internal/rpc/query_warm_batch which sends nq queries in a single
// HTTP request, dramatically reducing HTTP overhead vs N separate requests.
// Returns integer indices so the Python caller can compute recall vs G1/G2.
func runHTTPQuery() {
	if *dataset == "" {
		fmt.Fprintf(os.Stderr, "--dataset required for http-query mode\n")
		os.Exit(1)
	}

	vecs, n, dim, err := loadFbin(*dataset, *limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		os.Exit(1)
	}
	dim = len(vecs) / n
	fmt.Fprintf(os.Stderr, "[G4 http] loaded %d vecs dim=%d from %s\n", n, dim, *dataset)

	// Determine how many vectors to index (disjoint from query set for correct recall).
	nIndexed := *indexedCount
	if nIndexed == 0 {
		nIndexed = n
	}
	if nIndexed > n {
		nIndexed = n
	}
	nq := *nquery
	if nq > n {
		nq = n
	}

	// Query vectors: use the LAST nq vectors from the full dataset.
	qstart := n - nq
	if qstart < 0 {
		qstart = 0
	}
	flatQueries := make([]float32, nq*dim)
	for i := 0; i < nq; i++ {
		copy(flatQueries[i*dim:(i+1)*dim], vecs[(qstart+i)*dim:(qstart+i)*dim+dim])
	}

	// Step 1: Ingest ONLY the first nIndexed vectors into the server.
	// Indexed and query sets must be disjoint for correct recall.
	segID := *segmentID
	ingestURL := *serverURL + "/v1/internal/rpc/ingest_batch"
	fmt.Fprintf(os.Stderr, "[G4 http] ingesting %d indexed vectors into segment=%s\n", nIndexed, segID)

	var buf bytes.Buffer
	buf.Write([]byte("PLIB"))
	buf.WriteByte(2) // wire version 2 (adds indexed_count field)
	binary.Write(&buf, binary.LittleEndian, uint16(len(segID)))
	buf.WriteString(segID)
	binary.Write(&buf, binary.LittleEndian, uint32(nIndexed))
	binary.Write(&buf, binary.LittleEndian, uint32(dim))
	// Only ingest the first nIndexed vectors (indexed set)
	for i := 0; i < nIndexed; i++ {
		for j := 0; j < dim; j++ {
			binary.Write(&buf, binary.LittleEndian, vecs[i*dim+j])
		}
	}
	// Object IDs for indexed vectors
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

	// HTTP client with connection pooling
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

		// Build batch query payload: "PLQB" + ver(1) + segLen(2) + segID + topk(4) + nq(4) + dim(4) + data
		var qbuf bytes.Buffer
		qbuf.Write([]byte("PLQB"))
		qbuf.WriteByte(1)
		binary.Write(&qbuf, binary.LittleEndian, uint16(len(segID)))
		qbuf.WriteString(segID)
		binary.Write(&qbuf, binary.LittleEndian, uint32(*topk))
		binary.Write(&qbuf, binary.LittleEndian, uint32(batchNQ))
		binary.Write(&qbuf, binary.LittleEndian, uint32(dim))
		// Write flat query data: q0, q1, ..., q{BN-1}
		for i := batchStartIdx; i < batchEnd; i++ {
			for j := 0; j < dim; j++ {
				binary.Write(&qbuf, binary.LittleEndian, flatQueries[i*dim+j])
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

		// Decode binary response: [nq(4)][topk(4)][nq*topk*int64][nq*topk*float32]
		body, _ := io.ReadAll(qresp.Body)
		qresp.Body.Close()

		if len(body) < 8 {
			errors += batchNQ
			continue
		}

		respNQ := int(binary.LittleEndian.Uint32(body[0:4]))
		respTopK := int(binary.LittleEndian.Uint32(body[4:8]))
		idBytes := respNQ * respTopK * 8
		if len(body) < 8+idBytes {
			errors += batchNQ
			continue
		}

		// Extract integer IDs
		for i := 0; i < respNQ*respTopK; i++ {
			id := int64(binary.LittleEndian.Uint64(body[8+i*8 : 8+i*8+8]))
			allIntIDs = append(allIntIDs, id)
		}

		// Distribute wall latency per query
		perQuery := qElapsed / float64(batchNQ)
		for i := 0; i < batchNQ; i++ {
			latencies = append(latencies, perQuery)
		}
	}

	wallMs := time.Since(batchStart).Seconds() * 1000
	elapsedS := wallMs / 1000.0

	sorted := make([]float64, len(latencies))
	copy(sorted, latencies)
	sort.Float64s(sorted)
	p50 := percentile(sorted, 0.50)
	p95 := percentile(sorted, 0.95)
	p99 := percentile(sorted, 0.99)
	mean := 0.0
	for _, l := range latencies {
		mean += l
	}
	if len(latencies) > 0 {
		mean /= float64(len(latencies))
	}

	result := BenchResult{
		Mode:          "http-query",
		NIndexed:      nIndexed,
		NQueries:      nq,
		TopK:          *topk,
		Dim:           dim,
		BuildMs:       ingestMs,
		WallMs:        wallMs,
		QPS:           float64(nq) / elapsedS,
		MeanMs:        mean,
		P50Ms:         p50,
		P95Ms:         p95,
		P99Ms:         p99,
		Errors:        errors,
		RetrieverMode: retrievalplane.Version(),
		IntIDs:        allIntIDs,
	}
	json.NewEncoder(os.Stdout).Encode(result)
}
