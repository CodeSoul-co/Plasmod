# 30. Intelligent Scheduler

## 1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Engine |
| 原模块 | Scheduler / Orchestrator / consistency scheduler |
| 目标 | 根据 task、dependency、resource、consistency 和 SLA 生成并执行计划 |
| 关键路径 | consistency scheduler 是；unified intelligent scheduler 不是 |
| 当前成熟度 | 部分/规划 |

## 2. Existing code entry

| Component | Constructor | Methods |
|---|---|---|
| `worker.Orchestrator` | `CreateOrchestrator` | `Submit`, convenience submits, `Run`, `Stats` |
| consistency Controller | `NewController` | start/submit/read wait/mode/status/pause/reset/resume/shutdown |
| NodeManager | `CreateManager` | register/dispatch/topology |
| WorkerScheduler | `NewWorkerScheduler` | `Dispatch`, `Complete`, `Stats` |
| Microbatch | `CreateInMemoryMicroBatchScheduler` | enqueue/flush/run/info |

## 3. Existing fields

| Type | Fields |
|---|---|
| `Task` | ID, Type, Priority, Payload, Submitted |
| Orchestrator | Manager, four queues, concurrency/waitgroup, atomic stats, four Chain pointers |
| Controller task | WALEntry, mode, lag, accepted/deadline, generation, strict result channel, bounded shard reservation |
| Controller | WAL/project/config/checkpoint/tracker/mode/admission gates/slots/sharded queues/lifecycle contexts/active count/workers |
| WorkerScheduler | mutex + map worker type -> dispatched/active |
| Microbatch | ID, mutex, opaque queue, batch size |

## 4. Current input/output

| Scheduler | Input | Output |
|---|---|---|
| Orchestrator | Task with opaque chain payload | queued bool; no result future |
| Controller | Event + context + consistency mode/SLA | visible/pending ACK or error |
| NodeManager | concrete domain arguments | direct worker result or void |
| WorkerScheduler | worker type events | counters |
| Microbatch | opaque payload | flushed FIFO items |

## 5. Capability comparison

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

## 6. Call relationship and state

Bootstrap starts Orchestrator goroutines and registers it, but Gateway/Runtime do not submit main tasks。Controller directly wraps Event ingest and is the real critical scheduler。NodeManager dispatch bypasses WorkerScheduler counter unless explicitly integrated。

## 7. Correctness/failure

- Orchestrator priority is non-preemptive and can starve low priority。
- submit times out at 30 s and increments dropped；no durable queue/retry/result/cancel。
- `Completed` increments even if payload type mismatch or ChainResult failed。
- Controller has robust generation/pause/retry/checkpoint semantics but only for write visibility。
- No fairness, tenant isolation, resource health or deadline scheduling beyond write SLA。

## 8. 声明边界

可声明 fixed priority Chain dispatcher, consistency-aware sharded write scheduler, worker registry and microbatch primitive。

不可声明 intelligent/resource-aware/unified scheduler, dependency DAG, cost optimization, tenant fairness, deadline scheduling or auto-tuning。

## 9. Required target interface and fields

```text
Classify(Request) -> ScheduledTask
Plan(Task, ResourceSnapshot) -> ExecutionPlan
Submit(ctx, plan) -> Future[ExecutionResult]
Cancel(taskID) / Retry(taskID) / Resume(taskID)
```

Target task must include type/chain/priority/dependencies/deadline/SLA/consistency/tenant/scope/idempotency/resource estimate/attempt/trace。Target scheduler needs durable state, fair queues, worker health/load, result/error propagation, cancellation, retry policy, metrics feedback and integration tests proving all Gateway/Runtime paths use it before this Engine can be marked complete。
