//go:build !cuda
// +build !cuda

// Package embedding provides pluggable text-to-vector embedding generators.
//
// OnnxEmbedder CPU implementation using onnxruntime_go.
// Build with: go build -tags cuda (on Linux with CUDA) to enable CUDA acceleration.
//
// Prerequisites:
//
//	# Install onnxruntime_go (use v1.9.0 for onnxruntime 1.17.x)
//	go get github.com/yalue/onnxruntime_go@v1.9.0
//
//	# Download ONNX Runtime from https://github.com/microsoft/onnxruntime/releases
//	# For Mac (ARM64):
//	wget https://github.com/microsoft/onnxruntime/releases/download/v1.17.0/onnxruntime-osx-arm64-1.17.0.tgz
//	tar xzf onnxruntime-osx-arm64-1.17.0.tgz
//	export ONNXRUNTIME_LIB_PATH=/path/to/onnxruntime-osx-arm64-1.17.0/lib/libonnxruntime.dylib
//
//	# For Linux (x64):
//	wget https://github.com/microsoft/onnxruntime/releases/download/v1.17.0/onnxruntime-linux-x64-1.17.0.tgz
//	export ONNXRUNTIME_LIB_PATH=/path/to/onnxruntime-linux-x64-1.17.0/lib/libonnxruntime.so
//
//	# Download ONNX embedding model (e.g. all-MiniLM-L6-v2)
//	wget https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/onnx/model.onnx
package embedding

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

// ortInitialized tracks global ONNX Runtime initialization state
var ortInitialized bool
var ortInitMu sync.Mutex

// OnnxConfig holds configuration for the ONNX Runtime embedder.
type OnnxConfig struct {
	ModelPath    string   // Path to .onnx model file (required)
	LibraryPath  string   // Path to onnxruntime shared library (auto-detect if empty)
	VocabPath    string   // Path to BERT vocab.txt; empty = FNV hash fallback
	Device       string   // "cpu" (default)
	MaxBatchSize int      // Maximum batch size for inference (default: 32)
	MaxSeqLength int      // Maximum sequence length (default: 128)
	InputNames   []string // ONNX input tensor names (default: ["input_ids", "attention_mask", "token_type_ids"])
	OutputName   string   // ONNX output tensor name (default: "last_hidden_state")
	PoolingMode  string   // "cls" or "mean" (default: "mean")
}

// OnnxEmbedder implements Generator using ONNX Runtime for local inference.
type OnnxEmbedder struct {
	cfg       OnnxConfig
	dim       int
	session   *ort.AdvancedSession
	tokenizer *bertTokenizer
	mu        sync.Mutex
	closed    bool

	// Pre-allocated tensors for reuse
	inputIDs   *ort.Tensor[int64]
	attnMask   *ort.Tensor[int64]
	tokenTypes *ort.Tensor[int64]
	output     *ort.Tensor[float32]
}

// initOnnxRuntime initializes the ONNX Runtime environment (once globally)
func initOnnxRuntime(libraryPath string) error {
	ortInitMu.Lock()
	defer ortInitMu.Unlock()

	if ortInitialized {
		return nil
	}

	ort.SetSharedLibraryPath(libraryPath)
	if err := ort.InitializeEnvironment(); err != nil {
		return fmt.Errorf("failed to initialize ONNX Runtime: %w", err)
	}
	ortInitialized = true
	return nil
}

// NewOnnx creates an ONNX Runtime embedder (CPU version).
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

	// Check library exists
	if _, err := os.Stat(cfg.LibraryPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("%w: onnxruntime library not found at %s", ErrProviderUnavailable, cfg.LibraryPath)
	}

	// Defaults
	if cfg.MaxBatchSize == 0 {
		cfg.MaxBatchSize = 32
	}
	if cfg.MaxSeqLength == 0 {
		cfg.MaxSeqLength = 128
	}
	if len(cfg.InputNames) == 0 {
		cfg.InputNames = []string{"input_ids", "attention_mask", "token_type_ids"}
	}
	if cfg.OutputName == "" {
		cfg.OutputName = "last_hidden_state"
	}
	if cfg.PoolingMode == "" {
		cfg.PoolingMode = "mean"
	}
	if dim == 0 {
		if envDim := os.Getenv("PLASMOD_EMBEDDER_DIM"); envDim != "" {
			if n, err := strconv.Atoi(envDim); err == nil && n > 0 {
				dim = n
			}
		}
	}
	if dim == 0 {
		dim = 384 // Default for all-MiniLM-L6-v2
	}

	// Initialize ONNX Runtime
	if err := initOnnxRuntime(cfg.LibraryPath); err != nil {
		return nil, err
	}

	// Create input/output tensors
	seqLen := int64(cfg.MaxSeqLength)
	inputShape := ort.NewShape(1, seqLen)
	outputShape := ort.NewShape(1, seqLen, int64(dim))

	inputIDs, err := ort.NewTensor(inputShape, make([]int64, seqLen))
	if err != nil {
		return nil, fmt.Errorf("failed to create input_ids tensor: %w", err)
	}

	attnMask, err := ort.NewTensor(inputShape, make([]int64, seqLen))
	if err != nil {
		inputIDs.Destroy()
		return nil, fmt.Errorf("failed to create attention_mask tensor: %w", err)
	}

	tokenTypes, err := ort.NewTensor(inputShape, make([]int64, seqLen))
	if err != nil {
		inputIDs.Destroy()
		attnMask.Destroy()
		return nil, fmt.Errorf("failed to create token_type_ids tensor: %w", err)
	}

	output, err := ort.NewEmptyTensor[float32](outputShape)
	if err != nil {
		inputIDs.Destroy()
		attnMask.Destroy()
		tokenTypes.Destroy()
		return nil, fmt.Errorf("failed to create output tensor: %w", err)
	}

	// Create session
	session, err := ort.NewAdvancedSession(
		cfg.ModelPath,
		cfg.InputNames,
		[]string{cfg.OutputName},
		[]ort.ArbitraryTensor{inputIDs, attnMask, tokenTypes},
		[]ort.ArbitraryTensor{output},
		nil,
	)
	if err != nil {
		inputIDs.Destroy()
		attnMask.Destroy()
		tokenTypes.Destroy()
		output.Destroy()
		return nil, fmt.Errorf("failed to create ONNX session: %w", err)
	}

	tok, err := newBertTokenizer(cfg.VocabPath)
	if err != nil {
		inputIDs.Destroy()
		attnMask.Destroy()
		tokenTypes.Destroy()
		output.Destroy()
		return nil, fmt.Errorf("failed to load vocab: %w", err)
	}

	return &OnnxEmbedder{
		cfg:        cfg,
		dim:        dim,
		session:    session,
		tokenizer:  tok,
		inputIDs:   inputIDs,
		attnMask:   attnMask,
		tokenTypes: tokenTypes,
		output:     output,
	}, nil
}

// NewOnnxFromEnv creates an ONNX (CPU) embedder from environment variables.
//
//	PLASMOD_EMBEDDER_MODEL_PATH  — path to the .onnx model file (required)
//	ONNXRUNTIME_LIB_PATH      — path to libonnxruntime (auto-detect if unset)
//	PLASMOD_ONNX_VOCAB_PATH       — BERT vocab.txt for WordPiece tokenization
func NewOnnxFromEnv(ctx context.Context, dim int) (*OnnxEmbedder, error) {
	return NewOnnx(ctx, OnnxConfig{
		ModelPath:   os.Getenv("PLASMOD_EMBEDDER_MODEL_PATH"),
		LibraryPath: os.Getenv("ONNXRUNTIME_LIB_PATH"),
		VocabPath:   os.Getenv(vocabPathEnv),
	}, dim)
}

// Generate implements dataplane.EmbeddingGenerator.
func (e *OnnxEmbedder) Generate(text string) ([]float32, error) {
	vecs, err := e.BatchGenerate(context.Background(), []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("ONNX inference returned empty result")
	}
	return vecs[0], nil
}

// BatchGenerate runs inference on multiple texts.
func (e *OnnxEmbedder) BatchGenerate(ctx context.Context, texts []string) ([][]float32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil, fmt.Errorf("OnnxEmbedder is closed")
	}

	results := make([][]float32, len(texts))

	for i, text := range texts {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Tokenize using BERT WordPiece (or FNV fallback when no vocab file)
		ids, mask := e.tokenizer.tokenize(text, e.cfg.MaxSeqLength)

		// Copy to tensors
		copy(e.inputIDs.GetData(), ids)
		copy(e.attnMask.GetData(), mask)
		// token_type_ids stays zero

		// Run inference
		if err := e.session.Run(); err != nil {
			return nil, fmt.Errorf("ONNX inference failed: %w", err)
		}

		// Extract embedding with pooling
		outputData := e.output.GetData()
		embedding := make([]float32, e.dim)

		if e.cfg.PoolingMode == "cls" {
			// Use [CLS] token embedding (first token)
			copy(embedding, outputData[:e.dim])
		} else {
			// Mean pooling over valid tokens
			validTokens := 0
			for j := 0; j < e.cfg.MaxSeqLength; j++ {
				if mask[j] == 1 {
					validTokens++
					for k := 0; k < e.dim; k++ {
						embedding[k] += outputData[j*e.dim+k]
					}
				}
			}
			if validTokens > 0 {
				for k := 0; k < e.dim; k++ {
					embedding[k] /= float32(validTokens)
				}
			}
		}

		results[i] = embedding
	}

	return results, nil
}

// Dim implements dataplane.EmbeddingGenerator.
func (e *OnnxEmbedder) Dim() int { return e.dim }

// Reset is a no-op for ONNX embedders.
func (e *OnnxEmbedder) Reset() {}

// Provider implements Generator.
func (e *OnnxEmbedder) Provider() string { return "onnx" }

// Close releases the ONNX Runtime session.
func (e *OnnxEmbedder) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil
	}
	e.closed = true

	if e.session != nil {
		e.session.Destroy()
	}
	if e.inputIDs != nil {
		e.inputIDs.Destroy()
	}
	if e.attnMask != nil {
		e.attnMask.Destroy()
	}
	if e.tokenTypes != nil {
		e.tokenTypes.Destroy()
	}
	if e.output != nil {
		e.output.Destroy()
	}

	return nil
}
