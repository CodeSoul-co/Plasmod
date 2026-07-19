# 3. Scheduler Design

## 1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Architecture |
| 设计目标 | 说明 Chain、worker、写入可见性和资源如何被路由/限流/恢复 |
| 关键路径 | consistency scheduler 是；Orchestrator 和 WorkerScheduler 不是主请求 gate |
| 当前成熟度 | 部分 |

## 2. 三类现有调度组件

| Component | File/type | 实际职责 | Main-path reachability |
|---|---|---|---|
| Consistency scheduler | `worker/consistency.Controller` | write admission、mode gate、sharded queue、slot、retry、deadline、checkpoint | Event ingest 关键路径 |
| Chain Orchestrator | `worker.Orchestrator` | 4 priority queues + fixed worker pool + Chain type switch | bootstrap 启动，但无主路径 submit caller |
| WorkerScheduler | `coordinator.WorkerScheduler` | dispatched/active counter | registry 可见，不执行任务 |
| NodeManager | `nodes.Manager` | registration + first-worker synchronous dispatch | Runtime/Chain 真实调用 |
| Microbatch | `InMemoryMicroBatchScheduler` | opaque FIFO buffer + explicit flush | Collaboration enqueue；无定时 drain |

## 3. Task 和分类

| `worker.Task` field | Meaning |
|---|---|
| `ID` | caller-provided task identity |
| `Type` | ingest/memory/query/collaboration |
| `Priority` | 0 low, 1 normal, 2 high, 3 urgent |
| `Payload` | chain-specific `any` |
| `Submitted` | queue timestamp |

Orchestrator 没有 dependency、deadline、tenant、cost、resource request、retry count、cancellation token 或 result channel 字段。

## 4. Routing and Ordering

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

## 5. Backpressure, Cancellation and Recovery

| Component | Backpressure | Cancellation | Failure/retry |
|---|---|---|---|
| Controller | bounded slots/queues, blocks admission | request/root/admission context | configured projection/checkpoint retry |
| Orchestrator | queue send blocks up to 30 s then returns false | worker exits on context; queued task无单独取消 | no task retry/result |
| NodeManager | synchronous call | caller context不在多数 worker interface | many errors discarded by dispatch helper |
| Microbatch | unbounded until caller flush relative to batchSize behavior | none | none |

## 6. Metrics and State

| Metrics | Source | Limitation |
|---|---|---|
| Controller status | queue/active/tracker/checkpoint/mode | write-specific |
| Orchestrator stats | submitted/completed/dropped/in-flight/depth | 任务未从主路径提交时代表性有限 |
| WorkerScheduler stats | dispatched/active by worker type | 必须由 caller 显式 Dispatch/Complete；不等于 NodeManager real activity |
| global metrics | query/write latency, visibility, counters | in-process, not scheduler feedback loop |

## 7. Correctness

- Controller 通过 generation、pause/reset、mode gate 和 tracker 保证 reset/shutdown 不混入旧 task。
- Orchestrator `execute` 忽略 ChainResult/error，`Completed` 表示调用结束而不表示业务成功。
- 严格优先队列可能使低优先级任务饥饿；没有 aging/fairness。
- NodeManager 多实现注册不等价于冗余或负载均衡。

## 8. 声明边界

可声明：Plasmod 有写入一致性调度、固定优先级 Chain dispatcher 和 worker registry/dispatch 三类基础能力。

不可声明：有统一智能调度器、资源成本优化、tenant fairness、dependency DAG、deadline-aware scheduling 或自动反馈优化。

## 9. 缺口与目标接口

目标 `ScheduledTask` 至少需要：task/chain type、priority、dependencies、deadline、tenant/scope、consistency、resource estimate、attempt、idempotency key、context/result channel。

目标 Scheduler 需增加 classifier、dependency resolver、cost/SLA evaluator、resource allocator、worker health/load、fair queue、cancellation、retry policy 和 trace propagation；在接入前 [Intelligent Scheduler](../05-engines/30-intelligent-scheduler.md) 只能标为部分/规划。
