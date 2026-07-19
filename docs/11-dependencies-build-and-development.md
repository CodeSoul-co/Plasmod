# 11. 依赖、构建、测试与开发流程

> Language: 中文 | [English](en/11-dependencies-build-and-development.md)

---

说明 Go、C++、CGO、检索库、Badger、S3、embedding provider 和开发验证流程。

---

## 11.1. Badger Integration

Badger 是 `PLASMOD_STORAGE=disk` 的默认 canonical backend。

### 11.1.1. Stored records

Agent、Session、Event、Memory、State、Artifact、User、Edge、Version、Policy、ShareContract、segment/index metadata
及部分 algorithm/audit records。

### 11.1.2. Transaction boundary

同一 DB 中的 canonical projection 可以原子写 object、edge、version。Native index、FileWAL 和 S3 不在 Badger
transaction 内，由 runtime/consistency controller 协调。

### 11.1.3. Operational rules

- 一个数据目录只由一个兼容 Plasmod 进程写；
- 不手工编辑 `.sst`/value log；
- 备份前协调写入或使用受支持 snapshot；
- 关注磁盘空间和 value log GC；
- schema/key prefix 升级前备份并验证迁移。

---

## 11.2. Build And Link Model

### 11.2.1. Pure Go

```bash
go build ./src/cmd/server
```

未启用 `retrieval` tag 时使用 stub bridge。

### 11.2.2. Native

```bash
make cpp
make build
```

`make cpp` 使用 CMake 构建动态库；`make build` 检测库后添加 Go build tag/CGO link flags。Dockerfile 还安装
FAISS、ONNX Runtime 等镜像依赖。

### 11.2.3. Runtime linking

动态 linker 必须找到 `libplasmod_retrieval` 及其 FAISS/OpenMP/ONNX 依赖。macOS 使用 `otool -L`，Linux
使用 `ldd` 检查：

```bash
otool -L ./bin/plasmod
```

能编译不代表部署镜像中动态库路径正确；必须在最终运行环境执行启动 smoke test。

---

## 11.3. Dependency Inventory

| Dependency | Version/source evidence | Role | Optional | Owner |
|---|---|---|---:|---|
| Go | `go.mod`: 1.25 | Core server toolchain | No | Go project |
| Badger | `github.com/dgraph-io/badger/v4 v4.8.0` | Disk canonical storage | Storage-mode | Upstream |
| gRPC | `google.golang.org/grpc v1.72.1` | gRPC server/transport | Yes | Upstream |
| Protobuf | `google.golang.org/protobuf v1.36.6` | gRPC wire types | gRPC path | Upstream |
| Knowhere-style source | `cpp/vendor`; upstream commit not recorded in one manifest | ANN abstraction/engines | Native path | Upstream snapshot |
| HNSW engine | `cpp/vendor/engines/hnsw_engine` | Default native ANN | Native path | Upstream |
| FAISS engine | CMake option `ANDB_KNOWHERE_FAISS` | IVF family | Yes | Upstream |
| DiskANN engine | CMake option `ANDB_KNOWHERE_DISKANN` | Disk ANN | Yes | Upstream |
| OpenMP | system/Homebrew toolchain | Native batch parallelism | Yes | Toolchain |
| ONNX Runtime | Docker/native provider dependency | Local neural embedder | Yes | Upstream |
| S3/MinIO | External service | Cold storage | Yes | External |
| Python `requests` | `sdk/python/setup.py` | Python SDK HTTP | SDK only | Upstream |

Native vendor 缺少单一 upstream commit manifest 是升级风险；发布前应补 source revision 和 notices。精确版本以
`go.mod`、`cpp/CMakeLists.txt`、Dockerfile 和 SDK package metadata 为准。

---

## 11.4. Dependency Overview

Plasmod 依赖分为四层：

1. Go runtime：HTTP、Badger、对象模型、WAL、一致性、Evidence；
2. Native retrieval：C++17、Knowhere-style adapter、HNSW/FAISS/DiskANN；
3. External services：S3/MinIO、可选 embedding provider；
4. Build/runtime support：CMake、CGO、OpenMP、ONNX Runtime、Docker。

核心原则：第三方 retrieval 只负责物理候选，Agent scope、canonical objects、policy、provenance 和 consistency
仍由 Plasmod Go runtime 决定。

---

## 11.5. Dependency Troubleshooting

### 11.5.1. CGO/native library not found

检查 `cpp/build`、build tag、`CGO_ENABLED`、动态库依赖和 rpath。若只需 canonical 功能，可明确使用 pure Go
build，而不是伪造 native 成功。

### 11.5.2. FAISS/OpenMP errors

确认 CMake option、architecture 和 package manager prefix 一致。Apple Silicon 不要混用 x86_64 library。

### 11.5.3. Badger lock

确认没有第二个进程使用同一数据目录；检查 Docker 和本地进程。不要删除 LOCK 文件绕过真实并发写入。

### 11.5.4. MinIO connection

区分 API 9000 和 console 9001；检查 endpoint scheme、TLS、bucket、credential 和 container DNS。

### 11.5.5. Embedding mismatch

查看 model ID、dimension 和 segment embedding family。更换模型后执行受控 reindex，不要继续写入旧 segment。

---

## 11.6. Embedder Providers

DataPlane 通过 `EmbeddingGenerator`/`embedding.Generator` 生成向量，支持：

- TF-IDF：无外部模型，适合基础运行；
- ONNX local model：需要 model/tokenizer/runtime；
- 代码中已接入的其他 provider；
- precomputed vector：Event/Query 直接提供。

### 11.6.1. Compatibility tuple

每个向量空间至少由以下信息标识：

```text
embedding family + model ID + dimension + normalization/metric
```

任一项变化都可能要求 reindex。只比较 dimension 会把不兼容向量混入同一 segment。

### 11.6.2. Failure behavior

Embedding 失败不应悄悄写零向量。调用者可显式选择 lexical-only/skip-vector，或让 strict projection 返回失败。

---

## 11.7. Knowhere Integration

`cpp/vendor` 包含 Knowhere-style/upstream native source，`cpp/retrieval` 组合 Plasmod retrieval library。

### 11.7.1. Why this boundary

选择 source-level ANN engine 是为了复用成熟的 HNSW/IVF/DiskANN index lifecycle，同时让 Plasmod 保持一个
稳定 C ABI，不把上游 C++ 类型扩散到 Go schema。替代方案包括纯 Go ANN 或直接绑定每个 engine；前者会增加
核心算法维护，后者会让 backend 差异渗入 DataPlane。

### 11.7.2. Boundary

- C++：index create/load/search、dense/sparse primitive、batch optimization；
- Go CGO bridge：handle ownership、slice conversion、error mapping；
- Go DataPlane：namespace、scope、policy、fusion、tiering、Evidence。

### 11.7.3. Build flags

- HNSW 始终可构建；
- `ANDB_KNOWHERE_FAISS` 控制 FAISS；
- DiskANN 和 RAFT/GPU 由独立 option 控制；
- GPU 路径要求 Linux/CUDA，不是 macOS 默认能力。

### 11.7.4. Search paths

- native/raw：Knowhere batch search 返回原始 ID 和距离；
- optimized batch：`BatchPluginL2NormSort` 在 query rows 较多时重排并用 OpenMP 分发，之后恢复行顺序；
- Go result layer：映射 object ID，并用 RRF 合并 lexical/dense/sparse/tier candidates。

Batch plugin 不改变每行的逻辑归属；`row_lineage` 的 source fan-out 在 Go schema/service 层完成。

### 11.7.5. Error mapping and fallback

CGO bridge 把 create/build/search/handle 错误转为 Go error。没有 `retrieval` tag 时 stub 明确返回 unavailable，
上层可使用 lexical/canonical path；不能将 unavailable 伪装为空 ANN 结果。

兼容 flag 仍使用 `ANDB_` 前缀是当前构建历史，不应由文档伪装成已完成重命名。

---

## 11.8. Licenses And Attribution

### 11.8.1. Required checks

- 根仓库 LICENSE；
- `src/internal/platformpkg/UPSTREAM_LICENSE`；
- `cpp/vendor` 内各第三方 license；
- Go module licenses；
- FAISS、DiskANN、HNSW、ONNX Runtime、Badger 等分发要求。

### 11.8.2. Distribution

发布 source、binary 或 Docker image 前：

1. 生成 dependency inventory；
2. 保留 copyright 和 license text；
3. 标记修改过的上游代码；
4. 检查静态/动态链接是否改变义务；
5. 将 notices 放入发布物；
6. 不把内部来源说明当作法律结论。

新增 vendor 代码必须在 merge 前完成来源和 license 审核。

---

## 11.9. Native Retrieval Stack

### 11.9.1. Library

`cpp/CMakeLists.txt` 构建 `libplasmod_retrieval`，主要源文件包括 `retrieval.cpp`、`segment_index.cpp`、
`dense.cpp`、`sparse.cpp` 和 `batch_optimizer.cpp`。

### 11.9.2. Index types

- HNSW；
- IVF_FLAT；
- IVF_PQ；
- IVF_SQ8；
- DISKANN。

运行可用性取决于编译 feature，不是只由 API `index_type` 字符串决定。

### 11.9.3. Go bridge

`src/internal/dataplane/retrievalplane/bridge.go` 仅在 `retrieval` tag 下编译；stub 文件提供无 native library 的
兼容实现。所有 C handle 必须有明确 release，Go memory 不得被 C 长期持有。

### 11.9.4. TensorRT

仓库包含可选 TensorRT bridge。它不属于默认 macOS/CPU 启动路径，应在独立支持矩阵和镜像中启用。

---

## 11.10. S3 And MinIO Integration

S3/MinIO 是 ColdObjectStore，可保存归档 Memory、Embedding、AgentState、Artifact、Edge 和相应索引。

### 11.10.1. Required configuration

- endpoint、bucket；
- access/secret key；
- region、TLS；
- key prefix。

Docker Compose 启动 MinIO API `9000` 和 console `9001`。Plasmod 连接的是 API 端口，不是 console。

### 11.10.2. Consistency

S3 写入与 Badger canonical transaction 分离。归档流程应先确认对象成功写 Cold，再根据策略清理 Warm。
Cold purge 也需要验证对象 key 和 edge index key 都被处理。

### 11.10.3. Security

使用专用 bucket/credential、最小权限、TLS、server-side encryption 和 bucket lifecycle。不要把 compose 默认凭证
用于正式环境。

---

## 11.11. Dependency Upgrade Policy

### 11.11.1. Go modules

按小批次升级，运行 `go mod tidy` 后检查 diff，并执行 `go test ./src/...`。对 Badger、protobuf、gRPC 等核心
依赖单独验证持久化和 wire compatibility。

### 11.11.2. Native stack

1. 固定 upstream commit/version；
2. 记录本地 patch；
3. 在支持平台重建；
4. 验证所有启用 index type；
5. 检查 ABI、symbol 和 dynamic links；
6. 验证旧 segment load 或明确要求 rebuild。

### 11.11.3. Storage/external service

Badger 升级需测试旧目录打开、backup/restore、key scan。S3/MinIO 升级需测试 endpoint/TLS/signature 和
existing object keys。

依赖升级不得顺带改变 Agent schema 或 API 默认语义。

---

## 11.12. Build System

### 11.12.1. Go

`go.mod` 声明 Go 1.25。常用目标：

```bash
make build
make test
go test ./src/...
```

`make build` 检查 `cpp/build/libplasmod_retrieval.dylib` 或对应 `.so`；存在时启用 `retrieval` tag，否则构建
stub path。

### 11.12.2. C++

```bash
make cpp
```

目标调用 CMake，当前打开 FAISS option。更细 feature 以 `cpp/CMakeLists.txt` 为准。

### 11.12.3. Docker

Dockerfile 分阶段构建 C++ library、Go binary 和 runtime image。Compose 提供 unified/split 运行拓扑。

### 11.12.4. Artifacts

- Go binary：`bin/plasmod`；
- native library：`cpp/build/libplasmod_retrieval*`；
- runtime data：由 `PLASMOD_DATA_DIR` 决定。

构建产物与持久化数据都不应作为源码提交。

---

## 11.13. Coding Conventions

### 11.13.1. Go

- `gofmt`/`goimports`；
- interface 放在真正需要替换的边界；
- error 包含操作和稳定 ID，不吞掉底层 cause；
- context 从 handler 传到 storage/provider；
- goroutine 必须有 owner、cancel 和 shutdown；
- JSON tag 和持久化 key 视为兼容契约。

### 11.13.2. C++

- C++17；
- RAII 管理 native resource；
- C ABI 边界不抛异常；
- 检查维度、长度、null 和 handle 状态；
- 不把 Go pointer 保存到调用之后。

### 11.13.3. Architecture

- Agent-native semantics 保留在 Go core；
- retrieval backend 不执行租户授权；
- Event/Canonical/Projection 三层不混写；
- 实现扩展通过已有 contracts 和 composition root 接入；
- upstream/vendored 目录避免无关改写。

---

## 11.14. Common Code Changes

### 11.14.1. Add Event Type

更新 constants/validation、materializer dispatch、query filters、tests 和 schema docs。

### 11.14.2. Add Canonical Object

更新 schema、ObjectStore/RuntimeStorage、memory+Badger+S3 backend、key prefix、Gateway、query/evidence、replay、
delete/purge 和 migration。

### 11.14.3. Add Query Filter

更新 QueryRequest JSON tag、planner、所有 candidate path、evidence/cold path、SDK 和 contract tests。过滤必须在
返回前对所有层一致执行。

### 11.14.4. Add Environment Variable

在拥有该配置的 package 解析并验证，加入 effective config（脱敏）、Compose/template、配置文档和 tests。

### 11.14.5. Add Background Worker

通过 app composition 创建，使用 context/stop channel，定义 queue/backpressure/metrics，并接入 shutdown。

---

## 11.15. Contribution Guide

### 11.15.1. Scope

一个提交解决一个可解释问题。核心库不得引入特定外部测量流程、一次性数据路径或结果生成逻辑。

### 11.15.2. Workflow

1. 从最新 `dev` 开始；
2. 阅读 requirements、design 和 call path；
3. 编写/更新测试；
4. 最小化修改 active ownership area；
5. 更新 API/schema/ops 文档；
6. 运行格式、tests、build 和 safety check；
7. commit 并 push `dev`。

### 11.15.3. Review evidence

PR/commit 说明应包含：行为变化、持久化/API 影响、故障处理、验证命令、迁移要求。不能仅写“优化”或“修复”。

### 11.15.4. Upstream code

修改 upstream snapshot 时，说明来源、原因和无法在 wrapper 层解决的证据，并保留 license。

---

## 11.16. Local Development

### 11.16.1. Setup

```bash
go mod download
cp .env.example .env
```

不需要原生 ANN 时：

```bash
PLASMOD_STORAGE=disk PLASMOD_DATA_DIR=.andb_data \
PLASMOD_EMBEDDER=tfidf PLASMOD_GRPC_ENABLED=0 \
go run ./src/cmd/server
```

使用仓库脚本：

```bash
make dev
```

`scripts/dev_up.sh` 读取 `.env`，并根据原生库存在情况决定 build tag。

### 11.16.2. Isolated data

为每个开发实例使用独立 `PLASMOD_DATA_DIR`，避免 Badger lock 和测试污染。不要把 `.andb_data` 提交到 Git。

### 11.16.3. Before editing

1. `git fetch origin --prune`；
2. 确认 `dev` 和 `origin/dev` 状态；
3. 检查工作区已有修改；
4. 阅读 active call path 和 tests；
5. 保留无关用户修改。

---

## 11.17. Logging And Metrics

### 11.17.1. Logs

启动日志应记录监听器、storage mode/data dir、embedder/retrieval feature、admin key missing warning 和 shutdown。
不要记录 admin key、S3 secret、完整敏感 payload 或 embedding vector。

### 11.17.2. Metrics endpoint

`GET /v1/admin/metrics` 返回当前 runtime/admin 指标，需要 admin key。关注：

- write queue/backpressure；
- consistency progress/retry/failure；
- materialization/projection；
- query/tier usage；
- purge task；
- provider health。

### 11.17.3. Correlation

Event ID、object ID、LSN、session ID 和 request ID 是跨组件关联字段。新增日志应保留这些结构化字段，而不是
仅输出自由文本。

### 11.17.4. Production visibility

APP_MODE=prod 会从 API JSON 删除 debug/raw/log/chain trace 字段；这不影响服务端运维日志。

---

## 11.18. Native Dependencies

### 11.18.1. Configure manually

```bash
cmake -S cpp -B cpp/build -DCMAKE_BUILD_TYPE=Release
cmake --build cpp/build -j
```

若需要与 Makefile 一致的 FAISS 选项，查看 `make cpp` 展开的具体参数。

### 11.18.2. CGO build

```bash
CGO_ENABLED=1 go build -tags retrieval -o bin/plasmod ./src/cmd/server
```

需要正确的 include、library search path 和 runtime rpath。不要把本机绝对路径硬编码进源码。

### 11.18.3. Ownership

Go 传给 C 的 slice 只在调用期间有效；C++ handle 通过显式 destroy 释放；错误字符串复制回 Go。增加 API 时
需要同时测试空输入、维度错误、重复 close 和并发 search。

---

## 11.19. Run And Debug

### 11.19.1. Debug startup

```bash
APP_MODE=test \
PLASMOD_STORAGE=disk \
PLASMOD_DATA_DIR=.andb_data-debug \
PLASMOD_EMBEDDER=tfidf \
PLASMOD_GRPC_ENABLED=0 \
go run ./src/cmd/server
```

`APP_MODE=test` 会启用 `/v1/debug/echo` 和 debug response fields，不得用于开放网络。

### 11.19.2. Delve

```bash
dlv debug ./src/cmd/server
```

Native crash 需结合 lldb/gdb 和 core dump；Go stack 不能解释 C++ 内存错误。

### 11.19.3. Debug order

1. health/端口；
2. effective config；
3. WAL append/LSN；
4. canonical object；
5. projection；
6. query/evidence；
7. response visibility。

按链路定位可以区分“没写入”“已写但未物化”“已物化但未索引”和“查询被过滤”。

---

## 11.20. Test Guide

### 11.20.1. Core tests

```bash
go test ./src/...
```

### 11.20.2. Full repository target

```bash
make test
```

该目标运行 Go tests 和 Python tests。原生 path 需先构建 library，再执行带 `retrieval` tag 的相关测试。

### 11.20.3. Test layers

- schema normalize/validation；
- storage contract 和 Badger reopen；
- WAL append/scan/corruption；
- deterministic materialization/replay；
- consistency mode/backpressure/timeout；
- Gateway route/error/auth/visibility；
- DataPlane lexical/native/cold；
- Evidence graph/proof；
- shutdown/resource release。

### 11.20.4. Persistent compatibility

修改 key/schema/WAL codec 时，测试必须用旧 fixture 打开并读取，或提供明确 migration。只测试新建空库不够。

---

## 11.21. Development Troubleshooting

### 11.21.1. Port already in use

```bash
lsof -nP -iTCP:8080 -sTCP:LISTEN
lsof -nP -iTCP:19530 -sTCP:LISTEN
```

确认旧本地进程和 Docker container，而不是直接更改所有默认端口。

### 11.21.2. Badger lock

检查是否两个测试/服务共享 data dir。停止 owner 后重试，不删除 lock 绕过。

### 11.21.3. Build unexpectedly uses stub

确认 `cpp/build/libplasmod_retrieval.dylib`/`.so` 文件名和 Makefile 检测条件，查看 `go build` 是否包含
`-tags retrieval`。

### 11.21.4. Query returns object but no native hit

查看 `query_status` 是否为 supplemented，检查 vector projection、embedding family 和 warm segment。

### 11.21.5. Strict ingest times out

检查 controller queue、worker、projection error、embedder/native backend 和 checkpoint；先确认对象是否已物化再重试。
