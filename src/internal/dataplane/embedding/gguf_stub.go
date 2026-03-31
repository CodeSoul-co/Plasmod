//go:build !cuda && !gguf
// +build !cuda,!gguf

// Package embedding provides pluggable text-to-vector embedding generators.
//
// This is the stub implementation for GGUF when go-llama.cpp is not available.
// Build with: go build -tags gguf (CPU/Metal) or go build -tags cuda (CUDA) to enable real implementation.
package embedding

import (
	"context"
	"fmt"
)

// GGUFConfig holds configuration for the GGUF/llama.cpp embedder.
type GGUFConfig struct {
	ModelPath    string
	Device       string
	MaxBatchSize int
	NumThreads   int
	ContextSize  int
	GPULayers    int
}

// GGUFEmbedder is a stub that returns ErrProviderUnavailable when go-llama.cpp is not present.
type GGUFEmbedder struct{}

// NewGGUF returns ErrProviderUnavailable when go-llama.cpp is not built.
// Build with: go build -tags gguf (CPU) or go build -tags cuda (CUDA).
func NewGGUF(_ context.Context, _ GGUFConfig, _ int) (*GGUFEmbedder, error) {
	return nil, fmt.Errorf("%w: GGUF requires go-llama.cpp; build with -tags gguf or -tags cuda", ErrProviderUnavailable)
}

// NewGGUFFromEnv returns ErrProviderUnavailable when go-llama.cpp is not built.
func NewGGUFFromEnv(_ context.Context, _ int) (*GGUFEmbedder, error) {
	return nil, fmt.Errorf("%w: GGUF requires go-llama.cpp; build with -tags gguf or -tags cuda", ErrProviderUnavailable)
}

// Generate returns ErrProviderUnavailable.
func (e *GGUFEmbedder) Generate(_ string) ([]float32, error) {
	return nil, ErrProviderUnavailable
}

// BatchGenerate returns ErrProviderUnavailable.
func (e *GGUFEmbedder) BatchGenerate(_ context.Context, _ []string) ([][]float32, error) {
	return nil, ErrProviderUnavailable
}

// Dim returns 0.
func (e *GGUFEmbedder) Dim() int { return 0 }

// Reset is a no-op.
func (e *GGUFEmbedder) Reset() {}

// Provider returns "gguf".
func (e *GGUFEmbedder) Provider() string { return "gguf" }

// Close is a no-op.
func (e *GGUFEmbedder) Close() error { return nil }
