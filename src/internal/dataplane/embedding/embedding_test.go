package embedding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"andb/src/internal/dataplane"
)

func TestTfidfEmbedder_ImplementsGenerator(t *testing.T) {
	e := NewTfidf(256)
	if e.Dim() != 256 {
		t.Errorf("expected dim 256, got %d", e.Dim())
	}
	vec, err := e.Generate("hello world test")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if len(vec) != 256 {
		t.Errorf("expected 256-dim vector, got %d", len(vec))
	}
	if e.Provider() != "tfidf" {
		t.Errorf("expected provider tfidf, got %s", e.Provider())
	}
	if err := e.Close(); err != nil {
		t.Errorf("Close should not error: %v", err)
	}
}

func TestTfidfEmbedder_EmptyText(t *testing.T) {
	e := NewTfidf(128)
	vec, err := e.Generate("")
	if err != nil {
		t.Fatalf("Generate empty text: %v", err)
	}
	if len(vec) != 128 {
		t.Errorf("expected 128-dim zero vector, got %d", len(vec))
	}
}

func TestTfidfEmbedder_Reset(t *testing.T) {
	e := NewTfidf(256)
	e.Generate("some text here")
	e.Reset() // should not panic
}

func TestOpenAIConfig_Defaults(t *testing.T) {
	cfg := OpenAIConfig{
		BaseURL: "https://api.example.com/v1",
		Model:   "test-model",
		APIKey:  "test-key",
	}
	if cfg.Timeout != 0 {
		t.Errorf("expected zero Timeout default, got %v", cfg.Timeout)
	}
	if cfg.BatchSize != 0 {
		t.Errorf("expected zero BatchSize default, got %d", cfg.BatchSize)
	}
}

func TestOpenAIEmbedder_BatchGenerate_MissingBaseURL(t *testing.T) {
	_, err := NewOpenAI(nil, OpenAIConfig{Model: "test"}, 256)
	if err == nil {
		t.Error("expected error for missing BaseURL")
	}
}

func TestOpenAIEmbedder_BatchGenerate_MissingModel(t *testing.T) {
	_, err := NewOpenAI(nil, OpenAIConfig{BaseURL: "https://api.example.com/v1"}, 256)
	if err == nil {
		t.Error("expected error for missing Model")
	}
}

// mockOpenAIHandler returns a valid OpenAI embeddings response.
// It accepts both "Bearer <token>" (OpenAI/Cohere) and "api-key <key>" (Azure) auth.
func mockOpenAIHandler(dim int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		apiKey := r.Header.Get("api-key")
		if !strings.HasPrefix(auth, "Bearer ") && apiKey == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var req openAIRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		resp := openAIResponse{
			Object: "list",
			Model:  req.Model,
		}
		for i, txt := range req.Input {
			// Return a deterministic "fake" embedding vector
			vec := make([]float32, dim)
			for j := 0; j < dim; j++ {
				vec[j] = float32(len(txt)%256) * 0.01
			}
			resp.Data = append(resp.Data, struct {
				Object    string    `json:"object"`
				Index     int       `json:"index"`
				Embedding []float32 `json:"embedding"`
			}{Object: "embedding", Index: i, Embedding: vec})
		}
		resp.Usage.PromptTokens = len(req.Input)
		resp.Usage.TotalTokens = len(req.Input)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func TestHTTPEmbedder_Generate_Success(t *testing.T) {
	dim := 4
	server := httptest.NewServer(mockOpenAIHandler(dim))
	defer server.Close()

	cfg := OpenAIConfig{
		BaseURL: server.URL,
		Model:   "test-model",
		APIKey:  "test-key",
		BatchSize: 100,
	}
	e, err := NewOpenAI(context.Background(), cfg, dim)
	if err != nil {
		t.Fatalf("NewOpenAI failed: %v", err)
	}
	defer e.Close()

	vec, err := e.Generate("hello world")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if len(vec) != dim {
		t.Errorf("expected dim %d, got %d", dim, len(vec))
	}
	if e.Provider() != "openai" {
		t.Errorf("expected provider openai, got %s", e.Provider())
	}
}

func TestHTTPEmbedder_BatchGenerate_Success(t *testing.T) {
	dim := 4
	server := httptest.NewServer(mockOpenAIHandler(dim))
	defer server.Close()

	cfg := OpenAIConfig{
		BaseURL:   server.URL,
		Model:     "test-model",
		APIKey:    "test-key",
		BatchSize: 100,
	}
	e, err := NewOpenAI(context.Background(), cfg, dim)
	if err != nil {
		t.Fatalf("NewOpenAI failed: %v", err)
	}
	defer e.Close()

	vecs, err := e.BatchGenerate(context.Background(), []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("BatchGenerate failed: %v", err)
	}
	if len(vecs) != 3 {
		t.Errorf("expected 3 vectors, got %d", len(vecs))
	}
	for i, v := range vecs {
		if len(v) != dim {
			t.Errorf("vec[%d] dim=%d, expected %d", i, len(v), dim)
		}
	}
}

func TestHTTPEmbedder_BatchGenerate_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := OpenAIConfig{
		BaseURL: server.URL,
		Model:   "test-model",
		APIKey:  "test-key",
	}
	// dim=0 skips the probe so we can create the embedder successfully,
	// then test that Generate correctly propagates a server error.
	e, err := NewOpenAI(context.Background(), cfg, 0)
	if err != nil {
		t.Fatalf("NewOpenAI failed: %v", err)
	}
	defer e.Close()

	_, err = e.Generate("test")
	if err == nil {
		t.Error("expected error on server error")
	}
}

func TestHTTPEmbedder_Probe_FailsOnDimMismatch(t *testing.T) {
	dim := 4
	server := httptest.NewServer(mockOpenAIHandler(8)) // server returns 8-dim
	defer server.Close()

	cfg := OpenAIConfig{
		BaseURL: server.URL,
		Model:   "test-model",
		APIKey:  "test-key",
	}
	_, err := NewOpenAI(nil, cfg, dim) // expect 4-dim
	if err == nil {
		t.Error("expected error on dimension mismatch")
	}
}

func TestHTTPEmbedder_AzurePath(t *testing.T) {
	dim := 4
	server := httptest.NewServer(mockOpenAIHandler(dim))
	defer server.Close()

	cfg := OpenAIConfig{
		BaseURL:         server.URL,
		Model:           "my-deployment",
		APIKey:          "azure-key",
		APIType:         "azure",
		AzureDeployment: "my-deployment",
		Dimensions:      dim,
	}
	e, err := NewOpenAI(context.Background(), cfg, dim)
	if err != nil {
		t.Fatalf("NewOpenAI(azure) failed: %v", err)
	}
	defer e.Close()

	vec, err := e.Generate("azure test")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if len(vec) != dim {
		t.Errorf("expected dim %d, got %d", dim, len(vec))
	}
}

func TestHTTPEmbedder_ZhipuAI(t *testing.T) {
	dim := 4
	server := httptest.NewServer(mockOpenAIHandler(dim))
	defer server.Close()

	cfg := OpenAIConfig{
		BaseURL: server.URL,
		Model:   "embedding-3",
		APIKey:  "zhipuai-key",
	}
	e, err := NewOpenAI(context.Background(), cfg, dim)
	if err != nil {
		t.Fatalf("NewOpenAI failed: %v", err)
	}
	defer e.Close()

	vec, err := e.Generate("zhipuai test")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if len(vec) != dim {
		t.Errorf("expected dim %d, got %d", dim, len(vec))
	}
}

func TestCohereEmbedder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var req struct {
			Model  string   `json:"model"`
			Texts  []string `json:"texts"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		resp := struct {
			Embeddings [][]float32 `json:"embeddings"`
		}{
			Embeddings: make([][]float32, len(req.Texts)),
		}
		for i := range req.Texts {
			vec := make([]float32, 1024)
			vec[0] = float32(i)
			resp.Embeddings[i] = vec
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// CohereEmbedder uses api.cohere.ai; for the test we point it at our local server
	// by injecting a custom RoundTripper that rewrites the host.
	e := &CohereEmbedder{
		apiKey: "test",
		model:  "embed-english-v3.0",
		dim:    1024,
		client: &http.Client{
			Transport: &rewriteRoundTripper{base: server.URL, inner: http.DefaultTransport},
		},
	}

	vecs, err := e.BatchGenerate(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("BatchGenerate failed: %v", err)
	}
	if len(vecs) != 2 {
		t.Errorf("expected 2 vectors, got %d", len(vecs))
	}
	vec, err := e.Generate("single text")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if len(vec) != 1024 {
		t.Errorf("Generate returned dim %d, expected 1024", len(vec))
	}
	if e.Provider() != "cohere" {
		t.Errorf("expected provider cohere, got %s", e.Provider())
	}
	if e.Dim() != 1024 {
		t.Errorf("expected dim 1024, got %d", e.Dim())
	}
}

// rewriteRoundTripper rewrites requests to api.cohere.ai to a local test server.
type rewriteRoundTripper struct {
	base  string
	inner http.RoundTripper
}

func (t *rewriteRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	newReq := req.Clone(req.Context())
	newReq.URL.Scheme = "http"
	newReq.URL.Host = t.base[strings.Index(t.base, "//")+2:]
	newReq.Host = newReq.URL.Host
	return t.inner.RoundTrip(newReq)
}

func TestGeneratorInterface_Compatibility(t *testing.T) {
	// Verify that TfidfEmbedder satisfies dataplane.EmbeddingGenerator
	e := NewTfidf(256)
	var _ dataplane.EmbeddingGenerator = e
	var _ Generator = e

	// Verify that the embedding package Generator interface is compatible
	// with the dataplane.EmbeddingGenerator (required by TieredDataPlane)
	var _ dataplane.EmbeddingGenerator = (*HTTPEmbedder)(nil)
}

func TestClientPool_Reuse(t *testing.T) {
	pool := &clientPool{clients: make(map[time.Duration]*http.Client), maxConns: 4}

	c1 := pool.Get(10 * time.Second)
	c2 := pool.Get(10 * time.Second)
	if c1 != c2 {
		t.Error("same timeout should return same client")
	}

	c3 := pool.Get(20 * time.Second)
	if c3 == c1 {
		t.Error("different timeout should return different client")
	}

	pool.Put(c1)
	pool.Put(c2)
	pool.Put(c3)

	pool.Close() // should not panic
}

func TestOpenAIRequestSchema(t *testing.T) {
	// Verify the OpenAI request schema serialises correctly
	req := openAIRequest{
		Model:     "text-embedding-3-small",
		Input:     []string{"hello world", "foo bar"},
		Dimensions: 512,
	}
	bs, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var decoded openAIRequest
	if err := json.Unmarshal(bs, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if decoded.Model != "text-embedding-3-small" {
		t.Errorf("model mismatch: %s", decoded.Model)
	}
	if len(decoded.Input) != 2 {
		t.Errorf("input length: %d", len(decoded.Input))
	}
	if decoded.Dimensions != 512 {
		t.Errorf("dimensions: %d", decoded.Dimensions)
	}
}
