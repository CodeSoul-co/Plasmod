<div align="center">
  <img src="assets/cogdb.png" alt="CogDB Logo" width="480"/>
</div>

<div align="center">

[English](README.md) · [中文](README.zh-CN.md)

</div>

<div align="center">

[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Python](https://img.shields.io/badge/Python-3.x-3776AB?logo=python&logoColor=white)](https://www.python.org/)
[![C++](https://img.shields.io/badge/C++-17-00599C?logo=cplusplus&logoColor=white)](https://isocpp.org/)
[![CUDA](https://img.shields.io/badge/CUDA-12.x-76B900?logo=nvidia&logoColor=white)](https://developer.nvidia.com/cuda-toolkit)
[![Docker](https://img.shields.io/badge/Docker-ready-2496ED?logo=docker&logoColor=white)](https://www.docker.com/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

</div>

# Plasmod — 面向多智能体系统的原生智能体数据库

Plasmod 是一个面向多智能体系统的 agent-native database。受黏菌网络去中心化、自适应、自组织机制的启发，它将认知对象管理、事件驱动的物化过程与结构化证据检索统一到一个可运行的系统中。Plasmod 集成了分层的 segment-oriented retrieval plane、基于 append-only WAL 的事件主干、canonical object materialization layer、预计算 evidence fragments、1-hop 图扩展以及 structured evidence assembly，并将这些能力整合为一个单体可运行的 Go server，以支撑面向智能体的记忆管理与推理工作负载。


> **核心论点：** 智能体的记忆、状态、事件、产物与关系应当作为一等数据库对象建模，查询结果应返回结构化证据包，而非仅有 top-k 文本片段。

## 已实现功能

- Go 服务（[`src/cmd/server/main.go`](src/cmd/server/main.go)），在 [`Gateway.RegisterRoutes`](src/internal/access/gateway.go) 中注册 **25 条 HTTP 路径**（见下文「HTTP API 一览 v1」），通过 `context.WithCancel` 优雅关闭
- 管理员数据集清理：`POST /v1/admin/dataset/delete` 软删除匹配选择器的 **Memory**（**AND** 语义）。**`workspace_id` 必填**，`file_name`、`dataset_name`、`prefix` 至少填一个。`dry_run` 仅报告匹配，不写入。
  - 匹配规则：`Memory` 在摄入时若带有结构化字段，则优先用 `Memory.dataset_name`、`Memory.source_file_name`（来自 `Event.Payload`）；否则回退到对 `Memory.Content` 的 **token 安全**解析（`dataset=` 后精确文件名 token、`dataset_name:` 后标签边界、`prefix` 对文件 token 前缀匹配）。详见 `schemas.MemoryDatasetMatch`。
  - 响应字段：`matched`、`deleted`、`memory_ids`（`dry_run` 时 `deleted` 为 `0`，`memory_ids` 仍列出匹配项）。
- 管理员数据集**硬删除（purge）**：`POST /v1/admin/dataset/purge`，选择器与 **`workspace_id`** 要求同上。若运行时配置了 `TieredObjectStore`，则对匹配项执行 `HardDeleteMemory`（热/温/冷及边等）；**若无 tiered**，则降级为仅 warm 路径删除（响应中 `purge_backend` 为 `warm_only`，冷层嵌入可能残留）。默认 `only_if_inactive=true`；`false` 可清除仍活跃的匹配项。成功硬删会追加 `AuditRecord`（`reason_code=dataset_purge`）。
- 追加写 WAL，支持 `Scan` 和 `LatestLSN`，用于重放与水位追踪
- `MaterializeEvent` → `MaterializationResult`，在摄入时生成规范化 `Memory`、`ObjectVersion` 和类型化 `Edge` 记录
- 同步对象物化：`ObjectMaterializationWorker`、`ToolTraceWorker`、`StateCheckpoint` 在 `SubmitIngest` 中调用，使 State/Artifact/Version 对象可立即查询
- `ExecuteQuery` 中的补充规范检索：从 ObjectStore 中与检索平面结果一同取出 State/Artifact ID
- 三层数据平面：**热层**（内存 LRU）→ **温层**（段索引，设置 embedder 后启用混合模式）→ **冷层**（S3 或内存），统一 `DataPlane` 接口
- 热层 + 温层 + 冷层候选列表的 **RRF 融合** 排名
- 双存储后端：内存（默认）和 Badger 持久化（`ANDB_STORAGE=disk`），每个存储支持混合模式；`GET /v1/admin/storage` 报告解析后的配置
- 摄入时填充预计算 `EvidenceFragment` 缓存，查询时合并入证明轨迹；`QueryResponse.EvidenceCache` 报告命中/未命中统计
- `Assembler.Build` 路径中通过 `GraphEdgeStore.BulkEdges` 实现单跳图扩展
- 每次查询的 `QueryResponse` 包含：`Objects`、`Edges`、`Provenance`、`ProofTrace`、`Versions`、`AppliedFilters`、`ChainTraces`、`EvidenceCache`
- `QueryChain`（后检索推理）：多跳 BFS 证明轨迹 + 单跳子图扩展，去重后合并到响应
- `include_cold` 查询标志通过规划器和 TieredDataPlane 完整连通，即使热层满足 TopK 也可强制冷层合并
- 算法调度：Runtime 上的 `DispatchAlgorithm`、`DispatchRecall`、`DispatchShare`、`DispatchConflictResolve`；可插拔 `MemoryManagementAlgorithm` 接口，含 `BaselineMemoryAlgorithm`（默认）和 `MemoryBankAlgorithm`（8 维治理模型）
- **MemoryBank 治理**：8 种生命周期状态（candidate→active→reinforced→compressed→stale→quarantined→archived→deleted）、冲突检测（值冲突、偏好反转、事实分歧、实体冲突）、档案管理
- 所有算法参数外化至 `configs/algorithm_memorybank.yaml` 和 `configs/algorithm_baseline.yaml`
- 安全 DLQ：带溢出缓冲区（容量 256）的 panic 恢复 + 结构化 `OverflowBuffer()` + `OverflowCount` 指标——panic 绝不会悄无声息地丢失
- 10 种嵌入向量提供方：`TfidfEmbedder`（纯 Go）、`OpenAIEmbedder`（OpenAI/Azure/Ollama/ZhipuAI）、`CohereEmbedder`、`VertexAIEmbedder`、`HuggingFaceEmbedder`、`OnnxEmbedder`、`GGUFEmbedder`（go-llama.cpp/Metal）、`TensorRTEmbedder`（存根）；ZhipuAI 和 Ollama 真实 API 测试通过
- 22 个包的模块级测试覆盖
- Python SDK（`sdk/python`）和演示脚本
- 完整架构、模式与 API 文档

## HTTP API 一览 v1

权威列表：[`Gateway.RegisterRoutes`](src/internal/access/gateway.go)。JSON 请求需 `Content-Type: application/json`。

| 分组 | 端点 |
|------|------|
| **健康检查** | `GET /healthz` |
| **管理** | `GET /v1/admin/topology` · `GET /v1/admin/storage` · `POST /v1/admin/s3/export` · `POST /v1/admin/s3/snapshot-export` · `POST /v1/admin/dataset/delete` · `POST /v1/admin/dataset/purge` |
| **核心** | `POST /v1/ingest/events` · `POST /v1/query` |
| **规范 CRUD** | `GET` / `POST`：`/v1/agents`、`/v1/sessions`、`/v1/memory`、`/v1/states`、`/v1/artifacts`、`/v1/edges`、`/v1/policies`、`/v1/share-contracts`（列表与过滤见各 handler 与集成测试） |
| **证明轨迹** | `GET /v1/traces/{object_id}` |
| **内部（Agent SDK 桥接）** | `POST`：`/v1/internal/memory/recall`、`/v1/internal/memory/ingest`、`/v1/internal/memory/compress`、`/v1/internal/memory/summarize`、`/v1/internal/memory/decay`、`/v1/internal/memory/share`、`/v1/internal/memory/conflict/resolve` |

**运维说明：** 默认开发服务下 **`/v1/admin/*` 无鉴权**，生产请仅内网访问或反向代理加鉴权。`dataset/delete` 与 `dataset/purge` 的请求体须含 `workspace_id` 且至少一个选择器字段。

## 数据集批量导入与 CLI 删除/清除（E2E）

使用 [`scripts/e2e/import_dataset.py`](scripts/e2e/import_dataset.py) 通过 `POST /v1/ingest/events` 将向量文件推送到 ANDB，或循环调用 `POST /v1/admin/dataset/delete` / `POST /v1/admin/dataset/purge`（purge 默认仅删除已软删除的行，除非传入 `--purge-include-active`）。

- **摄入无跨行事务：** 失败后可使用 `--concurrency 1` + `--checkpoint` 断点续传，并配合 `--ingest-retries` / `--retry-backoff` 应对瞬时 HTTP 错误（见脚本 `--help`）。
- **支持的文件后缀：** `.fvecs`、`.ivecs`、`.ibin`、`.fbin`、`.arrow`（`.arrow` 需要 [`requirements.txt`](requirements.txt) 中的 `pyarrow`）。
- **摄入文本中的标记：** 每个事件的 `payload.text` 包含 `dataset=<file_basename>` 和 `dataset_name:<--dataset>`，支持按文件名、数据集标签或两者组合删除。
- **`.ibin` 数据类型：** 当文件名自动检测不准确时，使用 `--ibin-dtype auto|float32|int32`。
- **示例**（若服务器非 `http://127.0.0.1:8080`，请设置 `ANDB_BASE_URL`）：

```bash
# 摄入（限制每文件行数）
python3 scripts/e2e/import_dataset.py --file /path/to/base.10M.fbin --dataset deep1B --limit 200 --workspace-id w_demo

# 删除 dry-run
python3 scripts/e2e/import_dataset.py --delete --delete-dry-run --file /path/to/base.10M.fbin --dataset deep1B --workspace-id w_demo

# 真正执行删除
python3 scripts/e2e/import_dataset.py --delete --file /path/to/base.10M.fbin --dataset deep1B --workspace-id w_demo

# 清除 dry-run（软删除之后；按数据集 + workspace 限定范围）
python3 scripts/e2e/import_dataset.py --purge --purge-dry-run --dataset deep1B --workspace-id w_demo

# 真正执行清除（默认：仅非活跃记忆）
python3 scripts/e2e/import_dataset.py --purge --file /path/to/base.10M.fbin --dataset deep1B --workspace-id w_demo
```

## 项目背景

当前大多数智能体记忆栈是以下形式之一：

1. 向量数据库 + 元数据表
2. 用于 RAG 的块存储
3. 应用级事件日志或缓存
4. 与检索执行脱钩的图层

这些方案对于 MAS 工作负载而言虽有用但不完整，MAS 需要：

- 事件驱动的状态演化
- 对象化的记忆与状态管理
- 多表示检索
- 保留溯源信息的证据返回
- 关系扩展与可追踪推导
- 版本感知的推理上下文

ANDB 将数据库视为认知基础设施，而非仅仅是存储。

## v1 设计目标

- 存储规范化认知对象，而非仅有向量或文本块。
- 通过事件与物化驱动状态演化，而非直接覆盖写入。
- 支持对对象投影的密集、稀疏和过滤感知检索。
- 返回带有溯源信息、版本历史和证明注记的结构化证据包。
- 保持契约足够稳定以支持跨模块并行开发。

## 当前架构

系统围绕三个执行层组织：

```
HTTP API (access)
    └─ Runtime (worker)
          ├─ WAL + Bus  (eventbackbone)
          ├─ MaterializeEvent → Memory / ObjectVersion / Edges  (materialization)
          ├─ PreComputeService → EvidenceFragment cache  (materialization)
          ├─ HotCache → TieredDataPlane (hot→warm→cold)  (dataplane)
          └─ Assembler.Build → BulkEdges + EvidenceCache  (evidence)
```

**摄入路径：**
`API → WAL.Append → MaterializeEvent → PutMemory + PutVersion + PutEdge → PreCompute → HotCache → TieredDataPlane.Ingest`

**查询路径：**
`API → TieredDataPlane.Search → Assembler.Build → EvidenceCache.GetMany + BulkEdges(1-hop) → QueryResponse{Objects, Edges, ProofTrace}`

代码布局：

- [`src/internal/access`](src/internal/access)：HTTP 网关（`RegisterRoutes`），摄入、查询、管理端、规范 CRUD、轨迹、内部 SDK 桥接
- [`src/internal/coordinator`](src/internal/coordinator)：9 个协调器（schema、object、policy、version、worker、memory、index、shard、query）+ 模块注册表
- [`src/internal/eventbackbone`](src/internal/eventbackbone)：WAL（`Append`/`Scan`/`LatestLSN`）、Bus、混合时钟、水位发布器、推导日志
- [`src/internal/worker`](src/internal/worker)：`Runtime.SubmitIngest` 和 `Runtime.ExecuteQuery` 连线
- [`src/internal/worker/nodes`](src/internal/worker/nodes)：14 种工作节点类型契约（数据、索引、查询、记忆提取、图、证明轨迹等）
- [`src/internal/dataplane`](src/internal/dataplane)：`TieredDataPlane`（热/温/冷）、`SegmentDataPlane` 和 `DataPlane` 接口
- [`src/internal/materialization`](src/internal/materialization)：`Service.MaterializeEvent` → `MaterializationResult{Record, Memory, Version, Edges}`；`PreComputeService`
- [`src/internal/evidence`](src/internal/evidence)：`Assembler`（缓存感知，通过 `WithEdgeStore` 实现图扩展）、`EvidenceFragment`、`Cache`
- [`src/internal/storage`](src/internal/storage)：7 个存储 + `HotObjectCache` + `TieredObjectStore`；`GraphEdgeStore` 含 `BulkEdges`/`DeleteEdge`
- [`src/internal/semantic`](src/internal/semantic)：`ObjectModelRegistry`、`PolicyEngine`、5 种查询计划类型
- [`src/internal/schemas`](src/internal/schemas)：13 种规范 Go 类型 + 查询/响应契约
- [`sdk/python`](sdk/python)：Python SDK 和启动脚本
- [`cpp`](cpp)：C++ 检索存根，用于未来高性能执行

## Worker 架构

执行层被组织为**认知数据流流水线**，分解为八层，每层有明确的职责边界和可插拔的内存实现。

### 8 层 Worker 模型

| # | 层 | Workers |
|---|---|---|
| 1 | **数据平面** — 存储与索引 | `IndexBuildWorker`、`SegmentWorker`（压缩）、`VectorRetrievalExecutor` |
| 2 | **事件/日志层** — WAL 与版本骨干 | `IngestWorker`、`LogDispatchWorker`（发布-订阅）、`TimeTick / TSO Worker` |
| 3 | **对象层** — 规范对象 | `ObjectMaterializationWorker`、`StateMaterializationWorker`、`ToolTraceWorker` |
| 4 | **认知层** — 记忆生命周期 | `MemoryExtractionWorker`、`MemoryConsolidationWorker`、`SummarizationWorker`、`ReflectionPolicyWorker`、`BaselineMemoryAlgorithm`、`MemoryBankAlgorithm` |
| 5 | **结构层** — 图与张量结构 | `GraphRelationWorker`、`EmbeddingBuilderWorker`、`TensorProjectionWorker`（可选）|
| 6 | **策略层** — 治理与约束 | `PolicyWorker`、`ConflictMergeWorker`、`AccessControlWorker` |
| 7 | **查询/推理层** — 检索与推理 | `QueryWorker`、`ProofTraceWorker`、`SubgraphExecutor`、`MicroBatchScheduler` |
| 8 | **协调层** — 多智能体交互 | `CommunicationWorker`、`SharedMemorySyncWorker`、`ExecutionOrchestrator` |

所有 Worker 实现 [`src/internal/worker/nodes/contracts.go`](src/internal/worker/nodes/contracts.go) 中定义的类型接口，通过可插拔 `Manager` 注册。`ExecutionOrchestrator`（[`src/internal/worker/orchestrator.go`](src/internal/worker/orchestrator.go)）以优先级感知的队列和背压机制向 chain 调度任务。

> **当前实现状态：** 第 1–4 层和第 5–8 层的部分功能已完整实现（含 `indexing/subgraph.go` 中的 `SubgraphExecutorWorker`）。`VectorRetrievalExecutor`、`LogDispatchWorker`、`TSO Worker`、`EmbeddingBuilderWorker`、`TensorProjectionWorker`、`AccessControlWorker`、`SharedMemorySyncWorker` 计划在 v1.x / v2+ 中实现。

### 4 条流处理链

定义于 [`src/internal/worker/chain/chain.go`](src/internal/worker/chain/chain.go)。

#### 🔴 主链 — 主写路径

```
Request
  ↓
IngestWorker           (模式验证)
  ↓
WAL.Append             (事件持久化)
  ↓
ObjectMaterializationWorker  (Memory / State / Artifact 路由)
  ↓
ToolTraceWorker        (tool_call 产物捕获)
  ↓
IndexBuildWorker       (段 + 关键词索引)
  ↓
GraphRelationWorker    (derived_from 边)
  ↓
Response
```

#### 🟡 记忆流水线链 — 六层认知管理

记忆流水线实现了设计规范中的六层记忆管理架构。每条路径遵循核心原则：**上层智能体只能消费 `MemoryView`，永远不直接访问原始对象存储或索引。**

流水线将**固定的通用基础设施**与**算法拥有的流水线 Workers** 分离：

- `AlgorithmDispatchWorker` 和 `GraphRelationWorker` 是存在于每次部署中的固定节点（`worker/cognitive/`）。
- 其他一切——提取、整合、摘要、治理——由算法拥有，位于 `worker/cognitive/<algo>/` 下。

**物化路径 — 写入时（基线算法具体示例）：**

```
Event / Interaction
  ↓
baseline.MemoryExtractionWorker       level-0 情节记忆，LifecycleState=active
  ↓
baseline.MemoryConsolidationWorker    level-0 → level-1 语义/程序性
  ↓
baseline.SummarizationWorker          level-1/level-2 压缩
  ↓
GraphRelationWorker
  ↓
AlgorithmDispatchWorker [ingest]
  ↓
baseline.ReflectionPolicyWorker
    TTL 到期    → LifecycleState = decayed
    隔离        → LifecycleState = quarantined
    置信度覆盖 · 显著性衰减
    → PolicyDecisionLog + AuditStore
```

#### 🔵 查询链 — 检索 + 推理

```
QueryRequest
  ↓
TieredDataPlane.Search (hot → warm → cold)
  ↓
Assembler.Build
  ↓
EvidenceCache.GetMany + BulkEdges（单跳图扩展）
  ↓
ProofTraceWorker       （可解释轨迹组装）
  ↓
QueryResponse{Objects, Edges, Provenance, ProofTrace}
```

**基准测试结果（2026-03-28）：**

| 测试层 | QPS | 平均延迟 | 备注 |
|--------|-----|----------|------|
| HNSW 直接（deep1B，L2）| 12,211 | 0.082 ms | C++ Knowhere，10K 向量，100 维，self-recall@1=100% |
| QueryChain E2E | 223 | 4.48 ms | 完整流水线：Search + Metadata + SafetyFilter + RRF + ProofTrace BFS |

#### 🟢 协作链 — 带治理共享的多智能体协调

记忆共享不是将记录复制到共享命名空间，而是**受控投影**——原始 Memory 保留其溯源信息和所有者；目标智能体收到经过范围过滤、策略条件处理的视图。

**核心设计原则：**

- **共享即投影，非复制** — 溯源、所有者和基础载荷保留在原始对象；目标看到的是治理条件化的视图。
- **访问边界是动态的** — `AccessGraphSnapshot` 在请求时解析可见范围，而非作为 Memory 记录上的静态 ACL 字段。
- **每次共享和投影都被审计** — `AuditStore` 记录每次共享、读取、算法更新和策略变更操作。
- **`ShareContract` 是协议单元** — 它将 `read_acl`、`write_acl`、`derive_acl`、`ttl_policy`、`consistency_level`、`merge_policy`、`quarantine_policy`、`audit_policy` 编码为一等对象。

## v1 规范对象

v1 主要对象：

- `Agent`
- `Session`
- `Event`
- `Memory`
- `State`
- `Artifact`
- `Edge`
- `ObjectVersion`

当前权威 Go 定义位于 [`src/internal/schemas/canonical.go`](src/internal/schemas/canonical.go)。

## v1 查询契约

已实现的摄入到查询路径：

`事件摄入 → 规范对象物化 → 检索投影 → 分层检索（热→温→冷）→ 单跳图扩展 → 预计算证据合并 → 结构化 QueryResponse`

每次查询返回的 `QueryResponse` 包含：

- `Objects` — 按词法得分排名的检索对象 ID
- `Edges` — 所有检索对象的单跳图邻居
- `Provenance` — 贡献的流水线阶段列表（`event_projection`、`retrieval_projection`、`fragment_cache`、`graph_expansion`）
- `Versions` — 对象版本记录（由版本感知查询填充）
- `AppliedFilters` — `PolicyEngine` 从请求中推导的过滤器
- `ProofTrace` — 响应组装过程的逐步轨迹

Go 契约位于 [`src/internal/schemas/query.go`](src/internal/schemas/query.go)。

## 快速开始

### 前置条件

- Go 工具链
- Python 3
- `pip`

### 安装 Python SDK 依赖

```bash
pip install -r requirements.txt
pip install -e ./sdk/python
```

### 启动开发服务器

```bash
make dev
```

默认监听 `127.0.0.1:8080`，可通过 `ANDB_HTTP_ADDR` 覆盖。

### 填充 mock 事件

```bash
python scripts/seed_mock_data.py
```

### 运行演示查询

```bash
python scripts/run_demo.py
```

### 运行测试

```bash
make test
```

## 集成测试

集成测试套件位于 `integration_tests/`（已 gitignore，仅用于本地开发），分为两层：

| 层 | 位置 | 测试内容 |
|---|---|---|
| **Go HTTP 测试** | `integration_tests/*_test.go` | 所有 HTTP API 路由、协议、数据流、拓扑——纯标准库，无额外依赖 |
| **Python SDK 测试** | `integration_tests/python/` | `AndbClient.ingest_event()` / `.query()` SDK 封装 + 可选 S3 数据流 |

### 通过 Docker 运行完整栈

根目录 [`docker-compose.yml`](docker-compose.yml) 启动 **MinIO**（S3 API 端口 9000），创建 `andb-integration` 桶，并以 **`ANDB_STORAGE=disk`**、**`ANDB_DATA_DIR=/data`** 和 **`S3_*` 指向 MinIO** 运行 Go 服务。服务器在容器内监听 **`0.0.0.0:8080`**，对外发布为 **http://127.0.0.1:8080**。

```bash
docker compose up -d
make integration-test
```

### 运行所有集成测试

```bash
make integration-test
```

### 运行 Go 单元测试

```bash
go test ./src/internal/... -count=1 -timeout 30s
```

### 环境变量

| 变量 | 默认值 | 说明 |
|---|---|---|
| `ANDB_BASE_URL` | `http://127.0.0.1:8080` | 所有测试使用的服务器地址 |
| `ANDB_HTTP_TIMEOUT` | `10` | HTTP 超时（秒，Python SDK）|
| `ANDB_RUN_S3_TESTS` | _（空）_ | 设为 `true` 启用 S3 数据流测试 |
| `S3_ENDPOINT` | — | MinIO/S3 host:port |
| `S3_ACCESS_KEY` | — | 访问密钥 |
| `S3_SECRET_KEY` | — | 秘密密钥 |
| `S3_BUCKET` | — | 桶名称 |
| `S3_SECURE` | `false` | 使用 TLS |
| `S3_REGION` | `us-east-1` | 区域（MinIO 忽略此项）|
| `S3_PREFIX` | `andb/integration_tests` | 对象键前缀 |

## 仓库结构

```text
agent-native-db/
├── README.md
├── README.zh-CN.md
├── assets/
├── configs/
├── cpp/
├── docs/
├── sdk/
├── scripts/
├── src/
├── tests/
├── Makefile
├── go.mod
├── pyproject.toml
└── requirements.txt
```

## 核心文档

- [架构概览](docs/architecture/overview.md)
- [主流程](docs/architecture/main-flow.md)
- [规范对象](docs/schema/canonical-objects.md)
- [查询模式](docs/schema/query-schema.md)
- [贡献指南](docs/contributing.md)
- [v1 范围](docs/v1-scope.md)

## 路线图

### v1 — 当前

- 端到端事件摄入与结构化证据查询 ✅
- 热→温→冷分层检索与 RRF 融合 ✅
- 每个 `QueryResponse` 中的单跳图扩展 ✅
- 预计算 `EvidenceFragment` 缓存在查询时合并入 `ProofTrace` ✅
- Go HTTP API（`RegisterRoutes` 共 25 条路径）、Python SDK 和集成测试套件 ✅
- 可插拔记忆治理算法（Baseline + MemoryBank）✅
- 10 种嵌入向量提供方实现（TF-IDF、OpenAI、Cohere、VertexAI、HuggingFace、ONNX、GGUF、TensorRT）✅
- `include_cold` 查询标志完整连通 ✅

### v1.x — 近期

- **DFS 冷层搜索**：冷 S3 嵌入向量的稠密向量相似度搜索（而非仅词法冷搜索）
- 与简单 top-k 检索的基准对比
- 通过 WAL `Scan` 重放实现时间旅行查询
- 多智能体会话隔离与范围执行
- MemoryBank 算法与 Agent SDK 端点集成

### v2+ — 长期

- 策略感知检索与可见性执行
- 更强的版本和时间语义
- 共享契约与治理对象
- 更丰富的图推理与证明重放
- 张量记忆算子
- 云原生分布式编排

设计哲学与贡献指南请参见 [`docs/v1-scope.md`](docs/v1-scope.md) 和 [`docs/contributing.md`](docs/contributing.md)。
