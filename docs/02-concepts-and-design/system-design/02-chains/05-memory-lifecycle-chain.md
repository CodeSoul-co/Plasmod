# 5. Memory Lifecycle Chain

## 1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Chain |
| 目标 | 对 canonical Memory 执行提取、强化、衰减、压缩、总结、归档、治理和召回 |
| 关键路径 | Query recall/内部 memory API 可达；完整 pipeline 不是每次写入 gate |
| 成熟度 | 部分 |

## 2. 代码入口和接口

| Entry | API/type |
|---|---|
| Chain | `MemoryPipelineChain.Run(MemoryPipelineInput)` |
| Runtime API | `DispatchAlgorithm`, `DispatchRecall` |
| HTTP | `/v1/internal/memory/{recall,ingest,compress,summarize,decay,stale}` |
| Plugin | `schemas.MemoryManagementAlgorithm` |
| Dispatcher | `InMemoryAlgorithmDispatchWorker.Dispatch` |
| Async | EventSubscriber direct extraction/reflection/consolidation dispatch |

## 3. Input/output

| Operation | Input | Output | Mutation |
|---|---|---|---|
| extraction | event/agent/session/content | level-0 Memory ID | Memory + derivation |
| ingest algorithm | Memory IDs + context | updated count | MemoryAlgorithmState, Memory ref, audit |
| recall | query + candidate IDs + context | scored refs/MemoryView | baseline dispatcher不持久化 recall state；plugin内部行为依实现 |
| update | signals keyed by memory ID | states | algorithm state/lifecycle/audit |
| decay | IDs + timestamp | states | lifecycle/strength/retention/audit |
| compress | source IDs/context | produced IDs | derived Memory + state/audit |
| summarize | source IDs/context | produced IDs | summary Memory + audit |
| reflection | object/policies/time | applied result | Memory lifecycle/policy/tier |

完整 typed fields 见 [Object and Message Registry](../06-cross-reference/object-and-message-registry.md)。

## 4. Internal composition

| Component | Responsibility | Decision ownership |
|---|---|---|
| MemoryPipelineChain | fixed extraction -> optional consolidation/summary -> reflection | caller flags determine optional stages |
| Algorithm dispatcher | load objects, call plugin, persist state/output/audit | 不做 lifecycle threshold |
| baseline plugin | simple strength/decay/recall/compress/summarize | plugin |
| MemoryBank-style | candidate/active/reinforced/stale/compressed/archive/quarantine logic | plugin |
| Zep-style | repository-local Zep-inspired behavior | plugin |
| TieredObjectStore/reflection | hot/cold movement and policy handling | policy/config |

## 5. Call relationship

`MemoryPipelineChain` 与 algorithm dispatcher 是两条并存路径：前者组合 legacy/baseline workers，后者服务 plugin API。当前没有一个 lifecycle coordinator 把 recall signal、plugin decision、tier movement、projection refresh、version/audit 统一提交。

## 6. Data and state

| State | Location |
|---|---|
| canonical content/scope/lifecycle | `schemas.Memory` in ObjectStore |
| plugin-specific strength/retention/profile | MemoryAlgorithmStateStore |
| versions/relations | Version/Edge stores，但 dispatcher lifecycle update 并非总是写 version/edge |
| audit | AuditStore for dispatcher/reflection operations |
| projection | DataPlane; derived/lifecycle updates不保证每条路径同步 refresh |
| hot/cold placement | TieredObjectStore/cache/cold store |

## 7. Correctness

- Dispatcher 原样应用 plugin 的 `SuggestedLifecycleState`，content-level 决策不在 dispatcher。
- Compress/Summarize 输出按 plugin 结果存储，source Memory 通常保留。
- Plugin profile 切换不会自动迁移历史 algorithm state。
- lifecycle mutation、Version、projection 和 tier movement 缺少统一 transaction/reconciliation。
- archived reactivation 没有通用 operation/状态流程；cold query 读取不等于 canonical reactivation。

## 8. 声明边界

可声明：可插拔 memory algorithm、独立 algorithm state、基础生命周期字段、压缩/总结/衰减/治理 worker。

不可声明：所有 recall 都产生持久 reinforcement、完整自动 archive/reactivate state machine，或 lifecycle change 总是原子更新版本/索引/tier/audit。

## 9. 缺口

1. 统一 lifecycle command/result contract；
2. lifecycle transition guard + version + audit + projection transaction；
3. archive/reactivate/quarantine-release/hard-purge 明确 API；
4. plugin profile state migration；
5. derived Memory source edges 和 projection refresh 的强制 contract；
6. lifecycle lag/transition failure metrics。
