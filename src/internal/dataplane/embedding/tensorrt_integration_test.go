//go:build cuda && tensorrt && linux && integration
// +build cuda,tensorrt,linux,integration

package embedding

import (
	"context"
	"os"
	"testing"
)

// TestTensorRTEmbedder_Integration loads the test engine and runs Generate.
// Run with: go test -tags cuda,tensorrt,linux,integration -run TestTensorRT ./src/internal/dataplane/embedding/
func TestTensorRTEmbedder_Integration(t *testing.T) {
	enginePath := os.Getenv("PLASMOD_TRT_TEST_ENGINE")
	if enginePath == "" {
		enginePath = "/home/duanzhenke/models/test_embed.engine"
	}
	if _, err := os.Stat(enginePath); os.IsNotExist(err) {
		t.Skipf("TRT test engine not found at %s — run cpp/create_test_engine first", enginePath)
	}

	const dim = 384
	e, err := NewTensorRT(context.Background(), TensorRTConfig{
		EnginePath:   enginePath,
		MaxSeqLength: 128,
		MaxBatchSize: 1,
		DeviceID:     0,
		FP16:         false,
	}, dim)
	if err != nil {
		t.Fatalf("NewTensorRT: %v", err)
	}
	defer e.Close()

	if e.Dim() != dim {
		t.Fatalf("Dim() = %d, want %d", e.Dim(), dim)
	}
	if e.Provider() != "tensorrt" {
		t.Fatalf("Provider() = %s, want tensorrt", e.Provider())
	}

	vec, err := e.Generate("hello world")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(vec) != dim {
		t.Fatalf("embedding length = %d, want %d", len(vec), dim)
	}

	// All output values should be non-NaN
	for i, v := range vec {
		if v != v { // NaN check
			t.Fatalf("NaN at index %d", i)
		}
	}
	t.Logf("TensorRT embedding[0:4] = %v", vec[:4])
	t.Logf("Provider: %s  Dim: %d  Engine: %s", e.Provider(), e.Dim(), enginePath)
}

func TestTensorRTEmbedder_BatchGenerate_Integration(t *testing.T) {
	enginePath := os.Getenv("PLASMOD_TRT_TEST_ENGINE")
	if enginePath == "" {
		enginePath = "/home/duanzhenke/models/test_embed.engine"
	}
	if _, err := os.Stat(enginePath); os.IsNotExist(err) {
		t.Skipf("TRT test engine not found at %s", enginePath)
	}

	const dim = 384
	e, err := NewTensorRT(context.Background(), TensorRTConfig{
		EnginePath:   enginePath,
		MaxSeqLength: 128,
		MaxBatchSize: 4,
		DeviceID:     0,
	}, dim)
	if err != nil {
		t.Fatalf("NewTensorRT: %v", err)
	}
	defer e.Close()

	texts := []string{"hello world", "semantic memory", "cognitive architecture", "vector search"}
	vecs, err := e.BatchGenerate(context.Background(), texts)
	if err != nil {
		t.Fatalf("BatchGenerate: %v", err)
	}
	if len(vecs) != len(texts) {
		t.Fatalf("got %d embeddings, want %d", len(vecs), len(texts))
	}
	for i, v := range vecs {
		if len(v) != dim {
			t.Fatalf("[%d] embedding length = %d, want %d", i, len(v), dim)
		}
		t.Logf("[%d] %q → embedding[0:4]=%v", i, texts[i], v[:4])
	}
}

// TestTensorRTEmbedder_RealModel uses the trtexec-converted FP16 MiniLM engine.
func TestTensorRTEmbedder_RealModel(t *testing.T) {
	enginePath := "/home/duanzhenke/CogDB/models/minilm-l6-v2-fp16.engine"
	vocabPath := "/home/duanzhenke/CogDB/models/minilm-l6-v2-vocab.txt"

	if _, err := os.Stat(enginePath); os.IsNotExist(err) {
		t.Skipf("real model engine not found: %s", enginePath)
	}

	const dim = 384
	e, err := NewTensorRT(context.Background(), TensorRTConfig{
		EnginePath:   enginePath,
		VocabPath:    vocabPath,
		MaxSeqLength: 128,
		MaxBatchSize: 4,
		DeviceID:     0,
		FP16:         true,
	}, dim)
	if err != nil {
		t.Fatalf("NewTensorRT (real model): %v", err)
	}
	defer e.Close()

	t.Logf("Engine loaded — numInputs=%d dim=%d", e.numInputs, e.Dim())

	texts := []string{
		"hello world",
		"semantic memory bank",
		"cognitive architecture for AI agents",
		"vector similarity search",
	}
	vecs, err := e.BatchGenerate(context.Background(), texts)
	if err != nil {
		t.Fatalf("BatchGenerate: %v", err)
	}
	if len(vecs) != len(texts) {
		t.Fatalf("got %d embeddings, want %d", len(vecs), len(texts))
	}

	for i, v := range vecs {
		if len(v) != dim {
			t.Fatalf("[%d] embedding length = %d, want %d", i, len(v), dim)
		}
		// Compute L2 norm — should be > 0 for real embeddings
		var norm float64
		for _, x := range v {
			norm += float64(x) * float64(x)
		}
		if norm == 0 {
			t.Errorf("[%d] embedding is all zeros", i)
		}
		t.Logf("[%d] %q → norm=%.4f  emb[0:4]=%v", i, texts[i], norm, v[:4])
	}

	// Different texts should produce different embeddings
	if vecs[0][0] == vecs[1][0] && vecs[0][1] == vecs[1][1] {
		t.Error("first two embeddings are identical — tokenizer or model may be broken")
	}
	t.Logf("Real model test passed — provider=%s dim=%d", e.Provider(), e.Dim())
}
