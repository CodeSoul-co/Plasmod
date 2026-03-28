//go:build tensorrt
// +build tensorrt

// Package embedding provides pluggable text-to-vector embedding generators.
//
// TensorRTEmbedder requires the tensorrt build tag and NVIDIA TensorRT library.
// Build with: go build -tags tensorrt ./...
//
// Prerequisites:
//   - NVIDIA GPU with CUDA support
//   - TensorRT SDK installed
//   - Set CGO_LDFLAGS and CGO_CFLAGS to point to TensorRT installation
//   - Convert ONNX model to TensorRT engine (.trt or .engine file)
package embedding

import (
	"context"
	"fmt"
	"os"
	"sync"
)

// TensorRTConfig holds configuration for the TensorRT embedder.
type TensorRTConfig struct {
	EnginePath   string // Path to .trt/.engine file (required)
	MaxBatchSize int    // Maximum batch size for inference (default: 32)
	DeviceID     int    // CUDA device ID (default: 0)
	FP16         bool   // Use FP16 precision (default: true for speed)
	// WorkspaceSize is the maximum GPU memory for TensorRT workspace in bytes.
	// Default: 1GB
	WorkspaceSize int64
}

// TensorRTEmbedder implements Generator using NVIDIA TensorRT for GPU inference.
//
// This embedder loads a pre-built TensorRT engine and runs inference on GPU.
// Provides the fastest inference for NVIDIA GPUs with optimized kernels.
type TensorRTEmbedder struct {
	cfg    TensorRTConfig
	dim    int
	mu     sync.Mutex
	closed bool
	// runtime would hold the TensorRT runtime handle
	// engine would hold the TensorRT engine handle
	// context would hold the TensorRT execution context
}

// NewTensorRT creates a TensorRT embedder.
//
//	ctx: passed to engine loading
//	cfg: see TensorRTConfig. EnginePath is required.
//	dim: expected output vector dimension. Pass 0 to auto-detect from engine.
func NewTensorRT(ctx context.Context, cfg TensorRTConfig, dim int) (*TensorRTEmbedder, error) {
	if cfg.EnginePath == "" {
		return nil, fmt.Errorf("TensorRTConfig.EnginePath is required")
	}
	if _, err := os.Stat(cfg.EnginePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("TensorRT engine not found: %s", cfg.EnginePath)
	}
	if cfg.MaxBatchSize == 0 {
		cfg.MaxBatchSize = 32
	}
	if cfg.WorkspaceSize == 0 {
		cfg.WorkspaceSize = 1 << 30 // 1GB
	}

	e := &TensorRTEmbedder{
		cfg: cfg,
		dim: dim,
	}

	// TODO: Initialize TensorRT runtime and load engine
	// This requires CGO bindings to TensorRT C++ API:
	//   - nvinfer1::createInferRuntime()
	//   - runtime->deserializeCudaEngine()
	//   - engine->createExecutionContext()
	//
	// For now, return an error indicating the feature is not yet implemented
	return nil, fmt.Errorf("TensorRT integration not yet implemented; requires CGO bindings and CUDA")
}

// NewTensorRTFromEnv creates a TensorRT embedder using environment variables:
//
//	ANDB_EMBEDDER_MODEL_PATH -> EnginePath
//	ANDB_EMBEDDER_MAX_BATCH_SIZE -> MaxBatchSize (default: 32)
//	CUDA_VISIBLE_DEVICES -> DeviceID (parsed from first device)
func NewTensorRTFromEnv(ctx context.Context, dim int) (*TensorRTEmbedder, error) {
	enginePath := os.Getenv("ANDB_EMBEDDER_MODEL_PATH")

	return NewTensorRT(ctx, TensorRTConfig{
		EnginePath: enginePath,
		FP16:       true,
	}, dim)
}

// Generate implements dataplane.EmbeddingGenerator.
func (e *TensorRTEmbedder) Generate(text string) ([]float32, error) {
	vecs, err := e.BatchGenerate(context.Background(), []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("TensorRT inference returned empty result")
	}
	return vecs[0], nil
}

// BatchGenerate runs inference on multiple texts.
func (e *TensorRTEmbedder) BatchGenerate(ctx context.Context, texts []string) ([][]float32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil, fmt.Errorf("TensorRTEmbedder is closed")
	}

	// TODO: Implement actual TensorRT inference
	// 1. Tokenize texts
	// 2. Copy input tensors to GPU
	// 3. Run inference via context->enqueueV2()
	// 4. Copy output tensors from GPU
	// 5. Extract embeddings

	return nil, fmt.Errorf("TensorRT inference not implemented")
}

// Dim implements dataplane.EmbeddingGenerator.
func (e *TensorRTEmbedder) Dim() int { return e.dim }

// Reset is a no-op for TensorRT embedders.
func (e *TensorRTEmbedder) Reset() {}

// Provider implements Generator.
func (e *TensorRTEmbedder) Provider() string { return "tensorrt" }

// Close releases the TensorRT engine and runtime.
func (e *TensorRTEmbedder) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil
	}
	e.closed = true

	// TODO: Release TensorRT resources
	// context->destroy()
	// engine->destroy()
	// runtime->destroy()

	return nil
}
