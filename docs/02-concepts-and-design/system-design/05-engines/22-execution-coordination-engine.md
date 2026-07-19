# 22. Execution Coordination Engine

## 1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Engine |
| 原模块 | `worker.Runtime` |
| 目标 | 协调 ingest/query/memory/collaboration/admin 的模块调用和状态 |
| 关键路径 | 是 |
| 成熟度 | 完整 direct coordinator；统一 task/plan engine 部分 |

## 2. Code entry

| Item | Code |
|---|---|
| Package | `src/internal/worker` |
| Main files | `runtime.go`, `runtime_consistency.go`, `orchestrator.go`, `subscriber.go` |
| Constructor | `CreateRuntime(...)` |
| Bootstrap | `app.BuildServer` constructs and configures Runtime |
| External callers | Gateway, transport RuntimeAPI, consistency project callback, admin handlers |

## 3. Runtime fields

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

## 4. Constructor dependencies and APIs

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

## 5. Inputs, outputs and state mutation

| Request | Output | Mutation |
|---|---|---|
| Event | ACK map | WAL/canonical/projection/cache/metrics |
| QueryRequest | QueryResponse | metrics/cache observations |
| algorithm op | AlgorithmDispatchOutput/MemoryView | Memory/state/audit |
| share/conflict | IDs | Memory/Edge |
| admin replay/reset/reindex | summary | multiple subsystems |

## 6. Internal components and routing

Request Manager：Gateway + Runtime methods。Task classifier/Chain Router：methods and switches, not a unified object。Dependency analyzer/planner：explicit code order。Module router：direct fields/NodeManager。Worker dispatcher：NodeManager。Failure monitor/replay：Controller + admin replay。Mode manager：consistency/runtime/provider flags。Metrics adapter：global collector。

## 7. Correctness

- Runtime is long-lived and concurrency-safe only where fields have mutex/atomic protection；mode booleans are direct mutable flags。
- Main ingest consistency is delegated to Controller。
- Runtime's direct function path is authoritative；Orchestrator registration does not intercept it。
- Partial failures across plane/store/cache/subscriber are not one transaction。
- Admin reset coordinates pause/subscriber/controller but remains high-risk multi-component mutation。

## 8. 声明边界

可声明 a concrete execution coordination engine for active single-process runtime。

不可声明 durable generic task execution DAG、all-request Chain routing、distributed resource allocation or uniform partial-result handling。

## 9. 缺口

Define Runtime service interfaces, ExecutionContext/Plan/Result, typed errors, context-aware worker dispatch, mode synchronization, durable task records, Orchestrator integration and component-level health/dependency checks。
