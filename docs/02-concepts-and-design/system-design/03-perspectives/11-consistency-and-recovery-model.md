# 11. Consistency and Recovery Model

## 1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Correctness Perspective |
| 问题 | WAL、canonical、projection、evidence 和 async worker 如何保持可验证一致 |
| 成熟度 | write visibility/replay 完整基础；cross-module reconciliation 部分 |

## 2. 代码入口

| Concern | Package / file | Interface / method |
|---|---|---|
| WAL durability/replay | `src/internal/eventbackbone/` | `WAL.Append`, `ScanWAL`, File/InMemory WAL |
| write scheduling | `src/internal/worker/consistency/controller.go` | `Submit`, projection loop, `Start` recovery |
| write state | `src/internal/worker/consistency/tracker.go` | accepted/projecting/retrying/visible/failed tracking |
| checkpoint | `src/internal/worker/consistency/checkpoint.go` | `CheckpointStore.Load/Save/Reset` |
| canonical commit | `src/internal/storage/contracts.go` and backend | `RuntimeStorage.ApplyCanonicalProjection` |
| subscriber maintenance | `src/internal/worker/subscriber.go` | queue, retry, overflow, in-memory DLQ |
| admin recovery | `src/internal/worker/runtime.go`, `src/internal/access/` admin handlers | replay/reindex/reset/purge operations |

## 3. 输入与输出

| Operation | Input | Success output | Failure output/state |
|---|---|---|---|
| submit event | Event + visibility mode/deadline | LSN + accepted/visible status | admission, WAL or visibility error |
| projection | WAL entry | projection callback success + watermark/checkpoint | retrying or failed-prefix state |
| startup recovery | checkpoint + WAL scan | replayed visible prefix | startup/recovery error |
| subscriber maintenance | published event/message | derived state/index/trace/lifecycle updates | ErrorCh, overflow, in-memory DLQ |
| admin replay/reindex | range/options or canonical scan | preview/apply/rebuilt count | partial operation error |

## 4. 内部组成

### Source of truth and boundaries

| Data | Authority | Recovery source |
|---|---|---|
| accepted Event/order | WAL + LSN | FileWAL scan |
| canonical object graph | RuntimeStorage | WAL re-materialization/backups |
| retrieval projection | DataPlane/native/cold index | canonical scan/reindex or replay |
| evidence | Edge/Version/derivation/policy | query rebuild; cache disposable |
| async lifecycle | canonical/algorithm/audit stores | partial replay/manual operation |

### Write state mapping

统一阶段、真实 tracker 状态和 ACK 条件见 [Execution State and Failure Matrix](../06-cross-reference/execution-state-and-failure-matrix.md)。代码没有单一 `RECEIVED..MAINTAINED` enum；Controller 显式跟踪 accepted/projecting/retrying/visible/failed prefix。

### Transaction and failure model

- FileWAL append 是 durability boundary。
- 同 backend canonical projection 可以 transactionally apply Memory/Artifact/Edges/Versions。
- DataPlane ingest、canonical transaction、cache、S3、subscriber workers 是多个 failure domains。
- strict mode visibility failure不会删除已提交 WAL，而是返回 accepted-not-visible。
- bounded/eventual ACK 表示 pending，不是全部可查询对象完成。

## 5. 调用关系

Gateway 将 Event 交给 Runtime/consistency Controller；Controller 先 append WAL，再按 mode 同步或排队调用 Runtime projection callback。Projection 写 DataPlane 和 canonical bundle，随后 watermark/checkpoint 前进；Event bus subscriber 独立执行 state/tool/index/lifecycle 等维护。

Strict 只等待主 projection 的 visible 条件，不等待所有 subscriber worker。Bounded/eventual 可以在 projection 完成前返回；read gate 使用 observed/required LSN 和 watermark 控制读取。

## 6. 数据与状态

| State | Location | Persistence |
|---|---|---|
| WAL entries/LSN | Event backbone | FileWAL 或 memory WAL |
| accepted/visible/failed prefix | consistency tracker | 主要内存状态 |
| replay checkpoint/generation | checkpoint store | file/memory/buffered |
| canonical object graph | RuntimeStorage | memory/Badger |
| projection/index | DataPlane/native/cold | 多 backend，可重建 |
| retry queues, slots, DLQ | Controller/subscriber | 主要内存状态 |
| derivation/policy/audit | dedicated logs/stores | backend-dependent |

## 7. 正确性

Recovery APIs：

| Capability | Entry |
|---|---|
| startup recovery | `Controller.Start -> recoverFromWAL` |
| checkpoint | File/Memory/Buffered checkpoint stores |
| admin replay | `/v1/admin/replay` preview/apply |
| reindex | `/v1/admin/embeddings/reindex`, warm prebuild |
| reset/wipe | Runtime pause subscribers/controller, reset stores/WAL/checkpoint |
| purge resume | hard-delete/purge task state where applicable |

### Idempotency, retry and audit

Deterministic IDs 和 upsert 支持 replay；Controller 重试 projection/checkpoint；subscriber 有进程内 DLQ/overflow/error channel。没有持久全局 dead-letter、divergence scanner、repair planner 或 post-repair verifier。

## 8. 声明边界

可声明三种 visibility mode、checkpointed WAL recovery、deterministic replay、专用 reindex/purge recovery。

不可声明跨 WAL/Badger/native/S3 的 ACID、所有 subscriber 已随 strict ACK 完成、自动 full reconciliation 或 evidence cache durability。

## 9. 缺口

- 缺少显式统一 `WriteState` schema 和可持久 stage marker；
- 缺少持久 DLQ、跨重启 retry intent 和 dead-letter replay API；
- 缺少 canonical/projection/evidence divergence scanner；
- 缺少 repair planner、执行进度和 post-repair verifier；
- 缺少跨 failure window 的系统 fault-injection contract tests；
- 详细目标边界见 [Reconciliation Manager](../05-engines/28-reconciliation-manager.md)。
