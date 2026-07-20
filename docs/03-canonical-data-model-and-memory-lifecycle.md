# 03. 规范对象模型与记忆生命周期

> Language: 中文 | [English](en/03-canonical-data-model-and-memory-lifecycle.md)

---

定义 Event 派生的规范对象，并拆解对象派生、记忆演化、规范对象图和分层存储 Engine。

---

## 03.1. Memory Subsystem Architecture

### 03.1.1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Perspective |
| 问题 | Memory 子系统由哪些对象、模块和能力共同构成 |
| 成熟度 | 部分到完整，取决于能力 |

### 03.1.2. 代码入口

| Role | Package / file | Constructor / public method |
|---|---|---|
| Event-derived Memory | `src/internal/materialization/service.go` | `materialization.NewService`, `MaterializeEvent` |
| Canonical storage | `src/internal/storage/contracts.go` | `RuntimeStorage.Objects`, `ApplyCanonicalProjection` |
| Algorithm dispatch | `src/internal/worker/cognitive/` | dispatcher constructor, `Dispatch`/`Run` |
| Agent-facing lifecycle adapter | `src/internal/agent/memory_manager.go` | `NewBaselineMemoryManager`; `Recall`, `Ingest`, `Compress`, `Summarize`, `Decay` |
| Tier placement | `src/internal/storage/tiered.go` | `NewTieredObjectStore`, promotion/archive/read methods |
| Retrieval projection | `src/internal/dataplane/` | `DataPlane.Ingest`, `Search`, `Flush` |
| Evidence and policy | `src/internal/evidence/`, `src/internal/semantic/policy.go`, cognitive reflection worker | `Assembler.Build`, policy/reflection methods |

接口、实现和构造选择的完整映射见 [Interface Implementation Registry](14-implementation-status-gaps-and-claim-boundaries.md)。

### 03.1.3. 输入与输出

| Operation | Typed input | Output | Main mutation / side effect |
|---|---|---|---|
| materialize | `schemas.Event` | `MaterializationResult` | Memory、Version、Edge，以及可选 State/Artifact candidate |
| lifecycle dispatch | `AlgorithmDispatchInput` | `AlgorithmDispatchOutput` | Memory lifecycle、algorithm state、audit；部分操作产生新 Memory |
| recall | query/scope/topK 或 `AlgorithmRecallInput` | `MemoryView`/ranked IDs | plugin 可计算 reinforcement；主 recall 路径不统一持久化它 |
| tier transition | memory ID + placement signal | tier result | hot cache、warm object、cold object/embedding 变化 |
| retrieval | `SearchInput` | `SearchOutput` | 读 projection；可附带 tier/segment trace |
| evidence | candidate IDs + query context | `EvidenceSubgraph`/`QueryResponse` | 读 Edge/Version/Policy/derivation；可写 bounded cache |

字段真值见 [Object and Message Registry](14-implementation-status-gaps-and-claim-boundaries.md)。

### 03.1.4. 内部组成

| Concern | Canonical type/module | Maturity |
|---|---|---|
| Memory object | `schemas.Memory` | 完整字段模型 |
| Event materialization | `materialization.Service`, extraction worker | 完整基础规则 |
| Canonical store | `ObjectStore` memory methods | 完整 |
| Algorithm state | `MemoryAlgorithmStateStore` | 完整存储接口 |
| Algorithm dispatch | plugin interface + dispatcher | 完整基础，profile migration缺失 |
| Lifecycle | fields + plugins + reflection worker | 部分 |
| Tiering | Hot cache/Warm ObjectStore/Cold store | 部分自动化 |
| Retrieval projection | DataPlane Ingest/Search | 完整基础 |
| Governance | PolicyRecord/ShareContract/Audit/filters | 部分 enforcement |
| Evidence/provenance | Edge/Version/derivation/Assembler | 完整基础 |

Memory 本体、算法状态和跨对象信息的分离如下：

Memory 本体包含 identity/type/content/scope/quality/validity/lifecycle 和外部引用；algorithm-specific strength/retention/profile 存在独立 state store；关系、版本、policy、audit 也在独立 store。字段全表见 [Object and Message Registry](14-implementation-status-gaps-and-claim-boundaries.md)。

这种分离允许更换算法而不修改 canonical Memory schema，但 `AlgorithmStateRef` 当前是单 string，和 `(memory,algorithm)` 多状态模型之间仍需更清晰引用规范。

对象关系为：

```text
Event -> Memory
Event -> State / Artifact
Memory -> source Event
Memory -> summary/compressed/shared Memory
Memory <-> Edge graph
Memory -> ObjectVersion / PolicyRecord / AuditRecord / AlgorithmState
Memory -> Retrieval projection
```

### 03.1.5. 调用关系

- Event ingest 创建基础 Memory；
- `/v1/memory` 提供 direct CRUD；
- `/v1/internal/memory/*` 调 algorithm/lifecycle/share；
- `/v1/query` 检索并构建 evidence；
- admin delete/purge/archive/reindex 管理 lifecycle/storage projection。

同步主链主要覆盖 event materialization、query read 和显式 internal lifecycle command；subscriber、reflection、summarization、index build 和部分 tier maintenance 是异步或显式触发路径。Memory 子系统不是由单一 facade 事务封装。

### 03.1.6. 数据与状态

| State class | Location | Persistence |
|---|---|---|
| canonical Memory | `ObjectStore` | memory 或 Badger backend |
| lifecycle | `Memory.LifecycleState`, `IsActive`, validity fields | 随 Memory 持久化 |
| algorithm state | `MemoryAlgorithmStateStore` | memory/Badger composite |
| graph/version/policy/audit | dedicated stores | backend-dependent persistent state |
| retrieval projection | segments/vector/sparse/native index | 可重建 acceleration state |
| hot/evidence cache | in-process bounded cache | 非持久 |
| cold object/embedding | `ColdObjectStore` | in-memory simulation 或 S3/MinIO adapter |

### 03.1.7. 正确性

Canonical Memory 是权威对象；retrieval record、hot cache、evidence fragment 可重建。Memory mutation 若绕过 Event chain，不自动同步 version/projection/evidence，这是当前最主要一致性边界。

Event-derived IDs 和 store upsert 支持幂等 replay；algorithm dispatcher 写 Memory、algorithm state 和 audit，但没有把 lifecycle mutation、ObjectVersion、Edge 和 projection refresh 放进统一事务。失败恢复依赖 WAL replay、reindex、显式 lifecycle 操作或人工修复；没有统一 Memory reconciliation loop。

### 03.1.8. 声明边界

可声明一个由 canonical object、algorithm state、tiering、retrieval、governance 和 evidence 组成的 memory subsystem。

不可声明所有组成已由单一 Memory service 事务协调，也不能把算法建议、policy annotation 或 cold placement 等同于全局强一致生命周期状态机。

### 03.1.9. 缺口

- 缺少统一 `MemoryMutation`/transition command 接口；
- 缺少 lifecycle、version、edge、projection 的原子更新契约；
- `LLMProvider`、`MASProvider` 只有 SDK 契约，没有核心生产实现；
- 缺少所有 Memory 读写入口上的一致 ACL enforcement；
- 缺少 profile migration、cache generation 和跨平面 repair manager；
- 需要跨 backend contract tests 与 invalid-transition tests。

---

## 03.2. Memory Lifecycle State Machine

### 03.2.1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Behavior Perspective |
| 问题 | Memory 有哪些真实状态、由谁触发和执行转换 |
| 成熟度 | 部分；存在 enum 和 plugin transition logic，但无统一全局 state machine |

### 03.2.2. 代码入口

| Concern | Package / file | Main API |
|---|---|---|
| enum and canonical fields | `src/internal/schemas/canonical.go` | `MemoryLifecycle`, `Memory.LifecycleState`, `IsActive` |
| algorithm suggestion | `src/internal/schemas/memory_management.go` | `MemoryManagementAlgorithm`, `AlgorithmDispatchOutput` |
| transition writer | `src/internal/worker/cognitive/` | dispatcher lifecycle/state/audit writes |
| policy-driven decay/quarantine | `src/internal/worker/cognitive/` reflection worker | `Reflect`/typed `Run` path |
| explicit stale/archive/delete | `src/internal/access/` admin handlers, `src/internal/worker/runtime.go`, `src/internal/storage/tiered.go` | internal/admin handlers and tier methods |
| agent adapter | `src/internal/agent/memory_manager.go` | `Compress`, `Summarize`, `Decay` |

### 03.2.3. 输入与输出

| Trigger input | Decision output | State mutation | Additional output |
|---|---|---|---|
| new Event | default/event lifecycle | new Memory lifecycle | Version/Edge/projection candidate |
| algorithm operation + Memory + state | `SuggestedLifecycleState` | Memory + algorithm state | produced IDs, audit metadata |
| recall query | score/reinforcement candidate | plugin-specific; not uniformly persisted | ranked memory view |
| reflection policy/TTL/confidence | retain/decay/archive/quarantine | Memory/tier/audit depending path | policy decision |
| admin delete/purge | logical or hard deletion | `IsActive`/state or physical removal | cleanup/audit result |

### 03.2.4. 内部组成

#### 03.2.4.1. Actual enum

代码 `schemas.MemoryLifecycle` 定义：

| State | Current meaning/producer |
|---|---|
| `active` | materialization/baseline/default active object |
| `candidate` | MemoryBank-style ingest/evaluation |
| `reinforced` | MemoryBank-style recall/update signal |
| `compressed` | plugin-derived or transitioned memory |
| `decayed` | baseline/reflection decay |
| `stale` | MemoryBank/Zep/internal stale route |
| `archived` | plugin/tier lifecycle suggestion |
| `quarantined` | plugin/reflection policy |
| `hidden` | retrieval exclusion state |
| `deleted_logically` | logical deletion state |

`Created`、`Weakened`、`Summarized`、`Reactivating`、`Reactivated`、物理 `Deleted` 不是当前 enum。Summary/compression 通常创建新 Memory，而不仅是状态。

#### 03.2.4.2. Transition ownership

| Trigger | Decision | Writer |
|---|---|---|
| Event ingest | Event object lifecycle or default | materializer |
| algorithm ingest/update/decay | plugin returns `SuggestedLifecycleState` | dispatcher applies verbatim |
| recall | plugin may score/reinforce internally；dispatcher recall不持久化 state | plugin/none |
| reflection | TTL/quarantine/confidence policy | reflection worker |
| internal stale | handler command | Gateway/store |
| archive/delete | admin/tier operation | storage/governance path |

#### 03.2.4.3. Guards and actions

MemoryBank-style lifecycle code contains candidate/active/reinforced/compressed/stale/archived/quarantine guards。其他 plugins 有不同规则；不存在跨 plugin 强制的 transition table。`IsActive` 与 `LifecycleState` 可能不完全一致，retrieval 还单独检查某些 policy tags/states。

### 03.2.5. 调用关系

Event ingest 初始化 Memory；internal memory routes 或 `AgentSession.MemoryManager` 触发 algorithm dispatcher；subscriber/reflection 可能异步更新 lifecycle；admin/tier paths 执行 archive/delete。各路径共享 `Memory` 字段，但不经过统一 transition service。

同步边界取决于调用入口：internal algorithm route 等待 dispatcher 结果；subscriber maintenance 在 ACK 后执行；query recall 不保证 reinforcement 已持久化。

### 03.2.6. 数据与状态

- Lifecycle state 在 canonical Memory 中；algorithm score/profile 在 `MemoryAlgorithmStateStore`；
- `IsActive`、validity interval、policy tags 会额外影响可见性；
- Hot/Warm/Cold 是物理 placement，不是 lifecycle enum；
- transition audit 存在于部分 dispatcher/reflection/admin 路径；
- summary/compression 可产生新 Memory，原对象通常保留并通过 Edge/Version 表达派生关系。

Reverse transition 与 tiering 的当前行为：

- Quarantine 可由 plugin suggestion 离开，但没有统一 release authorization flow。
- Archived -> active/reactivated 没有通用 transition；cold query 命中不是 reactivation。
- Lifecycle state 与 Hot/Warm/Cold 不是一一绑定；placement policy 可参考 lifecycle，但 tier 是物理状态。
- Logical delete 与 hard purge 分离。

### 03.2.7. 正确性

Dispatcher lifecycle update写 Memory + algorithm state + audit，但不统一写 ObjectVersion、Edge、retrieval refresh。Reflection/admin 路径也各自有副作用。状态机因此是“多个 writer 共享字段”，不是集中 transition service。

### 03.2.8. 声明边界

可声明 lifecycle enum、plugin-driven transition suggestions、policy decay/quarantine 和 archive/delete operations。

不可声明完整可验证 state machine、统一逆向转换、所有路径强制审计或 state-tier strict binding。

### 03.2.9. 缺口

- 缺少统一 transition command、guard/action registry 和 invalid-transition error；
- 缺少 `IsActive`、lifecycle、validity、policy visibility 的单一不变量；
- 缺少 archive -> reactivate 和 quarantine release 的授权流程；
- 缺少 transition、ObjectVersion、Edge、audit、projection 的原子/可恢复更新；
- 缺少跨 plugin transition contract tests、逆向转换测试和并发 mutation 测试。

---

## 03.3. Event-derived Memory Construction Mechanism

### 03.3.1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Mechanism |
| 目标 | Raw Event -> normalized Event -> typed canonical objects/relations/versions/projection |
| 关键路径 | Event ingest |
| 成熟度 | 完整基础规则，semantic/configurable derivation 部分 |

### 03.3.2. 代码入口

| Function/type | Role |
|---|---|
| `Event.NormalizeDynamicEventV04` | merge nested/legacy wire fields |
| `MaterializeEvent` | produce Memory, candidate State, optional Artifact, edges, versions, IngestRecord |
| Event helper methods | text/scope/state/artifact/causality extraction |
| specialized materialization workers | keyed State, Artifact/tool trace, graph/index side effects |
| `ApplyCanonicalProjection` | commit selected outputs |

### 03.3.3. 输入输出

输入是完整 Dynamic Event；输出字段见 [Object and Message Registry](14-implementation-status-gaps-and-claim-boundaries.md)。一个 Event 至少生成 Memory、MemoryVersion、stable keyed/`last_memory_id` State、StateVersion、retrieval record 和基础/因果边；满足 Artifact 规则时还会生成 Artifact 与 ArtifactVersion。原 Event 本身与这些派生结果共同进入 Canonical Projection。

### 03.3.4. Decision rules

| Signal | Decision |
|---|---|
| event type | resolve Memory type; tool/artifact/state route |
| `retrieval.index_text`/payload text | Memory content and index text |
| workspace/retrieval/session | scope and namespace |
| causality refs | typed edges/provenance |
| object descriptor | explicit object/artifact/state metadata |
| embedding vector/ref/flag | projection vector or skip-vector behavior |

规则由 Go helper 固化，尚无通用 declarative rule registry/semantic analyzer。

### 03.3.5. 调用关系与同步边界

Runtime 同步调用 Materializer；consistency worker提交 retrieval/canonical；subscriber 异步调用 specialized workers。`MainChain` 包装另一组 worker 顺序，但不在主 Runtime write path。

### 03.3.6. 状态变化

WAL/LSN 是派生输入顺序；canonical store 保存对象/关系/版本；DataPlane 保存投影；derivation log/audit/cache 保存辅助证据。Materializer 本身无状态。

### 03.3.7. 正确性

- deterministic primary IDs 支持 replay；
- duplicate Event ID 当前倾向 upsert/覆盖，没有全局 payload hash conflict rejection；
- multiple store writes 只在 canonical shared backend 内可 transaction；
- algorithm compress/summarize 直接产生 Memory，不自动重新作为 Event 进入该机制。

### 03.3.8. 声明边界

可声明 event-derived typed object construction 和 provenance/version/retrieval projection。

不可声明任意配置规则、LLM semantic classification、全局 dedup/conflict engine，或所有 derived Memory 都有对应 derived Event。

### 03.3.9. 缺口

仍需要 DerivationRule interface/registry、duplicate policy、payload hash conflict、derived Event contract、per-output validation、专用 worker 输出合并协议和 fault-injection tests。

---

## 03.4. Canonical Memory Representation Mechanism

### 03.4.1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Mechanism |
| 目标 | 用稳定 canonical Memory 连接内容、来源、生命周期、算法、治理、版本和关系 |
| 成熟度 | 完整字段模型；部分引用对象的 active persistence/validation 有限 |

### 03.4.2. Code and schema entry

- `schemas.Memory`, `MemoryAlgorithmState`, `ObjectVersion`, `Edge`, `PolicyRecord`, `AuditRecord`；
- `ObjectStore`, `MemoryAlgorithmStateStore`, Version/Edge/Policy/Audit stores；
- `ObjectModelRegistry` 将 Memory 标记为 versionable/indexable。

### 03.4.3. Representation

```text
Memory = identity/type/scope/content/quality/validity/lifecycle
       + source/provenance references
       + embedding and algorithm-state references
       + dataset lineage
```

完整字段见 [Object and Message Registry](14-implementation-status-gaps-and-claim-boundaries.md)。

### 03.4.4. Internal vs external fields

| Concern | Stored in Memory | External record |
|---|---|---|
| content/summary/type/scope | yes | retrieval text/vector projection |
| provenance | source IDs/ref | Edge + derivation log |
| lifecycle | state/isActive/TTL/valid interval | Policy/Audit/Version |
| algorithm | single ref | `(memory,algorithm)` state records |
| embedding | ref only | vector index / optional Embedding object model |
| policy/share | tags/scope | PolicyRecord/ShareContract |
| version | current number | ObjectVersion history |

### 03.4.5. Input/output and calls

Materializer/plugin/communication/reflection/admin handlers write Memory；query/algorithm/tier/governance/evidence read it。There is no single `MemoryRepository` service that validates every writer。

### 03.4.6. State semantics

`IsActive` 是粗粒度 serving flag，`LifecycleState` 是细粒度阶段，tier 是物理 placement；三者相关但不等价。`ValidFrom/ValidTo` 是 canonical validity，不等于 retrieval segment timestamp alone。

### 03.4.7. Correctness

- Stable ID/version/source refs are replay boundaries。
- Direct Memory POST can bypass version/projection/audit。
- `AlgorithmStateRef` 与 multi-algorithm state store 的引用语义尚不完全规范。
- Embedding object type 已定义，但 active path主要在 index metadata/vector store中保存 embedding。

### 03.4.8. 声明边界

可声明 canonical Memory 将算法状态、治理记录和检索投影从本体解耦。

不可声明所有引用都具有数据库 foreign-key enforcement，或 Memory row alone contains full provenance/evidence/embedding payload。

### 03.4.9. 缺口

需要 central Memory mutation validator、reference integrity checks、multi-algorithm ref schema、versioned validity update API、Embedding object persistence contract 和 writer inventory tests。

---

## 03.5. Memory Evolution Mechanism

### 03.5.1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Mechanism |
| 目标 | Recall/usage/time/policy/cost 信号驱动 memory state、content、tier 和 projection 演化 |
| 成熟度 | 部分 |

### 03.5.2. Entry and decision owners

| Entry | Decision owner |
|---|---|
| algorithm dispatch ingest/update/recall/decay/compress/summarize | selected plugin |
| reflection policy | PolicyRecord + baseline worker rules |
| admin archive/delete/purge | handler/storage policy |
| hot cache promotion/eviction | salience/hotness/config |

### 03.5.3. Signals and outputs

| Signal | Possible output |
|---|---|
| recall/query | scored order, plugin reinforcement state |
| elapsed time/TTL | decayed/stale/archive suggestion |
| importance/confidence/policy | salience adjustment/quarantine |
| conflict penalty | quarantine/stale in MemoryBank-style |
| content set | compressed/summary derived Memory |
| storage pressure/access | hot eviction/promotion |

### 03.5.4. Interfaces and state

`MemoryManagementAlgorithm` 是 decision plugin；dispatcher persists `MemoryAlgorithmState` and Memory outputs；reflection writes Memory/PolicyDecision/Audit and may archive。Canonical lifecycle, algorithm state and physical tier are distinct states。

### 03.5.5. Sync/async

Internal algorithm API synchronous；subscriber reflection/consolidation asynchronous；hot eviction happens on cache insert；cold archive mostly explicit or reflection-driven。No central evolution loop schedules all operations。

### 03.5.6. Correctness

- Source Memory remains for compression/summary semantics unless explicit deletion。
- Derived edges/version/projection are not uniformly required by dispatcher。
- Recall dispatch does not persist generic reinforcement state。
- Archived read does not automatically produce reactivation transition。

### 03.5.7. Failure/recovery

Partial mutation may leave lifecycle and projection/tier out of sync；current recovery relies on canonical inspection, reindex/archive retry and audit logs。No lifecycle transaction log/reconciler。

### 03.5.8. 声明边界

可声明 pluggable evolution algorithms and policy/tier actions。

不可声明 fully autonomous evolution engine、uniform score function、automatic reactivation or transactional lifecycle transitions。

### 03.5.9. 缺口

Define EvolutionCommand/Decision/Transition record, mandatory version/edge/audit/projection hooks, operation scheduler, reactivation and purge states, and transition metrics/tests。

---

## 03.6. Object Derivation Engine

### 03.6.1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Engine |
| 原模块 | Materializer + specialized materialization workers |
| 目标 | Event -> canonical objects/relations/versions/retrieval record |
| 关键路径 | 是 |
| 成熟度 | 完整基础规则；通用 semantic/configurable derivation 部分 |

### 03.6.2. 代码入口

| Item | Code |
|---|---|
| Package | `src/internal/materialization`, `src/internal/worker/materialization` |
| Main files | `service.go`, `pre_compute.go`, `object.go`, `state.go`, `tool_trace.go` |
| Constructors | `NewService`, `NewPreComputeServiceWithConfig`, `CreateInMemory*MaterializationWorker` |
| Public methods | `MaterializeEvent`, compatibility `ProjectEvent`; worker `Run/Materialize/Apply/Checkpoint/TraceToolCall` |
| Interfaces | Object/State/Tool worker interfaces, `Runnable` |

### 03.6.3. Engine fields

| Type | Field | Type/meaning |
|---|---|---|
| `Service` | none | stateless deterministic transformer |
| `MaterializationResult` | `Record` | retrieval `IngestRecord` |
| same | `Memory`, `Version`, `Edges` | always-derived core output |
| same | `State`, `StateVersion` | ingest checkpoint candidate |
| same | `Artifact`, `ArtifactVersion` | optional output |
| Object worker | `id`, object/edge/version stores, derivation logger | specialized Artifact route |
| State worker | `id`, object/version stores, derivation logger, mutex | store-backed keyed State version tracking and checkpoints |
| Tool worker | id/object/version/log dependencies | tool Artifact trace + recoverable version |
| PreCompute | cache/config | evidence fragment precomputation |

### 03.6.4. Input/output and field access

| Input | Reads | Output |
|---|---|---|
| `schemas.Event` | identity, actor, time, event/object/causality/access/materialization/retrieval/payload | `MaterializationResult` |
| `StateApplyInput` | actor IDs, event type, state key/value | State ID/version |
| `StateCheckpointInput` | agent/session | snapshot count + ObjectVersions |
| `ToolTraceInput` | tool event/object/payload | Artifact ID |

完整字段见 [Object and Message Registry](14-implementation-status-gaps-and-claim-boundaries.md)。

### 03.6.5. Internal components

| Suggested component | Actual implementation |
|---|---|
| Event Normalizer | Event helper/normalize methods，完整 |
| Event Parser | Go JSON decode + helper accessors，完整 |
| Semantic Analyzer | 无通用 analyzer；event-type switch/heuristics |
| Event Classifier | event/object helper rules，部分 |
| Object Deriver | `MaterializeEvent` + workers，完整基础 |
| Relation Generator | `deriveEdges`, schema edge builders, graph worker |
| Version Generator | materializer + state/object workers |
| Projection Builder | `IngestRecord` construction |
| Deduplicator | deterministic ID/upsert only，无 conflict-aware deduplicator |
| Validator | Gateway/ingest worker/schema helpers，分散 |

### 03.6.6. Calls and APIs

Upstream：Runtime `projectWALEntry`, MainChain, subscriber。Downstream：DataPlane, RuntimeStorage, evidence cache, derivation log。API：`POST /v1/ingest/events` 和 document/tool/state adapters；direct canonical POST 不调用本 Engine。

### 03.6.7. State, correctness and failure

- Service stateless；State worker 从 ObjectStore/VersionStore 读取当前状态，不再依赖进程内 key map 决定版本。
- Memory ID、scope-safe `CanonicalStateID` 和 mutation event 提供 replay 幂等依据。
- Runtime 将原 Event、Memory、stable keyed/`last_memory_id` State、可选 Artifact、关系和完整 snapshot 版本提交为一个 Canonical Projection；专用 tool trace 和生命周期 worker 的输出仍在该事务之外。
- Runtime 主链用 mutex 串行 state version resolution；该锁只覆盖单进程，跨进程并发仍需要外部 single-writer/conditional-write 协议。
- worker store writes can be duplicated with Runtime artifact writes；upsert hides some duplication but versions may differ。
- `enabled/targets` is not a hard routing gate。

### 03.6.8. 声明边界

可声明 typed deterministic object derivation with relation/version/projection records。

不可声明 semantic LLM analysis、declarative derivation DSL、global dedup/conflict handling、all outputs atomic or arbitrary object plugins。

### 03.6.9. 缺口

1. single DerivationPlan listing every output and commit status；
2. configured rule registry and custom object derivation interface；
3. duplicate payload conflict policy；
4. merge specialized worker outputs into one commit/replay contract；
5. per-output validation/error propagation；
6. tests proving same Event produces byte/semantic-equivalent outputs across replay。

---

## 03.7. Memory Evolution Engine

### 03.7.1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Engine |
| 原模块 | Memory Algorithm Dispatcher + plugins |
| 目标 | 执行 memory ingest/update/recall/decay/compress/summarize 并持久化结果 |
| 关键路径 | internal memory operations；普通 query 可通过 recall adapter |
| 成熟度 | 完整 plugin/dispatcher foundation；unified lifecycle transaction 部分 |

### 03.7.2. Code entry

| Item | Code |
|---|---|
| Interface | `schemas.MemoryManagementAlgorithm` |
| Dispatcher | `worker/cognitive/algorithm_dispatcher.go` |
| Plugins | `cognitive/baseline`, `memorybank`, `zep` |
| Constructor | `CreateAlgorithmDispatchWorker` |
| Runtime | `DispatchAlgorithm`, `DispatchRecall` |
| HTTP | `/v1/internal/memory/*` |

### 03.7.3. Dispatcher fields

| Field | Type | Meaning |
|---|---|---|
| `id` | string | worker/node identity |
| `algo` | `MemoryManagementAlgorithm` | active plugin |
| `objStore` | ObjectStore | canonical Memory read/write |
| `algoStore` | MemoryAlgorithmStateStore | plugin state persistence |
| `auditStore` | AuditStore | algorithm update audit |

Plugins additionally hold their own configuration and optional in-memory exported state maps；their durable portability depends on dispatcher state persistence/load usage。

### 03.7.4. Interface methods

| Method | Input | Output |
|---|---|---|
| `AlgorithmID` | none | stable ID |
| `Ingest` | Memories + context | initial states |
| `Update` | Memories + signals | updated states |
| `Recall` | query + candidates + context | scored Memories |
| `Compress` | Memories + context | derived Memories |
| `Decay` | Memories + timestamp | updated states |
| `Summarize` | Memories + context | summary Memories |
| `ExportState/LoadState` | memory ID/state | plugin state portability |

Dispatcher `Dispatch` supports operation strings `ingest|decay|recall|compress|summarize|update` and returns `AlgorithmDispatchOutput`。

### 03.7.5. Internal behavior

- fetch IDs from ObjectStore；
- construct AlgorithmContext；
- call exactly one selected operation；
- persist states and apply `SuggestedLifecycleState` verbatim；
- store derived Memory as returned；
- append algorithm-update AuditRecord；
- return counts/IDs/scored refs。

Dispatcher deliberately has no threshold/business decision logic。

### 03.7.6. State mutations and side effects

| Operation | Canonical Memory | Algorithm state | Audit | Version/Edge/Projection |
|---|---|---|---|---|
| ingest/update/decay | ref/lifecycle may change | yes | yes | not uniformly |
| recall | no generic mutation | no dispatcher persistence | no | none |
| compress/summarize | new Memory | plugin-dependent | yes | not uniformly |

### 03.7.7. Correctness/failure

- Unknown operation returns typed error string。
- Missing Memory IDs are silently omitted from input set。
- Store methods do not return errors, so persistence failure visibility depends implementation。
- Profile switch does not migrate/validate old algorithm states。
- Derived Memory may be absent from retrieval until separately indexed。

### 03.7.8. 声明边界

可声明 pluggable algorithm interface with canonical-independent state and baseline/MemoryBank-style/Zep-style implementations。

不可声明 plugins are externally equivalent to named products, recall always reinforces, or lifecycle/derived mutations are transactionally versioned/reindexed。

### 03.7.9. 缺口

Capability registry/versioning, plugin state migration, missing-ID/error reporting, lifecycle transition transaction, mandatory derived edges/versions/projection, operation metrics and deterministic plugin contract tests。

---

## 03.8. Canonical Object Graph Engine

### 03.8.1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Engine |
| 原模块 | Canonical Object Store / RuntimeStorage |
| 目标 | 持久化并查询 canonical objects、relations、versions、policies、contracts、audit 和 algorithm state |
| 关键路径 | 是 |
| 成熟度 | 完整单进程/memory/Badger core；graph query/constraints 部分 |

### 03.8.2. Code entry

| Item | Code |
|---|---|
| Interfaces | `src/internal/storage/contracts.go` |
| Memory implementation | `storage/memory.go` |
| Persistent implementation | `badger_stores.go`, `composite.go` |
| Factory | `BuildRuntimeFromEnv` |
| Canonical transaction | `ApplyCanonicalProjection` |
| Graph adapters | `schemas/graph_*`, `worker/indexing/graph.go` |
| Coordinators | Object/Memory/Version/Policy coordinators |

### 03.8.3. Engine fields and stores

The Engine is an aggregate boundary, not one `CanonicalObjectGraphEngine` struct。

| RuntimeStorage accessor | Record family |
|---|---|
| `Segments`, `Indexes` | retrieval metadata |
| `Objects` | Agent/Session/Event/Memory/State/Artifact/User |
| `Edges` | graph relations + src/dst indexes |
| `Versions` | object histories/latest |
| `Policies` | append-only PolicyRecord |
| `Contracts` | ShareContract |
| `Audits` | append-only AuditRecord |
| `AlgorithmStates` | per memory/algorithm state |
| `HotCache` | volatile object cache |

`CanonicalProjection` fields：Memory, State, Artifact, Versions, Edges, flags for base edges。

### 03.8.4. Interfaces and API surface

| Interface | Main methods |
|---|---|
| ObjectStore | put/get/list per object type, delete Memory |
| GraphEdgeStore | put/get/delete/from/to/bulk/list/prune |
| SnapshotVersionStore | put/history/latest |
| PolicyStore | append/get/list |
| ShareContractStore | put/get/by-scope/list |
| AuditStore | append/get/list/delete target |
| AlgorithmStateStore | put/get/list |
| RuntimeStorage | accessors, canonical projection, base-edge helpers |

External direct API includes canonical collection routes；Event/Query paths use Engine through Runtime/evidence。See [API to Engine Matrix](14-implementation-status-gaps-and-claim-boundaries.md)。

### 03.8.5. Input/output and data model

Input：canonical structs and mutations。Output：hydrated structs, edge neighborhoods, versions, policies/contracts/audits and lists。Full object fields are in [Object and Message Registry](14-implementation-status-gaps-and-claim-boundaries.md)。

### 03.8.6. Internal composition and indexes

Badger uses typed key prefixes for object/edge/version/policy/contract and source/destination edge secondary indexes。Memory implementation uses maps + locks。ObjectModelRegistry describes type PK/versionable/indexable metadata but does not enforce all stores automatically。

### 03.8.7. Correctness/failure

- Same Badger backend can atomically apply canonical projection。
- Store interfaces mostly omit `error` on writes, limiting backend failure propagation。
- Direct CRUD can bypass Event/WAL/version/projection/audit。
- Edge endpoints have no mandatory foreign-key or schema constraint validation。
- Latest version depends on store ordering；rollback API is partial。

### 03.8.8. 声明边界

可声明 canonical multi-object graph with version/policy/share/audit state and memory/Badger implementations。

不可声明 graph database query language、referential integrity、distributed transaction、arbitrary historical snapshot or every mutation Event-sourced。

### 03.8.9. 缺口

Error-returning mutation interface, transaction abstraction for all canonical writes, referential/type constraints, graph traversal service, version-at-time snapshots, Event-only public mutation policy and storage contract tests across implementations。

---

## 03.9. Tiered Storage Engine

### 03.9.1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Engine |
| 原模块 | Tiered Storage |
| 目标 | 在 Hot/Warm/Cold/Archive 之间读取、提升、归档、清理和重建对象/embedding |
| 关键路径 | ingest promotion/query fallback/archive/delete |
| 成熟度 | 完整基础三层实现；autonomous migration/resource optimization 部分 |

### 03.9.2. Code entry

| Item | Code |
|---|---|
| Object tiering | `storage/tiered.go` |
| Hot policy | `hot_cache_policy.go`, memory tier config |
| Cold implementations | `InMemoryColdStore`, `S3ColdStore` |
| Retrieval tiering | `dataplane.TieredDataPlane` |
| Constructor | `NewTieredObjectStoreWithThreshold`, `NewTieredDataPlaneWithEmbedderAndConfig` |
| API | S3 export/snapshot/cold purge, delete/purge, query include_cold |

### 03.9.3. Engine fields

| Type/field | Meaning |
|---|---|
| `HotEntry` | object ID/type/payload, salience, access count, last access, insert time |
| `HotObjectCache.mu/entries/order/maxSize/policy` | bounded concurrent cache and eviction policy |
| `TieredObjectStore.hot` | Hot cache |
| `.warm` | canonical ObjectStore |
| `.warmEdge` | warm graph edge store |
| `.cold` | S3 or in-memory cold store |
| `.embedder` | archive/reindex embedding generation |
| `.hotThreshold` | promotion threshold |
| `TieredDataPlane` fields | hot index, warm plane, embedder, cold search functions, RRF K |

### 03.9.4. Methods/input/output

| Operation | Input | Output/side effect |
|---|---|---|
| Hot `Put/Get/Contains/Evict/Clear/Len` | object metadata | volatile cache state |
| promote/get | Memory/ID + salience | Hot/Warm read/write |
| `ArchiveMemory` | Memory ID | cold Memory/embedding/edges and warm/hot transition per method |
| cold search | text/vector/TopK | object IDs + diagnostics |
| soft delete cleanup | Memory ID | hot eviction |
| hard delete | Memory ID | hot/warm/cold/edge cleanup |
| export/purge | selectors | S3/cold mutations/admin summary |

### 03.9.5. Placement logic

Hot default capacity 2000；cache hotness combines salience, recency and access count, while extended `HotCachePolicy` config exposes weighted recency/frequency/semantic/tier class controls。Warm is canonical/default serving tier。Cold is written on explicit archive/reflection, not every ingest。Cold is queried only when requested。

Lifecycle and tier are not identical：archived lifecycle can guide cold placement, but Hot/Warm/Cold is physical state and may temporarily diverge。

### 03.9.6. Calls and sync/async

Runtime promotes synchronously after canonical commit；flush loop asynchronously persists retrieval index；reflection may archive asynchronously；admin export/purge runs request/task-specific logic；query cold path synchronous when requested。

### 03.9.7. Correctness/failure

- Cold backend selected only when required S3 env is valid, otherwise in-memory simulation。
- Archive spans object/edge/embedding stores and lacks distributed transaction。
- Soft delete leaves cold aligned until hard purge；query filtering must avoid serving stale hot payload。
- S3/network errors are operation-level and retry is caller/admin responsibility。
- Hot cache is disposable and not authoritative。

### 03.9.8. 声明边界

可声明 real Hot/Warm/Cold routing, bounded hot cache, S3/MinIO cold backend, explicit archive/retrieval/delete operations。

不可声明 fully autonomous tier optimizer, zero-copy migration, transactional movement or automatic reactivation/prefetch based on learned cost。

### 03.9.9. 缺口

Persistent tier metadata/state machine, migration job/status/retry, warm-delete-after-cold-verify invariant, resource pressure feedback, rehydration API, per-object location diagnostics and cross-tier reconciliation tests。

---

## 03.10. Canonical Object Model

权威结构定义在 `src/internal/schemas/canonical.go`。

| Object | Primary ID | 关键关系 | Versionable | Indexable |
|---|---|---|---:|---:|
| Agent | `agent_id` | tenant/workspace/policy/capability | Yes | No |
| Session | `session_id` | agent/parent session/task | Yes | No |
| Event | `identity.event_id` | actor/causality/access/materialization | Yes | Yes |
| Memory | `memory_id` | source events/scope/provenance/lifecycle | Yes | Yes |
| AgentState | `state_id` | agent/session/state key/value | Yes | No |
| Artifact | `artifact_id` | session/owner/producer event | Yes | Yes |
| Edge | `edge_id` | source/type/target/provenance | No | No |
| ObjectVersion | object ID + version | mutation event/valid interval | N/A | No |
| PolicyRecord | `policy_id` | object/decision/visibility | No | No |
| ShareContract | `contract_id` | ACL/consistency/merge/audit policy | No | No |
| RetrievalSegment | `segment_id` | namespace/index/storage/tier | No | Physical metadata |

`semantic.ObjectModelRegistry` 保存 type metadata。Go 中 `AgentState` 是 `State` 的 alias；新 object type 名称应使用 `agent_state`，`state` 只用于兼容。

### 03.10.1. ID 与版本

Event 未提供 ID 时 Gateway 生成 `evt_*`。Memory 通常使用 `mem_ + event_id`；State materializer 使用 `state_ + agent_id + state_key`；Artifact 可由 Event object 指定或按默认规则生成。确定性 ID 让 replay 可以覆盖同一 canonical key，但不能自动保证所有外部副作用 exactly once。

### 03.10.2. 原子 projection

`storage.CanonicalProjection` 可包含 Memory、State、Artifact、Versions、Edges 和 base-edge flags。`storage.factory` 强制 objects、edges、versions 使用同一种 backend，Badger 实现才能在共享事务中提交这组变更。

### 03.10.3. Direct CRUD 边界

`/v1/agents`、`/v1/sessions`、`/v1/memory`、`/v1/states`、`/v1/artifacts`、`/v1/edges` 等 POST 路由直接写 store/coordinator，主要用于管理和兼容。需要 audit/replay/consistency 的业务写入应使用 Event ingest。

---

## 03.11. Event 到 Object 模型

### 03.11.1. 输入规范化

`schemas.Event.UnmarshalJSON` 接受 Dynamic Event v0.4 和已实现的 legacy flat aliases。`NormalizeDynamicEventV04()` 将 identity、actor、time、event、object、causality、access、materialization、retrieval、payload、runtime 和 extensions 归一化。canonical JSON 输出以 v0.4 nested fields 为准。

### 03.11.2. 默认路由

| Event 特征 | 主要 canonical 输出 |
|---|---|
| tool call / tool result / artifact-like | Artifact + base edges + version |
| state update / state change / checkpoint | AgentState；checkpoint 可生成 State versions |
| 其他 materializable event | Memory + base/causal edges + version |

Event 可以通过 `object.object_type/object_id` 影响 Artifact 等专用派生路径。`materialization.enabled` 和
`materialization.targets` 当前会被规范化、记录并用于查询过滤，但 active `materialization.Service` 尚未把它们
作为通用硬开关：每次 Event ingest 仍会创建默认 Memory、ObjectVersion 和 stable State；显式 state key 更新
对应键，否则更新 `last_memory_id`。专用 State/Artifact worker 再按 event/object type 和 payload 执行补充动作。是否进入向量索引由
retrieval fields 决定。

### 03.11.3. Memory materialization

`materialization.Service.MaterializeEvent` 解析 text、scope、memory type、confidence、importance、source event 和 lifecycle，生成 Memory 与推导 edges。Runtime 先将 canonical projection 写入 storage，再将 retrieval record 写入 data plane。

### 03.11.4. State materialization

主 Runtime 和 `InMemoryStateMaterializationWorker.Apply` 都从 Event 读取 state key/value，并用 tenant + workspace + agent + session + key 的 hash 生成 `CanonicalStateID`。版本从 ObjectStore/VersionStore 的当前记录递增，mutation event 已存在时幂等返回；主 Runtime 的 mutex 仅提供单进程串行化，因此 direct CRUD 和跨进程 writer 仍需要外部协调。

### 03.11.5. Artifact materialization

tool/artifact event 生成 Artifact，URI、MIME、name、body 从 Event object/payload 获取。内联 body 使用 `content_ref=inline` 并保存在 metadata。

### 03.11.6. Edge 与 derivation

默认 memory edges 包括 caused_by event、belongs_to_session、owned_by_agent 和 causal refs 的 derived_from。Artifact 也生成 producer/base edges。DerivationLog 保存 event -> object 的 operation，供 trace 使用。
