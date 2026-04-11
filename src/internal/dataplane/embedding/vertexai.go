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

// VertexAIConfig holds connection parameters for Google Cloud Vertex AI Embeddings API.
//
// Authentication: Uses Application Default Credentials (ADC) or explicit access token.
// Set GOOGLE_APPLICATION_CREDENTIALS env var to a service account JSON path,
// or provide AccessToken directly.
//
// Endpoint format:
//
//	https://{Location}-aiplatform.googleapis.com/v1/projects/{ProjectID}/locations/{Location}/publishers/google/models/{Model}:predict
type VertexAIConfig struct {
	ProjectID   string        // GCP project ID (required)
	Location    string        // e.g. "us-central1" (required)
	Model       string        // e.g. "text-embedding-004", "textembedding-gecko@003"
	AccessToken string        // OAuth2 access token; if empty, uses ADC
	HTTPClient  *http.Client  // optional; nil -> default
	Timeout     time.Duration // per-request timeout; 0 -> 30s default
	BatchSize   int           // texts per request; 0 -> 250 (Vertex AI limit)
}

// VertexAIEmbedder implements Generator for Google Cloud Vertex AI Embeddings API.
type VertexAIEmbedder struct {
	cfg VertexAIConfig
	dim int
}

// NewVertexAI creates a Vertex AI embedder.
//
//	ctx: passed to the connectivity probe
//	cfg: see VertexAIConfig. ProjectID, Location, and Model are required.
//	dim: expected output vector dimension. Pass 0 to skip dimension validation.
func NewVertexAI(ctx context.Context, cfg VertexAIConfig, dim int) (*VertexAIEmbedder, error) {
	if cfg.ProjectID == "" {
		return nil, fmt.Errorf("VertexAIConfig.ProjectID is required")
	}
	if cfg.Location == "" {
		return nil, fmt.Errorf("VertexAIConfig.Location is required")
	}
	if cfg.Model == "" {
		cfg.Model = "text-embedding-004"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 250 // Vertex AI limit
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = defaultClientPool.Get(cfg.Timeout)
	}

	e := &VertexAIEmbedder{cfg: cfg, dim: dim}

	if dim > 0 {
		if err := e.probe(ctx); err != nil {
			e.Close()
			return nil, fmt.Errorf("VertexAI probe failed: %w", err)
		}
	}
	return e, nil
}

// NewVertexAIFromEnv creates a VertexAI embedder using environment variables:
//
//	GOOGLE_CLOUD_PROJECT or PLASMOD_VERTEXAI_PROJECT -> ProjectID
//	PLASMOD_VERTEXAI_LOCATION -> Location (default: us-central1)
//	PLASMOD_VERTEXAI_MODEL -> Model (default: text-embedding-004)
//	GOOGLE_ACCESS_TOKEN -> AccessToken (optional)
func NewVertexAIFromEnv(ctx context.Context, dim int) (*VertexAIEmbedder, error) {
	projectID := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if projectID == "" {
		projectID = os.Getenv("PLASMOD_VERTEXAI_PROJECT")
	}
	location := os.Getenv("PLASMOD_VERTEXAI_LOCATION")
	if location == "" {
		location = "us-central1"
	}
	model := os.Getenv("PLASMOD_VERTEXAI_MODEL")
	if model == "" {
		model = "text-embedding-004"
	}
	accessToken := os.Getenv("GOOGLE_ACCESS_TOKEN")

	return NewVertexAI(ctx, VertexAIConfig{
		ProjectID:   projectID,
		Location:    location,
		Model:       model,
		AccessToken: accessToken,
	}, dim)
}

// Generate implements dataplane.EmbeddingGenerator.
func (e *VertexAIEmbedder) Generate(text string) ([]float32, error) {
	vecs, err := e.BatchGenerate(context.Background(), []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("%w: empty response", ErrProviderUnavailable)
	}
	return vecs[0], nil
}

// BatchGenerate sends texts to Vertex AI in batches and returns embeddings.
func (e *VertexAIEmbedder) BatchGenerate(ctx context.Context, texts []string) ([][]float32, error) {
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

// postEmbed sends a single batch to the Vertex AI endpoint.
func (e *VertexAIEmbedder) postEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	// Build request body
	instances := make([]map[string]string, len(texts))
	for i, t := range texts {
		instances[i] = map[string]string{"content": t}
	}
	reqBody := map[string]any{
		"instances": instances,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Build URL
	url := fmt.Sprintf(
		"https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/google/models/%s:predict",
		e.cfg.Location, e.cfg.ProjectID, e.cfg.Location, e.cfg.Model,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Auth header
	if e.cfg.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+e.cfg.AccessToken)
	}

	resp, err := e.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w: status %d: %s", ErrProviderUnavailable, resp.StatusCode, string(body))
	}

	var result vertexAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	embeddings := make([][]float32, len(result.Predictions))
	for i, pred := range result.Predictions {
		embeddings[i] = pred.Embeddings.Values
	}
	return embeddings, nil
}

// probe sends a test request to verify connectivity and dimension.
func (e *VertexAIEmbedder) probe(ctx context.Context) error {
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
func (e *VertexAIEmbedder) Dim() int { return e.dim }

// Reset is a no-op for HTTP embedders.
func (e *VertexAIEmbedder) Reset() {}

// Provider implements Generator.
func (e *VertexAIEmbedder) Provider() string { return "vertexai" }

// Close returns the HTTP client to the pool.
func (e *VertexAIEmbedder) Close() error {
	defaultClientPool.Put(e.cfg.HTTPClient)
	return nil
}

// vertexAIResponse is the Vertex AI predict response schema.
type vertexAIResponse struct {
	Predictions []struct {
		Embeddings struct {
			Values []float32 `json:"values"`
		} `json:"embeddings"`
	} `json:"predictions"`
}
