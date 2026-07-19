# 10. Memory Lifecycle State Machine

## 1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Behavior Perspective |
| 问题 | Memory 有哪些真实状态、由谁触发和执行转换 |
| 成熟度 | 部分；存在 enum 和 plugin transition logic，但无统一全局 state machine |

## 2. 代码入口

| Concern | Package / file | Main API |
|---|---|---|
| enum and canonical fields | `src/internal/schemas/canonical.go` | `MemoryLifecycle`, `Memory.LifecycleState`, `IsActive` |
| algorithm suggestion | `src/internal/schemas/memory_management.go` | `MemoryManagementAlgorithm`, `AlgorithmDispatchOutput` |
| transition writer | `src/internal/worker/cognitive/` | dispatcher lifecycle/state/audit writes |
| policy-driven decay/quarantine | `src/internal/worker/cognitive/` reflection worker | `Reflect`/typed `Run` path |
| explicit stale/archive/delete | `src/internal/access/` admin handlers, `src/internal/worker/runtime.go`, `src/internal/storage/tiered.go` | internal/admin handlers and tier methods |
| agent adapter | `src/internal/agent/memory_manager.go` | `Compress`, `Summarize`, `Decay` |

## 3. 输入与输出

| Trigger input | Decision output | State mutation | Additional output |
|---|---|---|---|
| new Event | default/event lifecycle | new Memory lifecycle | Version/Edge/projection candidate |
| algorithm operation + Memory + state | `SuggestedLifecycleState` | Memory + algorithm state | produced IDs, audit metadata |
| recall query | score/reinforcement candidate | plugin-specific; not uniformly persisted | ranked memory view |
| reflection policy/TTL/confidence | retain/decay/archive/quarantine | Memory/tier/audit depending path | policy decision |
| admin delete/purge | logical or hard deletion | `IsActive`/state or physical removal | cleanup/audit result |

## 4. 内部组成

### Actual enum

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

### Transition ownership

| Trigger | Decision | Writer |
|---|---|---|
| Event ingest | Event object lifecycle or default | materializer |
| algorithm ingest/update/decay | plugin returns `SuggestedLifecycleState` | dispatcher applies verbatim |
| recall | plugin may score/reinforce internally；dispatcher recall不持久化 state | plugin/none |
| reflection | TTL/quarantine/confidence policy | reflection worker |
| internal stale | handler command | Gateway/store |
| archive/delete | admin/tier operation | storage/governance path |

### Guards and actions

MemoryBank-style lifecycle code contains candidate/active/reinforced/compressed/stale/archived/quarantine guards。其他 plugins 有不同规则；不存在跨 plugin 强制的 transition table。`IsActive` 与 `LifecycleState` 可能不完全一致，retrieval 还单独检查某些 policy tags/states。

## 5. 调用关系

Event ingest 初始化 Memory；internal memory routes 或 `AgentSession.MemoryManager` 触发 algorithm dispatcher；subscriber/reflection 可能异步更新 lifecycle；admin/tier paths 执行 archive/delete。各路径共享 `Memory` 字段，但不经过统一 transition service。

同步边界取决于调用入口：internal algorithm route 等待 dispatcher 结果；subscriber maintenance 在 ACK 后执行；query recall 不保证 reinforcement 已持久化。

## 6. 数据与状态

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

## 7. 正确性

Dispatcher lifecycle update写 Memory + algorithm state + audit，但不统一写 ObjectVersion、Edge、retrieval refresh。Reflection/admin 路径也各自有副作用。状态机因此是“多个 writer 共享字段”，不是集中 transition service。

## 8. 声明边界

可声明 lifecycle enum、plugin-driven transition suggestions、policy decay/quarantine 和 archive/delete operations。

不可声明完整可验证 state machine、统一逆向转换、所有路径强制审计或 state-tier strict binding。

## 9. 缺口

- 缺少统一 transition command、guard/action registry 和 invalid-transition error；
- 缺少 `IsActive`、lifecycle、validity、policy visibility 的单一不变量；
- 缺少 archive -> reactivate 和 quarantine release 的授权流程；
- 缺少 transition、ObjectVersion、Edge、audit、projection 的原子/可恢复更新；
- 缺少跨 plugin transition contract tests、逆向转换测试和并发 mutation 测试。
