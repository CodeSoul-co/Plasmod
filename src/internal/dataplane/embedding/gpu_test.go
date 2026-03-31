//go:build cuda
// +build cuda

package embedding

// ---------------------------------------------------------------------------
// TensorRT tests (require -tags cuda,tensorrt)
// ---------------------------------------------------------------------------
// Run:
//
//	TRT_ENGINE_PATH=/path/to/model.engine \
//	LD_LIBRARY_PATH=/path/to/tensorrt_libs \
//	go test -v -tags cuda,tensorrt -run TestTensorRT ./src/internal/dataplane/embedding/

import (
	"context"
	"os"
	"testing"
)

// ---------------------------------------------------------------------------
// ONNX CUDA tests
// ---------------------------------------------------------------------------
// Run:
//
//	ONNXRUNTIME_LIB_PATH=/path/to/libonnxruntime.so \
//	ONNX_MODEL_PATH=/path/to/model.onnx \
//	LD_LIBRARY_PATH=/path/to/onnxruntime/lib \
//	go test -v -tags cuda -run TestOnnxEmbedder ./src/internal/dataplane/embedding/

func TestOnnxEmbedder_CUDA_Generate(t *testing.T) {
	libPath := os.Getenv("ONNXRUNTIME_LIB_PATH")
	modelPath := os.Getenv("ONNX_MODEL_PATH")
	if libPath == "" || modelPath == "" {
		t.Skip("ONNXRUNTIME_LIB_PATH or ONNX_MODEL_PATH not set — skipping CUDA test")
	}

	ctx := context.Background()
	embedder, err := NewOnnx(ctx, OnnxConfig{
		LibraryPath:  libPath,
		ModelPath:    modelPath,
		Device:       "cuda",
		UseCUDA:      true,
		CUDADeviceID: 0,
		PoolingMode:  "mean",
		OutputName:   "last_hidden_state",
	}, 384)
	if err != nil {
		t.Fatalf("NewOnnx (CUDA) failed: %v", err)
	}
	defer embedder.Close()

	vec, err := embedder.Generate("hello world GPU test")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if len(vec) != embedder.Dim() {
		t.Fatalf("dim mismatch: got %d want %d", len(vec), embedder.Dim())
	}
	t.Logf("ONNX CUDA: dim=%d  vec[0:4]=%v", embedder.Dim(), vec[:4])
}

func TestOnnxEmbedder_CUDA_BatchGenerate(t *testing.T) {
	libPath := os.Getenv("ONNXRUNTIME_LIB_PATH")
	modelPath := os.Getenv("ONNX_MODEL_PATH")
	if libPath == "" || modelPath == "" {
		t.Skip("ONNXRUNTIME_LIB_PATH or ONNX_MODEL_PATH not set — skipping CUDA test")
	}

	ctx := context.Background()
	embedder, err := NewOnnx(ctx, OnnxConfig{
		LibraryPath:  libPath,
		ModelPath:    modelPath,
		Device:       "cuda",
		UseCUDA:      true,
		CUDADeviceID: 0,
		PoolingMode:  "mean",
		OutputName:   "last_hidden_state",
	}, 384)
	if err != nil {
		t.Fatalf("NewOnnx (CUDA) failed: %v", err)
	}
	defer embedder.Close()

	texts := []string{
		"first sentence for batching",
		"second sentence for batching",
		"third sentence for batching",
	}
	vecs, err := embedder.BatchGenerate(ctx, texts)
	if err != nil {
		t.Fatalf("BatchGenerate failed: %v", err)
	}
	if len(vecs) != len(texts) {
		t.Fatalf("batch size mismatch: got %d want %d", len(vecs), len(texts))
	}
	for i, v := range vecs {
		if len(v) != embedder.Dim() {
			t.Errorf("vecs[%d] dim mismatch: got %d want %d", i, len(v), embedder.Dim())
		}
	}
	t.Logf("ONNX CUDA BatchGenerate: %d texts → %d vecs, dim=%d", len(texts), len(vecs), embedder.Dim())
}

// ---------------------------------------------------------------------------
// GGUF CUDA tests
// ---------------------------------------------------------------------------
// Run:
//
//	GGUF_MODEL_PATH=/path/to/model.gguf \
//	go test -v -tags cuda -run TestGGUFEmbedder ./src/internal/dataplane/embedding/

func ggufModelPath(t *testing.T) string {
	t.Helper()
	p := os.Getenv("GGUF_MODEL_PATH")
	if p != "" {
		return p
	}
	for _, candidate := range []string{"/models/tinyllama.gguf", "/tmp/tinyllama.gguf"} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	t.Skip("GGUF_MODEL_PATH not set and no default model found — skipping GGUF CUDA test")
	return ""
}

// TestGGUFEmbedder_CUDA combines NotStub + Generate in one test to avoid
// reloading the model twice (old llama.cpp has issues with repeated load/free).
func TestGGUFEmbedder_CUDA(t *testing.T) {
	modelPath := ggufModelPath(t)

	ctx := context.Background()
	// dim=0: auto-probe from model hidden size
	embedder, err := NewGGUF(ctx, GGUFConfig{
		ModelPath: modelPath,
		Device:    "cuda",
		GPULayers: 99,
	}, 0)
	if err != nil {
		t.Fatalf("NewGGUF (CUDA) failed: %v", err)
	}
	defer embedder.Close()

	t.Logf("NewGGUF returned real instance (not stub): dim=%d", embedder.Dim())
	if embedder.Dim() == 0 {
		t.Fatal("Dim() is 0 — model did not report embedding dimension")
	}

	vec, err := embedder.Generate("hello GGUF CUDA test")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if len(vec) != embedder.Dim() {
		t.Fatalf("dim mismatch: got %d want %d", len(vec), embedder.Dim())
	}
	t.Logf("GGUF CUDA Generate: dim=%d  vec[0:4]=%v", embedder.Dim(), vec[:4])
}

// ---------------------------------------------------------------------------
// TensorRT tests
// ---------------------------------------------------------------------------

func TestTensorRT_EngineLoad(t *testing.T) {
	enginePath := os.Getenv("TRT_ENGINE_PATH")
	if enginePath == "" {
		t.Skip("TRT_ENGINE_PATH not set — skipping TensorRT test")
	}

	ctx := context.Background()
	embedder, err := NewTensorRT(ctx, TensorRTConfig{
		EnginePath:   enginePath,
		DeviceID:     0,
		MaxSeqLength: 128,
		MaxBatchSize: 4,
	}, 384)
	if err != nil {
		t.Fatalf("NewTensorRT failed: %v", err)
	}
	defer embedder.Close()

	t.Logf("TensorRT engine loaded OK: dim=%d", embedder.Dim())
}

func TestTensorRT_Inference(t *testing.T) {
	enginePath := os.Getenv("TRT_ENGINE_PATH")
	if enginePath == "" {
		t.Skip("TRT_ENGINE_PATH not set — skipping TensorRT test")
	}

	ctx := context.Background()
	embedder, err := NewTensorRT(ctx, TensorRTConfig{
		EnginePath:   enginePath,
		DeviceID:     0,
		MaxSeqLength: 128,
		MaxBatchSize: 4,
	}, 384)
	if err != nil {
		t.Fatalf("NewTensorRT failed: %v", err)
	}
	defer embedder.Close()

	vec, err := embedder.Generate("hello TensorRT inference test")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if len(vec) != embedder.Dim() {
		t.Fatalf("dim mismatch: got %d want %d", len(vec), embedder.Dim())
	}
	t.Logf("TensorRT inference: dim=%d  vec[0:4]=%v", embedder.Dim(), vec[:4])
}
