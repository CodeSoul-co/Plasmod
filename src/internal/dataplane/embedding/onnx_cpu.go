//go:build !cuda
// +build !cuda

// Package embedding provides pluggable text-to-vector embedding generators.
//
// OnnxEmbedder CPU/CoreML implementation using onnxruntime_go.
// This version runs on CPU (all platforms) or CoreML (Mac).
// Build with: go build -tags cuda (on Linux with CUDA) to enable CUDA acceleration.
//
// Prerequisites:
//
//	# Download ONNX Runtime from https://github.com/microsoft/onnxruntime/releases
//	# For Mac (CPU + CoreML):
//	wget https://github.com/microsoft/onnxruntime/releases/download/v1.17.0/onnxruntime-osx-arm64-1.17.0.tgz
//	tar xzf onnxruntime-osx-arm64-1.17.0.tgz
//	export ONNXRUNTIME_LIB_PATH=/path/to/onnxruntime-osx-arm64-1.17.0/lib/libonnxruntime.dylib
//
//	# For Linux (CPU):
//	wget https://github.com/microsoft/onnxruntime/releases/download/v1.17.0/onnxruntime-linux-x64-1.17.0.tgz
//	tar xzf onnxruntime-linux-x64-1.17.0.tgz
//	export ONNXRUNTIME_LIB_PATH=/path/to/onnxruntime-linux-x64-1.17.0/lib/libonnxruntime.so
//
//	# Download embedding model (e.g. all-MiniLM-L6-v2)
//	# Convert to ONNX format using optimum-cli or download pre-converted
package embedding

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"
)

// OnnxConfig holds configuration for the ONNX Runtime embedder.
type OnnxConfig struct {
	ModelPath    string // Path to .onnx model file (required)
	LibraryPath  string // Path to onnxruntime shared library (auto-detect if empty)
	Device       string // "cpu" or "coreml" (default: auto-detect)
	MaxBatchSize int    // Maximum batch size for inference (default: 32)
	NumThreads   int    // Number of inference threads (default: runtime.NumCPU())
	MaxSeqLength int    // Maximum sequence length (default: 512)
	UseCoreML    bool   // Use CoreML on Mac (default: true on darwin)
}

// OnnxEmbedder implements Generator using ONNX Runtime for local inference.
// This is the CPU/CoreML version (no CUDA).
type OnnxEmbedder struct {
	cfg    OnnxConfig
	dim    int
	mu     sync.Mutex
	closed bool
	// Note: In production, this would hold the ONNX Runtime session
	// session *ort.DynamicAdvancedSession
}

// NewOnnx creates an ONNX Runtime embedder (CPU/CoreML version).
func NewOnnx(_ context.Context, cfg OnnxConfig, dim int) (*OnnxEmbedder, error) {
	if cfg.ModelPath == "" {
		return nil, fmt.Errorf("OnnxConfig.ModelPath is required")
	}
	if _, err := os.Stat(cfg.ModelPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("ONNX model not found: %s", cfg.ModelPath)
	}

	// Auto-detect library path
	if cfg.LibraryPath == "" {
		cfg.LibraryPath = os.Getenv("ONNXRUNTIME_LIB_PATH")
		if cfg.LibraryPath == "" {
			switch runtime.GOOS {
			case "darwin":
				cfg.LibraryPath = "/usr/local/lib/libonnxruntime.dylib"
			case "linux":
				cfg.LibraryPath = "/usr/local/lib/libonnxruntime.so"
			case "windows":
				cfg.LibraryPath = "onnxruntime.dll"
			}
		}
	}

	// Auto-detect device
	if cfg.Device == "" {
		switch runtime.GOOS {
		case "darwin":
			cfg.Device = "coreml"
			cfg.UseCoreML = true
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
	if cfg.MaxSeqLength == 0 {
		cfg.MaxSeqLength = 512
	}

	// Note: Full implementation requires onnxruntime_go library.
	// Install with: go get github.com/yalue/onnxruntime_go
	// And download ONNX Runtime shared library.
	//
	// When onnxruntime_go is available, this would:
	//   ort.SetSharedLibraryPath(cfg.LibraryPath)
	//   ort.InitializeEnvironment()
	//   opts, _ := ort.NewSessionOptions()
	//   if cfg.UseCoreML {
	//       opts.AppendExecutionProviderCoreML(0)
	//   }
	//   session, _ := ort.NewDynamicAdvancedSession(cfg.ModelPath, inputs, outputs, opts)

	return nil, fmt.Errorf("%w: ONNX requires onnxruntime library; see onnx_cpu.go for setup instructions", ErrProviderUnavailable)
}

// NewOnnxFromEnv creates an ONNX embedder using environment variables.
func NewOnnxFromEnv(ctx context.Context, dim int) (*OnnxEmbedder, error) {
	modelPath := os.Getenv("ANDB_EMBEDDER_MODEL_PATH")
	libraryPath := os.Getenv("ONNXRUNTIME_LIB_PATH")
	device := os.Getenv("ANDB_EMBEDDER_DEVICE")

	return NewOnnx(ctx, OnnxConfig{
		ModelPath:   modelPath,
		LibraryPath: libraryPath,
		Device:      device,
	}, dim)
}

// Generate implements dataplane.EmbeddingGenerator.
func (e *OnnxEmbedder) Generate(_ string) ([]float32, error) {
	return nil, ErrProviderUnavailable
}

// BatchGenerate runs inference on multiple texts.
func (e *OnnxEmbedder) BatchGenerate(_ context.Context, _ []string) ([][]float32, error) {
	return nil, ErrProviderUnavailable
}

// Dim implements dataplane.EmbeddingGenerator.
func (e *OnnxEmbedder) Dim() int { return e.dim }

// Reset is a no-op for ONNX embedders.
func (e *OnnxEmbedder) Reset() {}

// Provider implements Generator.
func (e *OnnxEmbedder) Provider() string { return "onnx" }

// Close releases the ONNX Runtime session and environment.
func (e *OnnxEmbedder) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil
	}
	e.closed = true

	// When session is available:
	// if e.session != nil {
	//     e.session.Destroy()
	//     e.session = nil
	// }
	// ort.DestroyEnvironment()

	return nil
}
