# 28. Reconciliation Manager

## 1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Engine |
| 目标 | 检测 canonical/projection/evidence/tier divergence，计划修复、执行并验证 |
| 关键路径 | recovery/admin only |
| 当前成熟度 | 占位/部分：没有一个 active `ReconciliationManager` type |

## 2. Existing code fragments

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

## 3. Current inputs/outputs

| Operation | Input | Output |
|---|---|---|
| replay preview/apply | from LSN + limit | counts/range/errors summary |
| reindex | canonical Memory scan + embedding spec | indexed count/error |
| purge task | dataset/source/object selectors | stage/status/counts/errors |
| startup recovery | checkpoint LSN + WAL tail | queued projection tasks |

## 4. Required target fields

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

## 5. Required interface

```text
Scan(scope) -> DivergenceReport
Plan(report) -> RepairPlan
Execute(ctx, plan) -> RepairResult
Verify(ctx, result) -> VerificationResult
Resume(repairID) / Cancel(repairID)
```

`RepairPlan` needs object IDs/LSN range, source of truth, ordered actions, idempotency keys, retry policy and expected postconditions。

## 6. Call relationships and state

Current operations are called by bootstrap/controller/admin Gateway separately；they do not share a durable repair record。Canonical/WAL remains recovery authority；projection/cache/tier are targets。

## 7. Correctness/failure

Replaying deterministic IDs is mostly idempotent but auxiliary worker behavior and direct mutations can violate it。Reindex resets in-memory retrieval then scans canonical records；failure mid-run needs explicit rerun。No unified lock prevents every concurrent admin repair combination。

## 8. 声明边界

可声明 startup WAL recovery, replay, reindex and staged purge operations。

不可声明 implemented general Reconciliation Manager, automatic divergence detection, durable dead-letter repair or verified self-healing。

## 9. Missing implementation

This Engine remains a required new design：implement durable divergence/repair schemas, scanner registry, planner/executor, relation/evidence rebuilders, DLQ, operation locking, resumability, verification and failure-injection tests before marking partial/complete system-level reconciliation。
