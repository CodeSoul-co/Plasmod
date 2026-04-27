//go:build retrieval
// +build retrieval

// cmd/benchmark/main.go — Plasmod retrieval benchmark binary.
//
// Run:  go run -tags retrieval ./src/cmd/benchmark --mode vector-only --dataset /path/to/data.fbin --limit 10000 --queries 1000 --topk 10
//
// Modes:
//   knowhere-build  — load vectors, build Knowhere HNSW via cgo, report build time
//   vector-only     — call GlobalSegmentRetriever.Search with precomputed vectors (bypass HTTP/embedder)
//
// G1 (FAISS) and G2 (Knowhere ctypes) are handled by the Python benchmark script.
// This binary handles G3 (Plasmod direct Go call with precomputed embeddings).
package main

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"time"

	"plasmod/retrievalplane"
)

var (
	mode      = flag.String("mode", "vector-only", "vector-only|knowhere-build")
	dataset   = flag.String("dataset", "", "Path to .fbin test data")
	limit     = flag.Int("limit", 10000, "Max vectors to load")
	nquery    = flag.Int("queries", 1000, "Number of queries")
	topk      = flag.Int("topk", 10, "Top-k results")
	segmentID = flag.String("segment", "bench.layer1", "Warm segment ID for vector-only mode")
)

type BenchResult struct {
	Mode          string  `json:"mode"`
	NIndexed      int     `json:"n_indexed"`
	NQueries      int     `json:"n_queries"`
	TopK          int     `json:"topk"`
	Dim           int     `json:"dim"`
	BuildMs       float64 `json:"build_ms"`
	WallMs        float64 `json:"wall_ms"`
	QPS           float64 `json:"qps"`
	MeanMs        float64 `json:"mean_ms"`
	P50Ms         float64 `json:"p50_ms"`
	P95Ms         float64 `json:"p95_ms"`
	P99Ms         float64 `json:"p99_ms"`
	Errors        int     `json:"errors"`
	RetrieverMode string  `json:"retriever_mode"`
}

func main() {
	flag.Parse()
	switch *mode {
	case "vector-only":
		runVectorOnly()
	case "knowhere-build":
		runKnowhereBuild()
	default:
		fmt.Fprintf(os.Stderr, "unknown mode: %s\n", *mode)
		os.Exit(1)
	}
}

// loadFbin reads a float32 .fbin file with an 8-byte little-endian header: n (uint32), dim (uint32).
// Returns flat float32 slice of length n*dim, plus the vector count.
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
		// Use what's available
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

// runVectorOnly measures GlobalSegmentRetriever.Search with precomputed vectors.
// This is G3: Plasmod vector-only (direct Go call, precomputed embeddings).
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

	// Build segment via cgo (Knowhere/HNSW)
	segID := *segmentID
	t0 := time.Now()
	if err := retrievalplane.GlobalSegmentRetriever.BuildSegment(segID, vecs, n, dim); err != nil {
		fmt.Fprintf(os.Stderr, "BuildSegment: %v\n", err)
		os.Exit(1)
	}
	buildMs := time.Since(t0).Seconds() * 1000

	// Query vectors: use last NQUERY vectors (distinct from indexed range)
	qstart := 0
	nq := *nquery
	availableQueries := n - qstart
	if availableQueries < nq {
		nq = availableQueries
	}

	queryVecs := make([][]float32, nq)
	for i := 0; i < nq; i++ {
		vec := make([]float32, dim)
		copy(vec, vecs[(qstart+i)*dim:(qstart+i)*dim+dim])
		queryVecs[i] = vec
	}

	// Warm-up
	if nq > 0 {
		_, _, _ = retrievalplane.GlobalSegmentRetriever.Search(segID, queryVecs[0], 1, *topk)
	}

	// Measure latency per query
	latencies := make([]float64, nq)
	errors := 0
	for i := 0; i < nq; i++ {
		start := time.Now()
		_, _, err := retrievalplane.GlobalSegmentRetriever.Search(segID, queryVecs[i], 1, *topk)
		if err != nil {
			errors++
		}
		latencies[i] = time.Since(start).Seconds() * 1000
	}

	wallMs := time.Since(t0).Seconds() * 1000
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
		Mode:          "vector-only",
		NIndexed:      n,
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
	}
	json.NewEncoder(os.Stdout).Encode(result)
}

// runKnowhereBuild builds the segment and reports build time.
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
	t0 := time.Now()
	if err := retrievalplane.GlobalSegmentRetriever.BuildSegment(*segmentID, vecs, n, dim); err != nil {
		fmt.Fprintf(os.Stderr, "BuildSegment: %v\n", err)
		os.Exit(1)
	}
	result := map[string]any{
		"mode":      "knowhere-build",
		"n":         n,
		"dim":       dim,
		"build_ms":  time.Since(t0).Seconds() * 1000,
		"retriever": retrievalplane.Version(),
	}
	json.NewEncoder(os.Stdout).Encode(result)
}