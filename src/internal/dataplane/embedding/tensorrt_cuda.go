//go:build cuda && tensorrt && linux
// +build cuda,tensorrt,linux

// Package embedding provides pluggable text-to-vector embedding generators.
//
// TensorRTEmbedder requires Linux + CUDA + TensorRT.
// Build with: go build -tags cuda ./...
//
// Prerequisites:
//
//	# Install CUDA Toolkit
//	# https://developer.nvidia.com/cuda-downloads
//
//	# Install TensorRT
//	# https://developer.nvidia.com/tensorrt
//
//	# Set environment variables
//	export LD_LIBRARY_PATH=/usr/local/cuda/lib64:/usr/local/TensorRT/lib:$LD_LIBRARY_PATH
//	export CGO_LDFLAGS="-L/usr/local/cuda/lib64 -L/usr/local/TensorRT/lib -lcudart -lnvinfer"
//	export CGO_CFLAGS="-I/usr/local/cuda/include -I/usr/local/TensorRT/include"
//
//	# Convert ONNX model to TensorRT engine
//	trtexec --onnx=model.onnx --saveEngine=model.trt --fp16
package embedding

/*
#cgo CFLAGS: -I${SRCDIR}/../../../../cpp -I/usr/local/cuda-12.9/include -I/usr/include/x86_64-linux-gnu
#cgo LDFLAGS: -L${SRCDIR}/../../../../cpp/build -landb_tensorrt -lcudart -lnvinfer -Wl,-rpath,${SRCDIR}/../../../../cpp/build

#include <stdlib.h>
#include <cuda_runtime.h>

// CUDA memory management helpers
static int cuda_malloc(void** ptr, size_t size) {
    return cudaMalloc(ptr, size);
}

static int cuda_free(void* ptr) {
    return cudaFree(ptr);
}

static int cuda_memcpy_h2d(void* dst, const void* src, size_t size) {
    return cudaMemcpy(dst, src, size, cudaMemcpyHostToDevice);
}

static int cuda_memcpy_d2h(void* dst, const void* src, size_t size) {
    return cudaMemcpy(dst, src, size, cudaMemcpyDeviceToHost);
}

static int cuda_set_device(int device) {
    return cudaSetDevice(device);
}

static int cuda_device_synchronize() {
    return cudaDeviceSynchronize();
}

// TensorRT engine management
// Real implementation provided by cpp/tensorrt_bridge.cpp
// Linked via libandb_tensorrt.so

typedef struct {
    void* runtime;
    void* engine;
    void* context;
    void* stream;
    int   numIOTensors;
} TRTEngine;

// Load TensorRT engine from file
// Implementation in cpp/tensorrt_bridge.cpp
extern TRTEngine* trt_load_engine(const char* engine_path);

// Execute TensorRT inference
// Implementation in cpp/tensorrt_bridge.cpp
extern int trt_execute_inference(TRTEngine* engine, void** bindings);

// Free TensorRT engine resources
// Implementation in cpp/tensorrt_bridge.cpp
extern void trt_free_engine(TRTEngine* engine);

// Set dynamic input shapes (batch_size × seq_len) before inference
// Implementation in cpp/tensorrt_bridge.cpp
extern int trt_set_input_shapes(TRTEngine* engine, int batch_size, int seq_len);

// Return the number of input tensors (used to build bindings at runtime)
// Implementation in cpp/tensorrt_bridge.cpp
extern int trt_get_num_inputs(TRTEngine* engine);
*/
import "C"

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"unsafe"
)

// TensorRTConfig holds configuration for the TensorRT embedder.
type TensorRTConfig struct {
	EnginePath    string // Path to .trt/.engine file (required)
	VocabPath     string // Path to BERT vocab.txt; empty = FNV hash fallback
	MaxBatchSize  int    // Maximum batch size for inference (default: 32)
	DeviceID      int    // CUDA device ID (default: 0)
	FP16          bool   // Use FP16 precision (default: true for speed)
	MaxSeqLength  int    // Maximum sequence length (default: 512)
	WorkspaceSize int64  // Maximum GPU memory for TensorRT workspace in bytes (default: 1GB)
}

// TensorRTEmbedder implements Generator using NVIDIA TensorRT for GPU inference.
type TensorRTEmbedder struct {
	cfg         TensorRTConfig
	dim         int
	tokenizer   *bertTokenizer
	numInputs   int // queried from engine at init: 2 for test engine, 3 for real BERT
	closedMu    sync.RWMutex
	inferenceMu sync.Mutex
	closed      bool

	// TensorRT engine handle
	engine unsafe.Pointer // *C.TRTEngine

	// GPU memory buffers
	inputIDsGPU      unsafe.Pointer
	attentionMaskGPU unsafe.Pointer
	tokenTypeIDsGPU  unsafe.Pointer // BERT segment IDs (always 0)
	outputGPU        unsafe.Pointer
}

// NewTensorRT creates a TensorRT embedder with CUDA GPU acceleration.
func NewTensorRT(_ context.Context, cfg TensorRTConfig, dim int) (*TensorRTEmbedder, error) {
	if dim <= 0 {
		return nil, fmt.Errorf("dim must be > 0, got %d", dim)
	}
	if cfg.EnginePath == "" {
		return nil, fmt.Errorf("TensorRTConfig.EnginePath is required")
	}
	if _, err := os.Stat(cfg.EnginePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("TensorRT engine not found: %s", cfg.EnginePath)
	}
	if cfg.MaxBatchSize == 0 {
		cfg.MaxBatchSize = 32
	}
	if cfg.MaxSeqLength == 0 {
		cfg.MaxSeqLength = 512
	}
	if cfg.WorkspaceSize == 0 {
		cfg.WorkspaceSize = 1 << 30 // 1GB
	}

	// Set CUDA device
	if ret := C.cuda_set_device(C.int(cfg.DeviceID)); ret != 0 {
		return nil, fmt.Errorf("failed to set CUDA device %d: error code %d", cfg.DeviceID, ret)
	}

	// Allocate GPU memory for input/output buffers
	inputSize := cfg.MaxBatchSize * cfg.MaxSeqLength * 4 // int32
	effDim := dim
	// Real BERT output is [batch, seq_len, dim]; allocate full 3D buffer.
	outputSize := cfg.MaxBatchSize * cfg.MaxSeqLength * effDim * 4 // float32

	var inputIDsGPU, attentionMaskGPU, outputGPU unsafe.Pointer

	if ret := C.cuda_malloc(&inputIDsGPU, C.size_t(inputSize)); ret != 0 {
		return nil, fmt.Errorf("failed to allocate GPU memory for input_ids: error code %d", ret)
	}

	if ret := C.cuda_malloc(&attentionMaskGPU, C.size_t(inputSize)); ret != 0 {
		C.cuda_free(inputIDsGPU)
		return nil, fmt.Errorf("failed to allocate GPU memory for attention_mask: error code %d", ret)
	}

	if ret := C.cuda_malloc(&outputGPU, C.size_t(outputSize)); ret != 0 {
		C.cuda_free(inputIDsGPU)
		C.cuda_free(attentionMaskGPU)
		return nil, fmt.Errorf("failed to allocate GPU memory for output: error code %d", ret)
	}

	var tokenTypeIDsGPU unsafe.Pointer
	if ret := C.cuda_malloc(&tokenTypeIDsGPU, C.size_t(inputSize)); ret != 0 {
		C.cuda_free(inputIDsGPU)
		C.cuda_free(attentionMaskGPU)
		C.cuda_free(outputGPU)
		return nil, fmt.Errorf("failed to allocate GPU memory for token_type_ids: error code %d", ret)
	}
	// token_type_ids are all 0 for single-sentence BERT — zero once at init
	zeros := make([]int32, cfg.MaxBatchSize*cfg.MaxSeqLength)
	C.cuda_memcpy_h2d(tokenTypeIDsGPU, unsafe.Pointer(&zeros[0]), C.size_t(inputSize))

	tok, err := newBertTokenizer(cfg.VocabPath)
	if err != nil {
		C.cuda_free(inputIDsGPU)
		C.cuda_free(attentionMaskGPU)
		C.cuda_free(outputGPU)
		return nil, fmt.Errorf("failed to load vocab: %w", err)
	}

	// Load TensorRT engine from the serialised .engine/.trt file.
	cEnginePath := C.CString(cfg.EnginePath)
	defer C.free(unsafe.Pointer(cEnginePath))
	trtEngine := C.trt_load_engine(cEnginePath)
	if trtEngine == nil {
		C.cuda_free(inputIDsGPU)
		C.cuda_free(attentionMaskGPU)
		C.cuda_free(outputGPU)
		return nil, fmt.Errorf("failed to load TensorRT engine: %s", cfg.EnginePath)
	}

	numInputs := int(C.trt_get_num_inputs(trtEngine))
	if numInputs <= 0 {
		numInputs = 2 // safe fallback
	}

	e := &TensorRTEmbedder{
		cfg:              cfg,
		dim:              dim,
		tokenizer:        tok,
		numInputs:        numInputs,
		engine:           unsafe.Pointer(trtEngine),
		inputIDsGPU:      inputIDsGPU,
		attentionMaskGPU: attentionMaskGPU,
		tokenTypeIDsGPU:  tokenTypeIDsGPU,
		outputGPU:        outputGPU,
	}

	return e, nil
}

// NewTensorRTFromEnv creates a TensorRT embedder from environment variables.
//
//	ANDB_EMBEDDER_MODEL_PATH  — path to the .engine/.trt file (required)
//	CUDA_VISIBLE_DEVICES       — first digit used as CUDA device ID
//	ANDB_ONNX_VOCAB_PATH       — BERT vocab.txt for WordPiece tokenization
func NewTensorRTFromEnv(ctx context.Context, dim int) (*TensorRTEmbedder, error) {
	deviceID := 0
	if devices := os.Getenv("CUDA_VISIBLE_DEVICES"); devices != "" {
		if v, err := strconv.Atoi(devices[:1]); err == nil {
			deviceID = v
		}
	}
	return NewTensorRT(ctx, TensorRTConfig{
		EnginePath: os.Getenv("ANDB_EMBEDDER_MODEL_PATH"),
		VocabPath:  os.Getenv(vocabPathEnv),
		DeviceID:   deviceID,
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

// BatchGenerate runs inference on multiple texts using TensorRT on GPU.
func (e *TensorRTEmbedder) BatchGenerate(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}
	if len(texts) > e.cfg.MaxBatchSize {
		// Align behavior with ONNX CPU provider: split oversized batches automatically.
		out := make([][]float32, 0, len(texts))
		for i := 0; i < len(texts); i += e.cfg.MaxBatchSize {
			j := i + e.cfg.MaxBatchSize
			if j > len(texts) {
				j = len(texts)
			}
			chunk, err := e.BatchGenerate(ctx, texts[i:j])
			if err != nil {
				return nil, err
			}
			out = append(out, chunk...)
		}
		return out, nil
	}

	// Tokenize and prepare input
	batchSize := len(texts)
	seqLen := e.cfg.MaxSeqLength
	inputIDsHost := make([]int32, batchSize*seqLen)
	attentionMaskHost := make([]int32, batchSize*seqLen)
	outputHost := make([]float32, batchSize*seqLen*e.dim)

	for i, text := range texts {
		ids64, mask64 := e.tokenizer.tokenize(text, seqLen)
		for j := 0; j < seqLen; j++ {
			idx := i*seqLen + j
			inputIDsHost[idx] = int32(ids64[j])
			attentionMaskHost[idx] = int32(mask64[j])
		}
	}

	e.inferenceMu.Lock()
	defer e.inferenceMu.Unlock()

	e.closedMu.RLock()
	closed := e.closed
	e.closedMu.RUnlock()
	if closed {
		return nil, fmt.Errorf("TensorRTEmbedder is closed")
	}

	// Copy input to GPU
	inputSize := C.size_t(batchSize * seqLen * 4)
	if ret := C.cuda_memcpy_h2d(e.inputIDsGPU, unsafe.Pointer(&inputIDsHost[0]), inputSize); ret != 0 {
		return nil, fmt.Errorf("failed to copy input_ids to GPU: error code %d", ret)
	}
	if ret := C.cuda_memcpy_h2d(e.attentionMaskGPU, unsafe.Pointer(&attentionMaskHost[0]), inputSize); ret != 0 {
		return nil, fmt.Errorf("failed to copy attention_mask to GPU: error code %d", ret)
	}

	// For dynamic-shape engines, set actual input dimensions before inference.
	if ret := C.trt_set_input_shapes((*C.TRTEngine)(e.engine), C.int(batchSize), C.int(seqLen)); ret != 0 {
		return nil, fmt.Errorf("TensorRT setInputShape failed for batch=%d seq=%d", batchSize, seqLen)
	}

	// Build bindings array: inputs first, then output.
	// numInputs is queried from the engine at init (2 or 3 for BERT models).
	var bindings [4]unsafe.Pointer
	switch e.numInputs {
	case 3: // real BERT: input_ids, attention_mask, token_type_ids, output
		bindings = [4]unsafe.Pointer{e.inputIDsGPU, e.attentionMaskGPU, e.tokenTypeIDsGPU, e.outputGPU}
	default: // 2-input (test engine or future 2-input models): input_ids, attention_mask, output
		bindings = [4]unsafe.Pointer{e.inputIDsGPU, e.attentionMaskGPU, e.outputGPU, nil}
	}
	if ret := C.trt_execute_inference((*C.TRTEngine)(e.engine), &bindings[0]); ret != 0 {
		return nil, fmt.Errorf("TensorRT inference failed: error code %d", ret)
	}

	// Copy output from GPU: layout is [batchSize, seqLen, dim]
	outputSize := C.size_t(batchSize * seqLen * e.dim * 4)
	if ret := C.cuda_memcpy_d2h(unsafe.Pointer(&outputHost[0]), e.outputGPU, outputSize); ret != 0 {
		return nil, fmt.Errorf("failed to copy output from GPU: error code %d", ret)
	}

	// Mean-pool over seq dimension (weighted by attention_mask) → [batchSize, dim]
	results := make([][]float32, batchSize)
	for i := 0; i < batchSize; i++ {
		emb := make([]float32, e.dim)
		var tokenCount float32
		for j := 0; j < seqLen; j++ {
			if attentionMaskHost[i*seqLen+j] == 0 {
				continue
			}
			tokenCount++
			base := (i*seqLen + j) * e.dim
			for d := 0; d < e.dim; d++ {
				emb[d] += outputHost[base+d]
			}
		}
		if tokenCount > 0 {
			for d := 0; d < e.dim; d++ {
				emb[d] /= tokenCount
			}
		}
		results[i] = emb
	}

	return results, nil
}

// Dim implements dataplane.EmbeddingGenerator.
func (e *TensorRTEmbedder) Dim() int { return e.dim }

// Reset is a no-op for TensorRT embedders.
func (e *TensorRTEmbedder) Reset() {}

// Provider implements Generator.
func (e *TensorRTEmbedder) Provider() string { return "tensorrt" }

// Close releases the TensorRT engine, CUDA memory, and runtime.
func (e *TensorRTEmbedder) Close() error {
	e.closedMu.Lock()
	if e.closed {
		e.closedMu.Unlock()
		return nil
	}
	e.closed = true
	e.closedMu.Unlock()

	// Wait for any in-flight GPU inference to complete before freeing resources.
	e.inferenceMu.Lock()
	defer e.inferenceMu.Unlock()

	// Free TensorRT engine
	if e.engine != nil {
		C.trt_free_engine((*C.TRTEngine)(e.engine))
		e.engine = nil
	}

	// Free GPU memory
	if e.inputIDsGPU != nil {
		C.cuda_free(e.inputIDsGPU)
		e.inputIDsGPU = nil
	}
	if e.attentionMaskGPU != nil {
		C.cuda_free(e.attentionMaskGPU)
		e.attentionMaskGPU = nil
	}
	if e.tokenTypeIDsGPU != nil {
		C.cuda_free(e.tokenTypeIDsGPU)
		e.tokenTypeIDsGPU = nil
	}
	if e.outputGPU != nil {
		C.cuda_free(e.outputGPU)
		e.outputGPU = nil
	}

	return nil
}
