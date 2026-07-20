# 15. 架构决策记录

> Language: 中文 | [English](en/15-architecture-decision-records.md)

---

合并 Architecture Decision Records，记录选择、替代方案、后果和必须保持的不变量。

---

## 15.1. ADR-0001: Event/WAL 作为因果源

- Status: Accepted
- Context: agent state、memory、artifact 和 relation 会并发演进，直接覆盖对象无法恢复因果顺序。
- Decision: 核心业务写入先表达为 Event，并通过 WAL 获得 LSN；projection 生成 canonical objects。
- Consequences: 可以 replay、追踪 accepted/visible 和绑定 mutation event；调用方必须处理 accepted-not-visible。
- Alternatives: 直接 CRUD 作为唯一写路径，被拒绝，因为缺少顺序和 replay。direct CRUD 仅保留为管理/兼容入口。
- Invariant: 成功 projection 前不得推进 visible watermark。

---

## 15.2. ADR-0002: Canonical object 优先于 Retrieval

- Status: Accepted
- Context: ANN/lexical index 适合查找，但无法完整表达对象版本、关系和治理。
- Decision: ObjectStore/EdgeStore/VersionStore 保存权威对象；retrieval index 是可重建 projection。
- Consequences: embedding family 变化可 reindex；查询需要将 retrieval IDs 与 canonical/evidence 合并。
- Alternatives: 向量行作为唯一事实，被拒绝，因为 replay、state 和 provenance 语义不足。
- Invariant: index 命中不得覆盖或创造 canonical object 内容。

---

## 15.3. ADR-0003: Go Runtime + C++ Retrieval

- Status: Accepted, Conditional
- Context: runtime、HTTP、WAL 和 storage 需要 Go 的并发/工程生态；ANN backend 主要来自 C++ 生态。
- Decision: Go 定义业务 contract，CGO 调用 `libplasmod_retrieval`，C++ 封装 vendored Knowhere-style engine。
- Consequences: 需要 CMake、CGO、runtime library path 和 ABI 管理；无 native library 时使用 stub/lexical 降级。
- Alternatives: 全 Go ANN 或把全部 runtime 下沉 C++，当前均未采用。
- Invariant: RRF、policy、evidence 和 canonical semantics 留在 Go，不声明为第三方 ANN 内核能力。

---

## 15.4. ADR-0004: Hot/Warm/Cold 分层

- Status: Accepted
- Context: agent runtime 同时需要低延迟活跃对象、完整在线对象和低成本归档。
- Decision: HotObjectCache 保存高活跃对象，warm ObjectStore/segment 保存在线事实，cold store 保存显式归档对象。
- Consequences: promotion/archive/delete 必须跨 tier 协调；cold query 只在显式 `include_cold` 时执行。
- Alternatives: 所有对象常驻内存或每次写入同步 S3，因容量或写放大被拒绝。
- Invariant: cold copy 不替代 canonical/WAL backup，archive 不等于 hard delete。

---

## 15.5. ADR-0005: Query 返回 Structured Evidence

- Status: Accepted
- Context: agent 决策需要知道来源、版本和关系，而不只是 top-k IDs。
- Decision: `QueryResponse` 包含 objects、edges、versions、provenance、proof trace、filters 和 retrieval/cache summary。
- Consequences: query stage 需要 canonical lookups 与 graph expansion；prod middleware 可隐藏 debug traces。
- Alternatives: 只返回相似度列表，被保留为 warm/internal/objects-only 条件路径，不作为完整语义。
- Invariant: proof/provenance 只能来自已存事实或明确的 planner/retrieval step，不伪造外部证据。

---

## 15.6. ADR-0006: Use Knowhere-style Native ANN Behind An Adapter

### 15.6.1. Status

Accepted, build-dependent.

### 15.6.2. Context

Plasmod 需要 HNSW、IVF 和 DiskANN 等物理索引能力，但不应在 Go runtime 中重复实现成熟 ANN engine，也不能
把第三方内部对象模型暴露为 Agent database contract。

### 15.6.3. Decision

在 `cpp/vendor` 保留 source-level Knowhere-style engine，通过 `cpp/retrieval` 组合 C ABI，再由
`dataplane/retrievalplane` CGO bridge 调用。Go 层保留 object ID mapping、scope、policy、tiering、fusion 和
Evidence。

### 15.6.4. Consequences

- 获得多 index backend 与 native performance；
- 增加 CMake、CGO、ABI、license 和平台维护成本；
- build feature 决定某 index 是否可用；
- pure Go stub 仍可运行 canonical/lexical 路径；
- 不得把 Knowhere 内部实现宣称为 Plasmod 自有算法。

---

## 15.7. ADR-0007: Fuse Retrieval Candidates At The Result Layer

### 15.7.1. Status

Accepted.

### 15.7.2. Context

Lexical、dense、sparse、Hot、Warm 和 Cold 候选的原始 score 尺度不同，直接数值相加会依赖 backend 的距离
定义和归一化细节。

### 15.7.3. Decision

在 Go DataPlane 的结果层使用 rank-based fusion（RRF）合并候选，再执行 canonical load、scope/policy filter 和
Evidence assembly。Native backend 只返回自身候选和距离。

### 15.7.4. Consequences

- 不要求不同 backend score 可直接比较；
- 可以替换 native backend 而不移动业务语义；
- rank tie、candidate depth 和 RRF constant 会影响结果，必须稳定配置；
- RRF 不替代 latest-version、policy 或 relation constraints。

---

## 15.8. ADR-0008: Keep Vector-only Ingest As A Physical Projection Interface

### 15.8.1. Status

Accepted.

### 15.8.2. Context

预计算 embedding 和批量 segment 构建需要绕过 Event text embedding，但纯向量记录无法表达 Session、State、
Artifact、Edge、Version、Policy 和 provenance。

### 15.8.3. Decision

保留 `/v1/ingest/vectors` 和 Warm Segment register/query 作为物理 retrieval projection 接口。Agent-native
业务写入仍使用 Event/canonical path；vector-only interface 不升级为 canonical source of truth。

### 15.8.4. Consequences

- 可复用外部预计算向量并减少重复 embedding；
- 调用方必须提供稳定 object ID mapping 和 embedding compatibility tuple；
- 只写向量无法通过 WAL replay 恢复完整 Agent 对象；
- Query 使用这些候选后仍需 Go 层 canonical/evidence 处理。

---

## 15.9. ADR-0009: 显式一致性模式

| Field | Decision record |
|---|---|
| Status | Accepted |
| Context | Agent workload 对 ACK 延迟、可见性和后台吞吐的要求不同，隐式一致性无法给出清晰保证。 |
| Decision | consistency controller 提供 strict、bounded-staleness 和 eventual visibility。 |
| Consequences | 各 mode 的等待阶段和 timeout 不同；response/metrics 必须区分 accepted、projecting、visible。 |
| Rejected alternative | 无论完成阶段都返回同一个 success。 |
| Invariant | 较弱 mode 可以更早 ACK，但不能声明比实际完成阶段更强的 freshness。 |

---

## 15.10. ADR-0010: 原子 Canonical Projection 要求存储共置

| Field | Decision record |
|---|---|
| Status | Accepted |
| Context | object、edge 和 version 构成同一次 canonical mutation，拆到独立 transaction 会产生图/版本分歧。 |
| Decision | storage factory 要求 object、edge、version backend 可共同实现 `ApplyCanonicalProjection`；Badger 使用同一 transaction。 |
| Consequences | 限制任意混用 backend；未来分布式 backend 必须提供等价原子 contract 或明确降低保证。 |
| Rejected alternative | 对三个独立 store 做 best-effort write 却仍声明 canonical atomicity。 |
| Invariant | Runtime 必须拒绝无法满足 canonical projection contract 的配置。 |

---

## 15.11. ADR-0011: Canonical-first Projection 与 Watermark Fence

| Field | Decision record |
|---|---|
| Status | Accepted |
| Context | retrieval-first 会留下“索引候选存在但 canonical object 不存在”的泄漏窗口；canonical-first 会留下已持久化但索引尚未完成的内部窗口。 |
| Decision | 主回调先提交 canonical write set，再写 retrieval projection；只有二者成功才推进 visible watermark。Canonical object 持久化 `MutationLSN`，query 以 `ReadWatermarkLSN` 作为 visibility fence。 |
| Consequences | retrieval 失败时 canonical snapshot 可用于同 LSN 重试/reindex，普通 query 不暴露该 mutation；两个引擎仍非单一 ACID transaction。 |
| Rejected alternative | retrieval-first，或 canonical commit 后立即对外 visible。 |
| Invariant | `MutationLSN > ReadWatermarkLSN` 的 object 不得由普通 canonical query path 返回。 |

---

## 15.12. ADR-0012: Canonical Access 与 Evidence-safe Traversal

| Field | Decision record |
|---|---|
| Status | Accepted, security boundary partial |
| Context | 单一 `scope` 字符串无法表达 owner、层级 scope、agent/role grant 和 share contract；仅过滤 seed 会从图扩展泄漏 private endpoint。 |
| Decision | Memory、State、Artifact、Edge、ObjectVersion 持久化 `CanonicalAccess`。`/v1/query` 在 hydration 前执行 access gate，并在 evidence assembly 后重验 node/edge endpoint/proof/provenance；允许原因通过 `AccessDecision` 返回。 |
| Consequences | shared derivation 绑定 typed ShareContract 并经 WAL 创建；调用方必须由可信 gateway 绑定 requester identity。raw CRUD/lifecycle route 仍需后续统一 write gate。 |
| Rejected alternative | 只依赖 retrieval metadata filter 或事后 contamination counter。 |
| Invariant | 未授权 object 或其 graph reference 不得出现在普通 QueryResponse；拒绝 decision 不披露对象存在。 |

---

## 15.13. ADR Index

| ADR | Decision | Primary code boundary |
|---|---|---|
| 0001 | Event/WAL 作为因果 source | `eventbackbone`, ingest runtime |
| 0002 | Canonical object 是权威事实 | `storage`, `evidence`, `dataplane` |
| 0003 | Go runtime + C++ retrieval | `retrievalplane`, `cpp/` |
| 0004 | Hot/Warm/Cold 分层 | `storage/tiered.go`, DataPlane |
| 0005 | Query 返回结构化 Evidence | `schemas/query.go`, `evidence` |
| 0006 | Native ANN 置于 adapter 后 | C ABI and CGO bridge |
| 0007 | Go result layer 执行 RRF | DataPlane/RRF |
| 0008 | Vector-only 是 projection API | vector/warm routes |
| 0009 | 一致性模式显式化 | `worker/consistency` |
| 0010 | Canonical projection store 共置 | storage factory/projection transaction |
| 0011 | Canonical-first + watermark fence | Runtime projection/query access |
| 0012 | Canonical access + evidence-safe traversal | schemas/semantic/runtime access |

新增跨模块、影响 API/持久化/故障语义的设计决定应增加 ADR，并记录 context、decision、alternatives、consequences
和 migration impact。ADR 不替代实现证据，第 14 章仍须标记实际完成度。
