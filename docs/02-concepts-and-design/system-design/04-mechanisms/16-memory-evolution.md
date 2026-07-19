# 16. Memory Evolution Mechanism

## 1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Mechanism |
| 目标 | Recall/usage/time/policy/cost 信号驱动 memory state、content、tier 和 projection 演化 |
| 成熟度 | 部分 |

## 2. Entry and decision owners

| Entry | Decision owner |
|---|---|
| algorithm dispatch ingest/update/recall/decay/compress/summarize | selected plugin |
| reflection policy | PolicyRecord + baseline worker rules |
| admin archive/delete/purge | handler/storage policy |
| hot cache promotion/eviction | salience/hotness/config |

## 3. Signals and outputs

| Signal | Possible output |
|---|---|
| recall/query | scored order, plugin reinforcement state |
| elapsed time/TTL | decayed/stale/archive suggestion |
| importance/confidence/policy | salience adjustment/quarantine |
| conflict penalty | quarantine/stale in MemoryBank-style |
| content set | compressed/summary derived Memory |
| storage pressure/access | hot eviction/promotion |

## 4. Interfaces and state

`MemoryManagementAlgorithm` 是 decision plugin；dispatcher persists `MemoryAlgorithmState` and Memory outputs；reflection writes Memory/PolicyDecision/Audit and may archive。Canonical lifecycle, algorithm state and physical tier are distinct states。

## 5. Sync/async

Internal algorithm API synchronous；subscriber reflection/consolidation asynchronous；hot eviction happens on cache insert；cold archive mostly explicit or reflection-driven。No central evolution loop schedules all operations。

## 6. Correctness

- Source Memory remains for compression/summary semantics unless explicit deletion。
- Derived edges/version/projection are not uniformly required by dispatcher。
- Recall dispatch does not persist generic reinforcement state。
- Archived read does not automatically produce reactivation transition。

## 7. Failure/recovery

Partial mutation may leave lifecycle and projection/tier out of sync；current recovery relies on canonical inspection, reindex/archive retry and audit logs。No lifecycle transaction log/reconciler。

## 8. 声明边界

可声明 pluggable evolution algorithms and policy/tier actions。

不可声明 fully autonomous evolution engine、uniform score function、automatic reactivation or transactional lifecycle transitions。

## 9. 缺口

Define EvolutionCommand/Decision/Transition record, mandatory version/edge/audit/projection hooks, operation scheduler, reactivation and purge states, and transition metrics/tests。
