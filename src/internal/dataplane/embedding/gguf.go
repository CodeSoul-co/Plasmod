//go:build gguf
// +build gguf

// Package embedding provides pluggable text-to-vector embedding generators.
//
// GGUFEmbedder requires the gguf build tag and llama.cpp library.
// Build with: go build -tags gguf ./...
//
// Prerequisites:
//   - Build llama.cpp with embedding support
//   - Set CGO_LDFLAGS and CGO_CFLAGS to point to llama.cpp installation
//   - Download a GGUF embedding model (e.g. nomic-embed-text-v1.5.Q4_K_M.gguf)
package embedding

import (
	"context"
	"fmt"
	"os"
	"sync"
)

// GGUFConfig holds configuration for the GGUF/llama.cpp embedder.
type GGUFConfig struct {
	ModelPath    string // Path to .gguf model file (required)
	Device       string // "cpu", "cuda", or "metal" (default: cpu)
	MaxBatchSize int    // Maximum batch size for inference (default: 32)
	NumThreads   int    // Number of inference threads (default: 4)
	ContextSize  int    // Context window size (default: 512)
	GPULayers    int    // Number of layers to offload to GPU (default: 0)
}

// GGUFEmbedder implements Generator using llama.cpp for local GGUF model inference.
//
// This embedder loads a quantized GGUF model and runs inference locally.
// Supports CPU, CUDA, and Metal backends for efficient inference.
type GGUFEmbedder struct {
	cfg    GGUFConfig
	dim    int
	mu     sync.Mutex
	closed bool
	// model would hold the llama.cpp model handle
	// ctx would hold the llama.cpp context handle
}

// NewGGUF creates a GGUF/llama.cpp embedder.
//
//	ctx: passed to model loading
//	cfg: see GGUFConfig. ModelPath is required.
//	dim: expected output vector dimension. Pass 0 to auto-detect from model.
func NewGGUF(ctx context.Context, cfg GGUFConfig, dim int) (*GGUFEmbedder, error) {
	if cfg.ModelPath == "" {
		return nil, fmt.Errorf("GGUFConfig.ModelPath is required")
	}
	if _, err := os.Stat(cfg.ModelPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("GGUF model not found: %s", cfg.ModelPath)
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
	if cfg.ContextSize == 0 {
		cfg.ContextSize = 512
	}

	e := &GGUFEmbedder{
		cfg: cfg,
		dim: dim,
	}

	// TODO: Initialize llama.cpp model and context
	// This requires CGO bindings to llama.cpp:
	//   - llama_backend_init()
	//   - llama_model_load_from_file()
	//   - llama_new_context_with_model()
	//
	// For now, return an error indicating the feature is not yet implemented
	return nil, fmt.Errorf("GGUF/llama.cpp integration not yet implemented; requires CGO bindings")
}

// NewGGUFFromEnv creates a GGUF embedder using environment variables:
//
//	ANDB_EMBEDDER_MODEL_PATH -> ModelPath
//	ANDB_EMBEDDER_DEVICE -> Device (default: cpu)
//	ANDB_EMBEDDER_MAX_BATCH_SIZE -> MaxBatchSize (default: 32)
func NewGGUFFromEnv(ctx context.Context, dim int) (*GGUFEmbedder, error) {
	modelPath := os.Getenv("ANDB_EMBEDDER_MODEL_PATH")
	device := os.Getenv("ANDB_EMBEDDER_DEVICE")
	if device == "" {
		device = "cpu"
	}

	return NewGGUF(ctx, GGUFConfig{
		ModelPath: modelPath,
		Device:    device,
	}, dim)
}

// Generate implements dataplane.EmbeddingGenerator.
func (e *GGUFEmbedder) Generate(text string) ([]float32, error) {
	vecs, err := e.BatchGenerate(context.Background(), []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("GGUF inference returned empty result")
	}
	return vecs[0], nil
}

// BatchGenerate runs inference on multiple texts.
func (e *GGUFEmbedder) BatchGenerate(ctx context.Context, texts []string) ([][]float32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil, fmt.Errorf("GGUFEmbedder is closed")
	}

	// TODO: Implement actual llama.cpp inference
	// 1. Tokenize texts using llama_tokenize()
	// 2. Run inference via llama_decode()
	// 3. Extract embeddings via llama_get_embeddings()

	return nil, fmt.Errorf("GGUF inference not implemented")
}

// Dim implements dataplane.EmbeddingGenerator.
func (e *GGUFEmbedder) Dim() int { return e.dim }

// Reset is a no-op for GGUF embedders.
func (e *GGUFEmbedder) Reset() {}

// Provider implements Generator.
func (e *GGUFEmbedder) Provider() string { return "gguf" }

// Close releases the llama.cpp model and context.
func (e *GGUFEmbedder) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil
	}
	e.closed = true

	// TODO: Release llama.cpp resources
	// llama_free(e.ctx)
	// llama_free_model(e.model)
	// llama_backend_free()

	return nil
}
