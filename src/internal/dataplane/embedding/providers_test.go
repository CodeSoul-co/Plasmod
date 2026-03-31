package embedding

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// HTTPEmbedder Provider Tests
//
// Run all tests: go test -v -run TestProvider ./src/internal/dataplane/embedding/
//
// Run specific provider:
//   ANDB_OPENAI_API_KEY=sk-xxx go test -v -run TestProvider_OpenAI
//   ANDB_ZHIPUAI_API_KEY=xxx go test -v -run TestProvider_ZhipuAI
//   ANDB_AZURE_API_KEY=xxx ANDB_AZURE_ENDPOINT=xxx ANDB_AZURE_DEPLOYMENT=xxx go test -v -run TestProvider_Azure
//   go test -v -run TestProvider_Ollama  # requires local Ollama running
// ============================================================================

// TestProvider_OpenAI tests the OpenAI embedding API.
// Model: text-embedding-3-small (1536 dim) or text-embedding-ada-002 (1536 dim)
func TestProvider_OpenAI(t *testing.T) {
	apiKey := os.Getenv("ANDB_OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("ANDB_OPENAI_API_KEY not set, skipping OpenAI test")
	}

	model := os.Getenv("ANDB_OPENAI_MODEL")
	if model == "" {
		model = "text-embedding-3-small"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	embedder, err := NewOpenAI(ctx, OpenAIConfig{
		BaseURL: "https://api.openai.com/v1",
		Model:   model,
		APIKey:  apiKey,
	}, 0) // dim=0 to skip probe
	if err != nil {
		t.Fatalf("NewOpenAI failed: %v", err)
	}
	defer embedder.Close()

	runEmbedderTest(t, embedder, "OpenAI")
}

// TestProvider_ZhipuAI tests the ZhipuAI/GLM embedding API.
// Model: embedding-3 (2048 dim)
func TestProvider_ZhipuAI(t *testing.T) {
	apiKey := os.Getenv("ANDB_ZHIPUAI_API_KEY")
	if apiKey == "" {
		t.Skip("ANDB_ZHIPUAI_API_KEY not set, skipping ZhipuAI test")
	}

	model := os.Getenv("ANDB_ZHIPUAI_MODEL")
	if model == "" {
		model = "embedding-3"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	embedder, err := NewZhipuAI(ctx, apiKey, model, 0)
	if err != nil {
		t.Fatalf("NewZhipuAI failed: %v", err)
	}
	defer embedder.Close()

	runEmbedderTest(t, embedder, "ZhipuAI")
}

// TestProvider_Azure tests Azure OpenAI embedding API.
// Requires: ANDB_AZURE_API_KEY, ANDB_AZURE_ENDPOINT, ANDB_AZURE_DEPLOYMENT
func TestProvider_Azure(t *testing.T) {
	apiKey := os.Getenv("ANDB_AZURE_API_KEY")
	endpoint := os.Getenv("ANDB_AZURE_ENDPOINT")
	deployment := os.Getenv("ANDB_AZURE_DEPLOYMENT")

	if apiKey == "" || endpoint == "" || deployment == "" {
		t.Skip("Azure env vars not set (ANDB_AZURE_API_KEY, ANDB_AZURE_ENDPOINT, ANDB_AZURE_DEPLOYMENT)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	embedder, err := NewOpenAI(ctx, OpenAIConfig{
		BaseURL:         endpoint,
		Model:           deployment,
		APIKey:          apiKey,
		APIType:         "azure",
		AzureDeployment: deployment,
	}, 0)
	if err != nil {
		t.Fatalf("NewOpenAI (Azure) failed: %v", err)
	}
	defer embedder.Close()

	runEmbedderTest(t, embedder, "Azure")
}

// TestProvider_Ollama tests local Ollama embedding API.
// Requires: Ollama running locally with an embedding model (e.g. nomic-embed-text)
// Start Ollama: ollama serve
// Pull model: ollama pull nomic-embed-text
func TestProvider_Ollama(t *testing.T) {
	baseURL := os.Getenv("ANDB_OLLAMA_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:11434/v1"
	}

	model := os.Getenv("ANDB_OLLAMA_MODEL")
	if model == "" {
		model = "nomic-embed-text"
	}

	// Skip if Ollama is not reachable (connection refused / timeout).
	probe, err := http.Get(strings.TrimSuffix(baseURL, "/v1") + "/api/tags")
	if err != nil || probe.StatusCode >= 500 {
		if probe != nil {
			probe.Body.Close()
		}
		t.Skipf("Ollama not reachable at %s: %v", baseURL, err)
	}
	probe.Body.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	embedder, err := NewOpenAI(ctx, OpenAIConfig{
		BaseURL: baseURL,
		Model:   model,
		APIKey:  "ollama",
	}, 0)
	if err != nil {
		t.Skipf("Ollama not available at %s: %v", baseURL, err)
	}
	defer embedder.Close()

	runEmbedderTest(t, embedder, "Ollama")
}

// runEmbedderTest is a shared test helper for all HTTPEmbedder providers.
func runEmbedderTest(t *testing.T, embedder *HTTPEmbedder, providerName string) {
	t.Helper()

	t.Logf("=== Testing %s ===", providerName)
	t.Logf("Provider: %s", embedder.Provider())

	// Test 1: Single embedding
	text := "Hello, this is a test for embedding API."
	vec, err := embedder.Generate(text)
	if err != nil {
		t.Fatalf("[%s] Generate failed: %v", providerName, err)
	}

	t.Logf("[%s] Single embedding:", providerName)
	t.Logf("  Input: %q", text)
	t.Logf("  Output dimension: %d", len(vec))
	if len(vec) >= 5 {
		t.Logf("  First 5 values: %v", vec[:5])
	}

	if len(vec) == 0 {
		t.Fatalf("[%s] Empty embedding returned", providerName)
	}

	// Test 2: Batch embedding
	ctx := context.Background()
	texts := []string{
		"First test sentence for batch embedding.",
		"Second test sentence with different content.",
		"Third test sentence to verify batch processing.",
	}
	vecs, err := embedder.BatchGenerate(ctx, texts)
	if err != nil {
		t.Fatalf("[%s] BatchGenerate failed: %v", providerName, err)
	}

	t.Logf("[%s] Batch embedding:", providerName)
	t.Logf("  Batch size: %d", len(vecs))
	for i, v := range vecs {
		if len(v) >= 3 {
			t.Logf("  [%d] dim=%d, first 3: %v", i, len(v), v[:3])
		} else {
			t.Logf("  [%d] dim=%d", i, len(v))
		}
	}

	if len(vecs) != len(texts) {
		t.Fatalf("[%s] Expected %d embeddings, got %d", providerName, len(texts), len(vecs))
	}

	// Verify all embeddings have same dimension
	dim := len(vecs[0])
	for i, v := range vecs {
		if len(v) != dim {
			t.Errorf("[%s] Embedding %d has dim %d, expected %d", providerName, i, len(v), dim)
		}
	}

	t.Logf("[%s] Test PASSED (dim=%d)", providerName, dim)
}

// ============================================================================
// Summary of Provider Test Status
// ============================================================================
//
// | Provider | Model | Dimension | Test Status |
// |----------|-------|-----------|-------------|
// | OpenAI | text-embedding-3-small | 1536 | Pending real API test |
// | ZhipuAI | embedding-3 | 2048 | TESTED (real API) |
// | Azure | (deployment) | varies | Pending real API test |
// | Ollama | nomic-embed-text | 768 | Pending local test |
//
// ============================================================================
