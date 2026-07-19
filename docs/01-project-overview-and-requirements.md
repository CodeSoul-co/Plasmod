# 01. 项目总览、需求与系统边界

---

统一说明 Plasmod 的问题定义、目标用户、使用场景、能力边界、术语和可追踪需求。

---

## 01.1. 能力地图

状态基于当前启动链和测试，不代表长期兼容承诺。

| 能力 | 用户入口 | 核心对象 | 主要实现 | 状态 |
|---|---|---|---|---|
| 写入 Agent Event | `POST /v1/ingest/events`, gRPC | Event | `access.Gateway`, `worker.Runtime`, WAL | Implemented |
| WAL 持久化与扫描 | Runtime/Admin | Event, LSN | `eventbackbone.FileWAL` / `InMemoryWAL` | Implemented |
| Event 物化 | Event ingest | Memory/State/Artifact/Edge/Version | `materialization.Service`, worker materializers | Implemented |
| 查询 Memory 与对象 | `POST /v1/query` | Memory 等 | planner, TieredDataPlane, Assembler | Implemented |
| 查询最新 State | `/v1/states` 或 query selector | AgentState | ObjectStore, state materializer | Partial |
| Artifact 管理 | `/v1/artifacts`, Event ingest | Artifact | object materializer, ObjectStore | Implemented |
| Relation/Edge | `/v1/edges`, query/trace | Edge | GraphEdgeStore, evidence assembler | Implemented |
| Trace 与 provenance | `GET /v1/traces/{id}` | Edge/Version/derivation | Gateway, Evidence, logs | Implemented |
| Strict/Bounded/Eventual | Event/query + admin mode | LSN/visibility | consistency.Controller/Tracker | Implemented |
| Replay | `/v1/admin/replay` | Event/Object | Runtime + WAL scan | Implemented |
| Rollback | `/v1/admin/rollback` | Version/Object | Gateway/Storage | Partial |
| Hot/Warm/Cold memory | config/query | Memory | TieredObjectStore/TieredDataPlane | Implemented |
| S3/MinIO cold tier | env + compose/admin | archived objects | S3ColdStore | Conditional |
| Hybrid retrieval | query | retrieval projection | lexical + CGO ANN + RRF | Conditional |
| Warm vector batch | HTTP internal/binary/gRPC | RetrievalSegment | transport + retrievalplane | Experimental |
| Governance policy | `/v1/policies`, query trace | PolicyRecord | PolicyEngine/PolicyStore | Partial |
| Share contract | `/v1/share-contracts` | ShareContract | ContractStore/PolicyEngine | Partial |
| Memory lifecycle | internal memory routes | Memory/AlgorithmState | cognitive workers | Experimental |
| Python SDK | `sdk/python` | HTTP objects | `PlasmodClient` | Partial |
| Node SDK | `sdk/nodejs` | consistency mode | `AndbClient` | Partial |
| gRPC | `:19531` | core ingest/query/vector | `PlasmodAPIService` | Implemented, limited surface |

### 01.1.1. 状态解释

“Partial” 常见原因包括：直接 CRUD 绕过 Event/WAL；缺少分页或完整 ACL；只覆盖一部分对象类型；或实现存在但没有稳定 SDK 映射。“Conditional” 表示需要编译标签、native library、外部服务或额外配置。

---

## 01.2. 文档地图

| 板块 | 内容 | 适合读者 |
|---|---|---|
| [00 文档入口](README.md) | 阅读顺序、角色路径与状态标签 | 所有人 |
| [01 Requirements](01-project-overview-and-requirements.md) | 问题、用例、功能/非功能要求和追踪 | 产品、架构、核心开发 |
| [02 Concepts and Design](02-system-architecture-and-design.md) | source of truth、写/读路径、一致性、失败模型和 [30 项系统设计核对](14-implementation-status-gaps-and-claim-boundaries.md) | 架构与核心开发 |
| [03 Getting Started](09-getting-started-and-user-guide.md) | 安装、启动、首次请求、验证、清理 | 新用户 |
| [04 User Guide](09-getting-started-and-user-guide.md) | 按用户任务使用每项核心能力 | 应用开发者 |
| [05 API and Reference](08-api-schema-and-sdk-reference.md) | HTTP/gRPC/binary、schema、配置与错误 | 集成开发者 |
| [06 Codebase](10-codebase-interfaces-and-call-paths.md) | 仓库、package、interface、调用链、存储 key | 核心开发者 |
| [07 Dependencies](11-dependencies-build-and-development.md) | native stack、Badger、S3、embedder 与升级边界 | 构建与维护人员 |
| [08 Development](11-dependencies-build-and-development.md) | 构建、调试、测试、贡献和常见修改 | 贡献者 |
| [09 Operations](12-deployment-operations-and-troubleshooting.md) | 部署、安全、监控、恢复与排障 | 运维人员 |
| [10 Extensibility](13-extensibility-compatibility-and-evolution.md) | 新 schema、materializer、operator、backend | 扩展开发者 |
| [11 Evolution](14-implementation-status-gaps-and-claim-boundaries.md) | 成熟度、兼容、迁移和限制 | 发布与维护人员 |

### 01.2.1. 代码事实入口

- 启动：`src/cmd/server/main.go`, `src/internal/app/bootstrap.go`
- HTTP：`src/internal/access/gateway.go`
- gRPC：`src/internal/api/grpc/proto/plasmod/v1/plasmod.proto`
- Event schema：`src/internal/schemas/dynamic_event.go`
- canonical schema：`src/internal/schemas/canonical.go`
- runtime：`src/internal/worker/runtime.go`, `runtime_consistency.go`
- storage：`src/internal/storage/contracts.go`, `factory.go`
- retrieval：`src/internal/dataplane/`, `src/internal/dataplane/retrievalplane/`, `cpp/`
- consistency：`src/internal/worker/consistency/`

### 01.2.2. 详细设计入口

| 需要核对的内容 | 文档 |
|---|---|
| 3 Architecture + 4 Chains + 5 Perspectives + 8 Mechanisms + 10 Engines | [System Design Reference](14-implementation-status-gaps-and-claim-boundaries.md) |
| canonical/Event/Query/Worker 字段与 typed I/O | [Object and Message Registry](14-implementation-status-gaps-and-claim-boundaries.md) |
| interface、实现、constructor、接线状态 | [Interface Implementation Registry](14-implementation-status-gaps-and-claim-boundaries.md) |
| route 到 Chain/Engine 的真实映射 | [API to Engine Matrix](14-implementation-status-gaps-and-claim-boundaries.md) |
| 同步/异步、阶段、失败窗口和恢复 | [Execution State and Failure Matrix](14-implementation-status-gaps-and-claim-boundaries.md) |
| 可以声明与不可过度声明的能力 | [Claim and Test Boundary](14-implementation-status-gaps-and-claim-boundaries.md) |

---

## 01.3. 定位与边界

### 01.3.1. 与 Vector + Metadata 的区别

Vector + Metadata 通常以“向量行”为主要事实，更新、版本、状态和关系由应用自行编码。Plasmod 将 Event、Memory、AgentState、Artifact、Edge 与 ObjectVersion 放入 canonical object model；向量、稀疏和 lexical index 是可以从 canonical 数据重建的 retrieval projection。

这种区别不意味着 Plasmod 的 ANN 内核天然优于专用向量数据库。Plasmod 的核心价值是 agent 对象语义、可见性、恢复、版本和 evidence 组合；ANN 部分由 Plasmod bridge 与第三方引擎共同提供。

### 01.3.2. 与普通向量数据库的关系

Plasmod 可以接收预计算向量并构建 warm segment，也可以使用 TF-IDF 或外部/本地 embedder 产生向量。它同时维护 canonical object、WAL 和关系/版本存储。专用向量数据库通常提供更成熟的分布式 ANN、集群管理和生态；Plasmod 当前不承诺这些能力的完全等价。

### 01.3.3. 与 Agent Framework 的关系

Plasmod 不是 agent planner、LLM 调用器或工具执行框架。Framework 负责决定何时产生 event、如何执行任务和如何消费查询结果；Plasmod 负责接收结构化 runtime 数据、持久化、物化、检索与恢复。`src/internal/agent/` 提供接入抽象，但不构成完整 framework runtime。

### 01.3.4. 与 MemoryBank/Zep 类算法的关系

MemoryBank、Zep 和 baseline profile 位于 `src/internal/worker/cognitive/`，通过 algorithm dispatcher 影响 memory lifecycle、recall 或 graph processing。它们是可插拔策略，不是 canonical storage 的替代品。算法状态通过 `MemoryAlgorithmStateStore` 保存，canonical Memory 仍由 Plasmod storage 所有。

### 01.3.5. Plasmod 负责

- Event 接受、WAL 顺序和 replay 输入。
- Event 到 canonical object 的 projection。
- canonical object、edge、version、policy、contract 和算法状态存储。
- retrieval projection、query planning 和 structured evidence assembly。
- consistency admission、可见性等待、checkpoint 和恢复。
- hot/warm/cold 路由以及可选 S3/MinIO 接入。
- HTTP、内部 binary/SSE、gRPC 和 SDK 接口边界。

### 01.3.6. 交给外部组件

- LLM 推理、tool execution 和 agent orchestration。
- OpenAI、Cohere、Vertex AI、Hugging Face 等 embedding provider 的服务可用性。
- S3 服务本身的 durability、replication、IAM 和生命周期策略。
- Knowhere/FAISS/DiskANN、ONNX Runtime、llama.cpp、TensorRT 的内部算法与 ABI。
- TLS、WAF、完整身份系统和网络隔离。

### 01.3.7. 明确的非目标或未保证能力

- 不提供跨所有对象和外部系统的全局 ACID 事务。
- 不保证所有 materializer 的 exactly-once 副作用。
- 不提供完整分布式集群控制面的生产承诺。
- 不提供默认开启的用户级认证；只有 admin shared-key middleware。
- 不保证所有配置 YAML 都进入当前启动路径。
- 不保证每种平台、GPU、ANN backend 和 index 文件之间的二进制兼容。

更精确的约束见 [Constraints and Non-goals](01-project-overview-and-requirements.md)。

---

## 01.4. 项目总览

### 01.4.1. 一句话定位

Plasmod 是面向 agent runtime 的数据库核心：以事件作为写入入口，将 Memory、AgentState、Artifact、Edge 和 ObjectVersion 物化为一等对象，并在查询阶段返回对象、版本、关系、provenance 与 proof trace 组成的结构化证据。

### 01.4.2. 要解决的问题

长时间运行的 agent 不只需要“找到相似文本”，还需要回答：某条信息来自哪个事件、当前状态是哪一版、对象之间是什么关系、谁可以看到它、写入何时可见，以及服务重启后如何恢复。仅把文本、向量和 metadata 放进一个扁平集合，无法自然表达这些约束。

Plasmod 的核心路径因此分为：

1. Gateway 接收 Event 或 canonical object 请求。
2. Event 写入 WAL，获得单调 LSN。
3. consistency controller 按 strict、bounded 或 eventual 模式安排 projection。
4. materialization 将 Event 派生为 Memory、AgentState、Artifact、Edge 与 ObjectVersion。
5. canonical store 保存对象事实；retrieval plane 保存可重建的 lexical/vector projection。
6. query planner 执行检索和过滤，evidence assembler 补充关系、版本与证明链。

真实入口分别位于 `src/internal/access/gateway.go`、`src/internal/worker/runtime_consistency.go`、`src/internal/worker/runtime.go`、`src/internal/materialization/`、`src/internal/storage/` 和 `src/internal/evidence/assembler.go`。

### 01.4.3. 主要使用者

- Agent framework 开发者：将 runtime event、session、state 和 artifact 接入统一存储。
- Agent 应用开发者：通过 HTTP、gRPC 或 Python SDK 写入和查询。
- 平台运维人员：配置持久化、S3/MinIO、可见性模式、恢复和管理 API。
- 核心开发者：扩展 schema、materializer、query operator、storage 或 retrieval backend。

### 01.4.4. 当前核心能力

- Dynamic Event v0.4 输入及旧扁平事件输入兼容。
- file/in-memory WAL、LSN 扫描、恢复 checkpoint 与 replay admin API。
- Memory、State、Artifact、Edge、Version、Policy、ShareContract 的 canonical 存储。
- strict、bounded staleness、eventual visibility 三种 runtime 一致性模式。
- hot/warm/cold 对象路径；Badger 持久化；可选 S3/MinIO cold store。
- lexical + 可选 dense/sparse ANN retrieval；C++ bridge 不可用时降级。
- structured evidence response、1-hop edge expansion、version 与 proof trace。
- unified/split HTTP 监听和独立 gRPC 监听。
- Python SDK；Node SDK 当前只覆盖一致性模式控制。

### 01.4.5. 当前状态

核心 Event ingest、query、canonical store、Badger、WAL 和 consistency controller 为 **Implemented**。权限隔离、通用认证、分页、跨对象事务、完整 SDK 覆盖和生产级备份编排为 **Partial**。可选 ANN backend、GPU、TensorRT、MemoryBank/Zep profile 等属于 **Experimental** 或条件能力，不能默认视为所有构建都具备。

### 01.4.6. 最短阅读路径

继续阅读 [定位与边界](01-project-overview-and-requirements.md)、[能力地图](01-project-overview-and-requirements.md)、[设计总览](02-system-architecture-and-design.md) 和 [Quickstart](09-getting-started-and-user-guide.md)。

---

## 01.5. 术语表

| 术语 | 工程定义 |
|---|---|
| Event | agent runtime 中一次结构化事实或动作；`schemas.Event` 使用 Dynamic Event v0.4 canonical JSON。 |
| WAL | Event 的接受顺序与 replay 来源；`Append` 返回 LSN，`Scan` 从指定 LSN 读取。 |
| LSN | WAL 内单调递增的 log sequence number，不等同于 wall-clock 时间。 |
| Canonical Object | 可持久化的权威对象表示，如 Memory、AgentState、Artifact。 |
| Memory | 从 Event 物化的知识对象，带类型、scope、版本、生命周期与来源 Event。 |
| AgentState | 某 agent/session/state key 的当前值；Go 类型名仍为 `State`，`AgentState` 是别名。 |
| Artifact | agent 产生的外部或内联产物，如文本、报告、代码或工具结果。 |
| Edge | 两个 canonical object 之间的有向类型关系。 |
| ObjectVersion | 对象版本/快照记录，保存 mutation event 与有效时间。 |
| Materialization | 将已接受 Event 转换为 canonical object、edge 和 version 的过程。 |
| Canonical Projection | 一次 Event 对应的 Event、Memory、checkpoint State、可选 Artifact、Edges 和 Versions 规范写入集合。 |
| Retrieval Projection | 从 canonical object 派生的 lexical/dense/sparse index，可重建，不是 source of truth。 |
| Evidence | 查询命中的对象加上 edge、version、provenance、proof trace 和过滤说明。 |
| Proof Trace | planner、retrieval、policy、tier、graph 与 derivation 步骤组成的可解释链。 |
| Watermark | runtime 已完成 projection、可用于可见性判断的最高 LSN。 |
| Strict | 写请求等待对应 LSN projection 可见后返回。 |
| Bounded Staleness | 写入按 freshness SLA 排队并在 deadline 前推进，读取可等待 watermark。 |
| Eventual | WAL 接受后允许异步 projection，读取只保证最终推进。 |
| Hot | 高显著度/近期对象的进程内 cache 与快速 index。 |
| Warm | canonical object store 与主要 retrieval segment 所在层。 |
| Cold | 显式归档对象所在的 S3/MinIO 或 in-memory cold store。 |
| RRF | Reciprocal Rank Fusion，用于合并多路检索排名。 |
| Vector-only mode | 关闭 graph/policy/provenance 的条件模式；不代表完整 Plasmod 语义。 |
| Source of Truth | 恢复和冲突判断时的权威事实来源；Event 为因果源，canonical store 为当前对象事实。 |

---

## 01.6. 约束与非目标

### 01.6.1. 一致性与事务

- Event ingest 有 WAL 和 projection 一致性控制，但 canonical direct CRUD 路由不全部经过 WAL。
- 不提供跨 Badger、S3、external embedder 和 native index 的全局 ACID 事务。
- 不保证所有异步 materializer 和外部副作用 exactly once；实现以 LSN、checkpoint、deterministic ID 和 retry 降低重复影响。

### 01.6.2. 系统边界

- Plasmod 不是 agent framework、LLM gateway、tool executor 或 workflow scheduler。
- Plasmod 不实现 S3、Badger、Knowhere、FAISS、DiskANN、ONNX Runtime 或 TensorRT 的内部算法。
- `coordinator/controlplane`、`eventbackbone/streamplane`、`platformpkg` 中的大量上游兼容代码不等于当前 BuildServer 已启用完整分布式控制面。

### 01.6.3. API 与安全

- 当前没有统一用户认证、OAuth/OIDC、细粒度 RBAC 或 TLS termination。
- `PLASMOD_ADMIN_API_KEY` 只保护 `/v1/admin/*`；公共与 internal data routes 仍需网络边界保护。
- 列表型 CRUD 缺少统一 pagination/cursor contract。
- `/v1/internal/*` 不是稳定公开 API。

### 01.6.4. 存储与部署

- 默认启动为单进程；disk mode 默认目录为 `.andb_data`。
- S3 cold tier 是可选归档 backend，不替代 canonical Badger/WAL 的所有职责。
- index、WAL 和 Badger format 的跨版本迁移工具仍不完整。
- Docker Compose 使用开发凭据，不能直接视为生产安全配置。

### 01.6.5. SDK 与可选能力

- Python SDK 仅覆盖部分 HTTP；Node SDK 当前主要覆盖 consistency mode。
- gRPC 只覆盖 health、event/vector ingest 和 query/batch query。
- ANN、GPU、ONNX、GGUF、TensorRT 和部分 index type 依赖 build tag/native library/platform。
- MemoryBank/Zep profile 的存在不代表所有论文算法或外部产品行为已完整复现。

---

## 01.7. 功能需求

下表采用可追踪的精简格式；详细代码/测试映射见 [Requirements Traceability](01-project-overview-and-requirements.md)。

### 01.7.1. FR-ING-001 Event 接受

#### 01.7.1.1. Requirement
系统必须接受 Dynamic Event v0.4，并兼容已实现的旧扁平输入别名；缺失 event ID 时生成 ID。

#### 01.7.1.2. Rationale
统一新写入语义，同时保留迁移能力。

#### 01.7.1.3. Inputs
`schemas.Event` JSON。

#### 01.7.1.4. Expected Behavior
规范化后写入 WAL，返回 event ID、LSN 和 projection/visibility 结果。

#### 01.7.1.5. Failure Behavior
无效 JSON、无效 consistency 或写入 backpressure 返回非 2xx。

#### 01.7.1.6. Acceptance Criteria
gateway 与 dynamic event tests 通过。

#### 01.7.1.7. Current Status
Implemented。

#### 01.7.1.8. Related Code
`schemas/dynamic_event.go`, `access/gateway.go`, `worker/runtime_consistency.go`。

### 01.7.2. FR-ORD-001 Event 顺序与 replay

#### 01.7.2.1. Requirement
每个已接受 Event 必须获得 LSN，并可按 LSN 扫描；disk mode 必须使用 file WAL。

#### 01.7.2.2. Rationale
为恢复与 visibility watermark 提供统一顺序。

#### 01.7.2.3. Inputs
Event 和 `from_lsn`。

#### 01.7.2.4. Expected Behavior
`Append`, `Scan`, `LatestLSN` 遵守 WAL contract。

#### 01.7.2.5. Failure Behavior
持久 WAL 解码/IO 错误必须传播。

#### 01.7.2.6. Acceptance Criteria
WAL recovery/corruption tests 通过。

#### 01.7.2.7. Current Status
Implemented。

#### 01.7.2.8. Related Code
`eventbackbone/contracts.go`, `wal.go`, `wal_file.go`。

### 01.7.3. FR-MAT-001 Canonical materialization

#### 01.7.3.1. Requirement
Event 必须按类型派生 Memory、AgentState 或 Artifact，并保存相关 Edge 与 ObjectVersion。

#### 01.7.3.2. Rationale
避免把 agent 对象压缩为不可解释的向量行。

#### 01.7.3.3. Inputs
规范化 Event。

#### 01.7.3.4. Expected Behavior
canonical projection 在共享 Badger backend 时原子提交对象/edge/version 集合。

#### 01.7.3.5. Failure Behavior
projection 失败不得推进 visible checkpoint；controller 按配置重试。

#### 01.7.3.6. Acceptance Criteria
materialization、canonical projection 和 consistency tests 通过。

#### 01.7.3.7. Current Status
Implemented；部分 direct CRUD 不经过该路径。

#### 01.7.3.8. Related Code
`materialization/service.go`, `storage/canonical_projection.go`, worker materializers。

### 01.7.4. FR-STA-001 State 更新

#### 01.7.4.1. Requirement
相同 agent/session/state key 的更新必须指向稳定 State ID 并递增版本。

#### 01.7.4.2. Rationale
查询“当前状态”时需要确定性覆盖语义。

#### 01.7.4.3. Inputs
state update/change/checkpoint Event。

#### 01.7.4.4. Expected Behavior
State materializer 更新 State，checkpoint 生成 ObjectVersion。

#### 01.7.4.5. Failure Behavior
缺少 state key 时不生成 State；不得生成重复的竞争 State ID。

#### 01.7.4.6. Acceptance Criteria
state materialization tests 通过。

#### 01.7.4.7. Current Status
Implemented，跨进程 state key map 恢复语义为 Partial。

#### 01.7.4.8. Related Code
`worker/materialization/state.go`。

### 01.7.5. FR-RET-001 Retrieval 与结构化查询

#### 01.7.5.1. Requirement
系统必须支持 query text、scope、object/memory type、time window、target IDs、cold tier 和 precomputed embedding 等过滤/操作符。

#### 01.7.5.2. Rationale
agent 查询需要语义检索与精确 selector 共存。

#### 01.7.5.3. Inputs
`schemas.QueryRequest`。

#### 01.7.5.4. Expected Behavior
planner 生成 SearchInput；dataplane 返回 candidates；assembler 输出 objects、edges、versions、provenance 和 proof trace。

#### 01.7.5.5. Failure Behavior
无效 selector 或 backend 错误返回明确错误；零命中不是 transport failure。

#### 01.7.5.6. Acceptance Criteria
query、tiered adapter、evidence tests 通过。

#### 01.7.5.7. Current Status
Implemented；部分 access filter 为 Partial。

#### 01.7.5.8. Related Code
`semantic/operators.go`, `worker/runtime.go`, `evidence/assembler.go`。

### 01.7.6. FR-CON-001 可控一致性

#### 01.7.6.1. Requirement
系统必须支持 strict、bounded staleness 和 eventual visibility，并允许 Event/Query 覆盖默认模式。

#### 01.7.6.2. Rationale
不同 agent 操作需要在 latency、throughput 和 freshness 之间明确选择。

#### 01.7.6.3. Inputs
runtime config、Event access、Query access consistency。

#### 01.7.6.4. Expected Behavior
controller 负责 admission、queue、retry、watermark、checkpoint 和 query wait。

#### 01.7.6.5. Failure Behavior
queue full、paused、deadline、accepted-not-visible 和 projection failure 必须可区分。

#### 01.7.6.6. Acceptance Criteria
consistency controller/tracker tests 通过。

#### 01.7.6.7. Current Status
Implemented。

#### 01.7.6.8. Related Code
`worker/consistency/`, `runtime_consistency.go`。

### 01.7.7. FR-GOV-001 Governance 与共享

#### 01.7.7.1. Requirement
系统应保存 PolicyRecord、ShareContract 和 AuditRecord，并在 evidence/query 阶段应用基础约束。

#### 01.7.7.2. Rationale
多 agent 数据需要可追踪的共享与生命周期决策。

#### 01.7.7.3. Inputs
policy、contract、memory operation。

#### 01.7.7.4. Expected Behavior
append policy/audit，按 object/scope 读取，在 trace 中暴露治理说明。

#### 01.7.7.5. Failure Behavior
不得把基础 policy engine 当作完整身份认证或授权系统。

#### 01.7.7.6. Acceptance Criteria
storage/governance tests 通过。

#### 01.7.7.7. Current Status
Partial。

#### 01.7.7.8. Related Code
`semantic/policy.go`, storage policy/contract/audit stores。

### 01.7.8. FR-OPS-001 删除、purge 与恢复

#### 01.7.8.1. Requirement
系统必须区分 logical delete、hard purge、data wipe 和 replay；跨 tier 删除应清理对象、edge、segment ref 与 cold data。

#### 01.7.8.2. Rationale
生命周期操作必须可审计且不留下可检索孤儿。

#### 01.7.8.3. Inputs
dataset/source/memory selector 和 admin credential。

#### 01.7.8.4. Expected Behavior
批处理删除、后台 hard-delete、audit/outbox 和状态查询。

#### 01.7.8.5. Failure Behavior
部分失败必须可观测，不得静默报告全量成功。

#### 01.7.8.6. Acceptance Criteria
purge、hard delete、wipe 和 replay tests 通过。

#### 01.7.8.7. Current Status
Implemented/Partial，取决于 backend 和操作。

#### 01.7.8.8. Related Code
`access/hard_delete_manager.go`, `storage/purge_warm.go`, admin handlers。

### 01.7.9. FR-SDK-001 SDK 与 transport

#### 01.7.9.1. Requirement
核心 ingest/query 应通过 HTTP 与 gRPC 可用；SDK 字段必须与 schema 保持一致。

#### 01.7.9.2. Rationale
避免调用方依赖内部 Go package。

#### 01.7.9.3. Inputs
JSON、protobuf 或 row-major binary payload。

#### 01.7.9.4. Expected Behavior
transport 映射到同一 Gateway service methods。

#### 01.7.9.5. Failure Behavior
协议错误应映射为 transport-level error，不能 panic。

#### 01.7.9.6. Acceptance Criteria
gRPC、framing、Python/Node tests 通过。

#### 01.7.9.7. Current Status
HTTP Implemented；gRPC limited；SDK Partial。

#### 01.7.9.8. Related Code
`gateway_rpc.go`, `api/grpc/`, `transport/`, `sdk/`。

---

## 01.8. 非功能需求

本文定义工程要求，不给出基准结果。

| ID | 类别 | 要求 | 当前证据/边界 |
|---|---|---|---|
| NFR-PERF-001 | Performance | 写入必须有有界 admission；retrieval index build 不应在每次写入做全量同步重建。 | Gateway semaphore、consistency queue、background flush。 |
| NFR-FRESH-001 | Freshness | 系统必须区分 WAL accepted、object visible 和 retrieval visible，并暴露 watermark/lag。 | consistency tracker/controller。 |
| NFR-COR-001 | Correctness | canonical object、edge、version 的共享持久 backend 必须支持原子 projection。 | factory 强制 objects/edges/versions 同 backend；Badger transaction。 |
| NFR-DUR-001 | Durability | disk mode 的 WAL 与 canonical store 必须可在进程重启后读取。 | FileWAL + Badger；需要外部备份策略。 |
| NFR-AVL-001 | Availability | 可恢复错误应返回明确状态；shutdown 必须停止 admission、drain worker 并关闭存储。 | controller lifecycle 与 ServerBundle.Shutdown。 |
| NFR-SCL-001 | Scalability | queue、worker、write semaphore 和 batch 接口必须可配置；单机实现不得被描述为已验证分布式集群。 | env config；当前核心启动为单进程。 |
| NFR-SEC-001 | Security | admin route 在部署时必须启用 shared key 或由反向代理保护；生产响应不得暴露 debug/raw 字段。 | admin auth + visibility middleware。 |
| NFR-ISO-001 | Tenant isolation | tenant/workspace/session 标识必须贯穿 Event、Query 和对象；服务端必须明确哪些路径实际强制过滤。 | schema 完整，enforcement 为 Partial。 |
| NFR-OBS-001 | Observability | 必须提供 health、admin metrics、topology、storage/config 状态和可辨认错误。 | HTTP management routes 与 runtime stats。 |
| NFR-MNT-001 | Maintainability | source of truth、projection、third-party ownership 和 extension registration 必须文档化。 | 本文档体系与 package contracts。 |
| NFR-COMP-001 | Compatibility | API、schema、WAL、storage key、embedding family 和 native ABI 变更必须有迁移/回滚说明。 | evolution docs；当前版本化能力不完整。 |
| NFR-PORT-001 | Portability | pure Go/lexical 路径应在无 native bridge 时可启动；native/GPU 能力按平台声明。 | retrieval stub、build tags、CMake options。 |

### 01.8.1. 验证原则

功能测试证明行为，不替代生产容量、故障注入、安全审计或平台认证。任何性能、可用性或扩展性结论都必须在独立验证中产生，不能写进核心功能文档作为既定事实。

---

## 01.9. 问题定义

### 01.9.1. 扁平 memory store 的不足

当 agent memory 只是一组文本、向量和 metadata 时，系统可以完成相似度检索，却难以稳定回答对象版本、因果链、状态覆盖、共享范围和恢复顺序。应用层往往被迫维护第二套 event log、state table 和 provenance graph，导致事实分散。

### 01.9.2. 动态状态的问题

tool result、plan update 和 checkpoint 会持续修改 agent state。直接覆盖一个 metadata 字段会丢失 mutation event、版本、可见时刻和恢复依据。并发写入还需要明确哪个 LSN 已经物化、查询是否允许看到旧状态。

### 01.9.3. Multi-agent scope 与 provenance

多个 agent 共享 workspace 时，同一个 memory 可能是 private、session、team 或 shared。仅用一个 `scope` 字符串无法表达 visible agents、roles、policy tags、share contract 和派生权限。查询结果还需要说明“为什么返回”和“由什么关系支持”。

### 01.9.4. Plasmod 的工程回答

- Event：记录因果输入和接受顺序。
- WAL/LSN：提供 replay 与可见性推进基准。
- Canonical Object：保存当前可查询事实。
- ObjectVersion：保存对象历史边界。
- Edge/Derivation：保存关系和来源。
- Retrieval Projection：为 query 提供速度，但允许从 canonical data 重建。
- Evidence Response：将命中对象、过滤、版本、边和 proof trace 一起返回。

这些能力必须在 runtime、storage、query 与 recovery 中保持同一套不变量，而不是由单个 SDK 或上层 framework 临时拼接。

---

## 01.10. 需求追踪矩阵

| Requirement | Design | Module/API | Test | Status |
|---|---|---|---|---|
| FR-ING-001 | Event-first, write path | `schemas/dynamic_event.go`; `POST /v1/ingest/events` | dynamic event/gateway tests | Implemented |
| FR-ORD-001 | Source of truth, failure model | `eventbackbone/*wal*`; admin replay | WAL tests | Implemented |
| FR-MAT-001 | Event-to-object, canonical projection | materialization + storage projection | materialization/projection tests | Implemented |
| FR-STA-001 | Canonical object/version model | state materializer; `/v1/states` | state/runtime tests | Implemented/Partial |
| FR-RET-001 | Query path, evidence | semantic/dataplane/evidence; `/v1/query` | query/evidence/tiered tests | Implemented |
| FR-CON-001 | Consistency model | worker/consistency; admin mode | controller/tracker tests | Implemented |
| FR-GOV-001 | Security/policy model | semantic policy + policy/contract/audit stores | governance tests | Partial |
| FR-OPS-001 | Failure/recovery/lifecycle | admin delete/purge/wipe/replay | access/storage/runtime tests | Implemented/Partial |
| FR-SDK-001 | Transport model | HTTP/gRPC/binary + SDK | gRPC/framing/SDK tests | Partial |
| NFR-FRESH-001 | Consistency/watermark | tracker/controller | consistency tests | Implemented |
| NFR-COR-001 | Canonical atomicity | storage factory/projection | Badger projection tests | Implemented for shared Badger backend |
| NFR-SEC-001 | Security model | admin auth/visibility | auth/visibility tests | Partial |
| NFR-PORT-001 | Dependency/build model | retrieval stub/build tags | standard and tagged builds | Partial |

### 01.10.1. 维护规则

新增或修改功能时，应同步更新 requirement、设计文档、route/schema reference、代码映射和至少一项测试。只有接口存在但启动链不可达时，状态必须写为 Not Confirmed，而不是 Implemented。

---

## 01.11. Stakeholders 与 Use Cases

### 01.11.1. UC-001: Framework 写入 runtime event

#### 01.11.1.1. Actor
Agent framework developer。

#### 01.11.1.2. Goal
将 observation、tool result、state update 或 artifact 作为 Event 写入，并获得接受/可见状态。

#### 01.11.1.3. Preconditions
服务健康；Event 至少有 actor、event type 和 payload；所选 consistency mode 有效。

#### 01.11.1.4. Main Flow
Framework 调用 `/v1/ingest/events`；Gateway 规范化 v0.4；WAL 分配 LSN；controller 执行 projection；返回 event/object/visibility ack。

#### 01.11.1.5. Alternative Flow
eventual 模式先返回 WAL 接受；projection 后台完成。队列满、runtime paused 或 projection 失败时返回可重试错误。

#### 01.11.1.6. Data Written
Event、Memory/State/Artifact、Edge、ObjectVersion、retrieval projection。

#### 01.11.1.7. Data Queried
Query、canonical CRUD、trace。

#### 01.11.1.8. Consistency Requirement
由 Event `access.consistency` 覆盖 runtime default。

#### 01.11.1.9. Failure Expectation
WAL 接受后但未可见必须以明确错误/状态区分，不能伪装成未写入。

#### 01.11.1.10. Related API
`POST /v1/ingest/events`, `GET/POST /v1/admin/consistency-mode`。

### 01.11.2. UC-002: Tool-use agent 查询最新状态

#### 01.11.2.1. Actor
Tool-use agent。

#### 01.11.2.2. Goal
在连续 tool result 和 state update 后读取指定 agent/session 的最新 State。

#### 01.11.2.3. Preconditions
state event 带 `object.state_key` 或兼容 payload 字段。

#### 01.11.2.4. Main Flow
写入 state update；State materializer 按 agent + state key 生成稳定 state ID 并递增 version；客户端查询 `/v1/states` 或结构化 query selector。

#### 01.11.2.5. Alternative Flow
eventual 模式下客户端等待/重试或使用 strict read。

#### 01.11.2.6. Data Written
Event、State、derivation、可选 ObjectVersion checkpoint。

#### 01.11.2.7. Data Queried
State list/latest selector。

#### 01.11.2.8. Consistency Requirement
决策关键路径使用 strict；容忍旧值时使用 bounded/eventual。

#### 01.11.2.9. Failure Expectation
不得把“未找到”与“已接受但尚未物化”混为一类。

#### 01.11.2.10. Related API
`POST /v1/ingest/events`, `GET /v1/states`, `POST /v1/query`。

### 01.11.3. UC-003: Research agent 获取 evidence

#### 01.11.3.1. Actor
Research agent。

#### 01.11.3.2. Goal
查询 memory，同时获得来源 event、关系、版本与 proof trace。

#### 01.11.3.3. Preconditions
查询 scope 与 object filter 合法；相关对象已进入 canonical/retrieval store。

#### 01.11.3.4. Main Flow
调用 `/v1/query`；planner 生成 SearchInput；tiered dataplane 检索；assembler 扩展 edge/version/provenance。

#### 01.11.3.5. Alternative Flow
设置 `target_object_ids` 走 canonical selector；设置 `include_cold` 扩展到归档层。

#### 01.11.3.6. Data Written
无；evidence cache 可在 ingest 阶段预计算。

#### 01.11.3.7. Data Queried
Memory、Edge、ObjectVersion、PolicyRecord、EvidenceFragment。

#### 01.11.3.8. Consistency Requirement
查询 mode 决定是否等待可见 watermark。

#### 01.11.3.9. Failure Expectation
零 retrieval hit 与 canonical supplement 必须由 `query_status` 区分。

#### 01.11.3.10. Related API
`POST /v1/query`, `GET /v1/traces/{object_id}`。

### 01.11.4. UC-004: Multi-agent 共享与冲突处理

#### 01.11.4.1. Actor
Multi-agent runtime。

#### 01.11.4.2. Goal
表达 owner、visibility、share contract 与冲突关系。

#### 01.11.4.3. Preconditions
agent、workspace 和 contract 标识稳定。

#### 01.11.4.4. Main Flow
写入共享 memory/contract；policy 与 access filter 参与查询；冲突通过 internal memory route 和 edge/audit 记录。

#### 01.11.4.5. Alternative Flow
未配置 contract 时只使用基础 visibility/ACL 判断。

#### 01.11.4.6. Data Written
Memory、ShareContract、PolicyRecord、Edge、AuditRecord。

#### 01.11.4.7. Data Queried
scope-aware query 与 trace。

#### 01.11.4.8. Consistency Requirement
关键共享决策使用 strict。

#### 01.11.4.9. Failure Expectation
当前 ACL 是基础实现，上层仍需身份认证和租户边界保护。

#### 01.11.4.10. Related API
`/v1/share-contracts`, `/v1/policies`, internal memory share/conflict routes。

### 01.11.5. UC-005: Operator 恢复服务

#### 01.11.5.1. Actor
AI platform operator。

#### 01.11.5.2. Goal
服务中断后从 durable WAL 和 checkpoint 恢复 projection。

#### 01.11.5.3. Preconditions
`PLASMOD_STORAGE=disk`，数据目录和 WAL 可读；版本/embedding 配置兼容。

#### 01.11.5.4. Main Flow
BuildServer 打开 Badger/FileWAL；consistency controller 读取 checkpoint；扫描后续 WAL；重放未完成 projection；健康检查通过。

#### 01.11.5.5. Alternative Flow
先调用 replay preview，再由 admin replay apply；embedding family 不兼容时进行受控 reindex。

#### 01.11.5.6. Data Written
checkpoint、canonical projection、retrieval index。

#### 01.11.5.7. Data Queried
admin storage/config/consistency/replay 状态。

#### 01.11.5.8. Consistency Requirement
恢复期间不得把未推进 watermark 的数据报告为可见。

#### 01.11.5.9. Failure Expectation
WAL 解码、checkpoint 或 projection 错误应阻止错误启动或返回明确失败。

#### 01.11.5.10. Related API
`/v1/admin/replay`, `/v1/admin/consistency-mode`, `/v1/admin/storage`。
