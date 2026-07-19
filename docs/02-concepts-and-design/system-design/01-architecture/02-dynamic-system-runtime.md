# 2. Dynamic System Runtime

## 1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Architecture |
| 设计目标 | 还原外部请求/内部事件进入系统后的真实函数级路径 |
| 关键路径 | 是 |
| 当前成熟度 | 完整的 Event/Query 主路径；统一 Chain Router 仅部分接线 |

## 2. 请求入口

| Boundary | Entry | Calls Runtime directly? |
|---|---|---|
| HTTP Event/Query | `Gateway.RegisterAPIRoutes` handlers | 是 |
| HTTP canonical CRUD | Gateway handlers | 否，直接 store/coordinator |
| Admin | Gateway management handlers | 视操作调用 Runtime/store |
| Internal transport | `transport.Server` | 通过 `RuntimeAPI` 调 warm/native methods |
| gRPC | gRPC server -> Gateway/service adapter | 是/间接 |
| WAL background | consistency recovery, EventSubscriber | Controller project callback / NodeManager |

## 3. 真实动态路径

### Event write

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

### Query

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

### Direct canonical mutation

```text
HTTP canonical POST -> handler -> Object/Edge/Policy/Contract store
```

该路径不自动执行 WAL、projection、version、evidence precompute 或 replay 语义。

## 4. Chain 选择真值

| Chain | Concrete object | 主路径如何进入 |
|---|---|---|
| MainChain | `chain.MainChain` | Orchestrator task 可调用；Event Runtime 主写不调用它 |
| MemoryPipelineChain | concrete | Orchestrator task 可调用；subscriber 通常直接逐 worker dispatch |
| QueryChain | Runtime 持有 | `executeQuery` 在 retrieval/assembler 后同步调用 |
| CollaborationChain | concrete | Orchestrator task 可调用；internal API 通常直接 Runtime dispatch |

因此不存在一个对所有请求生效的 central Chain Router。`Orchestrator.execute` 有 type switch，但没有 Gateway/Runtime submit call site。

## 5. 输入输出和状态变化

| Path | Input | Output | Durable mutations |
|---|---|---|---|
| Event | Dynamic Event | ACK with LSN/status/object IDs | WAL, Memory/Artifact/Edge/Version, retrieval projection；State/maintenance 异步 |
| Query | QueryRequest | QueryResponse | metrics/cache reads；通常无 canonical mutation |
| Memory operation | AlgorithmDispatchInput-like body | AlgorithmDispatchOutput/MemoryView | Memory, algorithm state, audit depending operation |
| Collaboration | agent/memory/conflict IDs | shared/winner result | Memory/Edge; WAL/version/projection 不统一 |
| Admin recovery | LSN/range/action | summary/status | replay/reindex/reset/purge dependent |

## 6. 同步/异步与返回条件

| Mode/path | Return condition |
|---|---|
| strict ingest | WAL + projection + tracker visible；辅助 subscriber 不在 gate 内 |
| bounded/eventual ingest | WAL accepted and task enqueued |
| query | read gate + current retrieval/evidence build complete |
| Orchestrator task | `Submit` only returns queued bool；没有 result future |
| subscriber | background polling；panic goes to in-memory DLQ/overflow/error channel |

## 7. 失败处理

- Context cancel/deadline 在 Gateway -> Runtime -> consistency read/write gate 传播。
- Projection 重试由 Controller 执行；strict 失败返回 accepted-not-visible。
- NodeManager 多个 void dispatch helper 不向上游传播 worker error。
- Query `DataPlane.Search` 返回 value 而非 error，检索内部失败主要通过空结果/metrics/log 暴露。
- partial windows 与恢复见 [Execution State and Failure Matrix](../06-cross-reference/execution-state-and-failure-matrix.md)。

## 8. 声明边界

可声明：Event 和 Query 有明确 Runtime 主链，canonical state 和 projection 在 Runtime 汇合，后台维护由 subscriber/flush loop 执行。

不可声明：Gateway 总是先调用 Orchestrator，或四条 Chain 对所有阶段提供统一 task result/retry/cancellation。

## 9. 缺口

1. 统一决定 Runtime direct orchestration 与 Orchestrator 的关系；
2. 为辅助 worker 增加 error/result propagation；
3. 为 collaboration mutation 补齐 WAL/version/projection 一致路径；
4. 为 query retrieval error 增加 typed error contract；
5. 统一 request/task trace ID 的跨模块传播。
