//go:build onnx
// +build onnx

// Package embedding provides pluggable text-to-vector embedding generators.
//
// OnnxEmbedder requires the onnx build tag and the ONNX Runtime C library.
// Build with: go build -tags onnx ./...
//
// Prerequisites:
//   - Install ONNX Runtime: https://onnxruntime.ai/
//   - Set CGO_LDFLAGS and CGO_CFLAGS to point to the ONNX Runtime installation
//   - Download an ONNX embedding model (e.g. all-MiniLM-L6-v2.onnx)
package embedding

import (
	"context"
	"fmt"
	"os"
	"sync"
)

// OnnxConfig holds configuration for the ONNX Runtime embedder.
type OnnxConfig struct {
	ModelPath    string // Path to .onnx model file (required)
	Device       string // "cpu" or "cuda" (default: cpu)
	MaxBatchSize int    // Maximum batch size for inference (default: 32)
	NumThreads   int    // Number of inference threads (default: 4)
}

// OnnxEmbedder implements Generator using ONNX Runtime for local inference.
//
// This embedder loads a pre-trained ONNX model and runs inference locally
// without any network calls. Suitable for offline/air-gapped deployments.
type OnnxEmbedder struct {
	cfg    OnnxConfig
	dim    int
	mu     sync.Mutex
	closed bool
	// session would hold the ONNX Runtime session handle
	// For now this is a stub implementation
}

// NewOnnx creates an ONNX Runtime embedder.
//
//	ctx: passed to model loading
//	cfg: see OnnxConfig. ModelPath is required.
//	dim: expected output vector dimension. Pass 0 to auto-detect from model.
func NewOnnx(ctx context.Context, cfg OnnxConfig, dim int) (*OnnxEmbedder, error) {
	if cfg.ModelPath == "" {
		return nil, fmt.Errorf("OnnxConfig.ModelPath is required")
	}
	if _, err := os.Stat(cfg.ModelPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("ONNX model not found: %s", cfg.ModelPath)
	}
	if cfg.Device == "" {
		cfg.Device = "cpu"
	}
	if cfg.MaxBatchSize == 0 {
		cfg.MaxBatchSize = 32
	}
	if cfg.NumThreads == 0 {
		cfg.NumThreads = 4
	}

	e := &OnnxEmbedder{
		cfg: cfg,
		dim: dim,
	}

	// TODO: Initialize ONNX Runtime session
	// This requires CGO bindings to onnxruntime C API:
	//   - ort.CreateEnv()
	//   - ort.CreateSessionOptions()
	//   - ort.CreateSession(modelPath)
	//
	// For now, return an error indicating the feature is not yet implemented
	return nil, fmt.Errorf("ONNX Runtime integration not yet implemented; requires CGO bindings")
}

// NewOnnxFromEnv creates an ONNX embedder using environment variables:
//
//	ANDB_EMBEDDER_MODEL_PATH -> ModelPath
//	ANDB_EMBEDDER_DEVICE -> Device (default: cpu)
//	ANDB_EMBEDDER_MAX_BATCH_SIZE -> MaxBatchSize (default: 32)
func NewOnnxFromEnv(ctx context.Context, dim int) (*OnnxEmbedder, error) {
	modelPath := os.Getenv("ANDB_EMBEDDER_MODEL_PATH")
	device := os.Getenv("ANDB_EMBEDDER_DEVICE")
	if device == "" {
		device = "cpu"
	}

	return NewOnnx(ctx, OnnxConfig{
		ModelPath: modelPath,
		Device:    device,
	}, dim)
}

// Generate implements dataplane.EmbeddingGenerator.
func (e *OnnxEmbedder) Generate(text string) ([]float32, error) {
	vecs, err := e.BatchGenerate(context.Background(), []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("ONNX inference returned empty result")
	}
	return vecs[0], nil
}

// BatchGenerate runs inference on multiple texts.
func (e *OnnxEmbedder) BatchGenerate(ctx context.Context, texts []string) ([][]float32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil, fmt.Errorf("OnnxEmbedder is closed")
	}

	// TODO: Implement actual ONNX inference
	// 1. Tokenize texts using the model's tokenizer
	// 2. Create input tensors (input_ids, attention_mask)
	// 3. Run inference via ort.Run()
	// 4. Extract embeddings from output tensor
	// 5. Apply mean pooling if needed

	return nil, fmt.Errorf("ONNX inference not implemented")
}

// Dim implements dataplane.EmbeddingGenerator.
func (e *OnnxEmbedder) Dim() int { return e.dim }

// Reset is a no-op for ONNX embedders.
func (e *OnnxEmbedder) Reset() {}

// Provider implements Generator.
func (e *OnnxEmbedder) Provider() string { return "onnx" }

// Close releases the ONNX Runtime session.
func (e *OnnxEmbedder) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil
	}
	e.closed = true

	// TODO: Release ONNX Runtime resources
	// ort.ReleaseSession(e.session)
	// ort.ReleaseEnv(e.env)

	return nil
}
