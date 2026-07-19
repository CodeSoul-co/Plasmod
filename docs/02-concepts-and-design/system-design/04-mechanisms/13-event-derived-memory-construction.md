# 13. Event-derived Memory Construction Mechanism

## 1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Mechanism |
| 目标 | Raw Event -> normalized Event -> typed canonical objects/relations/versions/projection |
| 关键路径 | Event ingest |
| 成熟度 | 完整基础规则，semantic/configurable derivation 部分 |

## 2. 代码入口

| Function/type | Role |
|---|---|
| `Event.NormalizeDynamicEventV04` | merge nested/legacy wire fields |
| `MaterializeEvent` | produce Memory, candidate State, optional Artifact, edges, versions, IngestRecord |
| Event helper methods | text/scope/state/artifact/causality extraction |
| specialized materialization workers | keyed State, Artifact/tool trace, graph/index side effects |
| `ApplyCanonicalProjection` | commit selected outputs |

## 3. 输入输出

输入是完整 Dynamic Event；输出字段见 [Object and Message Registry](../06-cross-reference/object-and-message-registry.md)。一个 Event 至少产生 Memory + MemoryVersion + retrieval record + base edges；也可产生 Artifact/State candidate 和更多 causal edges。

## 4. Decision rules

| Signal | Decision |
|---|---|
| event type | resolve Memory type; tool/artifact/state route |
| `retrieval.index_text`/payload text | Memory content and index text |
| workspace/retrieval/session | scope and namespace |
| causality refs | typed edges/provenance |
| object descriptor | explicit object/artifact/state metadata |
| embedding vector/ref/flag | projection vector or skip-vector behavior |

规则由 Go helper 固化，尚无通用 declarative rule registry/semantic analyzer。

## 5. 调用关系与同步边界

Runtime 同步调用 Materializer；consistency worker提交 retrieval/canonical；subscriber 异步调用 specialized workers。`MainChain` 包装另一组 worker 顺序，但不在主 Runtime write path。

## 6. 状态变化

WAL/LSN 是派生输入顺序；canonical store 保存对象/关系/版本；DataPlane 保存投影；derivation log/audit/cache 保存辅助证据。Materializer 本身无状态。

## 7. 正确性

- deterministic primary IDs 支持 replay；
- duplicate Event ID 当前倾向 upsert/覆盖，没有全局 payload hash conflict rejection；
- multiple store writes 只在 canonical shared backend 内可 transaction；
- algorithm compress/summarize 直接产生 Memory，不自动重新作为 Event 进入该机制。

## 8. 声明边界

可声明 event-derived typed object construction 和 provenance/version/retrieval projection。

不可声明任意配置规则、LLM semantic classification、全局 dedup/conflict engine，或所有 derived Memory 都有对应 derived Event。

## 9. 缺口

需要 DerivationRule interface/registry、duplicate policy、payload hash conflict、derived Event contract、per-output validation、selected State candidate persistence 和 fault-injection tests。
