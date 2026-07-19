# 14. Canonical Memory Representation Mechanism

## 1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Mechanism |
| 目标 | 用稳定 canonical Memory 连接内容、来源、生命周期、算法、治理、版本和关系 |
| 成熟度 | 完整字段模型；部分引用对象的 active persistence/validation 有限 |

## 2. Code and schema entry

- `schemas.Memory`, `MemoryAlgorithmState`, `ObjectVersion`, `Edge`, `PolicyRecord`, `AuditRecord`；
- `ObjectStore`, `MemoryAlgorithmStateStore`, Version/Edge/Policy/Audit stores；
- `ObjectModelRegistry` 将 Memory 标记为 versionable/indexable。

## 3. Representation

```text
Memory = identity/type/scope/content/quality/validity/lifecycle
       + source/provenance references
       + embedding and algorithm-state references
       + dataset lineage
```

完整字段见 [Object and Message Registry](../06-cross-reference/object-and-message-registry.md)。

## 4. Internal vs external fields

| Concern | Stored in Memory | External record |
|---|---|---|
| content/summary/type/scope | yes | retrieval text/vector projection |
| provenance | source IDs/ref | Edge + derivation log |
| lifecycle | state/isActive/TTL/valid interval | Policy/Audit/Version |
| algorithm | single ref | `(memory,algorithm)` state records |
| embedding | ref only | vector index / optional Embedding object model |
| policy/share | tags/scope | PolicyRecord/ShareContract |
| version | current number | ObjectVersion history |

## 5. Input/output and calls

Materializer/plugin/communication/reflection/admin handlers write Memory；query/algorithm/tier/governance/evidence read it。There is no single `MemoryRepository` service that validates every writer。

## 6. State semantics

`IsActive` 是粗粒度 serving flag，`LifecycleState` 是细粒度阶段，tier 是物理 placement；三者相关但不等价。`ValidFrom/ValidTo` 是 canonical validity，不等于 retrieval segment timestamp alone。

## 7. Correctness

- Stable ID/version/source refs are replay boundaries。
- Direct Memory POST can bypass version/projection/audit。
- `AlgorithmStateRef` 与 multi-algorithm state store 的引用语义尚不完全规范。
- Embedding object type 已定义，但 active path主要在 index metadata/vector store中保存 embedding。

## 8. 声明边界

可声明 canonical Memory 将算法状态、治理记录和检索投影从本体解耦。

不可声明所有引用都具有数据库 foreign-key enforcement，或 Memory row alone contains full provenance/evidence/embedding payload。

## 9. 缺口

需要 central Memory mutation validator、reference integrity checks、multi-algorithm ref schema、versioned validity update API、Embedding object persistence contract 和 writer inventory tests。
