# Execution State and Failure Matrix

## Active Write Stages

代码没有一个覆盖全系统的 `WriteState` enum。下表把实际 tracker/runtime 状态映射到统一术语，不能将统一术语误写成已存在的单一状态机。

| 统一阶段 | 代码事件 | 持久/内存位置 | 是否 ACK 前完成 |
|---|---|---|---|
| `RECEIVED` | Gateway decode/normalize | request memory | 是 |
| `WAL_COMMITTED` | `WAL.Append` 返回 LSN | FileWAL 或 InMemoryWAL | 所有模式是 |
| `PROJECTING` | tracker `MarkProjecting` | tracker memory/checkpoint context | strict 等待；其他异步 |
| `INDEXED` | `DataPlane.Ingest` 成功 | warm retrieval structures/native index | strict 是；其他最终完成 |
| `PERSISTED` | `ApplyCanonicalProjection` 成功 | object/edge/version stores | strict 是；其他最终完成 |
| `EVIDENCE_READY` | precompute cache fragment 写入 | in-memory evidence cache | 非 visibility gate；可能跳过 |
| `VISIBLE` | tracker `MarkVisible` + checkpoint/watermark | tracker + checkpoint | strict 是；bounded/eventual ACK 后 |
| `MAINTAINED` | subscriber/reflection/consolidation/flush | multiple stores/cache/index | 否 |

当前 `projectWALEntry` 实际顺序是 retrieval ingest -> canonical projection -> hot promotion/conflict/precompute/worker dispatch。它通过“先检索失败再避免 canonical mutation”缩小 partial window，但 retrieval 与 canonical store 不属于同一跨系统事务。

## Consistency Modes

| Mode | Submit response | Read gate | Guarantee |
|---|---|---|---|
| strict/strict_visible | 等 projection + visible；失败返回 accepted-not-visible | waits through accepted prefix | request scoped read-after-write |
| bounded/bounded_staleness | WAL 后 pending ACK；sharded queue | waits within configured lag/through current accepted prefix | bounded lag，SLA breach 可观测 |
| eventual/eventual_visibility | WAL 后 pending ACK | minimal ordering gate | eventual projection |

## Sync/Async Boundary

| Operation | 同步部分 | 异步部分 |
|---|---|---|
| Event ingest | validation, WAL, strict projection wait | bounded/eventual projection, subscriber maintenance, periodic flush |
| Canonical projection | retrieval ingest, object/edge/version transaction, hot promotion | specialized state/tool/consolidation/reflection via subscriber |
| Query | planner, retrieval, filters, assembler, QueryChain | no durable background completion promised |
| Memory algorithm API | selected plugin dispatch and store writes | provider shadow path may be external; no generic job tracker |
| Collaboration | conflict/share call | microbatch queue only processes when explicitly flushed |
| Archive/purge | request validation and task enqueue/start | hard delete/export stages where handler creates task |

## Failure Windows

| Window | Possible state | Detection | Current recovery |
|---|---|---|---|
| WAL append fails | no accepted write | returned error | caller retry with same event ID |
| WAL succeeds, projection fails | durable Event, object not visible | tracker failure/status, 503 for strict | bounded retry; restart recovery scans WAL; admin replay |
| Retrieval ingest succeeds, canonical projection fails | candidate may exist without canonical record | projection error; query hydration/filter anomalies | replay/reindex; no automatic two-plane rollback |
| Canonical commit succeeds, precompute fails/skips | object visible without cached fragment | cache miss stats | query-time delta evidence |
| Canonical succeeds, subscriber worker fails | main object visible, auxiliary state/audit/index may lag | logs/ErrorCh/in-memory DLQ/overflow；limited per-worker status | WAL subscriber re-scan/restart, manual replay；无 durable dead-letter queue |
| Checkpoint save fails | object may be visible but durable progress uncertain | `checkpointVisibilityError`, buffered checkpoint last error | retry/flush, restart WAL scan |
| Cold archive partially fails | warm remains authoritative if deletion order respected | admin response/diagnostics | retry export; no global transaction |
| Hard purge partially fails | stores diverge | purge task stage/error | resume/retry task/manual cleanup |

## Idempotency and Ordering

| Area | Current mechanism | Limitation |
|---|---|---|
| Event replay | deterministic IDs from event ID | direct workers with alternate IDs must preserve same rule |
| WAL ordering | monotonic LSN and serialized append section | global distributed ordering is not implemented by active single-process core |
| Canonical projection | same-backend transaction and upsert | retrieval/S3/cache are outside transaction |
| State version | state worker reads current state and increments | in-memory `stateKeys` plus store lookup; cross-process coordination absent |
| Edge generation | deterministic source/type/destination IDs in builders | direct Edge POST can introduce duplicates/semantic inconsistency |
| Algorithm state | composite memory/algorithm key | switching algorithm profile does not migrate old state |

## Retry, Cancellation and Backpressure

| Component | Behavior |
|---|---|
| Consistency Controller | bounded queues, global slots, bounded shard slots, exponential/configured retry, context cancellation, pause/reset/resume/shutdown |
| Gateway | request context passed to Runtime for ingest/query; write semaphore controls HTTP concurrency |
| Orchestrator | four priority queues, fixed worker pool, 30 s submit timeout; no task result/future/cancellation handle |
| NodeManager | synchronous first-worker dispatch; worker errors often not propagated by void dispatch helpers |
| DataPlane flush loop | periodic retry by leaving dirty flag set and logging failure |
| Subscriber | WAL polling, context cancellation, retry/overflow and in-memory DLQ；no persistent dead-letter queue |

## Recovery Capability Matrix

| Capability | Implementation | Maturity |
|---|---|---|
| startup WAL recovery | consistency controller `recoverFromWAL` from checkpoint | 完整主路径 |
| admin replay preview/apply | Runtime WAL scan and resubmit | 完整基础路径 |
| canonical re-materialization | replay deterministic materializer | 部分：受 worker/idempotency 边界约束 |
| embedding/retrieval reindex | `Runtime.ReindexEmbeddings`, warm prebuild | 完整专用操作 |
| relation repair | replay/build edges or manual operations | 部分，无独立 scanner/planner |
| evidence rebuild | query delta + ingest precompute | 部分，无全量 durable evidence rebuild manager |
| divergence scanner | no unified active component | 规划 |
| dead-letter handler | no active durable queue | 规划 |
| post-repair verification | tests/admin summaries, no unified verifier | 规划 |

## Correctness Review Rule

任何新阶段都必须说明：谁持有状态、是否在 ACK 前完成、失败是否推进 checkpoint、是否可重入、如何观察、如何修复，以及它是否属于 visibility guarantee。
