//go:build !cuda
// +build !cuda

// Package embedding provides pluggable text-to-vector embedding generators.
//
// GGUFEmbedder CPU/Metal implementation using go-llama.cpp.
// Build with: go build -tags cuda (on Linux with CUDA) to enable CUDA acceleration.
//
// Prerequisites (requires building go-llama.cpp C++ library first):
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
//	# Set paths before building Go code
//	export LIBRARY_PATH=/path/to/go-llama.cpp
//	export C_INCLUDE_PATH=/path/to/go-llama.cpp
//
//	# Download embedding model (e.g. nomic-embed-text)
//	wget https://huggingface.co/nomic-ai/nomic-embed-text-v1.5-GGUF/resolve/main/nomic-embed-text-v1.5.Q4_K_M.gguf
//
// Note: This file contains stub implementation. After building go-llama.cpp,
// replace this with gguf_llama.go that imports the library.
package embedding

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync"
	// llama "github.com/go-skynet/go-llama.cpp"
	// Uncomment above after building go-llama.cpp:
	//   git clone --recurse-submodules https://github.com/go-skynet/go-llama.cpp
	//   cd go-llama.cpp && BUILD_TYPE=metal make libbinding.a
	//   export LIBRARY_PATH=$PWD C_INCLUDE_PATH=$PWD
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
// This is a stub implementation - requires go-llama.cpp to be built first.
type GGUFEmbedder struct {
	cfg    GGUFConfig
	dim    int
	mu     sync.Mutex
	closed bool
}

// NewGGUF creates a GGUF/llama.cpp embedder.
// Returns ErrProviderUnavailable until go-llama.cpp is built and linked.
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
			cfg.Device = "metal"
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
		cfg.GPULayers = 99
	}

	// Stub: go-llama.cpp requires building C++ library first
	// See file header for build instructions
	return nil, fmt.Errorf("%w: GGUF requires go-llama.cpp C++ library; see gguf_cpu.go header for build instructions", ErrProviderUnavailable)
}

// NewGGUFFromEnv creates a GGUF embedder using environment variables.
func NewGGUFFromEnv(ctx context.Context, dim int) (*GGUFEmbedder, error) {
	modelPath := os.Getenv("ANDB_EMBEDDER_MODEL_PATH")
	device := os.Getenv("ANDB_EMBEDDER_DEVICE")

	gpuLayers := 0
	if device == "metal" || (device == "" && runtime.GOOS == "darwin") {
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
