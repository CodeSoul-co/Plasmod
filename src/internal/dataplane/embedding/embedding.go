// Package embedding provides pluggable text-to-vector embedding generators.
//
// Default: TfidfEmbedder (pure Go, no external service required).
// HTTP providers: OpenAI-compatible REST API, ZhipuAI, Cohere, and any
// provider that follows the OpenAI Embeddings v1 request/response schema.
//
// Usage:
//
//	// Default: pure Go TF-IDF
//	eg := embedding.NewTfidf(256)
//
//	// OpenAI-compatible HTTP API (e.g. local Ollama, ZhipuAI, Azure OpenAI)
//	cfg := embedding.OpenAIConfig{
//		BaseURL: "https://open.bigmodel.cn/api/paas/v4",
//		Model:   "embedding-3",
//		APIKey:  os.Getenv("ZHIPUAI_API_KEY"),
//	}
//	eg, err := embedding.NewOpenAI(ctx, cfg, 1536)
//
//	// ZhipuAI (a thin wrapper with different auth header)
//	eg, err := embedding.NewZhipuAI(ctx, cfg, 1536)
//
// All generators satisfy the dataplane.EmbeddingGenerator interface and
// can be passed directly to NewTieredDataPlaneWithEmbedder.
package embedding

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"plasmod/src/internal/dataplane"
)

// ErrProviderUnavailable is returned when the embedding service is unreachable
// or returns a non-2xx response.
var ErrProviderUnavailable = errors.New("embedding provider unavailable")

// ─── Provider interface ─────────────────────────────────────────────────────────

// Generator is the top-level interface satisfied by all embedding implementations.
// It is compatible with dataplane.EmbeddingGenerator.
//
// The pure-Go TfidfEmbedder is created via NewTfidf.
// HTTP-based generators are created via NewOpenAI / NewZhipuAI / NewCohere.
type Generator interface {
	dataplane.EmbeddingGenerator
	// Close releases resources (HTTP client, connection pool). Idempotent.
	Close() error
	// Provider returns a short identifier for the backend (e.g. "openai", "zhipuai", "tfidf").
	Provider() string
}

// ─── TfidfEmbedder (pure Go, default) ────────────────────────────────────────

// TfidfEmbedder wraps the dataplane TfidfEmbedder so it satisfies Generator.
// Use dataplane.NewTfidfEmbedder directly when no HTTP client pooling is needed.
type TfidfEmbedder struct{ *dataplane.TfidfEmbedder }

func (e *TfidfEmbedder) Close() error     { return nil }
func (e *TfidfEmbedder) Provider() string { return "tfidf" }

// NewTfidf is a convenience constructor compatible with the Generator interface.
// dim must be a positive power-of-2 for best hash distribution.
func NewTfidf(dim int) *TfidfEmbedder {
	if dim <= 0 {
		dim = dataplane.DefaultEmbeddingDim
	}
	return &TfidfEmbedder{dataplane.NewTfidfEmbedder(dim)}
}

// ─── OpenAI-compatible HTTP embedder ─────────────────────────────────────────

// OpenAIConfig holds connection parameters for OpenAI-compatible embedding APIs.
// This includes: OpenAI, Azure OpenAI, local Ollama, ZhipuAI, Jina AI, Cohere,
// and any other provider that follows the OpenAI Embeddings v1 schema.
//
// Azure OpenAI note: set Model to your deployment name and optionally set
// AzureDeployment so the URL is constructed as {BaseURL}/deployments/{Deployment}/embeddings.
type OpenAIConfig struct {
	BaseURL         string        // e.g. "https://api.openai.com/v1" or "https://open.bigmodel.cn/api/paas/v4"
	Model           string        // e.g. "text-embedding-3-small", "embedding-3"
	APIKey          string        // API key; read from env or secret manager
	OrgID           string        // optional, for OpenAI Organisation
	APIType         string        // optional, "azure" triggers Azure AD path
	AzureDeployment string        // required when APIType=="azure"
	HTTPClient      *http.Client  // optional; nil → default
	Timeout         time.Duration // per-request timeout; 0 → 30s default
	// Dimensions controls output vector size (OpenAI 3 only; ignored by other backends).
	// Set to 0 to use the model's default (full) dimensions.
	Dimensions int
	// BatchSize controls how many texts are grouped per HTTP request (default 100).
	// Most providers cap at 2048 inputs per request.
	BatchSize int
}

// NewOpenAI creates an OpenAI-compatible HTTP embedder. It performs a live
// connectivity check; if the endpoint is unreachable, an error is returned.
//
//	ctx: passed to the connectivity probe. Use context.Background() if no deadline.
//	cfg: see OpenAIConfig. BaseURL and Model are required.
//	dim: expected output vector dimension. The constructor verifies this
//	    by sending a test embedding request; pass 0 to skip the check.
func NewOpenAI(ctx context.Context, cfg OpenAIConfig, dim int) (*HTTPEmbedder, error) {
	if cfg.BaseURL == "" {
		return nil, errors.New("OpenAIConfig.BaseURL is required")
	}
	if cfg.Model == "" {
		return nil, errors.New("OpenAIConfig.Model is required")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 100
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = defaultClientPool.Get(cfg.Timeout)
	}

	e := &HTTPEmbedder{
		cfg:   cfg,
		model: &modelInfo{dim: dim},
	}
	if dim > 0 {
		if err := e.probe(ctx); err != nil {
			e.Close()
			return nil, fmt.Errorf("embedding provider probe failed: %w", err)
		}
	}
	return e, nil
}

// ─── ZhipuAI ─────────────────────────────────────────────────────────────────

// NewZhipuAI is a convenience constructor for ZhipuAI's embedding API.
// ZhipuAI follows the OpenAI-compatible schema with an auth header difference.
// It is equivalent to:
//
//	NewOpenAI(ctx, OpenAIConfig{
//	    BaseURL: "https://open.bigmodel.cn/api/paas/v4",
//	    Model:   model,
//	    APIKey:  apiKey,
//	}, dim)
func NewZhipuAI(ctx context.Context, apiKey, model string, dim int) (*HTTPEmbedder, error) {
	return NewOpenAI(ctx, OpenAIConfig{
		BaseURL: "https://open.bigmodel.cn/api/paas/v4",
		Model:   model,
		APIKey:  apiKey,
	}, dim)
}

// ─── Cohere ──────────────────────────────────────────────────────────────────

// NewCohere creates a Cohere embedder. Cohere uses a different API shape:
// POST /embed → { embeddings: string[][] }.
//
//	The model parameter supports: "embed-english-v3.0", "embed-multilingual-v3.0",
//	"embed-english-light-v3.0", "embed-multilingual-light-v3.0".
func NewCohere(ctx context.Context, apiKey, model string, dim int) (*CohereEmbedder, error) {
	if apiKey == "" {
		return nil, errors.New("Cohere API key is required")
	}
	if dim <= 0 {
		return nil, errors.New("Cohere dimension must be positive")
	}
	return &CohereEmbedder{
		apiKey: apiKey,
		model:  model,
		dim:    dim,
		client: defaultClientPool.Get(30 * time.Second),
	}, nil
}

// ─── HTTP embedder (shared logic for OpenAI-compatible APIs) ─────────────────

// HTTPEmbedder is the shared implementation for all OpenAI-schema APIs.
// It handles batching, retries, and response parsing.
type HTTPEmbedder struct {
	cfg   OpenAIConfig
	model *modelInfo // nil until probe completes
}

// modelInfo captures the validated embedding dimensions.
type modelInfo struct {
	dim int
}

// Provider implements Generator.
func (e *HTTPEmbedder) Provider() string { return "openai" }

// Generate calls the remote API for a single text input.
// Thread-safe: concurrent Generate calls are serialized per-client.
// For high-throughput workloads, use BatchGenerate.
func (e *HTTPEmbedder) Generate(text string) ([]float32, error) {
	vecs, err := e.BatchGenerate(context.Background(), []string{text})
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}

// BatchGenerate sends texts to the remote API in batches and returns
// all embedding vectors in input order. Empty texts produce zero vectors.
func (e *HTTPEmbedder) BatchGenerate(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	var out [][]float32
	// If Dimensions is set, include it in every batch request
	batchCfg := e.cfg
	if e.cfg.Dimensions > 0 {
		// Clone so Dimensions doesn't leak into shared cfg
		batchCfg.Dimensions = e.cfg.Dimensions
	}
	for i := 0; i < len(texts); i += batchCfg.BatchSize {
		end := i + batchCfg.BatchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch, err := e.postEmbed(ctx, batchCfg, texts[i:end])
		if err != nil {
			return nil, err
		}
		out = append(out, batch...)
	}
	return out, nil
}

// postEmbed sends a single batch to the OpenAI-compatible endpoint.
// It follows the OpenAI Embeddings v1 request schema.
func (e *HTTPEmbedder) postEmbed(ctx context.Context, cfg OpenAIConfig, texts []string) ([][]float32, error) {
	if cfg.APIType == "azure" {
		return e.postAzureEmbed(ctx, cfg, texts)
	}
	reqBody := openAIRequest{Model: cfg.Model, Input: texts}
	if cfg.Dimensions > 0 {
		reqBody.Dimensions = cfg.Dimensions
	}
	var resp openAIResponse
	if err := e.doRequest(ctx, cfg, "embeddings", reqBody, &resp); err != nil {
		return nil, err
	}
	return parseOpenAIResponse(resp, len(texts))
}

// postAzureEmbed handles the Azure OpenAI request path where the URL is
// constructed as {BaseURL}/deployments/{AzureDeployment}/embeddings?api-version=2023-05-15.
func (e *HTTPEmbedder) postAzureEmbed(ctx context.Context, cfg OpenAIConfig, texts []string) ([][]float32, error) {
	reqBody := openAIRequest{Model: cfg.Model, Input: texts}
	if cfg.Dimensions > 0 {
		reqBody.Dimensions = cfg.Dimensions
	}
	var resp openAIResponse
	if err := e.doAzureRequest(ctx, cfg, "embeddings", reqBody, &resp); err != nil {
		return nil, err
	}
	return parseOpenAIResponse(resp, len(texts))
}

// doRequest sends a JSON POST to {BaseURL}/{path}.
func (e *HTTPEmbedder) doRequest(ctx context.Context, cfg OpenAIConfig, path string, body, dest any) error {
	url := cfg.BaseURL
	if len(url) > 0 && url[len(url)-1] != '/' {
		url += "/"
	}
	url += path
	return doHTTPRequest(ctx, cfg.HTTPClient, url, cfg.APIKey, "", body, dest)
}

// doAzureRequest sends a request with the Azure API version query param.
func (e *HTTPEmbedder) doAzureRequest(ctx context.Context, cfg OpenAIConfig, path string, body, dest any) error {
	deployment := cfg.AzureDeployment
	if deployment == "" {
		deployment = cfg.Model
	}
	url := cfg.BaseURL
	if len(url) > 0 && url[len(url)-1] != '/' {
		url += "/"
	}
	url += "deployments/" + deployment + "/" + path + "?api-version=2023-05-15"
	return doHTTPRequest(ctx, cfg.HTTPClient, url, cfg.APIKey, "azure", body, dest)
}

// probe sends a single test request to verify connectivity and dimension alignment.
func (e *HTTPEmbedder) probe(ctx context.Context) error {
	testTexts := []string{"probe"}
	resp, err := e.postEmbed(ctx, e.cfg, testTexts)
	if err != nil {
		return fmt.Errorf("%w", err)
	}
	if len(resp) != 1 || len(resp[0]) != e.model.dim {
		return fmt.Errorf("dimension mismatch: expected %d, got %d",
			e.model.dim, len(resp[0]))
	}
	return nil
}

// Dim implements dataplane.EmbeddingGenerator.
// Returns 0 when the provider has not been probed (dim=0 passed to NewOpenAI).
func (e *HTTPEmbedder) Dim() int {
	if e.model == nil {
		return 0
	}
	return e.model.dim
}

// Reset is a no-op for HTTP embedders (stateless).
func (e *HTTPEmbedder) Reset() {}

// Close returns the HTTP client to the pool.
func (e *HTTPEmbedder) Close() error {
	defaultClientPool.Put(e.cfg.HTTPClient)
	return nil
}

// ─── Cohere embedder ─────────────────────────────────────────────────────────

// CohereEmbedder implements Generator for Cohere's /embed API.
type CohereEmbedder struct {
	apiKey string
	model  string
	dim    int
	client *http.Client
}

// Generate implements Generator.
func (e *CohereEmbedder) Generate(text string) ([]float32, error) {
	vecs, err := e.BatchGenerate(context.Background(), []string{text})
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}

// BatchGenerate sends a request to Cohere's v2/embed endpoint.
func (e *CohereEmbedder) BatchGenerate(ctx context.Context, texts []string) ([][]float32, error) {
	reqBody := map[string]any{
		"model":      e.model,
		"texts":      texts,
		"input_type": "search_document",
	}
	var resp struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	err := doHTTPRequest(ctx, e.client,
		"https://api.cohere.ai/v2/embed", e.apiKey, "", reqBody, &resp)
	if err != nil {
		return nil, err
	}
	return resp.Embeddings, nil
}

// Dim implements dataplane.EmbeddingGenerator.
func (e *CohereEmbedder) Dim() int { return e.dim }

// Reset is a no-op.
func (e *CohereEmbedder) Reset() {}

// Provider implements Generator.
func (e *CohereEmbedder) Provider() string { return "cohere" }

// Close returns the client to the pool.
func (e *CohereEmbedder) Close() error {
	defaultClientPool.Put(e.client)
	return nil
}

// ─── OpenAI REST schema ───────────────────────────────────────────────────────

type openAIRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions int      `json:"dimensions,omitempty"`
}

type openAIResponse struct {
	Object string `json:"object"`
	Data   []struct {
		Object    string    `json:"object"`
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Model string `json:"model"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

func parseOpenAIResponse(resp openAIResponse, expect int) ([][]float32, error) {
	if len(resp.Data) != expect {
		return nil, fmt.Errorf("%w: expected %d embeddings, got %d",
			ErrProviderUnavailable, expect, len(resp.Data))
	}
	out := make([][]float32, expect)
	for _, d := range resp.Data {
		out[d.Index] = d.Embedding
	}
	return out, nil
}

// ─── Factory from environment variables ──────────────────────────────────────

// NewFromEnv creates an embedder based on PLASMOD_EMBEDDER environment variable.
// Supported values: tfidf, openai, zhipuai, cohere, vertexai, huggingface, onnx, gguf, tensorrt
//
// Each provider reads its own environment variables:
//   - openai:      PLASMOD_OPENAI_API_KEY, PLASMOD_OPENAI_BASE_URL, PLASMOD_OPENAI_MODEL
//   - zhipuai:     PLASMOD_ZHIPUAI_API_KEY, PLASMOD_ZHIPUAI_MODEL
//   - cohere:      PLASMOD_COHERE_API_KEY
//   - vertexai:    PLASMOD_VERTEXAI_API_KEY, PLASMOD_VERTEXAI_PROJECT, PLASMOD_VERTEXAI_LOCATION
//   - huggingface: PLASMOD_HUGGINGFACE_API_KEY, PLASMOD_HUGGINGFACE_MODEL
//   - onnx:        PLASMOD_EMBEDDER_MODEL_PATH, ONNXRUNTIME_LIB_PATH
//   - gguf:        PLASMOD_EMBEDDER_MODEL_PATH, PLASMOD_EMBEDDER_DEVICE
//   - tensorrt:    PLASMOD_EMBEDDER_MODEL_PATH
func NewFromEnv(ctx context.Context, dim int) (Generator, error) {
	provider := os.Getenv("PLASMOD_EMBEDDER")
	if provider == "" {
		provider = "tfidf"
	}

	switch provider {
	case "tfidf":
		return NewTfidf(dim), nil

	case "openai":
		baseURL := os.Getenv("PLASMOD_OPENAI_BASE_URL")
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		model := os.Getenv("PLASMOD_OPENAI_MODEL")
		if model == "" {
			model = "text-embedding-3-small"
		}
		return NewOpenAI(ctx, OpenAIConfig{
			BaseURL: baseURL,
			Model:   model,
			APIKey:  os.Getenv("PLASMOD_OPENAI_API_KEY"),
		}, dim)

	case "zhipuai":
		model := os.Getenv("PLASMOD_ZHIPUAI_MODEL")
		if model == "" {
			model = "embedding-3"
		}
		return NewZhipuAI(ctx, os.Getenv("PLASMOD_ZHIPUAI_API_KEY"), model, dim)

	case "cohere":
		return NewCohere(ctx, os.Getenv("PLASMOD_COHERE_API_KEY"), "embed-multilingual-v3.0", dim)

	case "vertexai":
		return NewVertexAI(ctx, VertexAIConfig{
			ProjectID:   os.Getenv("PLASMOD_VERTEXAI_PROJECT"),
			Location:    os.Getenv("PLASMOD_VERTEXAI_LOCATION"),
			AccessToken: os.Getenv("PLASMOD_VERTEXAI_ACCESS_TOKEN"),
		}, dim)

	case "huggingface":
		model := os.Getenv("PLASMOD_HUGGINGFACE_MODEL")
		if model == "" {
			model = "sentence-transformers/all-MiniLM-L6-v2"
		}
		return NewHuggingFace(ctx, HuggingFaceConfig{
			APIKey: os.Getenv("PLASMOD_HUGGINGFACE_API_KEY"),
			Model:  model,
		}, dim)

	case "onnx":
		return NewOnnxFromEnv(ctx, dim)

	case "gguf":
		return NewGGUFFromEnv(ctx, dim)

	case "tensorrt":
		return NewTensorRTFromEnv(ctx, dim)

	default:
		return nil, fmt.Errorf("unknown embedding provider: %s", provider)
	}
}
