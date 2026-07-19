# 4. Ingest Chain

## 1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Chain |
| 目标 | 将 Dynamic Event 变为 WAL record、canonical objects 和 retrieval projection |
| 关键路径 | 是 |
| 成熟度 | 完整主写链；`MainChain` concrete abstraction 仅部分接线 |

## 2. 代码入口

| Layer | Entry |
|---|---|
| HTTP | `Gateway.handleIngest` |
| Runtime | `SubmitIngestContext`, `projectWALEntry` |
| Consistency | `Controller.Submit`, `projectWithRetry`, `Tracker` |
| Derivation | `materialization.Service.MaterializeEvent` |
| Persistence | `RuntimeStorage.ApplyCanonicalProjection` |
| Conceptual chain type | `chain.MainChain.Run` |
| Background | `EventSubscriber.addBuiltinHandlers` |

## 3. 实际阶段、输入输出和顺序

| # | Stage | Input | Output/mutation | Sync rule |
|---|---|---|---|---|
| 1 | decode/normalize | Event JSON | normalized Event | sync |
| 2 | admission/mode | Event + context | slot/shard/mode/deadline | sync |
| 3 | WAL append | Event | WALEntry/LSN | sync all modes |
| 4 | materialize in memory | WALEntry.Event | IngestRecord, Memory, State candidate, Artifact, Edge, Version | projection worker |
| 5 | retrieval ingest | IngestRecord | lexical/vector segment state | before canonical commit |
| 6 | canonical commit | Memory/Artifact/Edges/Versions | store transaction/upserts | visibility gate |
| 7 | hot/conflict/precompute | Memory/Event | cache, conflict edge, evidence fragment | same projection callback but not all are gate-critical |
| 8 | tracker visible | LSN | checkpoint/watermark | strict waits |
| 9 | subscriber maintenance | WAL scan | keyed State, tool trace, reflection, conflict, consolidation | async |

重要事实：`MaterializationResult.State/StateVersion` 虽被构造，当前 `Runtime.projectWALEntry` 没有把它们放入 `CanonicalProjection`；可查询 keyed State 主要由异步 `StateMaterializationWorker` 持久化。不能把 State 的辅助完成时间等同于 strict Memory visibility。

## 4. MainChain abstraction

`MainChain.Run(MainChainInput)` 依次调用 object/state/tool/index/graph workers。它假设 WAL 已写，但 Runtime 主写链并不调用 MainChain；Orchestrator 才会调用它，且 Orchestrator 没有主请求 submit caller。因此 MainChain 是真实可测试组合对象，但不是当前权威 Event write transaction。

## 5. Canonical object rules

| Event signal | Output |
|---|---|
| every accepted Event | `mem_<event_id>`, Memory version, base/causal edges, retrieval record |
| artifact/tool-like | Artifact + version + artifact edges；worker 还可能生成 tool trace |
| payload with state key | keyed `state_<agent_id>_<state_key>` via State worker |
| parent/causal/source/target refs | typed Edge according to builders |

`materialization.enabled/targets` 当前作为 normalized metadata/hook input，不是跳过默认 Memory projection 的通用 gate。

## 6. State and side effects

| Plane | Mutation |
|---|---|
| Event | WAL append, LSN/logical time |
| Canonical | Memory, optional Artifact, Edge, Version; async State/tool artifacts |
| Projection | DataPlane ingest, dirty flush flag |
| Evidence | in-memory fragment cache, derivation log through workers |
| Metrics | write latency/visible, counts, session step |

## 7. Correctness and failure

- Deterministic IDs support replay upsert/idempotency.
- Projection retry is LSN-based；strict returns only after tracker visible。
- WAL committed but projection failed is an accepted-not-visible state, not a clean rollback。
- Retrieval success followed by canonical failure can leave projection/canonical divergence；replay/reindex repairs it。
- Auxiliary worker errors do not consistently fail the original ACK；subscriber uses in-memory DLQ/overflow/error reporting, not durable dead-letter storage。

## 8. 声明边界

可声明：Event-first ingest、WAL LSN、deterministic canonical derivation、retrieval projection、three visibility modes 和 replay。

不可声明：ACK 总是等待 State/Artifact/tool/evidence 全部辅助物化，或 WAL/retrieval/canonical/S3 在一个 ACID transaction 中。

## 9. 缺口

1. 决定 State candidate 是否进入 visibility transaction；
2. 合并 MainChain 与 Runtime 主链或明确废弃其主链含义；
3. worker error 必须进入 ACK/maintenance status；
4. 增加 projection/canonical divergence marker；
5. 为 duplicate Event 的 payload conflict 定义拒绝/覆盖策略。
