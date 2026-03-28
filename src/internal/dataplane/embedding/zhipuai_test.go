package embedding

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestZhipuAI_RealAPI tests the ZhipuAI/GLM embedding API with a real API key.
// Run with: ANDB_ZHIPUAI_API_KEY=xxx go test -v -run TestZhipuAI_RealAPI
func TestZhipuAI_RealAPI(t *testing.T) {
	apiKey := os.Getenv("ANDB_ZHIPUAI_API_KEY")
	if apiKey == "" {
		t.Skip("ANDB_ZHIPUAI_API_KEY not set, skipping real API test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// ZhipuAI embedding-3 model outputs 2048-dim vectors
	embedder, err := NewZhipuAI(ctx, apiKey, "embedding-3", 0)
	if err != nil {
		t.Fatalf("NewZhipuAI failed: %v", err)
	}
	defer embedder.Close()

	t.Logf("Provider: %s", embedder.Provider())

	// Test single embedding
	text := "Hello, this is a test for ZhipuAI GLM embedding API."
	vec, err := embedder.Generate(text)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	t.Logf("Input: %q", text)
	t.Logf("Output dimension: %d", len(vec))
	t.Logf("First 5 values: %v", vec[:min(5, len(vec))])

	if len(vec) == 0 {
		t.Fatal("Empty embedding returned")
	}

	// Test batch embedding
	texts := []string{
		"First test sentence.",
		"Second test sentence.",
		"Third test sentence.",
	}
	vecs, err := embedder.BatchGenerate(ctx, texts)
	if err != nil {
		t.Fatalf("BatchGenerate failed: %v", err)
	}

	t.Logf("Batch size: %d", len(vecs))
	for i, v := range vecs {
		t.Logf("  [%d] dim=%d, first 3 values: %v", i, len(v), v[:min(3, len(v))])
	}

	if len(vecs) != len(texts) {
		t.Fatalf("Expected %d embeddings, got %d", len(texts), len(vecs))
	}

	t.Log("ZhipuAI/GLM embedding API test PASSED")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
