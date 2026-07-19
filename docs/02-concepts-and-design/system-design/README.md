# Plasmod System Design Reference

本目录是 Plasmod 核心库的系统设计核对入口。它严格区分 Architecture、Chain、Perspective、Mechanism 和 Engine，所有结论均以当前 `dev` 分支的可执行代码、接口、构造链和测试为依据。

## 分类规则

| 类型 | 回答的问题 | 是否是代码对象 |
|---|---|---|
| Architecture | 系统由什么构成、启动后如何协作 | 通常是多个 package 的组合 |
| Chain | 请求或事件按什么动态顺序执行 | 四条 Chain 有具体类型，但主路径接线程度不同 |
| Perspective | 从某个正确性或数据角度如何观察系统 | 不是独立模块 |
| Mechanism | 系统为什么能提供某项能力 | 由多个 Engine 和不变量共同实现 |
| Engine | 机制在代码中由哪些具体组件落地 | 应落到 type/interface/constructor/method |

## 成熟度标签

| 标签 | 判定标准 |
|---|---|
| 完整 | 在 `app.BuildServer` 中接线、关键路径可达，核心行为有测试 |
| 部分 | 有真实实现和调用，但能力范围、持久化、治理或恢复语义不完整 |
| 占位 | 有 type、接口或注册对象，但默认关键路径不调用或只记录元数据 |
| 规划 | 当前 active core 中没有可声明的实现，只能描述目标接口和缺口 |

“完整”不表示 API 永久稳定；公共/内部稳定性仍以 [Route Index](../../05-api-and-reference/route-index.md) 为准。

## 统一核对模板

每个条目均按以下九部分组织：

1. 定位：类型、设计目标、关键路径和成熟度；
2. 代码入口：package、主文件、constructor、接口和方法；
3. 输入与输出：字段、状态变更和副作用；
4. 内部组成：子组件、数据结构、策略和替换点；
5. 调用关系：上游、下游、所属 Chain、同步/异步边界；
6. 数据与状态：canonical、projection、algorithm/lifecycle、持久/内存状态；
7. 正确性：事务、幂等、重试、失败、恢复、审计与指标；
8. 声明边界：当前代码可以和不可以支持的系统声明；
9. 缺口：缺失实现、占位、测试、指标和重构要求。

## 30 项设计目录

### Architecture

1. [Static System Architecture](01-architecture/01-static-system-architecture.md)
2. [Dynamic System Runtime](01-architecture/02-dynamic-system-runtime.md)
3. [Scheduler Design](01-architecture/03-scheduler-design.md)

### Chain

4. [Ingest Chain](02-chains/04-ingest-chain.md)
5. [Memory Lifecycle Chain](02-chains/05-memory-lifecycle-chain.md)
6. [Query and Evidence Chain](02-chains/06-query-and-evidence-chain.md)
7. [Collaboration Chain](02-chains/07-collaboration-chain.md)

### Core Design Perspective

8. [Memory Subsystem Architecture](03-perspectives/08-memory-subsystem-architecture.md)
9. [Canonical State vs Retrieval Projection](03-perspectives/09-canonical-vs-retrieval-projection.md)
10. [Memory Lifecycle State Machine](03-perspectives/10-memory-lifecycle-state-machine.md)
11. [Consistency and Recovery Model](03-perspectives/11-consistency-and-recovery-model.md)
12. [Evidence Construction Pipeline](03-perspectives/12-evidence-construction-pipeline.md)

### Mechanism

13. [Event-derived Memory Construction](04-mechanisms/13-event-derived-memory-construction.md)
14. [Canonical Memory Representation](04-mechanisms/14-canonical-memory-representation.md)
15. [Dual-plane Data](04-mechanisms/15-dual-plane-data.md)
16. [Memory Evolution](04-mechanisms/16-memory-evolution.md)
17. [Evidence Construction](04-mechanisms/17-evidence-construction.md)
18. [Runtime Coordination](04-mechanisms/18-runtime-coordination.md)
19. [Cross-module Consistency](04-mechanisms/19-cross-module-consistency.md)
20. [Memory Scope and Governance](04-mechanisms/20-memory-scope-and-governance.md)

### Engine

21. [Object Derivation Engine](05-engines/21-object-derivation-engine.md)
22. [Execution Coordination Engine](05-engines/22-execution-coordination-engine.md)
23. [Adaptive Retrieval Engine](05-engines/23-adaptive-retrieval-engine.md)
24. [Evidence Assembly Engine](05-engines/24-evidence-assembly-engine.md)
25. [Memory Evolution Engine](05-engines/25-memory-evolution-engine.md)
26. [Canonical Object Graph Engine](05-engines/26-canonical-object-graph-engine.md)
27. [Tiered Storage Engine](05-engines/27-tiered-storage-engine.md)
28. [Reconciliation Manager](05-engines/28-reconciliation-manager.md)
29. [Memory Governance Engine](05-engines/29-memory-governance-engine.md)
30. [Intelligent Scheduler](05-engines/30-intelligent-scheduler.md)

## 跨模块注册表

| 注册表 | 用途 |
|---|---|
| [Object and Message Registry](06-cross-reference/object-and-message-registry.md) | canonical、Event、Query、Worker I/O 的字段真值 |
| [Interface Implementation Registry](06-cross-reference/interface-implementation-registry.md) | interface、实现、constructor、bootstrap 选择和替换边界 |
| [API to Engine Matrix](06-cross-reference/api-to-engine-matrix.md) | 所有 HTTP/transport 接口到 Chain/Engine 的映射 |
| [Execution State and Failure Matrix](06-cross-reference/execution-state-and-failure-matrix.md) | 阶段、同步/异步、事务、失败窗口和恢复动作 |
| [Claim and Test Boundary](06-cross-reference/claim-and-test-boundary.md) | 系统声明、代码证据、测试证据和不可声明项 |

## 事实边界

- Active composition root 是 `src/internal/app/bootstrap.go`。
- Gateway 的主 Event/Query 路径直接调用 `worker.Runtime`，不是统一提交到 `worker.Orchestrator`。
- `worker/chain` 中四条 Chain 是真实类型，但各自只覆盖一段执行流程。
- `coordinator/controlplane`、`eventbackbone/streamplane`、`platformpkg` 和 `cpp/vendor` 属于上游/兼容区域；除非 active adapter 明确调用，否则不能当作默认进程能力。
- 本目录只描述核心库，不记录外部实验数据、运行结果或实验仓库操作。
