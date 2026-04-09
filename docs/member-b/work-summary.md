# 成员B 工作总览

> Branch: `feature/retrieval-b`  
> 负责范围: 检索层全栈（C++ ANN 引擎 → CGO 桥接 → Go 检索引擎 → Runtime 集成） + Embedding Provider（CPU/GPU 加速）  
> 最后更新: 2026-04-08

---

## 一、职责范围总览

成员B 负责 CogDB **检索平面（Retrieval Plane）** 的全栈实现，以及 **Embedding 向量化加速层**，具体包括：

| 层级 | 代码位置 | 简述 |
|------|---------|------|
| **C++ ANN 检索引擎** | `cpp/retrieval/` + `cpp/include/` | Dense/Sparse/Segment 索引，Knowhere/HNSW，CUDA RAFT 可选 |
| **C++ TensorRT Bridge** | `cpp/tensorrt_bridge.cpp` + `cpp/create_test_engine.cpp` | TensorRT 10.x C bridge，供 Go CGO 调用 |
| **CGO Bridge** | `src/internal/dataplane/retrievalplane/` | Go ↔ C++ 桥接（`libandb_retrieval.so`） |
| **Embedding Provider** | `src/internal/dataplane/embedding/` | ONNX/GGUF/TensorRT/HuggingFace/VertexAI 向量化 |
| **Go 检索引擎** | `src/internal/retrieval/` | RRF 重排、安全过滤、候选种子标记 |
| **Runtime 集成** | `src/internal/worker/runtime.go` | 将 SeedIDs 传入 QueryChain 驱动 graph 扩展 |
| **Bootstrap** | `src/internal/app/bootstrap.go` | ONNX/TensorRT/HuggingFace/VertexAI provider 初始化 |
| **构建脚本 & Docker** | `scripts/build_cpp.sh` 等 + `scripts/docker/Dockerfile.memberb` | Linux GPU 编译与镜像 |

---

## 二、C++ 检索引擎层

### 2.1 `cpp/retrieval/` — ANN 索引实现

| 文件 | 作用 |
|------|------|
| `dense.cpp` | Dense 向量检索（CPU: HNSW via hnswlib；GPU: NVIDIA RAFT CAGRA，运行时自动 fallback） |
| `sparse.cpp` | Sparse 倒排检索：真正的 Posting List 索引（O(Q×L̄) 查询，TF 归一化，Serialize/Deserialize/Load/Save 完整实现） |
| `segment_index.cpp` | SegmentIndexManager：per-segment Knowhere HNSW 索引（IP metric，M=16，efConstruction=256） |
| `retrieval.cpp` | 顶层 C API 入口（`andb_retriever_*` flat index path；`andb_segment_*` segment path；`Version()`） |

设计原则：RRF 重排、Safety Filter、Seed 标记等业务逻辑**全部在 Go 层**（`src/internal/retrieval/`），C++ 层只负责纯 ANN 索引操作。

### 2.2 `cpp/include/` — C 头文件（Go CGO 调用接口）

| 文件 | 作用 |
|------|------|
| `include/andb_c_api.h` | 对外 C API 总入口（`extern "C"` 声明） |
| `include/retrieval.h` | `andb_retriever_*` / `andb_segment_*` 函数声明 |
| `include/dense.h` | DenseRetriever 类声明 |
| `include/sparse.h` | SparseRetriever 类声明 |
| `include/segment_index.h` | SegmentIndexManager 类声明 |
| `include/types.h` | 共用类型定义（`AndbSearchResult`, `AndbVector` 等） |

### 2.3 `cpp/tensorrt_bridge.cpp` — TensorRT C Bridge

TensorRT 10.x API 适配，供 `tensorrt_cuda.go` CGO 调用：

| 函数 | 作用 |
|------|------|
| `trt_load_engine(path)` | 加载 `.engine`/`.trt` 文件，返回 `TRTEngine*` |
| `trt_execute_inference(engine, bindings)` | 执行推理（使用 `enqueueV3` TRT 10.x API） |
| `trt_set_input_shapes(engine, batch, seq)` | 动态 shape engine 推理前设置输入尺寸 |
| `trt_get_num_inputs(engine)` | 返回 engine 输入张量数（2-input test engine vs 3-input BERT） |
| `trt_free_engine(engine)` | 释放 engine 资源（用 `delete` 替代旧 `destroy()`） |

### 2.4 `cpp/create_test_engine.cpp` — 测试用 Engine 生成器

离线生成一个 2-input 合成 BERT engine，用于不依赖真实模型的 CI 测试。

### 2.5 `cpp/vendor/` — Knowhere/HNSW 第三方库（含已修复的编译问题）

| 文件 | 修复内容 |
|------|--------|
| `cpp/vendor/CMakeLists.txt` | SIMD flags；x86_64 上排除 ARM NEON；LAPACK 链接；OpenMP Linux fix |
| `cpp/vendor/compat/omp.h` | `#include_next <omp.h>` on Linux |
| `cpp/vendor/include/knowhere/log.h` | 添加 `#include <cstring>` |
| `cpp/vendor/src/index/hnsw/hnsw.cc` | 修复 `rows < 10` 时除以零的 SIGFPE |

### 2.6 `cpp/CMakeLists.txt` — 统一编译入口

- 编译 `libandb_retrieval.so`（可选 `ANDB_WITH_GPU`）
- 编译 `libandb_tensorrt.so`（可选 `ANDB_WITH_TENSORRT`）
- 两者解耦，TensorRT build 不依赖 RAFT/GPU retrieval build

---

## 三、CGO Bridge 层

### `src/internal/dataplane/retrievalplane/`

| 文件 | 用途 | 构建标签 |
|------|------|---------|
| `contracts.go` | `SegmentRetriever` 接口定义 | — |
| `bridge.go` | CGO 实现，链接 `libandb_retrieval.so` | `retrieval` |
| `bridge_stub.go` | 非 retrieval 构建时的完整 stub | `!retrieval` |
| `integration_test.go` | bridge 集成测试（6 个测试函数） | `retrieval` |

---

## 四、Embedding Provider 层

### `src/internal/dataplane/embedding/`

| 文件 | 用途 | 构建标签 |
|------|------|---------|
| `embedding.go` | `Generator` / `BatchEmbeddingGenerator` 接口 | — |
| `onnx_cpu.go` | ONNX Runtime CPU 推理（`onnxruntime_go`） | `!cuda` |
| `onnx_cuda.go` | ONNX Runtime CUDA 推理 + mean-pooling | `cuda` |
| `onnx_tokenizer.go` | BERT WordPiece tokenizer（vocab.txt + FNV 回退） | — |
| `gguf_cpu.go` | llama.cpp CPU 推理（`go-llama.cpp`） | `gguf` |
| `gguf_cuda.go` | llama.cpp CUBLAS CUDA 推理 | `gguf,cuda` |
| `tensorrt_cuda.go` | TensorRT 10.x GPU 推理（CGO → `libandb_tensorrt`） | `cuda,tensorrt,linux` |
| `tensorrt_stub.go` | 非 CUDA/TensorRT 平台 stub | `!cuda \|\| !tensorrt \|\| !linux` |
| `huggingface.go` | HuggingFace Inference API（HTTP） | — |
| `vertexai.go` | Google VertexAI Embeddings API（HTTP） | — |
| `pool.go` | HTTP client 连接池 | — |

#### 各 Provider 关键设计点

**ONNX CPU/CUDA**
- `onnxruntime_go` 封装 ONNX Runtime C API；预分配 `Tensor[int64]` 复用
- tokenize 使用 `onnx_tokenizer.go` BERT WordPiece（已修复旧的字符哈希假 tokenizer）

**GGUF CPU/CUDA**
- `go-skynet/go-llama.cpp` binding；CUDA 需 `LLAMA_CUBLAS=ON` 编译
- `NewGGUF(dim=0)` 自动 probe 输出维度

**TensorRT**
- 动态 shape 支持（`trt_set_input_shapes` 每次推理前调用）
- 支持 2-input（测试 engine）和 3-input（BERT：+token_type_ids）engine
- Mean-pool `[batch, seq_len, dim]` 输出，加权 attention_mask
- **Pass 10 修复**：`dim<=0` guard；`closedMu`+`inferenceMu` 拆分全局锁；tokenize 在锁外；超大 batch 自动切片

**BERT WordPiece Tokenizer** (`onnx_tokenizer.go`)
- 标准化流程：lowercase → NFD → strip Mn → 控制字符清理 → CJK/标点加空格 → 空白折叠
- **Pass 10 修复**：`wordPieceSplit` 加 `bertMaxSubwords=200` 上限，防止 O(n²) 热路径

---

## 五、Go 检索引擎层

### `src/internal/retrieval/`

| 文件 | 作用 |
|------|------|
| `candidate.go` | `Candidate`、`CandidateList`、`SeedIDs []string` 结构体 |
| `retriever.go` | `Retriever` 核心：RRF × 重要性 × 新鲜度 × 置信度；SafetyFilter；seed 标记 |
| `filter.go` | `SafetyFilter` 7 条规则（TTL、quarantine、min version 等） |
| `proto/retrieval.proto` | 检索请求/响应 Protobuf 定义（与 Python 服务协议对齐） |

#### 算法流程（`Retriever.EnrichAndRank`）

```
TieredDataPlane.Search(query)
    → raw ObjectIDs (lexical + vector via CGO)
    → ObjectStore.GetMemory()  ← 加载 importance / timestamp / confidence
    → SafetyFilter.Apply()     ← 7 条规则过滤
    → scoreAndRank()           ← RRF(k=60) × importance × freshness × confidence
    → markSeeds()              ← normalised_score >= 0.5 → IsSeed=true
    → CandidateList{ Candidates, SeedIDs }
```

**`ForGraph` 模式**：TopK×2 候选，放宽过滤，优先覆盖

**`FilterOnly` 模式**：跳过 dense+sparse，仅按 importance 排序

#### Candidate Seed 接口（`candidate.go`）

```go
type CandidateList struct {
    Candidates []Candidate
    SeedIDs    []string  // 高质量候选 ID，传给 QueryChain
}
type Candidate struct {
    ObjectID  string
    Score     float64
    IsSeed    bool
    SeedScore float64
}
```

---

## 六、Runtime 集成层

### `src/internal/worker/runtime.go` — `ExecuteQuery` 查询主路径

```
DataPlane.Search(query)
    → raw ObjectIDs
    → retrieval.Retriever.EnrichAndRank()
    → CandidateList.SeedIDs
    → QueryChain.Run({ SeedObjectIDs: seedIDs })   ← graph 扩展只用 seed
    → QueryResponse{ Objects, Nodes, Edges, ProofTrace, Provenance }
```

Provenance 记录：
```
retrieval_seeds=N  graph_expansion_via=seed_ids
embedding_runtime_family=tfidf  embedding_runtime_dim=256  cross_dim_fusion=rrf_result_layer
```

---

## 七、Bootstrap 集成

### `src/internal/app/bootstrap.go` — Provider 初始化（Pass 9 补全）

| case | 读取的环境变量 |
|------|-------------|
| `onnx` | `ANDB_EMBEDDER_MODEL_PATH`, `ANDB_EMBEDDER_DIM`, `ONNXRUNTIME_LIB_PATH`, `ANDB_EMBEDDER_DEVICE`, `ANDB_ONNX_VOCAB_PATH` |
| `tensorrt` | `ANDB_EMBEDDER_MODEL_PATH`, `ANDB_EMBEDDER_DIM`, `ANDB_ONNX_VOCAB_PATH`, `CUDA_VISIBLE_DEVICES` |
| `huggingface` | `ANDB_EMBEDDER_MODEL`, `ANDB_EMBEDDER_API_KEY`, `ANDB_EMBEDDER_TIMEOUT`, `ANDB_EMBEDDER_DIM` |
| `vertexai` | `GOOGLE_CLOUD_PROJECT`, `GOOGLE_APPLICATION_CREDENTIALS`, `ANDB_EMBEDDER_DIM` |

---

## 八、构建脚本与 Docker

| 文件 | 作用 |
|------|------|
| `scripts/build_cpp.sh` | 编译 `libandb_retrieval.so`（自动检测 nvcc；`TRT_INC`/`TRT_LIB` 可选） |
| `scripts/build_embeddings.sh` | 克隆 `go-llama.cpp`（pinned `6a8041ef6b46`），打 CUDA patch，`LLAMA_CUBLAS=ON` |
| `scripts/test_build.sh` | 测试 stub 检测逻辑 |
| `scripts/docker/Dockerfile.memberb` | 多阶段 GPU 镜像：CUDA 11.8 + ONNX Runtime GPU 1.17.0 + TensorRT 10.x + go-llama.cpp + `libandb_retrieval.so` |
| `models/README.md` | 模型文件下载说明（ONNX, GGUF, engine 路径约定） |
| `libs/go-llama.cpp/` | go-llama.cpp stub（go.mod + llama.go，非 gguf 构建的占位） |

---

## 九、测试清单

### 9.1 Embedding — CPU 模式（无需 GPU，任意机器可运行）

运行命令：`go test ./src/internal/dataplane/embedding/`

| 测试函数 | 文件 | 状态 |
|---------|------|------|
| `TestTfidfEmbedder_ImplementsGenerator` | `embedding_test.go` | ✅ 已验证 |
| `TestTfidfEmbedder_EmptyText` | `embedding_test.go` | ✅ 已验证 |
| `TestTfidfEmbedder_Reset` | `embedding_test.go` | ✅ 已验证 |
| `TestHTTPEmbedder_Generate_Success` | `embedding_test.go` | ✅ 已验证 |
| `TestHTTPEmbedder_BatchGenerate_Success` | `embedding_test.go` | ✅ 已验证 |
| `TestHTTPEmbedder_BatchGenerate_ServerError` | `embedding_test.go` | ✅ 已验证 |
| `TestHTTPEmbedder_Probe_FailsOnDimMismatch` | `embedding_test.go` | ✅ 已验证 |
| `TestOpenAIConfig_Defaults` | `embedding_test.go` | ✅ 已验证 |
| `TestOpenAIRequestSchema` | `embedding_test.go` | ✅ 已验证 |
| `TestGeneratorInterface_Compatibility` | `embedding_test.go` | ✅ 已验证 |
| `TestClientPool_Reuse` | `embedding_test.go` | ✅ 已验证 |
| `TestVertexAIEmbedder_MissingConfig` | `providers_test.go` | ✅ 已验证 |
| `TestVertexAIEmbedder_MockServer` | `providers_test.go` | ✅ 已验证 |
| `TestHuggingFaceEmbedder_MissingConfig` | `providers_test.go` | ✅ 已验证 |
| `TestHuggingFaceEmbedder_MockServer` | `providers_test.go` | ✅ 已验证 |

### 9.2 Embedding — GPU 模式（需要 Docker + NVIDIA GPU）

运行命令：`go test -v -tags cuda ./src/internal/dataplane/embedding/ -run <TestName>`（在 Docker `--gpus all` 内）

| 测试函数 | 文件 | 状态 | 备注 |
|---------|------|------|------|
| `TestOnnxEmbedder_CUDA_Generate` | `gpu_test.go` | ✅ 已验证 (2026-03-31, TITAN RTX) | dim=384, vec=[0.584, 0.011, -0.488, 0.126] |
| `TestOnnxEmbedder_CUDA_BatchGenerate` | `gpu_test.go` | ✅ 已验证 | 3 texts → 3 vecs, dim=384 |
| `TestGGUFEmbedder_CUDA` | `gpu_test.go` | ✅ 已验证 | TinyLlama 1.1B Q4_K_M |
| `TestTensorRT_EngineLoad` | `gpu_test.go` | ✅ 已验证 | test_embed.engine (44.7MB), dim=384 |
| `TestTensorRT_Inference` | `gpu_test.go` | ✅ 已验证 | dim=384, vec=[0,0,0,0]（test engine） |
| `TestTensorRTEmbedder_Integration` | `tensorrt_integration_test.go` | ⬜ 待运行 | Pass 10 后新增 |
| `TestTensorRTEmbedder_BatchGenerate_Integration` | `tensorrt_integration_test.go` | ⬜ 待运行 | 测试 auto-split batch |
| `TestTensorRTEmbedder_RealModel` | `tensorrt_integration_test.go` | ⬜ 待运行 | 需真实 BERT .engine 文件 |

### 9.3 Retrieval CGO Bridge（需要 `libandb_retrieval.so`，建议在 Docker 内）

运行命令：`go test -v -tags retrieval ./src/internal/dataplane/retrievalplane/`

| 测试函数 | 文件 | 状态 | 耗时（参考） |
|---------|------|------|------------|
| `TestRetrieval_Bridge_Search` | `integration_test.go` | ✅ 已验证 (2026-03-31) | 0.12s |
| `TestRetrieval_BuildSegment` | `integration_test.go` | ✅ 已验证 | 0.04s |
| `TestRetrieval_Bridge_MultiSegment` | `integration_test.go` | ✅ 已验证 | 0.09s |
| `TestRetrieval_Bridge_Empty` | `integration_test.go` | ✅ 已验证 | 0.01s |
| `TestRetrieval_Bridge_LargeScale` | `integration_test.go` | ✅ 已验证 | 1.21s |
| `TestRetrieval_Bridge_Concurrent` | `integration_test.go` | ✅ 已验证 | 0.33s |

### 9.4 Go 检索引擎（无需 GPU，纯 Go 单测）

运行命令：`go test -v ./src/internal/retrieval/`

| 测试函数 | 文件 | 状态 |
|---------|------|------|
| `TestEnrichAndRank_BasicScoring` | `retriever_test.go` | ⬜ 待确认 |
| `TestSafetyFilter_Quarantine` | `retriever_test.go` | ⬜ 待确认 |
| `TestSafetyFilter_TTL` | `retriever_test.go` | ⬜ 待确认 |
| `TestSafetyFilter_IsActive` | `retriever_test.go` | ⬜ 待确认 |
| `TestSafetyFilter_MinVersion` | `retriever_test.go` | ⬜ 待确认 |
| `TestSeedMarking` | `retriever_test.go` | ⬜ 待确认 |
| `TestForGraphMode` | `retriever_test.go` | ⬜ 待确认 |
| `TestFilterOnlyMode` | `retriever_test.go` | ⬜ 待确认 |
| `TestQueryChainAlignment` | `retriever_test.go` | ⬜ 待确认 |
| `TestNonExistentObjectPassThrough` | `retriever_test.go` | ⬜ 待确认 |
| `TestDemoRetrieval_StandardQuery` | `demo_test.go` | ⬜ 待确认 |
| `TestDemoRetrieval_ForGraphMode` | `demo_test.go` | ⬜ 待确认 |
| `TestDemoRetrieval_AgentScoped` | `demo_test.go` | ⬜ 待确认 |
| `TestDemoRetrieval_SafetyFilterAll` | `demo_test.go` | ⬜ 待确认 |
| `TestDemoRetrieval_EnrichAndRank` | `demo_test.go` | ⬜ 待确认 |

### 9.5 Runtime 集成（seed → graph expansion）

运行命令：`go test -v -run TestRuntime_SeedDrivesGraphExpansion ./src/internal/worker/`

| 测试函数 | 文件 | 状态 | 关键验证点 |
|---------|------|------|----------|
| `TestRuntime_SeedDrivesGraphExpansion` | `runtime_test.go` | ✅ 已验证 (2026-04-01) | SeedIDs=2；Nodes=2；Edges=14；ProofTrace 8阶段 |

---

## 十、Bug 修复记录

### Code Review Pass 9（2026-04-07）

| 文件 | 问题 | 修复 |
|------|------|------|
| `onnx_cuda.go` | `simpleTokenize` 用 Unicode 码点哈希伪造 BERT token，推理向量完全无意义 | 新增 `onnx_tokenizer.go` 实现真正的 BERT WordPiece |
| `tensorrt_cuda.go` | `simpleTokenizeTRT` 同样是假 tokenizer | 改用 `bertTokenizer` |
| `bootstrap.go` | `switch ANDB_EMBEDDER` 缺少 `onnx`/`tensorrt`/`huggingface`/`vertexai` 4 个 case，设置了也不生效 | 补全 4 个 case |
| `go.mod` | `golang.org/x/text v0.35.0` 要求 Go 1.25，服务器只有 Go 1.24 | 降级到 `v0.24.0` |
| `libs/go-llama.cpp/` | replace 指令目录不存在，`go mod tidy` 报错 | 添加 stub `go.mod` + `llama.go` |

### Code Review Pass 10（2026-04-08）

| 文件 | 问题 | 修复 |
|------|------|------|
| `tensorrt_cuda.go` | `NewTensorRT(dim=0)` 存 `e.dim=0`，`BatchGenerate` 里 `outputSize=0`，结果全空或 panic | 构造函数加 `if dim <= 0 { return error }` |
| `tensorrt_cuda.go` | 全局 `sync.Mutex` 把 tokenize（CPU）和 GPU 操作一起锁住，无法并发 | 拆为 `closedMu`（closed 标志）+ `inferenceMu`（GPU 操作），tokenize 在锁外 |
| `tensorrt_cuda.go` | `len(texts) > MaxBatchSize` 直接报错，与 ONNX CPU 行为不一致 | 新增 `batchGenerateSplit`，自动切片合并 |
| `onnx_tokenizer.go` | `wordPieceSplit` 对超长 token O(n²) 无上限 | 加 `bertMaxSubwords=200`，超出返回 `[UNK]` |

---

## 十一、环境与依赖

### 编译标签对照

| 标签 | 启用的功能 |
|------|----------|
| （无标签） | TF-IDF（纯 Go） |
| `cuda` | ONNX CUDA |
| `cuda,tensorrt,linux` | TensorRT GPU 推理 |
| `gguf` | GGUF CPU（需 `libs/go-llama.cpp` 真实实现） |
| `gguf,cuda` | GGUF CUDA（需 `LLAMA_CUBLAS=ON` 编译的 llama.cpp） |
| `retrieval` | CGO Bridge（需 `libandb_retrieval.so`） |

### 关键环境变量

| 变量 | 用途 |
|------|------|
| `ANDB_EMBEDDER` | provider 选择（`onnx`/`tensorrt`/`gguf`/`openai`/`huggingface`/`vertexai`/…） |
| `ANDB_EMBEDDER_MODEL_PATH` | ONNX / TensorRT engine 文件路径 |
| `ANDB_EMBEDDER_DIM` | 向量维度（部分 provider 必填） |
| `ANDB_EMBEDDER_DEVICE` | `cpu` / `cuda` |
| `ANDB_ONNX_VOCAB_PATH` | BERT vocab.txt 路径（可选，无则 FNV 回退） |
| `ONNXRUNTIME_LIB_PATH` | `libonnxruntime.so` 路径 |
| `CUDA_VISIBLE_DEVICES` | CUDA 设备 ID |
| `LD_LIBRARY_PATH` | 需包含 `libandb_retrieval.so` 所在目录 |

### 关键依赖版本

| 依赖 | 版本 | 说明 |
|------|------|------|
| `github.com/yalue/onnxruntime_go` | v1.9.0 | ONNX Runtime Go binding |
| `github.com/go-skynet/go-llama.cpp` | `6a8041ef6b46`（pinned） | GGUF/llama.cpp binding |
| `golang.org/x/text` | v0.24.0 | BERT tokenizer NFD（兼容 Go 1.24） |
| CUDA Toolkit | 11.8 / 12.9 | GPU 运行时 |
| ONNX Runtime | 1.17.0-gpu | GPU 版本 |
| TensorRT | 10.x（libnvinfer-dev） | 高性能 GPU 推理 |
| Knowhere/HNSW | 源码集成于 `cpp/vendor/` | ANN 索引内核 |

---

## 十二、遗留/待确认事项

| 项 | 描述 | 优先级 |
|----|------|--------|
| `retrieval/` 包单测全量确认 | 15 个测试函数尚未在当前 feature/retrieval-b 上跑一遍 | 高 |
| `tensorrt_integration_test.go` 3 个新测试 | Pass 10 修复后未在 GPU 环境重跑 | 中 |
| `TestTensorRTEmbedder_RealModel` | 需真实 BERT `.engine` 文件（非 test engine） | 中 |
| `cpp/retrieval/sparse.cpp` | ~~Sparse 检索为 FNV hash brute-force stub~~ → **已修复**：实现真正的倒排 Posting List 索引（O(Q×L̄) 查询）+ 完整 Serialize/Deserialize/Load/Save 二进制格式 | — |
| Batch inference benchmark | `TieredDataPlane.BatchIngest` 1000 events 的 embedding 吞吐量基准数据未收集 | 低 |
| `Dockerfile.memberb` 完整 E2E | CUDA 12.9 + TensorRT 10.16 环境未做端到端验证 | 低 |
