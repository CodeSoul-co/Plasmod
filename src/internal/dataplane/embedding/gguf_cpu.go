//go:build !cuda
// +build !cuda

// Package embedding provides pluggable text-to-vector embedding generators.
//
// GGUFEmbedder CPU implementation using go-llama.cpp.
// This version runs on CPU (Mac/Linux without CUDA).
// Build with: go build -tags cuda (on Linux with CUDA) to enable GPU acceleration.
//
// Prerequisites:
//
//	# Clone and build go-llama.cpp
//	git clone --recurse-submodules https://github.com/go-skynet/go-llama.cpp
//	cd go-llama.cpp
//
//	# For Mac (Metal GPU acceleration):
//	BUILD_TYPE=metal make libbinding.a
//	export CGO_LDFLAGS="-framework Foundation -framework Metal -framework MetalKit -framework MetalPerformanceShaders"
//
//	# For CPU only:
//	make libbinding.a
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
	Device       string // "cpu" or "metal" (default: auto-detect)
	MaxBatchSize int    // Maximum batch size for inference (default: 32)
	NumThreads   int    // Number of inference threads (default: runtime.NumCPU())
	ContextSize  int    // Context window size (default: 512)
	GPULayers    int    // Number of layers to offload to GPU (default: 0 for CPU, 99 for Metal)
}

// GGUFEmbedder implements Generator using llama.cpp for local GGUF model inference.
// This is the CPU/Metal version (no CUDA).
type GGUFEmbedder struct {
	cfg    GGUFConfig
	dim    int
	mu     sync.Mutex
	closed bool
	// Note: In production, this would hold the llama.cpp model handle
	// For now, this is a stub that returns ErrProviderUnavailable
	// until go-llama.cpp is properly linked
}

// NewGGUF creates a GGUF/llama.cpp embedder (CPU/Metal version).
func NewGGUF(_ context.Context, cfg GGUFConfig, dim int) (*GGUFEmbedder, error) {
	if cfg.ModelPath == "" {
		return nil, fmt.Errorf("GGUFConfig.ModelPath is required")
	}
	if _, err := os.Stat(cfg.ModelPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("GGUF model not found: %s", cfg.ModelPath)
	}

	// Auto-detect device based on OS
	if cfg.Device == "" {
		switch runtime.GOOS {
		case "darwin":
			cfg.Device = "metal" // Mac uses Metal for GPU
		default:
			cfg.Device = "cpu"
		}
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
	if cfg.GPULayers == 0 && cfg.Device == "metal" {
		cfg.GPULayers = 99 // Offload all layers to Metal GPU
	}

	// Note: Full implementation requires go-llama.cpp library to be linked.
	// Build with proper CGO flags:
	//   LIBRARY_PATH=/path/to/go-llama.cpp C_INCLUDE_PATH=/path/to/go-llama.cpp go build
	//
	// For now, return error indicating library not linked.
	// When go-llama.cpp is available, this would call:
	//   model, err := llama.New(cfg.ModelPath, llama.EnableEmbeddings, llama.SetGPULayers(cfg.GPULayers))

	return nil, fmt.Errorf("%w: GGUF requires go-llama.cpp library; see gguf_cpu.go for build instructions", ErrProviderUnavailable)
}

// NewGGUFFromEnv creates a GGUF embedder using environment variables.
func NewGGUFFromEnv(ctx context.Context, dim int) (*GGUFEmbedder, error) {
	modelPath := os.Getenv("ANDB_EMBEDDER_MODEL_PATH")
	device := os.Getenv("ANDB_EMBEDDER_DEVICE")

	gpuLayers := 0
	if device == "metal" {
		gpuLayers = 99
	}
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
	return nil
}
