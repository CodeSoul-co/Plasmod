// Package embedding provides pluggable text-to-vector embedding generators.
package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// HuggingFaceConfig holds connection parameters for HuggingFace Inference API.
//
// Endpoint: https://api-inference.huggingface.co/pipeline/feature-extraction/{Model}
//
// Authentication: Requires HuggingFace API token (HF_TOKEN or HUGGINGFACE_API_KEY).
type HuggingFaceConfig struct {
	Model      string        // e.g. "sentence-transformers/all-MiniLM-L6-v2"
	APIKey     string        // HuggingFace API token
	HTTPClient *http.Client  // optional; nil -> default
	Timeout    time.Duration // per-request timeout; 0 -> 60s default (HF can be slow)
	BatchSize  int           // texts per request; 0 -> 32
	// UseGPU requests GPU inference (requires HF Pro subscription).
	UseGPU bool
	// WaitForModel waits for model to load if not ready (default true).
	WaitForModel bool
}

// HuggingFaceEmbedder implements Generator for HuggingFace Inference API.
type HuggingFaceEmbedder struct {
	cfg HuggingFaceConfig
	dim int
}

// NewHuggingFace creates a HuggingFace Inference API embedder.
//
//	ctx: passed to the connectivity probe
//	cfg: see HuggingFaceConfig. Model and APIKey are required.
//	dim: expected output vector dimension. Pass 0 to skip dimension validation.
func NewHuggingFace(ctx context.Context, cfg HuggingFaceConfig, dim int) (*HuggingFaceEmbedder, error) {
	if cfg.Model == "" {
		return nil, fmt.Errorf("HuggingFaceConfig.Model is required")
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("HuggingFaceConfig.APIKey is required")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 60 * time.Second // HF models can be slow to load
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 32
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = defaultClientPool.Get(cfg.Timeout)
	}

	e := &HuggingFaceEmbedder{cfg: cfg, dim: dim}

	if dim > 0 {
		if err := e.probe(ctx); err != nil {
			e.Close()
			return nil, fmt.Errorf("HuggingFace probe failed: %w", err)
		}
	}
	return e, nil
}

// NewHuggingFaceFromEnv creates a HuggingFace embedder using environment variables:
//
//	HF_TOKEN or HUGGINGFACE_API_KEY -> APIKey
//	ANDB_HUGGINGFACE_MODEL -> Model (default: sentence-transformers/all-MiniLM-L6-v2)
func NewHuggingFaceFromEnv(ctx context.Context, dim int) (*HuggingFaceEmbedder, error) {
	apiKey := os.Getenv("HF_TOKEN")
	if apiKey == "" {
		apiKey = os.Getenv("HUGGINGFACE_API_KEY")
	}
	model := os.Getenv("ANDB_HUGGINGFACE_MODEL")
	if model == "" {
		model = "sentence-transformers/all-MiniLM-L6-v2"
	}

	return NewHuggingFace(ctx, HuggingFaceConfig{
		Model:        model,
		APIKey:       apiKey,
		WaitForModel: true,
	}, dim)
}

// Generate implements dataplane.EmbeddingGenerator.
func (e *HuggingFaceEmbedder) Generate(text string) ([]float32, error) {
	vecs, err := e.BatchGenerate(context.Background(), []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("%w: empty response", ErrProviderUnavailable)
	}
	return vecs[0], nil
}

// BatchGenerate sends texts to HuggingFace in batches and returns embeddings.
func (e *HuggingFaceEmbedder) BatchGenerate(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	var out [][]float32
	for i := 0; i < len(texts); i += e.cfg.BatchSize {
		end := i + e.cfg.BatchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch, err := e.postEmbed(ctx, texts[i:end])
		if err != nil {
			return nil, err
		}
		out = append(out, batch...)
	}
	return out, nil
}

// postEmbed sends a single batch to the HuggingFace endpoint.
func (e *HuggingFaceEmbedder) postEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	// Build request body
	reqBody := map[string]any{
		"inputs": texts,
		"options": map[string]any{
			"wait_for_model": e.cfg.WaitForModel,
			"use_gpu":        e.cfg.UseGPU,
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Build URL
	url := fmt.Sprintf(
		"https://api-inference.huggingface.co/pipeline/feature-extraction/%s",
		e.cfg.Model,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.cfg.APIKey)

	resp, err := e.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w: status %d: %s", ErrProviderUnavailable, resp.StatusCode, string(body))
	}

	// HuggingFace returns [[float32]] for sentence-transformers models
	var embeddings [][]float32
	if err := json.NewDecoder(resp.Body).Decode(&embeddings); err != nil {
		// Some models return nested arrays, try alternative parsing
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return embeddings, nil
}

// probe sends a test request to verify connectivity and dimension.
func (e *HuggingFaceEmbedder) probe(ctx context.Context) error {
	vecs, err := e.postEmbed(ctx, []string{"probe"})
	if err != nil {
		return err
	}
	if len(vecs) != 1 {
		return fmt.Errorf("expected 1 embedding, got %d", len(vecs))
	}
	if len(vecs[0]) != e.dim {
		return fmt.Errorf("dimension mismatch: expected %d, got %d", e.dim, len(vecs[0]))
	}
	return nil
}

// Dim implements dataplane.EmbeddingGenerator.
func (e *HuggingFaceEmbedder) Dim() int { return e.dim }

// Reset is a no-op for HTTP embedders.
func (e *HuggingFaceEmbedder) Reset() {}

// Provider implements Generator.
func (e *HuggingFaceEmbedder) Provider() string { return "huggingface" }

// Close returns the HTTP client to the pool.
func (e *HuggingFaceEmbedder) Close() error {
	defaultClientPool.Put(e.cfg.HTTPClient)
	return nil
}
