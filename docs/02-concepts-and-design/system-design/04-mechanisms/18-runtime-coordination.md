# 18. Runtime Coordination Mechanism

## 1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Mechanism |
| 目标 | Trigger -> routing/dependencies/modules/workers/state/result/replay |
| 成熟度 | 完整 direct Runtime coordination；统一 task orchestration 部分 |

## 2. Code entry

`worker.Runtime`, consistency Controller, NodeManager, Chain types, Orchestrator, coordinator Hub/registry, Gateway, EventSubscriber。

## 3. Input/output

Runtime receives Event/Query/admin/algorithm/collaboration requests and outputs ACK, QueryResponse, algorithm/collaboration results or admin summaries。Side effects span WAL/stores/projection/cache/metrics/workers。

## 4. Internal composition

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

## 5. Sync/async boundary

Strict write and all query assembly are synchronous；bounded/eventual projection, subscriber and flush are asynchronous。Orchestrator tasks are async but not used by main API。NodeManager dispatch often synchronous and context-free。

## 6. State tracking

LSN/tracker/checkpoint track write progress；Runtime fields hold mode flags, last Memory map, embedding spec, flush dirty state；Orchestrator and scheduler expose independent counters。No unified ExecutionRecord/DAG persisted。

## 7. Failure/replay

Controller retries projection/checkpoint and recovers WAL；Runtime admin replay re-submits events；NodeManager worker errors may be lost；Orchestrator ignores ChainResult in stats。Partial results do not share one envelope across APIs。

## 8. 声明边界

可声明 coordinated Event/Query runtime with consistency and worker composition。

不可声明 every trigger uses dependency DAG, resource-aware scheduler, durable task state or uniform partial-result protocol。

## 9. 缺口

Introduce ExecutionPlan/ExecutionRecord, unified context/trace/error/result, explicit sync/async stage contract, worker futures, Orchestrator integration decision and persisted retry/replay metadata。
