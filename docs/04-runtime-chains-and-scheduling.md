# 04. 运行时、四条执行 Chain 与调度

> Language: 中文 | [English](en/04-runtime-chains-and-scheduling.md)

---

把 Chain 作为动态执行路径，依次还原 Ingest、Memory、Query 和 Collaboration 的真实调用。

---

## 04.1. Dynamic System Runtime

### 04.1.1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Architecture |
| 设计目标 | 还原外部请求/内部事件进入系统后的真实函数级路径 |
| 关键路径 | 是 |
| 当前成熟度 | 完整的 Event/Query 主路径；统一 Chain Router 仅部分接线 |

### 04.1.2. 请求入口

| Boundary | Entry | Calls Runtime directly? |
|---|---|---|
| HTTP Event/Query | `Gateway.RegisterAPIRoutes` handlers | 是 |
| HTTP canonical CRUD | Gateway handlers | 否，直接 store/coordinator |
| Admin | Gateway management handlers | 视操作调用 Runtime/store |
| Internal transport | `transport.Server` | 通过 `RuntimeAPI` 调 warm/native methods |
| gRPC | gRPC server -> Gateway/service adapter | 是/间接 |
| WAL background | consistency recovery, EventSubscriber | Controller project callback / NodeManager |

### 04.1.3. 真实动态路径

#### 04.1.3.1. Event write

```text
HTTP trigger
-> Gateway.handleIngest
-> Runtime.SubmitIngestContext
-> consistency.Controller.Submit
-> WAL.Append + LSN
-> projection queue
-> Runtime.projectWALEntry
-> Materializer
-> DataPlane.Ingest
-> RuntimeStorage.ApplyCanonicalProjection
-> tracker/checkpoint/watermark visible
-> strict response or pending response
-> EventSubscriber auxiliary maintenance
```

#### 04.1.3.2. Query

```text
HTTP trigger
-> Gateway.ServiceQueryContext
-> Runtime.ExecuteQueryContext
-> consistency read gate
-> QueryPlanner.Build
-> NodeManager.DispatchQuery -> DataPlane.Search
-> structured/canonical filters and supplements
-> Evidence.Assembler.Build
-> QueryChain.Run (proof + subgraph)
-> provenance/metrics/visibility filtering
-> QueryResponse
```

#### 04.1.3.3. Direct canonical mutation

```text
HTTP canonical POST -> handler -> Object/Edge/Policy/Contract store
```

该路径不自动执行 WAL、projection、version、evidence precompute 或 replay 语义。

### 04.1.4. Chain 选择真值

| Chain | Concrete object | 主路径如何进入 |
|---|---|---|
| MainChain | `chain.MainChain` | Orchestrator task 可调用；Event Runtime 主写不调用它 |
| MemoryPipelineChain | concrete | Orchestrator task 可调用；subscriber 通常直接逐 worker dispatch |
| QueryChain | Runtime 持有 | `executeQuery` 在 retrieval/assembler 后同步调用 |
| CollaborationChain | concrete | Orchestrator task 可调用；internal API 通常直接 Runtime dispatch |

因此不存在一个对所有请求生效的 central Chain Router。`Orchestrator.execute` 有 type switch，但没有 Gateway/Runtime submit call site。

### 04.1.5. 输入输出和状态变化

| Path | Input | Output | Durable mutations |
|---|---|---|---|
| Event | Dynamic Event | ACK with LSN/status/event/memory/state/artifact IDs | WAL、retrieval projection、Event/Memory/checkpoint State/可选 Artifact/Edge/Version；专用维护异步 |
| Query | QueryRequest | QueryResponse | metrics/cache reads；通常无 canonical mutation |
| Memory operation | AlgorithmDispatchInput-like body | AlgorithmDispatchOutput/MemoryView | Memory, algorithm state, audit depending operation |
| Collaboration | agent/memory/conflict IDs | shared/winner result | Memory/Edge; WAL/version/projection 不统一 |
| Admin recovery | LSN/range/action | summary/status | replay/reindex/reset/purge dependent |

### 04.1.6. 同步/异步与返回条件

| Mode/path | Return condition |
|---|---|
| strict ingest | WAL + projection + tracker visible；辅助 subscriber 不在 gate 内 |
| bounded/eventual ingest | WAL accepted and task enqueued |
| query | read gate + current retrieval/evidence build complete |
| Orchestrator task | `Submit` only returns queued bool；没有 result future |
| subscriber | background polling；panic goes to in-memory DLQ/overflow/error channel |

### 04.1.7. 失败处理

- Context cancel/deadline 在 Gateway -> Runtime -> consistency read/write gate 传播。
- Projection 重试由 Controller 执行；strict 失败返回 accepted-not-visible。
- NodeManager 多个 void dispatch helper 不向上游传播 worker error。
- Query `DataPlane.Search` 返回 value 而非 error，检索内部失败主要通过空结果/metrics/log 暴露。
- partial windows 与恢复见 [Execution State and Failure Matrix](14-implementation-status-gaps-and-claim-boundaries.md)。

### 04.1.8. 声明边界

可声明：Event 和 Query 有明确 Runtime 主链，canonical state 和 projection 在 Runtime 汇合，后台维护由 subscriber/flush loop 执行。

不可声明：Gateway 总是先调用 Orchestrator，或四条 Chain 对所有阶段提供统一 task result/retry/cancellation。

### 04.1.9. 缺口

1. 统一决定 Runtime direct orchestration 与 Orchestrator 的关系；
2. 为辅助 worker 增加 error/result propagation；
3. 为 collaboration mutation 补齐 WAL/version/projection 一致路径；
4. 为 query retrieval error 增加 typed error contract；
5. 统一 request/task trace ID 的跨模块传播。

---

## 04.2. Scheduler Design

### 04.2.1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Architecture |
| 设计目标 | 说明 Chain、worker、写入可见性和资源如何被路由/限流/恢复 |
| 关键路径 | consistency scheduler 是；Orchestrator 和 WorkerScheduler 不是主请求 gate |
| 当前成熟度 | 部分 |

### 04.2.2. 三类现有调度组件

| Component | File/type | 实际职责 | Main-path reachability |
|---|---|---|---|
| Consistency scheduler | `worker/consistency.Controller` | write admission、mode gate、sharded queue、slot、retry、deadline、checkpoint | Event ingest 关键路径 |
| Chain Orchestrator | `worker.Orchestrator` | 4 priority queues + fixed worker pool + Chain type switch | bootstrap 启动，但无主路径 submit caller |
| WorkerScheduler | `coordinator.WorkerScheduler` | dispatched/active counter | registry 可见，不执行任务 |
| NodeManager | `nodes.Manager` | registration + first-worker synchronous dispatch | Runtime/Chain 真实调用 |
| Microbatch | `InMemoryMicroBatchScheduler` | opaque FIFO buffer + explicit flush | Collaboration enqueue；无定时 drain |

### 04.2.3. Task 和分类

| `worker.Task` field | Meaning |
|---|---|
| `ID` | caller-provided task identity |
| `Type` | ingest/memory/query/collaboration |
| `Priority` | 0 low, 1 normal, 2 high, 3 urgent |
| `Payload` | chain-specific `any` |
| `Submitted` | queue timestamp |

Orchestrator 没有 dependency、deadline、tenant、cost、resource request、retry count、cancellation token 或 result channel 字段。

### 04.2.4. Routing and Ordering

| Feature | Current behavior |
|---|---|
| Chain selection | Orchestrator `TaskType` switch；主 Runtime 自行函数路由 |
| Priority | urgent -> high -> normal -> low，非抢占 |
| Dependency | 无统一 DAG；函数调用顺序隐式表达 |
| Worker selection | NodeManager 取注册列表第一个实现 |
| Load balancing | 无 active resource-aware selection |
| Fairness/tenant isolation | 无 |
| SLA/deadline | consistency bounded lag + request context；Orchestrator 无 task deadline |
| Rate limit | consistency slots/queue + Gateway write semaphore；无 tenant token bucket |
| Microbatch | buffer/explicit flush；不是通用 query batching scheduler |

### 04.2.5. Backpressure, Cancellation and Recovery

| Component | Backpressure | Cancellation | Failure/retry |
|---|---|---|---|
| Controller | bounded slots/queues, blocks admission | request/root/admission context | configured projection/checkpoint retry |
| Orchestrator | queue send blocks up to 30 s then returns false | worker exits on context; queued task无单独取消 | no task retry/result |
| NodeManager | synchronous call | caller context不在多数 worker interface | many errors discarded by dispatch helper |
| Microbatch | unbounded until caller flush relative to batchSize behavior | none | none |

### 04.2.6. Metrics and State

| Metrics | Source | Limitation |
|---|---|---|
| Controller status | queue/active/tracker/checkpoint/mode | write-specific |
| Orchestrator stats | submitted/completed/dropped/in-flight/depth | 任务未从主路径提交时代表性有限 |
| WorkerScheduler stats | dispatched/active by worker type | 必须由 caller 显式 Dispatch/Complete；不等于 NodeManager real activity |
| global metrics | query/write latency, visibility, counters | in-process, not scheduler feedback loop |

### 04.2.7. Correctness

- Controller 通过 generation、pause/reset、mode gate 和 tracker 保证 reset/shutdown 不混入旧 task。
- Orchestrator `execute` 忽略 ChainResult/error，`Completed` 表示调用结束而不表示业务成功。
- 严格优先队列可能使低优先级任务饥饿；没有 aging/fairness。
- NodeManager 多实现注册不等价于冗余或负载均衡。

### 04.2.8. 声明边界

可声明：Plasmod 有写入一致性调度、固定优先级 Chain dispatcher 和 worker registry/dispatch 三类基础能力。

不可声明：有统一智能调度器、资源成本优化、tenant fairness、dependency DAG、deadline-aware scheduling 或自动反馈优化。

### 04.2.9. 缺口与目标接口

目标 `ScheduledTask` 至少需要：task/chain type、priority、dependencies、deadline、tenant/scope、consistency、resource estimate、attempt、idempotency key、context/result channel。

目标 Scheduler 需增加 classifier、dependency resolver、cost/SLA evaluator、resource allocator、worker health/load、fair queue、cancellation、retry policy 和 trace propagation；在接入前 [Intelligent Scheduler](04-runtime-chains-and-scheduling.md) 只能标为部分/规划。

---

## 04.3. Ingest Chain

### 04.3.1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Chain |
| 目标 | 将 Dynamic Event 变为 WAL record、canonical objects 和 retrieval projection |
| 关键路径 | 是 |
| 成熟度 | 完整主写链；`MainChain` concrete abstraction 仅部分接线 |

### 04.3.2. 代码入口

| Layer | Entry |
|---|---|
| HTTP | `Gateway.handleIngest` |
| Runtime | `SubmitIngestContext`, `projectWALEntry` |
| Consistency | `Controller.Submit`, `projectWithRetry`, `Tracker` |
| Derivation | `materialization.Service.MaterializeEvent` |
| Persistence | `RuntimeStorage.ApplyCanonicalProjection` |
| Conceptual chain type | `chain.MainChain.Run` |
| Background | `EventSubscriber.addBuiltinHandlers` |

### 04.3.3. 实际阶段、输入输出和顺序

| # | Stage | Input | Output/mutation | Sync rule |
|---|---|---|---|---|
| 1 | decode/normalize | Event JSON | normalized Event | sync |
| 2 | admission/mode | Event + context | slot/shard/mode/deadline | sync |
| 3 | WAL append | Event | WALEntry/LSN | sync all modes |
| 4 | materialize in memory | WALEntry.Event | IngestRecord、Memory、checkpoint State、可选 Artifact、Edge、Version | projection worker |
| 5 | retrieval ingest | IngestRecord | lexical/vector segment state | before canonical commit |
| 6 | canonical commit | Event/Memory/checkpoint State/可选 Artifact/Edges/Versions | store transaction/upserts | visibility gate |
| 7 | hot/conflict/precompute | Memory/Event | cache, conflict edge, evidence fragment | same projection callback but not all are gate-critical |
| 8 | tracker visible | LSN | checkpoint/watermark | strict waits |
| 9 | subscriber maintenance | WAL scan | keyed State, tool trace, reflection, conflict, consolidation | async |

重要事实：`MaterializationResult.State/StateVersion` 虽被构造，当前 `Runtime.projectWALEntry` 没有把它们放入 `CanonicalProjection`；可查询 keyed State 主要由异步 `StateMaterializationWorker` 持久化。不能把 State 的辅助完成时间等同于 strict Memory visibility。

### 04.3.4. MainChain abstraction

`MainChain.Run(MainChainInput)` 依次调用 object/state/tool/index/graph workers。它假设 WAL 已写，但 Runtime 主写链并不调用 MainChain；Orchestrator 才会调用它，且 Orchestrator 没有主请求 submit caller。因此 MainChain 是真实可测试组合对象，但不是当前权威 Event write transaction。

### 04.3.5. Canonical object rules

| Event signal | Output |
|---|---|
| every accepted Event | `mem_<event_id>`, Memory version, base/causal edges, retrieval record |
| artifact/tool-like | Artifact + version + artifact edges；worker 还可能生成 tool trace |
| payload with state key | keyed `state_<agent_id>_<state_key>` via State worker |
| parent/causal/source/target refs | typed Edge according to builders |

`materialization.enabled/targets` 当前作为 normalized metadata/hook input，不是跳过默认 Memory projection 的通用 gate。

### 04.3.6. State and side effects

| Plane | Mutation |
|---|---|
| Event | WAL append, LSN/logical time |
| Canonical | Memory, optional Artifact, Edge, Version; async State/tool artifacts |
| Projection | DataPlane ingest, dirty flush flag |
| Evidence | in-memory fragment cache, derivation log through workers |
| Metrics | write latency/visible, counts, session step |

### 04.3.7. Correctness and failure

- Deterministic IDs support replay upsert/idempotency.
- Projection retry is LSN-based；strict returns only after tracker visible。
- WAL committed but projection failed is an accepted-not-visible state, not a clean rollback。
- Retrieval success followed by canonical failure can leave projection/canonical divergence；replay/reindex repairs it。
- Auxiliary worker errors do not consistently fail the original ACK；subscriber uses in-memory DLQ/overflow/error reporting, not durable dead-letter storage。

### 04.3.8. 声明边界

可声明：Event-first ingest、WAL LSN、deterministic canonical derivation、retrieval projection、three visibility modes 和 replay。

不可声明：ACK 会等待专用 keyed State、tool trace、lifecycle/evidence 维护全部完成，或 WAL/retrieval/canonical/S3 位于一个 ACID transaction 中。

### 04.3.9. 缺口

1. 已完成：checkpoint State 与 StateVersion 进入 visibility transaction，并返回 `state_id`；
2. 合并 MainChain 与 Runtime 主链或明确废弃其主链含义；
3. worker error 必须进入 ACK/maintenance status；
4. 增加 projection/canonical divergence marker；
5. 为 duplicate Event 的 payload conflict 定义拒绝/覆盖策略。

---

## 04.4. Memory Lifecycle Chain

### 04.4.1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Chain |
| 目标 | 对 canonical Memory 执行提取、强化、衰减、压缩、总结、归档、治理和召回 |
| 关键路径 | Query recall/内部 memory API 可达；完整 pipeline 不是每次写入 gate |
| 成熟度 | 部分 |

### 04.4.2. 代码入口和接口

| Entry | API/type |
|---|---|
| Chain | `MemoryPipelineChain.Run(MemoryPipelineInput)` |
| Runtime API | `DispatchAlgorithm`, `DispatchRecall` |
| HTTP | `/v1/internal/memory/{recall,ingest,compress,summarize,decay,stale}` |
| Plugin | `schemas.MemoryManagementAlgorithm` |
| Dispatcher | `InMemoryAlgorithmDispatchWorker.Dispatch` |
| Async | EventSubscriber direct extraction/reflection/consolidation dispatch |

### 04.4.3. Input/output

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

完整 typed fields 见 [Object and Message Registry](14-implementation-status-gaps-and-claim-boundaries.md)。

### 04.4.4. Internal composition

| Component | Responsibility | Decision ownership |
|---|---|---|
| MemoryPipelineChain | fixed extraction -> optional consolidation/summary -> reflection | caller flags determine optional stages |
| Algorithm dispatcher | load objects, call plugin, persist state/output/audit | 不做 lifecycle threshold |
| baseline plugin | simple strength/decay/recall/compress/summarize | plugin |
| MemoryBank-style | candidate/active/reinforced/stale/compressed/archive/quarantine logic | plugin |
| Zep-style | repository-local Zep-inspired behavior | plugin |
| TieredObjectStore/reflection | hot/cold movement and policy handling | policy/config |

### 04.4.5. Call relationship

`MemoryPipelineChain` 与 algorithm dispatcher 是两条并存路径：前者组合 legacy/baseline workers，后者服务 plugin API。当前没有一个 lifecycle coordinator 把 recall signal、plugin decision、tier movement、projection refresh、version/audit 统一提交。

### 04.4.6. Data and state

| State | Location |
|---|---|
| canonical content/scope/lifecycle | `schemas.Memory` in ObjectStore |
| plugin-specific strength/retention/profile | MemoryAlgorithmStateStore |
| versions/relations | Version/Edge stores，但 dispatcher lifecycle update 并非总是写 version/edge |
| audit | AuditStore for dispatcher/reflection operations |
| projection | DataPlane; derived/lifecycle updates不保证每条路径同步 refresh |
| hot/cold placement | TieredObjectStore/cache/cold store |

### 04.4.7. Correctness

- Dispatcher 原样应用 plugin 的 `SuggestedLifecycleState`，content-level 决策不在 dispatcher。
- Compress/Summarize 输出按 plugin 结果存储，source Memory 通常保留。
- Plugin profile 切换不会自动迁移历史 algorithm state。
- lifecycle mutation、Version、projection 和 tier movement 缺少统一 transaction/reconciliation。
- archived reactivation 没有通用 operation/状态流程；cold query 读取不等于 canonical reactivation。

### 04.4.8. 声明边界

可声明：可插拔 memory algorithm、独立 algorithm state、基础生命周期字段、压缩/总结/衰减/治理 worker。

不可声明：所有 recall 都产生持久 reinforcement、完整自动 archive/reactivate state machine，或 lifecycle change 总是原子更新版本/索引/tier/audit。

### 04.4.9. 缺口

1. 统一 lifecycle command/result contract；
2. lifecycle transition guard + version + audit + projection transaction；
3. archive/reactivate/quarantine-release/hard-purge 明确 API；
4. plugin profile state migration；
5. derived Memory source edges 和 projection refresh 的强制 contract；
6. lifecycle lag/transition failure metrics。

---

## 04.5. Query and Evidence Chain

### 04.5.1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Chain |
| 目标 | 将 Query 转成候选对象并返回 objects、relations、versions、provenance、policy 和 proof |
| 关键路径 | 是 |
| 成熟度 | 完整基础 structured evidence；高级 operator、ACL 和 hydration 仍部分 |

### 04.5.2. Entry and interfaces

| Layer | Entry/interface |
|---|---|
| HTTP | `POST /v1/query`, `Gateway.ServiceQueryContext` |
| Runtime | `ExecuteQueryContext`, `executeQuery` |
| Planner | `QueryPlanner.Build` |
| Retrieval | `DataPlane.Search` through `NodeManager.DispatchQuery` |
| Assembly | `Assembler.Build` |
| Reasoning | `QueryChain.Run` |
| Response | `schemas.QueryResponse` + visibility middleware |

### 04.5.3. Stages

| # | Stage | Concrete behavior |
|---|---|---|
| 1 | read consistency | strict/bounded waits；eventual skips |
| 2 | plan | defaults TopK, namespace, time/types/tier and carries descriptor filters |
| 3 | candidate retrieval | Hot/Warm, optional Cold；lexical/vector/sparse/native depending config |
| 4 | candidate correction | CJK fallback, type/selector/target filters, inactive filtering |
| 5 | canonical supplement | State/Artifact/other requested types from ObjectStore |
| 6 | policy filters | `PolicyEngine.ApplyQueryFilters` generates filters unless minimal mode |
| 7 | evidence assembly | edges, versions, provenance, policy annotations, cache fragments, proof skeleton |
| 8 | QueryChain | prefetch graph nodes/edges, BFS proof trace, one-hop subgraph, edge merge |
| 9 | provenance/metrics | embedding provenance, evidence support, contamination observation |
| 10 | output filtering | APP_MODE visibility strips internal/debug fields in production |

### 04.5.4. Inputs and outputs

`QueryRequest` 与 `QueryResponse` 的完整字段见 [Object and Message Registry](14-implementation-status-gaps-and-claim-boundaries.md)。

| Output section | Source |
|---|---|
| `objects` | retrieval IDs + canonical supplements |
| `nodes` | QueryChain hydrated Memory/Event/Artifact graph nodes |
| `edges` | assembler BulkEdges + subgraph merge |
| `versions` | latest version lookup |
| `provenance` | Memory source refs, State/Artifact event refs, Edge refs, versions, embedding annotations |
| `proof_trace` | assembler stages/cache fragments + BFS/derivation steps |
| `applied_filters` | PolicyEngine descriptor output |
| `retrieval` | tier/cold diagnostics and hit/add counts |

### 04.5.5. Retrieval details

- Default search is hot/warm；Cold 只在 `include_cold=true` 时参与。
- Go 层负责 candidate merge/filter/evidence；C++ native engine 只负责物理 ANN。
- RRF/score normalization 在 dataplane/retrieval implementation 内；默认 RRF 常数由代码定义。
- `objects_only` fast path 跳过 evidence/QueryChain。
- `/v1/query/batch` 是 direct warm ANN，不属于本 evidence chain。

### 04.5.6. Data and state

Query 通常只读 canonical/retrieval stores；会更新 metrics、hot cache observation 或 evidence cache stats。它不承诺把 proof response 持久化。

Inactive memories 被过滤；cold-origin archived IDs 在 explicit cold query 中可被保留。Quarantine/retracted 当前在 Assembler 中主要生成 annotation，完整 deny/mask enforcement 不是集中式完成。

### 04.5.7. Correctness and failure

- `query_status` 区分真实 retrieval hit 与 canonical supplement。
- Candidate ID hydration 依赖 deterministic prefix + ObjectStore；未知 ID 默认推断 memory，存在误分类风险。
- Graph expansion 是基于已有 Edge，不证明 graph completeness。
- `DataPlane.Search` 无 error 返回，backend failure 可表现为空结果。
- Evidence cache miss 退化为 query-time evidence，不应改变 canonical answer contract。

### 04.5.8. 声明边界

可声明：hybrid/tiered candidate retrieval、canonical supplement、Edge/Version/Provenance/Proof structured response。

不可声明：所有 policy 都在 EvidenceAssembler 强制执行、所有对象都完整 hydrate 到 payload、graph/proof 是形式化证明或 evidence completeness 已量化。

### 04.5.9. 缺口

1. typed retrieval error；
2. full object hydration contract 或明确只返回 ID；
3. policy deny/mask 与 annotation 分离；
4. advanced response mode executor 接线；
5. configurable graph depth/edge filters from QueryRequest；
6. evidence completeness/confidence formula 和 tests。

---

## 04.6. Collaboration Chain

### 04.6.1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Chain |
| 目标 | 支持 agent 间 share、conflict resolution、handoff 和 aggregate |
| 关键路径 | internal collaboration operations 可达；不在普通 Event/Query 必经路径 |
| 成熟度 | 部分 |

### 04.6.2. Entry points

| Entry | Concrete call |
|---|---|
| Chain | `CollaborationChain.Run(CollaborationChainInput)` |
| Runtime | `DispatchShare`, `DispatchConflictResolve` |
| HTTP | memory share/conflict, agent handoff, MAS aggregate/consistency, agent list |
| Workers | ConflictMergeWorker, CommunicationWorker, MicroBatchScheduler |
| Stores | ObjectStore, EdgeStore, ShareContractStore, PolicyStore |

### 04.6.3. Input/output

| Operation | Input | Output | Mutation |
|---|---|---|---|
| conflict merge | left/right Memory IDs, object type | winner/loser | loser inactive + conflict edge |
| share/broadcast | from/to agent + Memory ID | shared Memory ID | copied Memory with target agent/provenance |
| CollaborationChain | conflict input + agent IDs | winner/shared IDs | merge, microbatch enqueue, optional broadcast |
| handoff | source/target/session/task context | handler response | Event/share path depending body |
| aggregate/consistency | agent answers/results | score/aggregate | mainly response/metrics |

### 04.6.4. Scope and contract behavior

| Concern | Current behavior |
|---|---|
| Agent/session resolution | request fields and Memory fields |
| Share contract storage | canonical CRUD exists |
| Read ACL | contamination detector checks matching contract; not universal pre-read deny |
| Write/derive ACL | schema exists; no central enforcement on every collaboration mutation |
| Shared object model | creates a copied Memory `shared_<source>_to_<target>` |
| Handoff event | internal handler may submit Event; not a single canonical collaboration transaction |
| Conflict detection | same agent+session active Memories; LWW by Version |
| Conflict preservation | loser remains stored but inactive, edge points winner -> loser |

### 04.6.5. Chain and API relationship

`CollaborationChain.Run` 先 merge，再把 result 放入 in-memory microbatch，最后 broadcast。实际 internal HTTP share/conflict 多数直接调用 Runtime/NodeManager，而不调用 CollaborationChain。Microbatch 没有后台定时 flush，enqueue 不表示后续 fan-out 已处理。

### 04.6.6. Data/state

- Canonical：source/shared Memory、conflict Edge、ShareContract/Policy records。
- Provenance：shared Memory 的 `ProvenanceRef=shared_from:<agent>/<memory>`；conflict edge 当前不总带 provenance ref。
- Version：share/conflict worker 不统一创建 ObjectVersion。
- Projection：shared/winner mutation 不统一 reindex。
- WAL：直接 share/conflict 不统一写 Event/WAL。

### 04.6.7. Correctness and failure

- Share source missing 或 same-agent 时是 no-op，不一定返回 error。
- Conflict precondition不满足时是 no-op；LWW 相同 version 选择 left。
- Memory copy、Edge、Version、projection、audit 不在统一 transaction。
- Cross-agent contamination 当前是 metrics detection，不能替代 prevention。
- Scope/ACL/contract policy 对 direct canonical routes也未统一 enforce。

### 04.6.8. 声明边界

可声明：显式 ShareContract schema/store、shared Memory copy、same-session LWW conflict preservation、handoff/MAS internal adapter。

不可声明：完整 multi-agent transaction、全面 ACL/derive enforcement、自动 semantic conflict detection、统一 merge policy plugin 或跨 agent zero-leak guarantee。

### 04.6.9. 缺口

1. CollaborationCommand 经 policy authorize 后写 derived Event/WAL；
2. share/merge 必须一起写 Version/Edge/Audit/projection；
3. contract read/write/derive/merge policy 统一 evaluator；
4. semantic conflict detector 和 pluggable merge strategy；
5. durable microbatch/job status；
6. prevention-based contamination tests。

---

## 04.7. Runtime Coordination Mechanism

### 04.7.1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Mechanism |
| 目标 | Trigger -> routing/dependencies/modules/workers/state/result/replay |
| 成熟度 | 完整 direct Runtime coordination；统一 task orchestration 部分 |

### 04.7.2. Code entry

`worker.Runtime`, consistency Controller, NodeManager, Chain types, Orchestrator, coordinator Hub/registry, Gateway, EventSubscriber。

### 04.7.3. Input/output

Runtime receives Event/Query/admin/algorithm/collaboration requests and outputs ACK, QueryResponse, algorithm/collaboration results or admin summaries。Side effects span WAL/stores/projection/cache/metrics/workers。

### 04.7.4. Internal composition

| Role | Concrete owner |
|---|---|
| request lifecycle | Gateway + Runtime methods |
| write state | consistency Controller/tracker |
| dependency order | explicit function sequence |
| worker invocation | NodeManager |
| chain composition | four chain types |
| async maintenance | EventSubscriber/flush loop |
| module discovery | coordinator registry |
| optional queued routing | Orchestrator |

### 04.7.5. Sync/async boundary

Strict write and all query assembly are synchronous；bounded/eventual projection, subscriber and flush are asynchronous。Orchestrator tasks are async but not used by main API。NodeManager dispatch often synchronous and context-free。

### 04.7.6. State tracking

LSN/tracker/checkpoint track write progress；Runtime fields hold mode flags, last Memory map, embedding spec, flush dirty state；Orchestrator and scheduler expose independent counters。No unified ExecutionRecord/DAG persisted。

### 04.7.7. Failure/replay

Controller retries projection/checkpoint and recovers WAL；Runtime admin replay re-submits events；NodeManager worker errors may be lost；Orchestrator ignores ChainResult in stats。Partial results do not share one envelope across APIs。

### 04.7.8. 声明边界

可声明 coordinated Event/Query runtime with consistency and worker composition。

不可声明 every trigger uses dependency DAG, resource-aware scheduler, durable task state or uniform partial-result protocol。

### 04.7.9. 缺口

Introduce ExecutionPlan/ExecutionRecord, unified context/trace/error/result, explicit sync/async stage contract, worker futures, Orchestrator integration decision and persisted retry/replay metadata。

---

## 04.8. Execution Coordination Engine

### 04.8.1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Engine |
| 原模块 | `worker.Runtime` |
| 目标 | 协调 ingest/query/memory/collaboration/admin 的模块调用和状态 |
| 关键路径 | 是 |
| 成熟度 | 完整 direct coordinator；统一 task/plan engine 部分 |

### 04.8.2. Code entry

| Item | Code |
|---|---|
| Package | `src/internal/worker` |
| Main files | `runtime.go`, `runtime_consistency.go`, `orchestrator.go`, `subscriber.go` |
| Constructor | `CreateRuntime(...)` |
| Bootstrap | `app.BuildServer` constructs and configures Runtime |
| External callers | Gateway, transport RuntimeAPI, consistency project callback, admin handlers |

### 04.8.3. Runtime fields

| Field group | Fields | Responsibility |
|---|---|---|
| Event | `wal`, `bus`, derivation/policy logs | accepted order and semantic logs |
| Query/data | `plane`, `planner`, `policy`, `assembler`, `evCache` | plan/retrieve/evidence |
| Derivation | `materializer`, `preCompute` | Event projection/evidence fragment |
| Control | `coord`, `nodeManager`, `queryChain` | module discovery/worker/query reasoning |
| Storage | `storage`, `tieredObjects` | canonical and tier access |
| Conflict | `lastMem`, mutex | latest per agent/session |
| Consistency | controller/config/watermark, mutex | mode/queue/checkpoint/read gate |
| Background | subscribers + mutex, flush ticker/channels/dirty/once | async maintenance/index flush |
| Runtime modes | vector-only/minimal/governance-disabled | in-memory behavior switches |
| Memory provider | backend router | primary/shadow provider mode |
| Embedding | spec + mutex | family/dimension compatibility |
| Admin | wipe mutex | destructive operation serialization |

### 04.8.4. Constructor dependencies and APIs

`CreateRuntime` requires WAL, Bus, DataPlane, Hub, PolicyEngine, QueryPlanner, Materializer, PreCompute, Assembler, Cache, logs, NodeManager, RuntimeStorage and TieredObjectStore。

| API group | Main methods |
|---|---|
| ingest/query | `SubmitIngestContext`, `ExecuteQueryContext`, internal `projectWALEntry/executeQuery` |
| consistency | configure/start/shutdown/mode/status/pause/reset/resume |
| warm/native | ingest/search/register/unload/batch variants |
| retrieval maintenance | warm prebuild, embedding spec/reindex, flush loop |
| evidence | fragment/derivation/policy decision getters |
| memory/collaboration | algorithm dispatch/recall/share/conflict/provider mode |
| recovery/admin | wipe, replay preview/apply, topology |

### 04.8.5. Inputs, outputs and state mutation

| Request | Output | Mutation |
|---|---|---|
| Event | ACK map | WAL/canonical/projection/cache/metrics |
| QueryRequest | QueryResponse | metrics/cache observations |
| algorithm op | AlgorithmDispatchOutput/MemoryView | Memory/state/audit |
| share/conflict | IDs | Memory/Edge |
| admin replay/reset/reindex | summary | multiple subsystems |

### 04.8.6. Internal components and routing

Request Manager：Gateway + Runtime methods。Task classifier/Chain Router：methods and switches, not a unified object。Dependency analyzer/planner：explicit code order。Module router：direct fields/NodeManager。Worker dispatcher：NodeManager。Failure monitor/replay：Controller + admin replay。Mode manager：consistency/runtime/provider flags。Metrics adapter：global collector。

### 04.8.7. Correctness

- Runtime is long-lived and concurrency-safe only where fields have mutex/atomic protection；mode booleans are direct mutable flags。
- Main ingest consistency is delegated to Controller。
- Runtime's direct function path is authoritative；Orchestrator registration does not intercept it。
- Partial failures across plane/store/cache/subscriber are not one transaction。
- Admin reset coordinates pause/subscriber/controller but remains high-risk multi-component mutation。

### 04.8.8. 声明边界

可声明 a concrete execution coordination engine for active single-process runtime。

不可声明 durable generic task execution DAG、all-request Chain routing、distributed resource allocation or uniform partial-result handling。

### 04.8.9. 缺口

Define Runtime service interfaces, ExecutionContext/Plan/Result, typed errors, context-aware worker dispatch, mode synchronization, durable task records, Orchestrator integration and component-level health/dependency checks。

---

## 04.9. Intelligent Scheduler

### 04.9.1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Engine |
| 原模块 | Scheduler / Orchestrator / consistency scheduler |
| 目标 | 根据 task、dependency、resource、consistency 和 SLA 生成并执行计划 |
| 关键路径 | consistency scheduler 是；unified intelligent scheduler 不是 |
| 当前成熟度 | 部分/规划 |

### 04.9.2. Existing code entry

| Component | Constructor | Methods |
|---|---|---|
| `worker.Orchestrator` | `CreateOrchestrator` | `Submit`, convenience submits, `Run`, `Stats` |
| consistency Controller | `NewController` | start/submit/read wait/mode/status/pause/reset/resume/shutdown |
| NodeManager | `CreateManager` | register/dispatch/topology |
| WorkerScheduler | `NewWorkerScheduler` | `Dispatch`, `Complete`, `Stats` |
| Microbatch | `CreateInMemoryMicroBatchScheduler` | enqueue/flush/run/info |

### 04.9.3. Existing fields

| Type | Fields |
|---|---|
| `Task` | ID, Type, Priority, Payload, Submitted |
| Orchestrator | Manager, four queues, concurrency/waitgroup, atomic stats, four Chain pointers |
| Controller task | WALEntry, mode, lag, accepted/deadline, generation, strict result channel, bounded shard reservation |
| Controller | WAL/project/config/checkpoint/tracker/mode/admission gates/slots/sharded queues/lifecycle contexts/active count/workers |
| WorkerScheduler | mutex + map worker type -> dispatched/active |
| Microbatch | ID, mutex, opaque queue, batch size |

### 04.9.4. Current input/output

| Scheduler | Input | Output |
|---|---|---|
| Orchestrator | Task with opaque chain payload | queued bool; no result future |
| Controller | Event + context + consistency mode/SLA | visible/pending ACK or error |
| NodeManager | concrete domain arguments | direct worker result or void |
| WorkerScheduler | worker type events | counters |
| Microbatch | opaque payload | flushed FIFO items |

### 04.9.5. Capability comparison

| Required intelligent component | Current status |
|---|---|
| Task profiler/classifier | fixed TaskType / Event consistency mode |
| Chain selector | Orchestrator type switch, not main path |
| Dependency resolver | missing; code order only |
| Candidate plan generator | missing |
| Priority scorer | caller integer only |
| Cost estimator | missing |
| SLA evaluator | bounded freshness only |
| Consistency evaluator | implemented for Event/read gate |
| Resource allocator/worker pool selector | fixed pool/first worker |
| Feedback optimizer | metrics exist, no feedback loop |

### 04.9.6. Call relationship and state

Bootstrap starts Orchestrator goroutines and registers it, but Gateway/Runtime do not submit main tasks。Controller directly wraps Event ingest and is the real critical scheduler。NodeManager dispatch bypasses WorkerScheduler counter unless explicitly integrated。

### 04.9.7. Correctness/failure

- Orchestrator priority is non-preemptive and can starve low priority。
- submit times out at 30 s and increments dropped；no durable queue/retry/result/cancel。
- `Completed` increments even if payload type mismatch or ChainResult failed。
- Controller has robust generation/pause/retry/checkpoint semantics but only for write visibility。
- No fairness, tenant isolation, resource health or deadline scheduling beyond write SLA。

### 04.9.8. 声明边界

可声明 fixed priority Chain dispatcher, consistency-aware sharded write scheduler, worker registry and microbatch primitive。

不可声明 intelligent/resource-aware/unified scheduler, dependency DAG, cost optimization, tenant fairness, deadline scheduling or auto-tuning。

### 04.9.9. Required target interface and fields

```text
Classify(Request) -> ScheduledTask
Plan(Task, ResourceSnapshot) -> ExecutionPlan
Submit(ctx, plan) -> Future[ExecutionResult]
Cancel(taskID) / Retry(taskID) / Resume(taskID)
```

Target task must include type/chain/priority/dependencies/deadline/SLA/consistency/tenant/scope/idempotency/resource estimate/attempt/trace。Target scheduler needs durable state, fair queues, worker health/load, result/error propagation, cancellation, retry policy, metrics feedback and integration tests proving all Gateway/Runtime paths use it before this Engine can be marked complete。
