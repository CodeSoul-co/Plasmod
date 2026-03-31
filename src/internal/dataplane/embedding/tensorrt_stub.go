//go:build !cuda || !linux || !tensorrt
// +build !cuda !linux !tensorrt

// Package embedding provides pluggable text-to-vector embedding generators.
//
// This is the stub implementation for TensorRT when CUDA is not available.
// On Mac or without CUDA, TensorRT is not supported.
// Build with: go build -tags cuda (on Linux with CUDA) to enable real implementation.
package embedding

import (
	"context"
	"fmt"
)

// TensorRTConfig holds configuration for the TensorRT embedder.
type TensorRTConfig struct {
	EnginePath    string // Path to .trt/.engine file (required)
	MaxBatchSize  int    // Maximum batch size for inference (default: 32)
	DeviceID      int    // CUDA device ID (default: 0)
	FP16          bool   // Use FP16 precision (default: true for speed)
	MaxSeqLength  int    // Maximum sequence length (default: 512)
	WorkspaceSize int64  // Maximum GPU memory for TensorRT workspace in bytes (default: 1GB)
}

// TensorRTEmbedder is a stub that returns ErrProviderUnavailable on non-CUDA systems.
type TensorRTEmbedder struct{}

// NewTensorRT returns ErrProviderUnavailable on non-CUDA systems.
// TensorRT requires Linux + NVIDIA GPU + CUDA toolkit.
// Build with: go build -tags cuda (on Linux with CUDA)
func NewTensorRT(_ context.Context, _ TensorRTConfig, _ int) (*TensorRTEmbedder, error) {
	return nil, fmt.Errorf("%w: TensorRT requires Linux + CUDA; build with -tags cuda on Linux", ErrProviderUnavailable)
}

// NewTensorRTFromEnv returns ErrProviderUnavailable on non-CUDA systems.
func NewTensorRTFromEnv(_ context.Context, _ int) (*TensorRTEmbedder, error) {
	return nil, fmt.Errorf("%w: TensorRT requires Linux + CUDA; build with -tags cuda on Linux", ErrProviderUnavailable)
}

// Generate returns ErrProviderUnavailable.
func (e *TensorRTEmbedder) Generate(_ string) ([]float32, error) {
	return nil, ErrProviderUnavailable
}

// BatchGenerate returns ErrProviderUnavailable.
func (e *TensorRTEmbedder) BatchGenerate(_ context.Context, _ []string) ([][]float32, error) {
	return nil, ErrProviderUnavailable
}

// Dim returns 0.
func (e *TensorRTEmbedder) Dim() int { return 0 }

// Reset is a no-op.
func (e *TensorRTEmbedder) Reset() {}

// Provider returns "tensorrt".
func (e *TensorRTEmbedder) Provider() string { return "tensorrt" }

// Close is a no-op.
func (e *TensorRTEmbedder) Close() error { return nil }
