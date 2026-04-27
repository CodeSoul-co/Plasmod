# Member B 任务完成状态审计报告

**审计日期**: 2026-03-30  
**审计范围**: GPU/CUDA 加速、Embedding Provider 实现、构建脚本

---

## 📊 总体状态

| 模块 | 状态 | 完成度 |
|------|------|--------|
| **ONNX CPU** | ✅ 完全实现 | 100% |
| **ONNX CUDA** | 🔴 Stub | 0% |
| **GGUF CPU/Metal** | ✅ 完全实现 | 100% |
| **GGUF CUDA** | 🔴 Stub | 0% |
| **TensorRT CUDA** | 🟡 部分实现 | 60% |
| **BatchGenerate** | ✅ 接口已实现 | 100% |
| **build_cpp.sh** | ✅ 已创建 | 100% |
| **build_embeddings.sh** | 🔴 缺失 | 0% |

---

## ✅ 已完成的部分

### 1. **ONNX CPU 实现** (`onnx_cpu.go`)
**状态**: ✅ 完全实现，可直接使用

**功能**:
- ✅ 使用 `onnxruntime_go` 库
- ✅ 支持 CPU 推理
- ✅ 实现了 `Generate()` 和 `BatchGenerate()`
- ✅ 支持 mean pooling 和 CLS token pooling
- ✅ 会话管理和资源清理
- ✅ Build tag: `//go:build !cuda`

**代码质量**: 生产就绪

---

### 2. **GGUF CPU/Metal 实现** (`gguf_cpu.go`)
**状态**: ✅ 完全实现，可直接使用

**功能**:
- ✅ 使用 `go-llama.cpp` 库
- ✅ 支持 CPU 和 Metal GPU (macOS)
- ✅ 实现了 `Generate()` 和 `BatchGenerate()`
- ✅ GPU 层卸载配置 (`GPULayers`)
- ✅ 模型加载和资源管理
- ✅ Build tag: `//go:build !cuda`

**代码质量**: 生产就绪

---

### 3. **TensorRT CUDA 部分实现** (`tensorrt_cuda.go`)
**状态**: 🟡 60% 完成

**已实现**:
- ✅ CUDA 内存管理 (malloc/free/memcpy)
- ✅ GPU 缓冲区分配
- ✅ 输入数据 Host→Device 传输
- ✅ 输出数据 Device→Host 传输
- ✅ `BatchGenerate()` 框架完整
- ✅ 简单 tokenizer
- ✅ Build tag: `//go:build cuda && linux`

**缺失部分**:
- ❌ TensorRT 引擎加载 (`.trt` 文件反序列化)
- ❌ `IRuntime` / `ICudaEngine` / `IExecutionContext` 绑定
- ❌ 实际推理执行 (`context->enqueueV2()`)

**注释位置**: 第 232-233 行
```go
// TensorRT inference execution would happen here:
// context->enqueueV2(bindings, stream, nullptr)
```

---

### 4. **BatchGenerate 接口**
**状态**: ✅ 完全实现

**已实现的提供者**:
- ✅ `onnx_cpu.go` - 完整批量推理
- ✅ `gguf_cpu.go` - 完整批量推理  
- ✅ `tensorrt_cuda.go` - 框架完整（缺引擎加载）
- ✅ `embedding.go` (HTTPEmbedder) - 远程 API 批量调用
- ✅ `huggingface.go` - 批量调用
- ✅ `vertexai.go` - 批量调用

**集成点**:
- ✅ `vectorstore.go:123-165` - 自动检测并调用 `BatchGenerate`
- ✅ `segment_adapter.go:102-127` - `BatchIngest` 实现
- ✅ `tiered_adapter.go:129-144` - 分层批量写入

---

### 5. **构建脚本**
**状态**: 🟡 部分完成

**已创建**:
- ✅ `scripts/build_cpp.sh` - 构建 `libplasmod_retrieval.so`
  - 支持 `ANDB_WITH_GPU=ON` 启用 CUDA
  - 支持自定义 CUDA 架构
  - 自动检测 `nvcc`
  - 完整的错误处理

**缺失**:
- ❌ `scripts/build_embeddings.sh` - 构建 `go-llama.cpp` CUDA 版本

---

## 🔴 未完成的部分（需要修复）

### 1. **ONNX CUDA 实现** (`onnx_cuda.go`)
**当前状态**: 完全是 stub，所有方法返回 `ErrProviderUnavailable`

**需要实现**:
```go
// 第 56-102 行需要替换为真实实现
func NewOnnx(ctx context.Context, cfg OnnxConfig, dim int) (*OnnxEmbedder, error) {
    // 当前: return nil, ErrProviderUnavailable
    // 需要: 
    // 1. 初始化 ONNX Runtime with CUDA provider
    // 2. ort.NewCUDAProviderOptions()
    // 3. opts.AppendExecutionProviderCUDA(cudaOpts)
    // 4. 创建 session
}

func (e *OnnxEmbedder) Generate(text string) ([]float32, error) {
    // 当前: return nil, ErrProviderUnavailable
    // 需要: 调用 BatchGenerate
}

func (e *OnnxEmbedder) BatchGenerate(ctx context.Context, texts []string) ([][]float32, error) {
    // 当前: return nil, ErrProviderUnavailable
    // 需要: 实现真实的 CUDA 推理逻辑（参考 onnx_cpu.go）
}
```

**参考实现**: `onnx_cpu.go:86-308` 可以直接复用大部分逻辑，只需修改：
- CUDA provider 初始化
- 去掉 CPU 特定的配置

---

### 2. **GGUF CUDA 实现** (`gguf_cuda.go`)
**当前状态**: 完全是 stub，所有方法返回 `ErrProviderUnavailable`

**需要实现**:
```go
// 第 58-98 行需要替换为真实实现
func NewGGUF(ctx context.Context, cfg GGUFConfig, dim int) (*GGUFEmbedder, error) {
    // 当前: return nil, ErrProviderUnavailable
    // 需要:
    // 1. 加载 go-llama.cpp CUDA 版本
    // 2. llama.SetGPULayers(cfg.GPULayers)
    // 3. 创建模型实例
}

func (e *GGUFEmbedder) Generate(text string) ([]float32, error) {
    // 当前: return nil, ErrProviderUnavailable
    // 需要: 调用 model.Embeddings()
}

func (e *GGUFEmbedder) BatchGenerate(ctx context.Context, texts []string) ([][]float32, error) {
    // 当前: return nil, ErrProviderUnavailable
    // 需要: 循环调用 model.Embeddings()
}
```

**参考实现**: `gguf_cpu.go:64-225` 可以直接复用，只需修改：
- 默认 `GPULayers = 99` (CUDA 全卸载)
- 去掉 Metal 特定的配置

---

### 3. **TensorRT 引擎加载**
**当前状态**: CUDA 内存管理完成，缺少引擎加载和推理执行

**需要添加** (在 `tensorrt_cuda.go` 第 155-159 行之后):

```cpp
// C++ 桥接代码需要添加到 CGO 部分
/*
#include <NvInfer.h>

// 全局变量
static nvinfer1::IRuntime* runtime = nullptr;
static nvinfer1::ICudaEngine* engine = nullptr;
static nvinfer1::IExecutionContext* context = nullptr;

// 加载引擎
int load_trt_engine(const char* engine_path) {
    runtime = nvinfer1::createInferRuntime(gLogger);
    std::ifstream file(engine_path, std::ios::binary);
    // ... 反序列化引擎
    engine = runtime->deserializeCudaEngine(engineData, engineSize);
    context = engine->createExecutionContext();
    return 0;
}

// 执行推理
int run_inference(void** bindings, cudaStream_t stream) {
    return context->enqueueV2(bindings, stream, nullptr) ? 0 : 1;
}
*/
```

**Go 代码修改** (第 232-233 行):
```go
// 替换注释为实际调用
if ret := C.run_inference(bindings, stream); ret != 0 {
    return nil, fmt.Errorf("TensorRT inference failed: error code %d", ret)
}
```

---

### 4. **build_embeddings.sh 脚本**
**当前状态**: 不存在

**需要创建**: `scripts/build_embeddings.sh`

```bash
#!/bin/bash
# 构建 go-llama.cpp CUDA 版本

set -e

LLAMA_DIR="${LLAMA_DIR:-/tmp/go-llama-cpp}"
CUDA_PATH="${CUDA_PATH:-/usr/local/cuda}"

echo "Building go-llama.cpp with CUDA support..."
echo "Target directory: $LLAMA_DIR"

# 检查 CUDA
if ! command -v nvcc &> /dev/null; then
    echo "ERROR: nvcc not found. CUDA Toolkit is required."
    exit 1
fi

# 克隆仓库
if [ ! -d "$LLAMA_DIR" ]; then
    git clone --recurse-submodules https://github.com/go-skynet/go-llama.cpp "$LLAMA_DIR"
fi

cd "$LLAMA_DIR"

# 构建 CUDA 版本
BUILD_TYPE=cublas make libbinding.a

echo "✓ go-llama.cpp CUDA build completed"
echo "Output: $LLAMA_DIR/libbinding.a"
echo ""
echo "To use in Go:"
echo "  export LIBRARY_PATH=$LLAMA_DIR"
echo "  export C_INCLUDE_PATH=$LLAMA_DIR"
echo "  export CGO_LDFLAGS=\"-lcublas -lcudart -L$CUDA_PATH/lib64\""
echo "  go build -tags cuda ./..."
```

---

## 📋 README 验证清单对照

### Member B 验证清单 (README:850-861)

```
[ ] ONNX CUDA: go test -tags cuda ./src/internal/dataplane/embedding/ -run TestOnnxEmbedder passes
    ❌ 未实现 - onnx_cuda.go 是 stub

[ ] GGUF CUDA: NewGGUF returns non-stub instance inside Docker + NVIDIA GPU
    ❌ 未实现 - gguf_cuda.go 是 stub

[ ] GGUF CUDA: Generate produces correct-dimension embeddings
    ❌ 未实现 - 依赖上一项

[ ] TensorRT: engine loads without error, inference produces output
    🟡 部分完成 - CUDA 内存管理完成，缺引擎加载

[ ] retrievalplane: libplasmod_retrieval.so builds on Linux (make -C cpp)
    ✅ 可以验证 - build_cpp.sh 已创建

[ ] retrievalplane: Search works inside Docker with HNSW index
    ⏳ 待验证 - 需要 Linux 环境

[ ] BatchGenerate: TieredDataPlane.Ingest calls batch embedder, not N x single
    ✅ 已实现 - vectorstore.go:123-165 自动检测

[ ] Linux build: go build -tags cuda,retrieval ./src/internal/... compiles cleanly
    ⏳ 待验证 - 需要 Linux + CUDA 环境

[ ] ONNX CPU: TestOnnxEmbedder_CPU passes (regression test)
    ✅ 可以验证 - onnx_cpu.go 完整实现

[ ] All embedding provider tests: go test ./src/internal/dataplane/embedding/ passes (CPU mode)
    ✅ 可以验证 - CPU 版本都已实现
```

---

## 🎯 修复优先级

### 高优先级（阻塞 Linux 部署）
1. **创建 `build_embeddings.sh`** - 5 分钟
2. **实现 GGUF CUDA** - 30 分钟（直接复制 gguf_cpu.go 并修改）
3. **实现 ONNX CUDA** - 30 分钟（直接复制 onnx_cpu.go 并修改）

### 中优先级（可选优化）
4. **完成 TensorRT 引擎加载** - 2 小时（需要 C++ 桥接）

### 低优先级（已有 workaround）
5. TensorRT 可以暂时跳过，使用 ONNX CUDA 或 GGUF CUDA 替代

---

## 💡 修复策略

### ONNX CUDA 修复方案
```bash
# 步骤 1: 复制 CPU 版本
cp src/internal/dataplane/embedding/onnx_cpu.go src/internal/dataplane/embedding/onnx_cuda_impl.go

# 步骤 2: 修改 build tag
# 将 //go:build !cuda 改为 //go:build cuda

# 步骤 3: 添加 CUDA provider 初始化
# 在 NewOnnx() 中添加:
#   cudaOpts, _ := ort.NewCUDAProviderOptions()
#   cudaOpts.Update(map[string]string{"device_id": "0"})
#   opts.AppendExecutionProviderCUDA(cudaOpts)

# 步骤 4: 删除旧的 stub 文件
rm src/internal/dataplane/embedding/onnx_cuda.go
mv src/internal/dataplane/embedding/onnx_cuda_impl.go src/internal/dataplane/embedding/onnx_cuda.go
```

### GGUF CUDA 修复方案
```bash
# 步骤 1: 复制 CPU 版本
cp src/internal/dataplane/embedding/gguf_cpu.go src/internal/dataplane/embedding/gguf_cuda_impl.go

# 步骤 2: 修改 build tag
# 将 //go:build !cuda 改为 //go:build cuda

# 步骤 3: 修改默认配置
# 将 Device 默认值从 "cpu"/"metal" 改为 "cuda"
# 将 GPULayers 默认值改为 99

# 步骤 4: 删除旧的 stub 文件
rm src/internal/dataplane/embedding/gguf_cuda.go
mv src/internal/dataplane/embedding/gguf_cuda_impl.go src/internal/dataplane/embedding/gguf_cuda.go
```

---

## 📝 结论

**已完成的工作量**: 约 70%
- ✅ CPU 版本完全实现
- ✅ 批量推理接口完整
- ✅ C++ 构建脚本完成
- ✅ TensorRT 内存管理完成

**剩余工作量**: 约 30%
- 🔴 ONNX CUDA stub → 实现 (30 分钟)
- 🔴 GGUF CUDA stub → 实现 (30 分钟)
- 🔴 build_embeddings.sh 缺失 (5 分钟)
- 🟡 TensorRT 引擎加载 (2 小时，可选)

**建议行动**:
1. 先创建 `build_embeddings.sh` (最快)
2. 实现 GGUF CUDA (复制粘贴 + 小修改)
3. 实现 ONNX CUDA (复制粘贴 + 小修改)
4. TensorRT 可以标记为 "部分实现"，在 README 中说明

**总耗时估计**: 1-2 小时可完成所有高优先级任务
