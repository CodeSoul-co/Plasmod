//go:build cuda
// +build cuda

// Package embedding provides pluggable text-to-vector embedding generators.
//
// OnnxEmbedder CUDA implementation using onnxruntime_go with CUDA execution provider.
// Build with: go build -tags cuda ./...
//
// Prerequisites:
//
//	# Download ONNX Runtime GPU version
//	wget https://github.com/microsoft/onnxruntime/releases/download/v1.17.0/onnxruntime-linux-x64-gpu-1.17.0.tgz
//	tar xzf onnxruntime-linux-x64-gpu-1.17.0.tgz
//	export ONNXRUNTIME_LIB_PATH=/path/to/onnxruntime-linux-x64-gpu-1.17.0/lib/libonnxruntime.so
//
//	# Ensure CUDA is installed
//	export LD_LIBRARY_PATH=/usr/local/cuda/lib64:$LD_LIBRARY_PATH
//
//	# Download embedding model
//	# Convert to ONNX format using optimum-cli or download pre-converted
package embedding

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync"
)

// OnnxConfig holds configuration for the ONNX Runtime embedder.
type OnnxConfig struct {
	ModelPath    string // Path to .onnx model file (required)
	LibraryPath  string // Path to onnxruntime shared library (auto-detect if empty)
	Device       string // "cpu" or "cuda" (default: cuda when built with -tags cuda)
	MaxBatchSize int    // Maximum batch size for inference (default: 32)
	NumThreads   int    // Number of inference threads (default: runtime.NumCPU())
	MaxSeqLength int    // Maximum sequence length (default: 512)
	UseCUDA      bool   // Use CUDA execution provider (default: true in cuda build)
	CUDADeviceID int    // CUDA device ID (default: 0)
}

// OnnxEmbedder implements Generator using ONNX Runtime for local inference.
// This is the CUDA version for Linux with NVIDIA GPU.
type OnnxEmbedder struct {
	cfg    OnnxConfig
	dim    int
	mu     sync.Mutex
	closed bool
	// Note: In production, this would hold the ONNX Runtime session
	// session *ort.DynamicAdvancedSession
}

// NewOnnx creates an ONNX Runtime embedder with CUDA GPU acceleration.
func NewOnnx(_ context.Context, cfg OnnxConfig, dim int) (*OnnxEmbedder, error) {
	if cfg.ModelPath == "" {
		return nil, fmt.Errorf("OnnxConfig.ModelPath is required")
	}
	if _, err := os.Stat(cfg.ModelPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("ONNX model not found: %s", cfg.ModelPath)
	}

	// Auto-detect library path for GPU version
	if cfg.LibraryPath == "" {
		cfg.LibraryPath = os.Getenv("ONNXRUNTIME_LIB_PATH")
		if cfg.LibraryPath == "" {
			cfg.LibraryPath = "/usr/local/lib/libonnxruntime.so"
		}
	}

	// CUDA build defaults to GPU
	if cfg.Device == "" {
		cfg.Device = "cuda"
		cfg.UseCUDA = true
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

	// Note: Full implementation requires onnxruntime_go library with CUDA support.
	// Install with: go get github.com/yalue/onnxruntime_go
	// And download ONNX Runtime GPU shared library.
	//
	// When onnxruntime_go is available, this would:
	//   ort.SetSharedLibraryPath(cfg.LibraryPath)
	//   ort.InitializeEnvironment()
	//   opts, _ := ort.NewSessionOptions()
	//   cudaOpts, _ := ort.NewCUDAProviderOptions()
	//   cudaOpts.Update(map[string]string{"device_id": strconv.Itoa(cfg.CUDADeviceID)})
	//   opts.AppendExecutionProviderCUDA(cudaOpts)
	//   session, _ := ort.NewDynamicAdvancedSession(cfg.ModelPath, inputs, outputs, opts)

	return nil, fmt.Errorf("%w: ONNX CUDA requires onnxruntime GPU library; see onnx_cuda.go for setup instructions", ErrProviderUnavailable)
}

// NewOnnxFromEnv creates an ONNX embedder using environment variables.
func NewOnnxFromEnv(ctx context.Context, dim int) (*OnnxEmbedder, error) {
	modelPath := os.Getenv("ANDB_EMBEDDER_MODEL_PATH")
	libraryPath := os.Getenv("ONNXRUNTIME_LIB_PATH")
	device := os.Getenv("ANDB_EMBEDDER_DEVICE")
	if device == "" {
		device = "cuda" // Default to CUDA in cuda build
	}

	deviceID := 0
	if s := os.Getenv("CUDA_VISIBLE_DEVICES"); s != "" {
		if v, err := strconv.Atoi(s[:1]); err == nil {
			deviceID = v
		}
	}

	return NewOnnx(ctx, OnnxConfig{
		ModelPath:    modelPath,
		LibraryPath:  libraryPath,
		Device:       device,
		UseCUDA:      true,
		CUDADeviceID: deviceID,
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
