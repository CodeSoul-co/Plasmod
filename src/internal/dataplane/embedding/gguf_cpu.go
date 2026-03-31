//go:build !cuda && gguf
// +build !cuda,gguf

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

	"github.com/go-skynet/go-llama.cpp"
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
type GGUFEmbedder struct {
	cfg    GGUFConfig
	dim    int
	model  *llama.LLama
	mu     sync.Mutex
	closed bool
}

// NewGGUF creates a GGUF/llama.cpp embedder.
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

	// Build model options
	opts := []llama.ModelOption{
		llama.SetContext(cfg.ContextSize),
		llama.EnableEmbeddings,
	}
	if cfg.GPULayers > 0 {
		opts = append(opts, llama.SetGPULayers(cfg.GPULayers))
	}

	// Load model
	model, err := llama.New(cfg.ModelPath, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load GGUF model: %w", err)
	}

	e := &GGUFEmbedder{
		cfg:   cfg,
		dim:   dim,
		model: model,
	}

	// Probe dimension if not specified
	if dim == 0 {
		testEmb, err := e.generateSingle("test")
		if err != nil {
			model.Free()
			return nil, fmt.Errorf("failed to probe embedding dimension: %w", err)
		}
		e.dim = len(testEmb)
	}

	return e, nil
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

// generateSingle generates embedding for a single text.
func (e *GGUFEmbedder) generateSingle(text string) ([]float32, error) {
	emb, err := e.model.Embeddings(text, llama.SetThreads(e.cfg.NumThreads))
	if err != nil {
		return nil, fmt.Errorf("embedding inference failed: %w", err)
	}
	// Convert []float64 to []float32
	result := make([]float32, len(emb))
	for i, v := range emb {
		result[i] = float32(v)
	}
	return result, nil
}

// Generate implements dataplane.EmbeddingGenerator.
func (e *GGUFEmbedder) Generate(text string) ([]float32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil, fmt.Errorf("GGUFEmbedder is closed")
	}
	return e.generateSingle(text)
}

// BatchGenerate runs inference on multiple texts.
func (e *GGUFEmbedder) BatchGenerate(ctx context.Context, texts []string) ([][]float32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil, fmt.Errorf("GGUFEmbedder is closed")
	}

	results := make([][]float32, len(texts))
	for i, text := range texts {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		emb, err := e.generateSingle(text)
		if err != nil {
			return nil, fmt.Errorf("batch item %d: %w", i, err)
		}
		results[i] = emb
	}
	return results, nil
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

	if e.model != nil {
		e.model.Free()
		e.model = nil
	}
	return nil
}
