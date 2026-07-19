# 05. 一致性、恢复与正确性模型

> Language: 中文 | [English](en/05-consistency-recovery-and-correctness.md)

---

定义 WAL、Retrieval Projection、Canonical Store、Evidence 和异步 Worker 的阶段与失败边界。

---

## 05.1. Consistency and Recovery Model

### 05.1.1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Correctness Perspective |
| 问题 | WAL、canonical、projection、evidence 和 async worker 如何保持可验证一致 |
| 成熟度 | write visibility/replay 完整基础；cross-module reconciliation 部分 |

### 05.1.2. 代码入口

| Concern | Package / file | Interface / method |
|---|---|---|
| WAL durability/replay | `src/internal/eventbackbone/` | `WAL.Append`, `ScanWAL`, File/InMemory WAL |
| write scheduling | `src/internal/worker/consistency/controller.go` | `Submit`, projection loop, `Start` recovery |
| write state | `src/internal/worker/consistency/tracker.go` | accepted/projecting/retrying/visible/failed tracking |
| checkpoint | `src/internal/worker/consistency/checkpoint.go` | `CheckpointStore.Load/Save/Reset` |
| canonical commit | `src/internal/storage/contracts.go` and backend | `RuntimeStorage.ApplyCanonicalProjection` |
| subscriber maintenance | `src/internal/worker/subscriber.go` | queue, retry, overflow, in-memory DLQ |
| admin recovery | `src/internal/worker/runtime.go`, `src/internal/access/` admin handlers | replay/reindex/reset/purge operations |

### 05.1.3. 输入与输出

| Operation | Input | Success output | Failure output/state |
|---|---|---|---|
| submit event | Event + visibility mode/deadline | LSN + accepted/visible status | admission, WAL or visibility error |
| projection | WAL entry | projection callback success + watermark/checkpoint | retrying or failed-prefix state |
| startup recovery | checkpoint + WAL scan | replayed visible prefix | startup/recovery error |
| subscriber maintenance | published event/message | derived state/index/trace/lifecycle updates | ErrorCh, overflow, in-memory DLQ |
| admin replay/reindex | range/options or canonical scan | preview/apply/rebuilt count | partial operation error |

### 05.1.4. 内部组成

#### 05.1.4.1. Source of truth and boundaries

| Data | Authority | Recovery source |
|---|---|---|
| accepted Event/order | WAL + LSN | FileWAL scan |
| canonical object graph | RuntimeStorage | WAL re-materialization/backups |
| retrieval projection | DataPlane/native/cold index | canonical scan/reindex or replay |
| evidence | Edge/Version/derivation/policy | query rebuild; cache disposable |
| async lifecycle | canonical/algorithm/audit stores | partial replay/manual operation |

#### 05.1.4.2. Write state mapping

统一阶段、真实 tracker 状态和 ACK 条件见 [Execution State and Failure Matrix](14-implementation-status-gaps-and-claim-boundaries.md)。代码没有单一 `RECEIVED..MAINTAINED` enum；Controller 显式跟踪 accepted/projecting/retrying/visible/failed prefix。

#### 05.1.4.3. Transaction and failure model

- FileWAL append 是 durability boundary。
- 同 backend canonical projection 可以 transactionally apply Event/Memory/checkpoint State/可选 Artifact/Edges/Versions。
- DataPlane ingest、canonical transaction、cache、S3、subscriber workers 是多个 failure domains。
- strict mode visibility failure不会删除已提交 WAL，而是返回 accepted-not-visible。
- bounded/eventual ACK 表示 pending，不是全部可查询对象完成。

### 05.1.5. 调用关系

Gateway 将 Event 交给 Runtime/consistency Controller；Controller 先 append WAL，再按 mode 同步或排队调用 Runtime projection callback。Projection 写 DataPlane 和 canonical bundle，随后 watermark/checkpoint 前进；Event bus subscriber 独立执行 state/tool/index/lifecycle 等维护。

Strict 只等待主 projection 的 visible 条件，不等待所有 subscriber worker。Bounded/eventual 可以在 projection 完成前返回；read gate 使用 observed/required LSN 和 watermark 控制读取。

### 05.1.6. 数据与状态

| State | Location | Persistence |
|---|---|---|
| WAL entries/LSN | Event backbone | FileWAL 或 memory WAL |
| accepted/visible/failed prefix | consistency tracker | 主要内存状态 |
| replay checkpoint/generation | checkpoint store | file/memory/buffered |
| canonical object graph | RuntimeStorage | memory/Badger |
| projection/index | DataPlane/native/cold | 多 backend，可重建 |
| retry queues, slots, DLQ | Controller/subscriber | 主要内存状态 |
| derivation/policy/audit | dedicated logs/stores | backend-dependent |

### 05.1.7. 正确性

Recovery APIs：

| Capability | Entry |
|---|---|
| startup recovery | `Controller.Start -> recoverFromWAL` |
| checkpoint | File/Memory/Buffered checkpoint stores |
| admin replay | `/v1/admin/replay` preview/apply |
| reindex | `/v1/admin/embeddings/reindex`, warm prebuild |
| reset/wipe | Runtime pause subscribers/controller, reset stores/WAL/checkpoint |
| purge resume | hard-delete/purge task state where applicable |

#### 05.1.7.1. Idempotency, retry and audit

Deterministic IDs 和 upsert 支持 replay；Controller 重试 projection/checkpoint；subscriber 有进程内 DLQ/overflow/error channel。没有持久全局 dead-letter、divergence scanner、repair planner 或 post-repair verifier。

### 05.1.8. 声明边界

可声明三种 visibility mode、checkpointed WAL recovery、deterministic replay、专用 reindex/purge recovery。

不可声明跨 WAL/Badger/native/S3 的 ACID、所有 subscriber 已随 strict ACK 完成、自动 full reconciliation 或 evidence cache durability。

### 05.1.9. 缺口

- 缺少显式统一 `WriteState` schema 和可持久 stage marker；
- 缺少持久 DLQ、跨重启 retry intent 和 dead-letter replay API；
- 缺少 canonical/projection/evidence divergence scanner；
- 缺少 repair planner、执行进度和 post-repair verifier；
- 缺少跨 failure window 的系统 fault-injection contract tests；
- 详细目标边界见 [Reconciliation Manager](05-consistency-recovery-and-correctness.md)。

---

## 05.2. Cross-module Consistency Mechanism

### 05.2.1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Mechanism |
| 目标 | WAL <-> materialization <-> canonical <-> projection <-> evidence <-> maintenance 对齐 |
| 成熟度 | visibility/replay 完整基础；continuous repair 部分/规划 |

### 05.2.2. Source of truth and markers

| Concern | Marker/authority |
|---|---|
| accepted order | WAL LSN |
| visible prefix | consistency Tracker + checkpoint/watermark |
| canonical mutation | deterministic object ID + ObjectVersion |
| projection compatibility | object ID + embedding family/dim |
| evidence lineage | Edge/Version/derivation/policy refs |
| async processing | subscriber cursor/count/DLQ memory |

### 05.2.3. Idempotency and checks

Deterministic IDs, store upsert and replay provide basic idempotency。Embedding compatibility validation prevents obvious vector-space mismatch。There is no unified per-object stage row, cross-store checksum or divergence table。

### 05.2.4. Failure handling

Controller detects projection/checkpoint failure；Gateway maps strict error；subscriber catches panic in in-memory DLQ；admin replay/reindex/purge handle selected repairs。Canonical success and auxiliary failure can still return visible。

### 05.2.5. Recovery planning/execution

Current repair is operation-specific：WAL replay、embedding reindex、warm prebuild、hard-delete task、data wipe/restart recovery。No component computes a generic repair plan based on divergence type。

### 05.2.6. Audit and verification

Policy/Audit/Derivation logs record selected semantic changes；metrics expose queue/latency/error counters。Post-repair verification is admin summary/tests/manual query, not a persisted verifier result。

### 05.2.7. Correctness boundary

See [Execution State and Failure Matrix](14-implementation-status-gaps-and-claim-boundaries.md)。Visibility means projection callback + checkpoint progress, not every async maintenance step。

### 05.2.8. 声明边界

可声明 WAL-based recovery, checkpointed visible prefix and deterministic rebuild operations。

不可声明 automatic cross-module reconciliation, durable dead-letter, distributed transaction or verified self-healing。

### 05.2.9. 缺口

Add StageRecord/idempotency key registry, divergence scanner, repair planner/executor, durable DLQ, relation/evidence rebuild, verification query/checksum and audit trail for repairs。

---

## 05.3. Reconciliation Manager

### 05.3.1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Engine |
| 目标 | 检测 canonical/projection/evidence/tier divergence，计划修复、执行并验证 |
| 关键路径 | recovery/admin only |
| 当前成熟度 | 占位/部分：没有一个 active `ReconciliationManager` type |

### 05.3.2. Existing code fragments

| Capability | Existing entry | Maturity |
|---|---|---|
| WAL recovery | `consistency.Controller.recoverFromWAL` | 完整 write-prefix recovery |
| replay | `Runtime.AdminReplayPreview/Apply` | 完整基础 |
| re-materialization | replay -> Runtime ingest/project | 部分 |
| embedding/index rebuild | `Runtime.ReindexEmbeddings`, warm prebuild/reset | 完整专用 |
| purge repair | hard delete manager/task + tier cleanup | 部分 |
| subscriber DLQ | in-memory DLQ/overflow/ErrorCh | 部分，非持久 |
| relation/evidence rebuild | replay/query delta/manual edges | 部分 |
| divergence scan/repair plan/verifier | no unified implementation | 规划 |

### 05.3.3. Current inputs/outputs

| Operation | Input | Output |
|---|---|---|
| replay preview/apply | from LSN + limit | counts/range/errors summary |
| reindex | canonical Memory scan + embedding spec | indexed count/error |
| purge task | dataset/source/object selectors | stage/status/counts/errors |
| startup recovery | checkpoint LSN + WAL tail | queued projection tasks |

### 05.3.4. Required target fields

The following are design requirements, not current fields：

| Field | Purpose |
|---|---|
| `scannerRegistry` | canonical/projection/relation/evidence/tier scanners |
| `checkpointReader`, `wal` | recovery boundary |
| `repairPlanner` | classify divergence and dependency order |
| `executors` | replay/re-materialize/reindex/rebuild/delete workers |
| `deadLetterStore` | durable failed repair records |
| `verificationRunner` | post-repair checks |
| `auditStore`, `metrics` | traceability/observability |
| `locks/generation` | prevent conflicting repair/reset |

### 05.3.5. Required interface

```text
Scan(scope) -> DivergenceReport
Plan(report) -> RepairPlan
Execute(ctx, plan) -> RepairResult
Verify(ctx, result) -> VerificationResult
Resume(repairID) / Cancel(repairID)
```

`RepairPlan` needs object IDs/LSN range, source of truth, ordered actions, idempotency keys, retry policy and expected postconditions。

### 05.3.6. Call relationships and state

Current operations are called by bootstrap/controller/admin Gateway separately；they do not share a durable repair record。Canonical/WAL remains recovery authority；projection/cache/tier are targets。

### 05.3.7. Correctness/failure

Replaying deterministic IDs is mostly idempotent but auxiliary worker behavior and direct mutations can violate it。Reindex resets in-memory retrieval then scans canonical records；failure mid-run needs explicit rerun。No unified lock prevents every concurrent admin repair combination。

### 05.3.8. 声明边界

可声明 startup WAL recovery, replay, reindex and staged purge operations。

不可声明 implemented general Reconciliation Manager, automatic divergence detection, durable dead-letter repair or verified self-healing。

### 05.3.9. Missing implementation

This Engine remains a required new design：implement durable divergence/repair schemas, scanner registry, planner/executor, relation/evidence rebuilders, DLQ, operation locking, resumability, verification and failure-injection tests before marking partial/complete system-level reconciliation。

---

## 05.4. 并发模型

| 区域 | 机制 | 目的 |
|---|---|---|
| Gateway writes | bounded channel semaphore | 限制并发写，过载立即 503 |
| WAL | implementation mutex/file append | 分配单调 LSN，保护记录 |
| Consistency admission | RW gates + append mutex | 模式切换与 append 顺序 |
| Projection | worker-sharded queues + global slots | 有界并发与 backpressure |
| Bounded mode | per-shard reservation | 防止同 shard SLA 过度排队 |
| Tracker/checkpoint | mutex + buffered checkpoint | 连续 watermark 与合并持久化 |
| EventSubscriber | polling goroutine + drain mutex | 按 visible LSN 串行 drain |
| Node manager | RWMutex | worker 注册与 dispatch |
| Orchestrator | 四级 priority channels | 有界 worker pool 调度 |
| Hot cache | RWMutex | cache get/put/eviction |
| Badger | transactions | concurrent KV read/write |
| Segment/index | package-level locks | build/search/unload 协调 |
| Shutdown | context cancellation + WaitGroup | 停止 admission、drain worker、关闭资源 |

### 05.4.1. 关键不变量

- 不在持有模式切换 write gate 时执行无界外部请求。
- bounded deadline 从 WAL accepted time 计算，而不是从排队前计算。
- projection worker 失败不能推进 Tracker。
- subscriber 不能读取高于 controller visible watermark 的 entry。
- panic 必须被隔离并进入 dead-letter 路径，不能杀死 drain loop。

### 05.4.2. Background goroutine 所有权

Gateway 拥有 hard-delete manager；Runtime 拥有 flush loop、subscriber、consistency lifecycle 与部分 outbox；ServerBundle.Shutdown 负责按依赖顺序停止它们并关闭 Badger/WAL/derivation store。新增 goroutine 必须明确 owner、cancel source、drain policy 和 shutdown timeout。

---

## 05.5. 一致性模型

### 05.5.1. 状态定义

- **Accepted**：Event 已 append 到 WAL，并获得 LSN。
- **Canonical visible**：该 LSN 的 object/edge/version projection 成功，Tracker 已标记 visible。
- **Retrieval visible**：对应对象已经能从目标 retrieval path 命中；background flush/index build 可能使它晚于 canonical visible。
- **Evidence visible**：查询所需 edge/version/cache fragment 已可组装。

### 05.5.2. 模式

| Mode | 写入返回 | 读取行为 | 适用场景 |
|---|---|---|---|
| `strict_visible` / `strict` | 等待 projection 可见；失败返回 accepted-not-visible 或 projection error | 等待目标 visibility | 下一步决策必须读到本次写入 |
| `bounded_staleness` / `bounded` | 预留 shard slot，在 freshness SLA 内推进 | 最多等待配置的 query timeout | 容忍有界陈旧 |
| `eventual_visibility` / `eventual` | WAL 接受后异步 projection | 不等待最新 LSN | 吞吐优先、可重试读取 |

### 05.5.3. 配置

默认模式为 strict。核心变量包括 `PLASMOD_CONSISTENCY_DEFAULT_MODE`、`BOUNDED_MAX_LAG`、`QUEUE_SIZE`、`WORKERS`、重试间隔、query/shutdown timeout、checkpoint path 与 flush interval。

### 05.5.4. 并发与顺序

Controller 使用 admission gate、mode gate、append mutex、全局 slots 和按 worker shard queues。bounded 写对同一 shard 只允许一个 reservation，避免 deadline 队列过度订阅。Tracker 只按连续成功 LSN 推进 watermark。

### 05.5.5. 模式切换

admin consistency mode 修改 runtime default，不重写已持久 Event 的 resolved mode。切换期间 controller 通过 gate/drain 防止不同模式的 admission 无序穿越。

### 05.5.6. 恢复

disk mode 的 checkpoint 默认位于 data dir。首次持久化启动可将 checkpoint bootstrap 到当前 WAL latest，随后从 checkpoint + 1 扫描。已有 checkpoint 时必须重放后续 entry；scan/decode error 终止恢复。

### 05.5.7. 限制

该模型约束 Event ingest 主路径；direct canonical CRUD 不自动获得相同 WAL/visibility 保证。跨 S3/native index 的 visibility 也不是全局事务提交点。

---

## 05.6. 失败模型

### 05.6.1. Before WAL

JSON 解码、schema/consistency 验证或 Gateway semaphore 失败。Event 未获得 LSN，客户端可以修正请求或使用同一 event ID 重试。

### 05.6.2. WAL append

file IO、encoding 或 sync 失败。不得返回 accepted。运维应检查 data dir 权限、磁盘空间和 WAL 状态。

### 05.6.3. After WAL, before visibility

Event 已有 LSN，但 projection queue、worker、canonical write 或 retrieval ingest 失败。strict 返回 `AcceptedNotVisibleError`/`ProjectionFailureError`；客户端必须保留 event ID 并查询/replay，不能盲目创建新 ID。

### 05.6.4. Canonical projection failure

共享 Badger backend 使用 transaction；任一 object/edge/version 编码或写入失败应回滚该 transaction。memory/hybrid backend 不提供跨独立 backend 的同等原子性，因此 factory 禁止 objects/edges/versions 混用 backend。

### 05.6.5. Retrieval projection failure

canonical data 可能已经可恢复而 index 未更新。系统应报告错误或保持 watermark，不应把 index 当作唯一事实。修复 embedder/native bridge 后使用 reindex。

### 05.6.6. Evidence failure

edge/version/cache fragment 缺失时 QueryResponse 可能退化但仍返回对象。需要强 proof 的调用方必须检查 proof/provenance，而不是只检查 HTTP 200。

### 05.6.7. Async worker failure

subscriber handler panic 被捕获并进入 dead-letter channel/overflow buffer；普通 error 的处理取决于 worker。二级算法失败不应倒写 WAL accepted 事实。

### 05.6.8. Shutdown during operation

controller 停止接受新任务、取消 admission、等待 active/worker 和 checkpoint flush。超时会返回 shutdown error；不得在资源仍被使用时先关闭 Badger。

### 05.6.9. Replay failure

WAL scan/decode、旧 schema、embedding family 或 projection error 可中断 replay。先使用 preview、备份 data dir，再执行 apply；不要通过删除 checkpoint 掩盖坏记录。
