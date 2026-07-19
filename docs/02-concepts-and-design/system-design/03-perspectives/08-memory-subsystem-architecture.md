# 8. Memory Subsystem Architecture

## 1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Perspective |
| 问题 | Memory 子系统由哪些对象、模块和能力共同构成 |
| 成熟度 | 部分到完整，取决于能力 |

## 2. 代码入口

| Role | Package / file | Constructor / public method |
|---|---|---|
| Event-derived Memory | `src/internal/materialization/service.go` | `materialization.NewService`, `MaterializeEvent` |
| Canonical storage | `src/internal/storage/contracts.go` | `RuntimeStorage.Objects`, `ApplyCanonicalProjection` |
| Algorithm dispatch | `src/internal/worker/cognitive/` | dispatcher constructor, `Dispatch`/`Run` |
| Agent-facing lifecycle adapter | `src/internal/agent/memory_manager.go` | `NewBaselineMemoryManager`; `Recall`, `Ingest`, `Compress`, `Summarize`, `Decay` |
| Tier placement | `src/internal/storage/tiered.go` | `NewTieredObjectStore`, promotion/archive/read methods |
| Retrieval projection | `src/internal/dataplane/` | `DataPlane.Ingest`, `Search`, `Flush` |
| Evidence and policy | `src/internal/evidence/`, `src/internal/semantic/policy.go`, cognitive reflection worker | `Assembler.Build`, policy/reflection methods |

接口、实现和构造选择的完整映射见 [Interface Implementation Registry](../06-cross-reference/interface-implementation-registry.md)。

## 3. 输入与输出

| Operation | Typed input | Output | Main mutation / side effect |
|---|---|---|---|
| materialize | `schemas.Event` | `MaterializationResult` | Memory、Version、Edge，以及可选 State/Artifact candidate |
| lifecycle dispatch | `AlgorithmDispatchInput` | `AlgorithmDispatchOutput` | Memory lifecycle、algorithm state、audit；部分操作产生新 Memory |
| recall | query/scope/topK 或 `AlgorithmRecallInput` | `MemoryView`/ranked IDs | plugin 可计算 reinforcement；主 recall 路径不统一持久化它 |
| tier transition | memory ID + placement signal | tier result | hot cache、warm object、cold object/embedding 变化 |
| retrieval | `SearchInput` | `SearchOutput` | 读 projection；可附带 tier/segment trace |
| evidence | candidate IDs + query context | `EvidenceSubgraph`/`QueryResponse` | 读 Edge/Version/Policy/derivation；可写 bounded cache |

字段真值见 [Object and Message Registry](../06-cross-reference/object-and-message-registry.md)。

## 4. 内部组成

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

Memory 本体包含 identity/type/content/scope/quality/validity/lifecycle 和外部引用；algorithm-specific strength/retention/profile 存在独立 state store；关系、版本、policy、audit 也在独立 store。字段全表见 [Object and Message Registry](../06-cross-reference/object-and-message-registry.md)。

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

## 5. 调用关系

- Event ingest 创建基础 Memory；
- `/v1/memory` 提供 direct CRUD；
- `/v1/internal/memory/*` 调 algorithm/lifecycle/share；
- `/v1/query` 检索并构建 evidence；
- admin delete/purge/archive/reindex 管理 lifecycle/storage projection。

同步主链主要覆盖 event materialization、query read 和显式 internal lifecycle command；subscriber、reflection、summarization、index build 和部分 tier maintenance 是异步或显式触发路径。Memory 子系统不是由单一 facade 事务封装。

## 6. 数据与状态

| State class | Location | Persistence |
|---|---|---|
| canonical Memory | `ObjectStore` | memory 或 Badger backend |
| lifecycle | `Memory.LifecycleState`, `IsActive`, validity fields | 随 Memory 持久化 |
| algorithm state | `MemoryAlgorithmStateStore` | memory/Badger composite |
| graph/version/policy/audit | dedicated stores | backend-dependent persistent state |
| retrieval projection | segments/vector/sparse/native index | 可重建 acceleration state |
| hot/evidence cache | in-process bounded cache | 非持久 |
| cold object/embedding | `ColdObjectStore` | in-memory simulation 或 S3/MinIO adapter |

## 7. 正确性

Canonical Memory 是权威对象；retrieval record、hot cache、evidence fragment 可重建。Memory mutation 若绕过 Event chain，不自动同步 version/projection/evidence，这是当前最主要一致性边界。

Event-derived IDs 和 store upsert 支持幂等 replay；algorithm dispatcher 写 Memory、algorithm state 和 audit，但没有把 lifecycle mutation、ObjectVersion、Edge 和 projection refresh 放进统一事务。失败恢复依赖 WAL replay、reindex、显式 lifecycle 操作或人工修复；没有统一 Memory reconciliation loop。

## 8. 声明边界

可声明一个由 canonical object、algorithm state、tiering、retrieval、governance 和 evidence 组成的 memory subsystem。

不可声明所有组成已由单一 Memory service 事务协调，也不能把算法建议、policy annotation 或 cold placement 等同于全局强一致生命周期状态机。

## 9. 缺口

- 缺少统一 `MemoryMutation`/transition command 接口；
- 缺少 lifecycle、version、edge、projection 的原子更新契约；
- `LLMProvider`、`MASProvider` 只有 SDK 契约，没有核心生产实现；
- 缺少所有 Memory 读写入口上的一致 ACL enforcement；
- 缺少 profile migration、cache generation 和跨平面 repair manager；
- 需要跨 backend contract tests 与 invalid-transition tests。
