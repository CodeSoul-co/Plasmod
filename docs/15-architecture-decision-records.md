# 15. 架构决策记录

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

## 15.9. ADR Index

| ADR | Decision |
|---|---|
| [`0001`](15-architecture-decision-records.md) | Event 作为因果 source |
| [`0002`](15-architecture-decision-records.md) | Canonical object 与 retrieval projection 分层 |
| [`0003`](15-architecture-decision-records.md) | Go runtime + C++ retrieval |
| [`0004`](15-architecture-decision-records.md) | Hot/Warm/Cold 分层 |
| [`0005`](15-architecture-decision-records.md) | 返回结构化 Evidence |
| [`0006`](15-architecture-decision-records.md) | Knowhere-style native ANN adapter |
| [`0007`](15-architecture-decision-records.md) | 结果层 RRF fusion |
| [`0008`](15-architecture-decision-records.md) | Vector-only 仅作为物理投影接口 |

新增跨模块、影响 API/持久化/故障语义的设计决定应增加 ADR，并记录 context、decision、alternatives、consequences
和 migration impact。
