package dataplane

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"
)

// loadDeep1BVectors reads vectors from deep1B.ibin format:
// Header: [numVectors uint32] [dim uint32]
// Data: numVectors * dim * float32
func loadDeep1BVectors(path string) ([][]float32, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	var header [2]uint32
	if err := binary.Read(f, binary.LittleEndian, &header); err != nil {
		return nil, 0, fmt.Errorf("read header: %w", err)
	}
	numVectors := int(header[0])
	dim := int(header[1])

	vectors := make([][]float32, numVectors)
	for i := 0; i < numVectors; i++ {
		vec := make([]float32, dim)
		if err := binary.Read(f, binary.LittleEndian, vec); err != nil {
			return nil, 0, fmt.Errorf("read vector %d: %w", i, err)
		}
		vectors[i] = vec
	}

	return vectors, dim, nil
}

// BenchmarkVectorStore_Deep1B benchmarks vector retrieval using deep1B.ibin data.
// Run with: go test -bench=BenchmarkVectorStore_Deep1B -benchtime=10s ./src/internal/dataplane/...
func BenchmarkVectorStore_Deep1B(b *testing.B) {
	dataPath := "../../../deep1B.ibin"
	if _, err := os.Stat(dataPath); os.IsNotExist(err) {
		b.Skip("deep1B.ibin not found, skipping benchmark")
	}

	vectors, dim, err := loadDeep1BVectors(dataPath)
	if err != nil {
		b.Fatalf("Failed to load vectors: %v", err)
	}
	b.Logf("Loaded %d vectors, dim=%d", len(vectors), dim)

	// Create a dummy embedder (not used since we add vectors directly)
	embedder := NewTfidfEmbedder(dim)

	// Use L2 metric for deep1B data (Euclidean distance)
	vs, err := NewVectorStore(embedder, VectorStoreConfig{Dim: dim, Metric: "L2"})
	if err != nil {
		b.Fatalf("NewVectorStore failed: %v", err)
	}
	defer vs.Close()

	// Ingest all vectors
	b.Log("Ingesting vectors...")
	ingestStart := time.Now()
	for i, vec := range vectors {
		vs.AddVector(fmt.Sprintf("obj_%d", i), vec)
	}
	b.Logf("Ingest time: %v", time.Since(ingestStart))

	// Build HNSW index
	b.Log("Building HNSW index...")
	buildStart := time.Now()
	if err := vs.Build(); err != nil {
		b.Fatalf("Build failed: %v", err)
	}
	b.Logf("Build time: %v", time.Since(buildStart))

	if !vs.Ready() {
		b.Skip("VectorStore not ready (CGO unavailable), skipping search benchmark")
	}

	// Prepare random query vectors
	numQueries := 100
	queryIndices := make([]int, numQueries)
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < numQueries; i++ {
		queryIndices[i] = rng.Intn(len(vectors))
	}

	topK := 10

	// Benchmark search
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		queryVec := vectors[queryIndices[i%numQueries]]
		_, _, err := vs.Search(queryVec, topK)
		if err != nil {
			b.Fatalf("Search failed: %v", err)
		}
	}
}

// TestVectorStore_Deep1B_Recall tests recall accuracy using deep1B.ibin data.
func TestVectorStore_Deep1B_Recall(t *testing.T) {
	dataPath := "../../../deep1B.ibin"
	if _, err := os.Stat(dataPath); os.IsNotExist(err) {
		t.Skip("deep1B.ibin not found, skipping test")
	}

	vectors, dim, err := loadDeep1BVectors(dataPath)
	if err != nil {
		t.Fatalf("Failed to load vectors: %v", err)
	}
	t.Logf("Loaded %d vectors, dim=%d", len(vectors), dim)

	embedder := NewTfidfEmbedder(dim)
	// Use L2 metric for deep1B data (Euclidean distance)
	vs, err := NewVectorStore(embedder, VectorStoreConfig{Dim: dim, Metric: "L2"})
	if err != nil {
		t.Fatalf("NewVectorStore failed: %v", err)
	}
	defer vs.Close()

	// Ingest
	t.Log("Ingesting vectors...")
	for i, vec := range vectors {
		vs.AddVector(fmt.Sprintf("obj_%d", i), vec)
	}

	// Build
	t.Log("Building HNSW index...")
	buildStart := time.Now()
	if err := vs.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	t.Logf("Build time: %v", time.Since(buildStart))

	if !vs.Ready() {
		t.Skip("VectorStore not ready (CGO unavailable)")
	}

	// Test self-recall: query with vector[i], expect obj_i in top-1
	numTests := 100
	rng := rand.New(rand.NewSource(42))
	hits := 0

	t.Log("Testing self-recall...")
	searchStart := time.Now()
	for i := 0; i < numTests; i++ {
		idx := rng.Intn(len(vectors))
		ids, _, err := vs.Search(vectors[idx], 1)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		expected := fmt.Sprintf("obj_%d", idx)
		if len(ids) > 0 && ids[0] == expected {
			hits++
		}
	}
	searchTime := time.Since(searchStart)

	recall := float64(hits) / float64(numTests) * 100
	qps := float64(numTests) / searchTime.Seconds()

	t.Logf("=== Benchmark Results ===")
	t.Logf("Vectors: %d, Dim: %d", len(vectors), dim)
	t.Logf("Self-recall@1: %.1f%% (%d/%d)", recall, hits, numTests)
	t.Logf("Search QPS: %.1f", qps)
	t.Logf("Avg latency: %.3f ms", searchTime.Seconds()*1000/float64(numTests))

	// With L2 metric matching deep1B data, self-recall should be very high
	if recall < 90 {
		t.Errorf("Self-recall too low: %.1f%% (expected >= 90%%)", recall)
	}
}
