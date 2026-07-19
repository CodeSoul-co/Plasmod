# 19. Cross-module Consistency Mechanism

## 1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Mechanism |
| 目标 | WAL <-> materialization <-> canonical <-> projection <-> evidence <-> maintenance 对齐 |
| 成熟度 | visibility/replay 完整基础；continuous repair 部分/规划 |

## 2. Source of truth and markers

| Concern | Marker/authority |
|---|---|
| accepted order | WAL LSN |
| visible prefix | consistency Tracker + checkpoint/watermark |
| canonical mutation | deterministic object ID + ObjectVersion |
| projection compatibility | object ID + embedding family/dim |
| evidence lineage | Edge/Version/derivation/policy refs |
| async processing | subscriber cursor/count/DLQ memory |

## 3. Idempotency and checks

Deterministic IDs, store upsert and replay provide basic idempotency。Embedding compatibility validation prevents obvious vector-space mismatch。There is no unified per-object stage row, cross-store checksum or divergence table。

## 4. Failure handling

Controller detects projection/checkpoint failure；Gateway maps strict error；subscriber catches panic in in-memory DLQ；admin replay/reindex/purge handle selected repairs。Canonical success and auxiliary failure can still return visible。

## 5. Recovery planning/execution

Current repair is operation-specific：WAL replay、embedding reindex、warm prebuild、hard-delete task、data wipe/restart recovery。No component computes a generic repair plan based on divergence type。

## 6. Audit and verification

Policy/Audit/Derivation logs record selected semantic changes；metrics expose queue/latency/error counters。Post-repair verification is admin summary/tests/manual query, not a persisted verifier result。

## 7. Correctness boundary

See [Execution State and Failure Matrix](../06-cross-reference/execution-state-and-failure-matrix.md)。Visibility means projection callback + checkpoint progress, not every async maintenance step。

## 8. 声明边界

可声明 WAL-based recovery, checkpointed visible prefix and deterministic rebuild operations。

不可声明 automatic cross-module reconciliation, durable dead-letter, distributed transaction or verified self-healing。

## 9. 缺口

Add StageRecord/idempotency key registry, divergence scanner, repair planner/executor, durable DLQ, relation/evidence rebuild, verification query/checksum and audit trail for repairs。
