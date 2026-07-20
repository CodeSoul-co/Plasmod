# 10. 代码库、接口实现与函数调用路径

> Language: 中文 | [English](en/10-codebase-interfaces-and-call-paths.md)

---

把系统概念落到 package、文件、类型、字段、constructor、method、helper、状态和测试。

---

## 10.1. Architecture To Code Map

本页是快速索引。逐项 constructor、method、typed I/O、状态和成熟度以 [System Design Reference](14-implementation-status-gaps-and-claim-boundaries.md) 为准。

| Architecture concept | Primary code | Supporting code |
|---|---|---|
| Event source of truth | `eventbackbone.WAL` | `worker.Runtime`, consistency tracker |
| Canonical objects | `schemas/canonical.go` | `storage.ObjectStore`, coordinators |
| Materialization | `materialization.Service` | `worker/nodes` materializers |
| Retrieval projection | `dataplane` | `retrievalplane`, C++ bridge |
| Evidence | `evidence` | graph edges, versions, policies |
| Query planning | `semantic.QueryPlanner` | dataplane + evidence assembler |
| Consistency | `worker/consistency.Controller` | checkpoint, tracker, queues |
| Tiered storage | `storage/tiered.go` | Badger, S3/MinIO |
| HTTP boundary | `access.Gateway` | auth and visibility middleware |
| Process wiring | `app.BuildServer` | server lifecycle and ports |
| Runtime coordination | `worker.Runtime`, `worker/chain` | consistency controller, NodeManager, partially wired Orchestrator |
| Memory evolution | `schemas.MemoryManagementAlgorithm`, cognitive dispatcher | algorithm state, reflection, tiering, audit |
| Governance | policy/ShareContract stores and policy engine | Runtime filters, reflection, decision log |
| Collaboration | `CollaborationChain`, agent collaboration adapter | communication/conflict/microbatch workers; partial transaction integration |
| Reconciliation | replay/reindex/reset/purge fragments | no unified active Reconciliation Manager |
| Scheduling | consistency queues, Orchestrator, NodeManager, counter scheduler | no unified resource-aware Intelligent Scheduler |

若修改某概念，必须同时检查表中 primary 和 supporting code，不能只改 schema 或 route。

进一步核对：

- [Interface Implementation Registry](14-implementation-status-gaps-and-claim-boundaries.md)
- [API to Engine Matrix](14-implementation-status-gaps-and-claim-boundaries.md)
- [Claim and Test Boundary](14-implementation-status-gaps-and-claim-boundaries.md)

---

## 10.2. Bootstrap And Runtime Wiring

### 10.2.1. Entry

`src/cmd/server/main.go`：

1. 调用 `app.BuildServer`；
2. 注册 defer shutdown；
3. 调用 `app.RunServers`；
4. 处理启动错误和信号退出。

### 10.2.2. BuildServer 顺序

`src/internal/app/bootstrap.go` 依次组装：

1. clock、bus、WAL；
2. RuntimeStorage 和 cold store；
3. semantic、materialization、evidence；
4. embedder 和 DataPlane；
5. node manager 和 active coordinator hub；
6. worker Runtime；
7. consistency controller/checkpoint；
8. Gateway、transport 和可选 gRPC。

顺序体现依赖方向：Gateway 不直接创建 Badger/native index，worker 不直接读取环境变量决定 HTTP 端口。

### 10.2.3. Shutdown

关闭要停止 HTTP/gRPC 接收、Gateway background manager、consistency worker、runtime、storage/WAL 等资源。
新增后台 goroutine 必须挂到 server shutdown，不得依赖进程强制退出。

---

## 10.3. Artifact Creation

```text
artifact/tool result Event
  -> object descriptor + payload
  -> ArtifactIDOrDefault
  -> Artifact record
  -> produced_by/derived_from edges
  -> ObjectVersion
  -> optional retrieval projection for indexable text
```

大内容可以外置到 S3，Artifact 保留 URI/hash/mime/provenance。显式 object ID 在该链路被优先采用。

---

## 10.4. Batch Search

Gateway 的 `/v1/query/batch` 解码 `VectorWarmBatchQueryRequest`，校验每行维度和 lineage，再调用已注册
Warm Segment 的 native batch search。内部 transport 还提供 warm batch、serial batch 和 raw batch 变体。

原生 batch search 只负责向量候选；Go 层仍需逐 query 应用 scope、policy、canonical supplement 和 evidence。
因此该 route 返回 `VectorWarmBatchQueryResponse`，不是带 canonical Evidence 的通用 QueryResponse。

批量优化必须保持单请求等价语义，并对每项返回独立错误/状态。

---

## 10.5. Delete And Purge

```text
admin delete/purge request
  -> admin auth
  -> validate selector and scope
  -> hardDeleteManager task (where applicable)
  -> canonical/index/audit/outbox deletion stages
  -> optional cold deletion
  -> task status/metrics
```

多 store 清理不是单一分布式事务。处理器保存 task progress，以便区分排队、运行、取消、失败和完成。

---

## 10.6. Event Ingest

```text
POST /v1/ingest/events
  -> access.Gateway.handleIngest
  -> decode/normalize schemas.Event
  -> write concurrency semaphore
  -> worker.Runtime ingest
  -> consistency.Controller submit(mode)
  -> WAL.Append -> LSN
  -> materialize canonical projection
  -> retrieval projection
  -> tracker/checkpoint advance
  -> HTTP status mapping and response
```

Backpressure/paused/accepted-not-visible/projection failure 映射为 503；deadline/cancel 映射为 504/408。

---

## 10.7. Evidence Assembly

```text
ranked object IDs
  -> load canonical objects
  -> GraphEdgeStore traversal
  -> ObjectVersion lookup
  -> PolicyRecord/ShareContract filters
  -> derivation/provenance lookup
  -> GraphNode + ProofStep
  -> QueryResponse
```

Production visibility filtering happens after response construction and may remove chain/debug fields。Evidence assembler
不得把被 policy 拒绝的对象通过 Edge 侧漏。

---

## 10.8. Materialization

```text
Event + LSN
  -> materialization.Service.MaterializeEvent
  -> deterministic IDs
  -> Event/Memory/checkpoint State/optional Artifact/Edge/ObjectVersion records
  -> keyed state/object/tool specialized workers when targeted
  -> RuntimeStorage canonical projection
  -> retrieval projection
```

同一 Badger backend 中 object/edge/version 可使用一个事务。S3 和原生 index 不属于该事务，失败由 consistency
controller 记录并重试/报告。

---

## 10.9. Memory Algorithm Dispatch

```text
/v1/internal/memory/<operation>
  -> Gateway request decode
  -> active provider/profile lookup
  -> AlgorithmDispatchWorker/agent SDK service
  -> canonical Memory/algorithm state update
  -> policy/lifecycle effects
  -> response
```

Provider 实现可替换，但不能绕过 canonical storage、scope 和 Event provenance。运行时切换 profile 后，已有
algorithm state 不会自动转换，需由升级逻辑处理。

---

## 10.10. Query Execution

```text
POST /v1/query
  -> Gateway.handleQuery
  -> schemas.QueryRequest validation/defaults
  -> semantic QueryPlanner/operators
  -> DataPlane query
     -> Hot lexical/canonical candidates
     -> Warm lexical/vector segment candidates
     -> optional Cold candidates
  -> merge/filter/rank
  -> evidence assembler
  -> visibility middleware
  -> QueryResponse
```

`target_object_ids` 可能从 canonical store 补充对象；因此必须通过 `query_status` 区分 native retrieval hit 与
supplemented result。

---

## 10.11. Relation Creation

```text
Event causality/parents/relation descriptor
  -> validate source/destination/type
  -> deterministic Edge
  -> GraphEdgeStore
  -> source and destination edge indexes
  -> evidence traversal/proof trace
```

Edge 写入必须保留 scope 和 provenance。直接创建 Edge 时，Gateway 不会自动证明两端对象存在或语义正确。

---

## 10.12. Replay

```text
POST /v1/admin/replay
  -> authenticate admin request
  -> choose WAL range/checkpoint
  -> WAL.Scan
  -> Runtime reprocess Event
  -> canonical materialization
  -> retrieval projection
  -> tracker/checkpoint advance
  -> replay summary
```

FileWAL scan error必须传出。Replay 的重入正确性依赖 deterministic IDs 和 materializer 不变量。

---

## 10.13. Server Startup

```text
cmd/server.main
  -> app.BuildServer
     -> storage.BuildRuntimeStorage
     -> materialization/evidence/semantic constructors
     -> dataplane/embedder/retrieval constructors
     -> coordinator.NewHub
     -> worker.NewRuntime
     -> consistency.NewController
     -> access.NewGateway
  -> app.RunServers
     -> unified or split HTTP
     -> optional gRPC/transport
  -> signal
     -> shutdown in dependency-safe order
```

端口解析在 `app/ports.go`，存储选择在 `storage/factory.go`。定位启动问题时按此顺序检查，而不是先进入
上游 controlplane。

---

## 10.14. State Update

```text
state_update/tool_result Event
  -> normalize actor/session + state key/value
  -> materialization.Service
  -> CanonicalStateID(tenant, workspace, agent, session, key)
  -> Runtime.prepareStateMutation (deduplicate + monotonic version)
  -> ApplyCanonicalProjection(Event + AgentState + ObjectVersion snapshot)
  -> optional subscriber StateMaterializationWorker checkpoint/apply
  -> query by scope/key/latest version
```

没有显式 key 时主链更新 `last_memory_id`。同 event replay 幂等，旧 mutation 不回滚新状态。直接
`/v1/states` POST 绕过 Event/WAL/version chain，应只用于受信管理迁移。

---

## 10.15. Tier Promotion And Archive

```text
Warm object/segment
  -> admin export/archive
  -> encode canonical object + index metadata
  -> ColdObjectStore (S3/MinIO)
  -> cold diagnostics/key indexes

Cold query include_cold=true
  -> cold candidate/object read
  -> merge with hot/warm
  -> optional cache promotion
```

归档完成前不应删除 Warm。Promotion/cache 不改变 canonical object ID 和 provenance。

---

## 10.16. Component Ownership

### 10.16.1. Plasmod-owned active core

- top-level `internal/app`, `access`, `schemas`, `storage`, `materialization`, `evidence`, `semantic`；
- top-level lightweight coordinator files；
- `worker/consistency` 和 Agent-native workers；
- dataplane glue、SDK、配置和运维脚本；
- `cpp/retrieval` 的 Plasmod retrieval composition。

### 10.16.2. Upstream/vendored/compatibility areas

- `src/internal/platformpkg`：上游平台代码快照，保留独立 license；
- `src/internal/coordinator/controlplane`：大体量上游兼容控制面；
- `src/internal/eventbackbone/streamplane`：上游 stream/flush 组件；
- `cpp/vendor`：原生检索第三方源码。

这些目录的存在不等于 `app.BuildServer` 默认创建完整的分布式协调集群。判断运行使用情况必须沿构造函数和
interface 注入查看。

### 10.16.3. 修改原则

1. 新 Agent-native 逻辑放在 Plasmod-owned package；
2. 不为方便直接重写上游快照；
3. 上游修改保留来源、license 和差异说明；
4. active wrapper 与 upstream API 分离；
5. 更新依赖时先验证启动链路实际使用的子集。

---

## 10.17. Call Graph

### 10.17.1. Write

```text
Gateway.handleIngest
  -> Runtime.SubmitIngestContext
  -> consistency.Controller
  -> WAL.Append
  -> materialization.Service
  -> RuntimeStorage canonical projection
  -> DataPlane.Ingest
  -> tracker/checkpoint
```

### 10.17.2. Read

```text
Gateway.handleQuery
  -> Gateway.ServiceQueryContext
  -> semantic planner
  -> DataPlane Query hot/warm/(cold)
  -> canonical supplement/filter
  -> evidence assembler
  -> visibility middleware
```

### 10.17.3. Recovery

```text
Gateway.handleAdminReplay -> WAL.Scan -> Runtime processing -> projection -> checkpoint
```

---

## 10.18. Config Index

| Reader | Active inputs |
|---|---|
| `app/ports.go` | HTTP/gRPC address and size envs |
| `storage/factory.go` | storage mode, data dir, WAL/checkpoint, S3 envs |
| `worker/consistency` | mode, queue, workers, retry, timeout, checkpoint envs |
| `dataplane` | embedder/retrieval envs |
| memory provider loader | `configs/memory_tiering.yaml`, `configs/algorithm_*.yaml` |
| access middleware | APP_MODE, admin key |

`configs/app.yaml`、`storage.yaml`、`retrieval.yaml`、`graph.yaml` 当前不构成 BuildServer 的完整配置源。

---

## 10.19. Interface Implementation Map

本页保留 compact map；完整方法、constructor 和接线状态见 [Interface Implementation Registry](14-implementation-status-gaps-and-claim-boundaries.md)。

| Interface | Implementations/adapters |
|---|---|
| `eventbackbone.WAL` | FileWAL, InMemoryWAL |
| `storage.RuntimeStorage` | memory runtime bundle, Badger-backed runtime bundle |
| `storage.ObjectStore` | in-memory store, Badger object store, tiered wrapper |
| `storage.ColdObjectStore` | in-memory cold store, S3/MinIO store |
| `dataplane.EmbeddingGenerator` | TF-IDF, configured ONNX/provider adapter |
| `retrievalplane.SearchService` | CGO native bridge, unavailable stub |
| `semantic.QueryPlanner` | active semantic planner implementation |
| `consistency.CheckpointStore` | file checkpoint, in-memory/test stores |
| `transport.RuntimeAPI` | worker Runtime through Gateway/service adapter |
| `agent.MemoryManager` | BaselineMemoryManager HTTP adapter |
| `agent.LLMProvider`, `agent.MASProvider` | core contract only; no production implementation |
| `schemas.MemoryManagementAlgorithm` | baseline, MemoryBank-style, Zep-style plugins |
| `worker.IngestWorker.Accept` | PipelineIngestWorker; defined and tested, not wired into active Runtime |
| `schemas.GraphExpander` | no active implementation; worker chain uses SubgraphExecutorWorker |

构造和最终选择发生在 `app.BuildServer`、`storage/factory.go` 与 dataplane constructors。

---

## 10.20. Interface Index

完整接口到实现和接线状态见 [Interface Implementation Registry](14-implementation-status-gaps-and-claim-boundaries.md)。

| Domain | Interfaces |
|---|---|
| Event | WAL, ErrorAwareWAL, Bus, WatermarkReader, DerivationLogger |
| Storage | SegmentStore, IndexStore, ObjectStore, GraphEdgeStore, SnapshotVersionStore, RuntimeStorage |
| Query | DataPlane, QueryPlanner, SearchService |
| Embedding | EmbeddingGenerator, Generator, BatchEmbeddingGenerator |
| Runtime | IngestWorker, materialization workers, CheckpointStore |
| Transport | RuntimeAPI |
| Agent SDK | MemoryManager, LLMProvider, MASProvider |
| Memory algorithms | MemoryManagementAlgorithm |
| Optional capabilities | BatchEmbeddingGenerator, ColdHNSWSearcher, ColdTierDiagnosticsProvider |

具体签名见各 package `contracts.go` 或接口声明文件。

---

## 10.21. Package Dependency Graph

```text
cmd/server
  -> internal/app
      -> access -> worker/coordinator/schemas/storage
      -> transport -> worker runtime API
      -> worker -> eventbackbone/materialization/dataplane/storage
      -> semantic -> schemas
      -> evidence -> storage/schemas
      -> dataplane -> storage/retrievalplane/schemas
      -> storage -> schemas/eventbackbone
```

`schemas` 位于低层，不依赖 Gateway。`app` 是 composition root。C++ library 只通过 retrievalplane bridge 进入
Go graph。

---

## 10.22. Route Map

生成依据：`access.Gateway.RegisterMgmtRoutes`、`RegisterAPIRoutes` 和 `transport.NewServer`。

| Registry | Prefixes |
|---|---|
| Management | `/healthz`, `/v1/system`, `/v1/admin` |
| Application | `/v1/ingest`, `/v1/query`, canonical collections, `/v1/traces` |
| Runtime internal | `/v1/internal/memory`, task, plan, MAS, tool, agent, session |
| Transport internal | `/v1/internal/rpc`, `/v1/wal/stream` |

完整 route/method/stability 表见 [第 8 章的 Route Index](08-api-schema-and-sdk-reference.md)。

---

## 10.23. Storage Prefix Map

```text
seg|       retrieval segments
idx|       index metadata
obj|*|     canonical objects
edg|       graph edges
ver|       object versions
pol|       policy records
ctr|       share contracts
kpeS|      edge source index
kpeD|      edge destination index
```

源文件：`src/internal/storage/badger_stores.go`。完整对象 prefix 表见
[`../storage-key-layout.md`](10-codebase-interfaces-and-call-paths.md)。

---

## 10.24. Test Map

| Behavior | Primary test areas |
|---|---|
| Route/method/error/auth/visibility | `src/internal/access/*_test.go` |
| Bootstrap/config/ports | `src/internal/app/*_test.go`, `config/*_test.go` |
| Event normalization and schema | `src/internal/schemas/*_test.go` |
| WAL/Bus/derivation | `src/internal/eventbackbone/*_test.go` |
| Badger/memory/S3/tiering | `src/internal/storage/*_test.go` |
| Materialization IDs/objects/edges | `src/internal/materialization/*_test.go`, `worker/materialization/*_test.go` |
| Runtime and end-to-end query | `src/internal/worker/runtime*_test.go`, `e2e_query_test.go` |
| Consistency modes/checkpoint | `src/internal/worker/consistency/*_test.go` |
| Retrieval/embedding | `src/internal/dataplane/*_test.go`, `retrievalplane` tests |
| Evidence/query planner | `src/internal/evidence/*_test.go`, `semantic/*_test.go` |
| SDK | `sdk/python/tests`, `sdk/nodejs/src/index.test.js` |

---

## 10.25. Type Index

| Domain | Main types | Source |
|---|---|---|
| Event | Event, EventIdentity, EventActor, EventAccess, EventMaterialization, EventRetrieval | `schemas/dynamic_event.go` |
| Canonical | Agent, Session, Memory, State/AgentState, Artifact, Edge, ObjectVersion | `schemas/canonical.go` |
| Governance | Policy, PolicyRecord, ShareContract | `schemas/canonical.go` |
| Retrieval | RetrievalSegment, WarmVectorsIngestRequest, VectorWarmBatchQueryRequest | `schemas/*retrieval*`, `vector_batch_query.go` |
| Query | QueryRequest, QueryResponse, GraphExpandRequest/Response | `schemas/query.go` |
| Evidence | GraphNode, ProofStep, EvidenceSubgraph | `schemas/canonical.go`, evidence package |
| Runtime | MaterializationResult, IngestRecord, consistency status/checkpoint | materialization/dataplane/worker packages |

精确字段以 struct 和 JSON tag 为准。

---

## 10.26. Package Index

| Package | Active role | Entry files |
|---|---|---|
| `internal/app` | 组装依赖、启动/关闭 server | `bootstrap.go`, `ports.go`, `run.go` |
| `internal/access` | HTTP Gateway、安全和输出可见性 | `gateway.go`, `admin_auth.go`, `visibility.go` |
| `internal/schemas` | Event、canonical、query schema | `dynamic_event.go`, `canonical.go`, `query.go` |
| `internal/eventbackbone` | WAL、Bus、derivation log | `contracts.go`, file/memory WAL |
| `internal/worker` | Event runtime、物化、consistency | `runtime.go`, `consistency/*`, `nodes/*` |
| `internal/materialization` | 默认 Memory/checkpoint State/可选 Artifact/Edge/Version 派生 | `service.go` |
| `internal/storage` | RuntimeStorage、Badger、S3、tier | `contracts.go`, `factory.go`, `badger_stores.go` |
| `internal/dataplane` | embedding、warm/cold retrieval | `contracts.go`, `vectorstore.go`, `retrievalplane/*` |
| `internal/semantic` | query planning/operator | `operators.go` |
| `internal/evidence` | evidence/proof 组装 | assembler files |
| `internal/coordinator` | active object/index/policy coordinators | `hub.go`, top-level coordinator files |
| `internal/transport` | 组件 RPC 和 WAL stream | `server.go` |

更细说明见 [`packages/README.md`](10-codebase-interfaces-and-call-paths.md)。

---

## 10.27. app And access

`internal/app` 是 composition root。它读取环境配置、创建依赖、选择 unified/split listeners，并负责 shutdown。

`internal/access` 是 HTTP boundary：

- `gateway.go` 注册 route 和 handler；
- `admin_auth.go` 只保护 admin prefix；
- `visibility.go` 按 APP_MODE 过滤响应；
- write semaphore 控制并发写入；
- hard delete manager 运行后台 purge task。

Handler 应做协议转换和错误映射，不应重新实现 storage、materialization 或 query 业务。

---

## 10.28. coordinator

默认 active coordinator 由顶层 `hub.go` 及 object/memory/index/policy 等轻量 coordinator 组成，它们包裹
storage 和模块注册。

`coordinator/controlplane` 是上游兼容控制面，包含 meta/data/query/access proxy 组件。其存在支持未来/兼容
集成，但默认 BuildServer 不创建完整分布式集群。

新增 core coordination 优先扩展 active Hub contract；只有明确接入上游 lifecycle 时才修改 controlplane。

---

## 10.29. dataplane And retrieval

DataPlane 连接 embedder、lexical/vector store、tiered retrieval 和 query candidates。

`retrievalplane/bridge.go` 在 `retrieval` build tag 下调用 C++ library；stub build 让纯 Go 构建仍可运行 canonical/
lexical 路径。原生层负责 index/search，Go 层保留 scope、policy、fusion 和 evidence 语义。

预计算 query/event vector 可绕过 embedder。所有 vector path 都必须校验 dimension 和 embedding family。

---

## 10.30. eventbackbone

Active core 包括 WAL contract、FileWAL、InMemoryWAL、Bus、watermark、derivation/policy decision log。

FileWAL 位于 `<dataDir>/wal.log`，提供持久重放。InMemoryWAL 只随进程存在。Derivation log 默认位于
`<dataDir>/derivation.log`。

`eventbackbone/streamplane` 是上游兼容快照，包含 stream coordinator/node/flush pipeline 等大量代码；默认
单进程 BuildServer 不等于完整启用该子系统。

---

## 10.31. evidence And semantic

`semantic.QueryPlanner` 把 QueryRequest 转为检索/过滤操作。DataPlane 返回候选后，`evidence` 读取 canonical
object、Edge、Version、Policy 和 derivation，构建 GraphNode、ProofStep 和 provenance。

Evidence 是结构化查询输出，不是一个独立 source of truth。组装器必须尊重 scope 和 policy，且不能通过缺失
Edge 推断不存在的来源。

---

## 10.32. schemas

`internal/schemas` 是 API 和持久化共同依赖的类型层：

- `dynamic_event.go`：v0.4 嵌套 Event 和 legacy alias normalize；
- `canonical.go`：对象、关系、版本、policy、share contract；
- `constants.go`：object/event/edge/memory 类型；
- `query.go`：query/filter/evidence response；
- 其他文件：retrieval、governance、memory algorithm 和扩展类型。

Schema package 不应依赖 Gateway 或具体 Badger 实现。变更 JSON tag 前先搜索 SDK、WAL fixture、storage codec
和 handler tests。

---

## 10.33. storage

`storage/factory.go` 选择 disk 或 memory runtime bundle。Disk 模式组合 Badger canonical stores、FileWAL、
checkpoint 和可选 S3 cold store。

`contracts.go` 定义 RuntimeStorage；`badger_stores.go` 实现 prefix/key codec；`tiered.go` 组合 Hot/Warm/Cold；
`s3store.go` 管理 cold objects 和 edge indexes。

Object、Edge、Version 使用同一 Badger backend 时可通过 canonical projection transaction 原子提交。跨 S3 或
native index 不在同一事务中。

---

## 10.34. transport And SDKs

`internal/transport/server.go` 通过小型 `RuntimeAPI` interface 暴露 batch ingest、warm query、segment register
和 WAL stream，避免 transport 直接依赖 runtime concrete type。

Python SDK 是当前较完整应用客户端。Node SDK 仍保留旧包/类命名且功能有限。SDK 变更应配合 HTTP contract
test，不能将 internal transport 当公共 SDK endpoint。

---

## 10.35. Upstream Compatibility Areas

以下目录应先阅读其 license/source map 再修改：

- `src/internal/platformpkg`；
- `src/internal/coordinator/controlplane`；
- `src/internal/eventbackbone/streamplane`；
- `cpp/vendor`。

维护要求：保留 copyright/license；记录上游版本；把 Plasmod adapter 放在边界层；避免无关格式化导致巨大
diff；升级后执行 active startup、storage、retrieval 和 shutdown 验证。

---

## 10.36. worker And materialization

`worker.Runtime` 接收 Event 并协调 WAL、materialization、projection。`worker/consistency.Controller` 提供模式、
queue、slot、retry、tracker 和 checkpoint。

`materialization.Service` 负责通用 Event 到 Memory/checkpoint State/可选 Artifact/Edge/Version；`worker/nodes` 还包含 keyed state、object、
tool trace、index、proof、algorithm dispatch 等 worker contract/实现。

增加 materializer 时要明确 deterministic ID、重放重入、canonical transaction、projection failure 和 tracker
推进条件。

---

## 10.37. Repository Overview

Plasmod 核心仓库包含 Go runtime、C++ retrieval、SDK、配置、容器和工程文档。

| Path | Responsibility |
|---|---|
| `src/cmd/server` | 可执行程序入口 |
| `src/internal` | 核心 Go runtime |
| `cpp` | C++17 原生 retrieval library |
| `sdk/python` | Python HTTP SDK |
| `sdk/nodejs` | Node 兼容 SDK，能力较少 |
| `configs` | memory tier/provider 配置及参考配置 |
| `scripts` | 构建、启动、安全检查等脚本 |
| `docker-compose*.yml` | split/unified 容器拓扑 |
| `docs` | 当前核心工程文档 |

构建真值优先级：Makefile/CMakeLists/go.mod 和代码配置解析，高于注释、示例 YAML 或旧 README。

---

## 10.38. Repository Tree

```text
Plasmod/
├── src/
│   ├── cmd/server/main.go
│   └── internal/
│       ├── access/          # HTTP gateway, auth, response visibility
│       ├── app/             # dependency wiring and server lifecycle
│       ├── coordinator/     # active lightweight coordinators + upstream snapshot
│       ├── dataplane/       # embedding, retrieval, tiered query
│       ├── eventbackbone/   # WAL/bus/derivation + upstream streamplane
│       ├── evidence/        # evidence assembly
│       ├── materialization/ # event to canonical objects
│       ├── schemas/         # wire and canonical types
│       ├── semantic/        # query planning/operators
│       ├── storage/         # Badger, memory, S3, tiering
│       ├── transport/       # internal RPC/WAL stream
│       └── worker/          # runtime, materializers, consistency
├── cpp/                     # native retrieval and vendored source
├── sdk/
├── configs/
├── scripts/
├── docs/
├── Makefile
├── Dockerfile
└── docker-compose*.yml
```

运行产生的 `.andb_data`、`.gocache`、`cpp/build` 和 `bin` 不是源码模块。

---

## 10.39. Source Of Truth Map

| Question | Source of truth |
|---|---|
| Event 顺序和可回放事实 | FileWAL/InMemoryWAL records and LSN |
| 当前 canonical object | RuntimeStorage ObjectStore |
| 对象关系 | GraphEdgeStore |
| 历史版本 | SnapshotVersionStore |
| 治理决策 | PolicyStore/PolicyRecord |
| 共享协议 | ShareContractStore |
| 查询物理候选 | retrieval segment/index，属于派生层 |
| consistency 进度 | tracker + checkpoint |
| cold archive | S3/MinIO keys，显式归档后存在 |
| 实际进程配置 | env 解析结果 + effective config endpoint |

检索索引不是 canonical source of truth。索引可通过 canonical/WAL 重建，但直接 CRUD 未写 WAL 的历史不能凭空恢复。

---

## 10.40. Storage Key Layout

Badger store 使用稳定 prefix 区分逻辑表：

| Prefix | Record |
|---|---|
| `seg\|` | retrieval segment |
| `idx\|` | index metadata |
| `obj\|agent\|` | Agent |
| `obj\|session\|` | Session |
| `obj\|memory\|` | Memory |
| `obj\|state\|` | AgentState |
| `obj\|artifact\|` | Artifact |
| `obj\|event\|` | Event |
| `obj\|user\|` | User |
| `edg\|` | Edge |
| `ver\|` | ObjectVersion |
| `pol\|` | PolicyRecord |
| `ctr\|` | ShareContract |
| `kpeS\|` | source-oriented edge index |
| `kpeD\|` | destination-oriented edge index |

定义位于 `src/internal/storage/badger_stores.go`。部分 algorithm/audit/outbox 数据有各自 namespace，应通过
对应 store API 访问。

修改 prefix 会造成旧数据不可见。迁移必须双读/双写或离线转换，不能直接替换常量后发布。
