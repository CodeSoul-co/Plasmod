# 21. Object Derivation Engine

## 1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Engine |
| 原模块 | Materializer + specialized materialization workers |
| 目标 | Event -> canonical objects/relations/versions/retrieval record |
| 关键路径 | 是 |
| 成熟度 | 完整基础规则；通用 semantic/configurable derivation 部分 |

## 2. 代码入口

| Item | Code |
|---|---|
| Package | `src/internal/materialization`, `src/internal/worker/materialization` |
| Main files | `service.go`, `pre_compute.go`, `object.go`, `state.go`, `tool_trace.go` |
| Constructors | `NewService`, `NewPreComputeServiceWithConfig`, `CreateInMemory*MaterializationWorker` |
| Public methods | `MaterializeEvent`, compatibility `ProjectEvent`; worker `Run/Materialize/Apply/Checkpoint/TraceToolCall` |
| Interfaces | Object/State/Tool worker interfaces, `Runnable` |

## 3. Engine fields

| Type | Field | Type/meaning |
|---|---|---|
| `Service` | none | stateless deterministic transformer |
| `MaterializationResult` | `Record` | retrieval `IngestRecord` |
| same | `Memory`, `Version`, `Edges` | always-derived core output |
| same | `State`, `StateVersion` | ingest checkpoint candidate |
| same | `Artifact`, `ArtifactVersion` | optional output |
| Object worker | `id`, object/edge/version stores, derivation logger | specialized Artifact route |
| State worker | `id`, object/version stores, derivation logger, mutex, `stateKeys` | keyed State version tracking |
| Tool worker | id/store/log dependencies | tool Artifact trace |
| PreCompute | cache/config | evidence fragment precomputation |

## 4. Input/output and field access

| Input | Reads | Output |
|---|---|---|
| `schemas.Event` | identity, actor, time, event/object/causality/access/materialization/retrieval/payload | `MaterializationResult` |
| `StateApplyInput` | actor IDs, event type, state key/value | State ID/version |
| `StateCheckpointInput` | agent/session | snapshot count + ObjectVersions |
| `ToolTraceInput` | tool event/object/payload | Artifact ID |

完整字段见 [Object and Message Registry](../06-cross-reference/object-and-message-registry.md)。

## 5. Internal components

| Suggested component | Actual implementation |
|---|---|
| Event Normalizer | Event helper/normalize methods，完整 |
| Event Parser | Go JSON decode + helper accessors，完整 |
| Semantic Analyzer | 无通用 analyzer；event-type switch/heuristics |
| Event Classifier | event/object helper rules，部分 |
| Object Deriver | `MaterializeEvent` + workers，完整基础 |
| Relation Generator | `deriveEdges`, schema edge builders, graph worker |
| Version Generator | materializer + state/object workers |
| Projection Builder | `IngestRecord` construction |
| Deduplicator | deterministic ID/upsert only，无 conflict-aware deduplicator |
| Validator | Gateway/ingest worker/schema helpers，分散 |

## 6. Calls and APIs

Upstream：Runtime `projectWALEntry`, MainChain, subscriber。Downstream：DataPlane, RuntimeStorage, evidence cache, derivation log。API：`POST /v1/ingest/events` 和 document/tool/state adapters；direct canonical POST 不调用本 Engine。

## 7. State, correctness and failure

- Service stateless；State worker `stateKeys` 是进程内 accelerator。
- deterministic IDs support replay。
- Runtime only commits selected MaterializationResult fields；State candidate 当前不在 main canonical transaction。
- worker store writes can be duplicated with Runtime artifact writes；upsert hides some duplication but versions may differ。
- `enabled/targets` is not a hard routing gate。

## 8. 声明边界

可声明 typed deterministic object derivation with relation/version/projection records。

不可声明 semantic LLM analysis、declarative derivation DSL、global dedup/conflict handling、all outputs atomic or arbitrary object plugins。

## 9. 缺口

1. single DerivationPlan listing every output and commit status；
2. configured rule registry and custom object derivation interface；
3. duplicate payload conflict policy；
4. merge specialized worker outputs into one commit/replay contract；
5. per-output validation/error propagation；
6. tests proving same Event produces byte/semantic-equivalent outputs across replay。
