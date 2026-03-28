//go:build cuda
// +build cuda

// Package embedding provides pluggable text-to-vector embedding generators.
//
// GGUFEmbedder CUDA implementation using go-llama.cpp with CUDA backend.
// Build with: go build -tags cuda ./...
//
// Prerequisites:
//
//	# Clone and build go-llama.cpp with CUDA
//	git clone --recurse-submodules https://github.com/go-skynet/go-llama.cpp
//	cd go-llama.cpp
//
//	# Build with CUDA support
//	BUILD_TYPE=cublas make libbinding.a
//	export CGO_LDFLAGS="-lcublas -lcudart -L/usr/local/cuda/lib64/"
//
//	# Set paths
//	export LIBRARY_PATH=/path/to/go-llama.cpp
//	export C_INCLUDE_PATH=/path/to/go-llama.cpp
//
//	# Download embedding model
//	wget https://huggingface.co/nomic-ai/nomic-embed-text-v1.5-GGUF/resolve/main/nomic-embed-text-v1.5.Q4_K_M.gguf
package embedding

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync"
)

// GGUFConfig holds configuration for the GGUF/llama.cpp embedder.
type GGUFConfig struct {
	ModelPath    string // Path to .gguf model file (required)
	Device       string // "cpu" or "cuda" (default: cuda when built with -tags cuda)
	MaxBatchSize int    // Maximum batch size for inference (default: 32)
	NumThreads   int    // Number of inference threads (default: runtime.NumCPU())
	ContextSize  int    // Context window size (default: 512)
	GPULayers    int    // Number of layers to offload to GPU (default: 99 for full GPU)
}

// GGUFEmbedder implements Generator using llama.cpp for local GGUF model inference.
// This is the CUDA version for Linux with NVIDIA GPU.
type GGUFEmbedder struct {
	cfg    GGUFConfig
	dim    int
	mu     sync.Mutex
	closed bool
	// Note: In production, this would hold the llama.cpp model handle
	// model *llama.LLama
}

// NewGGUF creates a GGUF/llama.cpp embedder with CUDA GPU acceleration.
func NewGGUF(_ context.Context, cfg GGUFConfig, dim int) (*GGUFEmbedder, error) {
	if cfg.ModelPath == "" {
		return nil, fmt.Errorf("GGUFConfig.ModelPath is required")
	}
	if _, err := os.Stat(cfg.ModelPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("GGUF model not found: %s", cfg.ModelPath)
	}

	// CUDA build defaults to GPU
	if cfg.Device == "" {
		cfg.Device = "cuda"
	}

	if cfg.MaxBatchSize == 0 {
		cfg.MaxBatchSize = 32
	}
	if cfg.NumThreads == 0 {
		cfg.NumThreads = runtime.NumCPU()
	}
	if cfg.ContextSize == 0 {
		cfg.ContextSize = 512
	}
	if cfg.GPULayers == 0 {
		cfg.GPULayers = 99 // Offload all layers to CUDA GPU
	}

	// Note: Full implementation requires go-llama.cpp library built with CUDA.
	// Build with:
	//   BUILD_TYPE=cublas make libbinding.a
	//   CGO_LDFLAGS="-lcublas -lcudart -L/usr/local/cuda/lib64/" go build -tags cuda
	//
	// When go-llama.cpp is available, this would call:
	//   opts := []llama.ModelOption{
	//       llama.SetContext(cfg.ContextSize),
	//       llama.EnableEmbeddings,
	//       llama.SetGPULayers(cfg.GPULayers),
	//   }
	//   model, err := llama.New(cfg.ModelPath, opts...)

	return nil, fmt.Errorf("%w: GGUF CUDA requires go-llama.cpp built with cublas; see gguf_cuda.go for build instructions", ErrProviderUnavailable)
}

// NewGGUFFromEnv creates a GGUF embedder using environment variables.
func NewGGUFFromEnv(ctx context.Context, dim int) (*GGUFEmbedder, error) {
	modelPath := os.Getenv("ANDB_EMBEDDER_MODEL_PATH")
	device := os.Getenv("ANDB_EMBEDDER_DEVICE")
	if device == "" {
		device = "cuda" // Default to CUDA in cuda build
	}

	gpuLayers := 99 // Full GPU offload by default
	if s := os.Getenv("ANDB_EMBEDDER_GPU_LAYERS"); s != "" {
		if v, err := strconv.Atoi(s); err == nil {
			gpuLayers = v
		}
	}

	return NewGGUF(ctx, GGUFConfig{
		ModelPath: modelPath,
		Device:    device,
		GPULayers: gpuLayers,
	}, dim)
}

// Generate implements dataplane.EmbeddingGenerator.
func (e *GGUFEmbedder) Generate(_ string) ([]float32, error) {
	return nil, ErrProviderUnavailable
}

// BatchGenerate runs inference on multiple texts.
func (e *GGUFEmbedder) BatchGenerate(_ context.Context, _ []string) ([][]float32, error) {
	return nil, ErrProviderUnavailable
}

// Dim implements dataplane.EmbeddingGenerator.
func (e *GGUFEmbedder) Dim() int { return e.dim }

// Reset is a no-op for GGUF embedders.
func (e *GGUFEmbedder) Reset() {}

// Provider implements Generator.
func (e *GGUFEmbedder) Provider() string { return "gguf" }

// Close releases the llama.cpp model resources.
func (e *GGUFEmbedder) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil
	}
	e.closed = true

	// When model is available:
	// if e.model != nil {
	//     e.model.Free()
	//     e.model = nil
	// }

	return nil
}
