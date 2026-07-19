# 25. Memory Evolution Engine

## 1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Engine |
| 原模块 | Memory Algorithm Dispatcher + plugins |
| 目标 | 执行 memory ingest/update/recall/decay/compress/summarize 并持久化结果 |
| 关键路径 | internal memory operations；普通 query 可通过 recall adapter |
| 成熟度 | 完整 plugin/dispatcher foundation；unified lifecycle transaction 部分 |

## 2. Code entry

| Item | Code |
|---|---|
| Interface | `schemas.MemoryManagementAlgorithm` |
| Dispatcher | `worker/cognitive/algorithm_dispatcher.go` |
| Plugins | `cognitive/baseline`, `memorybank`, `zep` |
| Constructor | `CreateAlgorithmDispatchWorker` |
| Runtime | `DispatchAlgorithm`, `DispatchRecall` |
| HTTP | `/v1/internal/memory/*` |

## 3. Dispatcher fields

| Field | Type | Meaning |
|---|---|---|
| `id` | string | worker/node identity |
| `algo` | `MemoryManagementAlgorithm` | active plugin |
| `objStore` | ObjectStore | canonical Memory read/write |
| `algoStore` | MemoryAlgorithmStateStore | plugin state persistence |
| `auditStore` | AuditStore | algorithm update audit |

Plugins additionally hold their own configuration and optional in-memory exported state maps；their durable portability depends on dispatcher state persistence/load usage。

## 4. Interface methods

| Method | Input | Output |
|---|---|---|
| `AlgorithmID` | none | stable ID |
| `Ingest` | Memories + context | initial states |
| `Update` | Memories + signals | updated states |
| `Recall` | query + candidates + context | scored Memories |
| `Compress` | Memories + context | derived Memories |
| `Decay` | Memories + timestamp | updated states |
| `Summarize` | Memories + context | summary Memories |
| `ExportState/LoadState` | memory ID/state | plugin state portability |

Dispatcher `Dispatch` supports operation strings `ingest|decay|recall|compress|summarize|update` and returns `AlgorithmDispatchOutput`。

## 5. Internal behavior

- fetch IDs from ObjectStore；
- construct AlgorithmContext；
- call exactly one selected operation；
- persist states and apply `SuggestedLifecycleState` verbatim；
- store derived Memory as returned；
- append algorithm-update AuditRecord；
- return counts/IDs/scored refs。

Dispatcher deliberately has no threshold/business decision logic。

## 6. State mutations and side effects

| Operation | Canonical Memory | Algorithm state | Audit | Version/Edge/Projection |
|---|---|---|---|---|
| ingest/update/decay | ref/lifecycle may change | yes | yes | not uniformly |
| recall | no generic mutation | no dispatcher persistence | no | none |
| compress/summarize | new Memory | plugin-dependent | yes | not uniformly |

## 7. Correctness/failure

- Unknown operation returns typed error string。
- Missing Memory IDs are silently omitted from input set。
- Store methods do not return errors, so persistence failure visibility depends implementation。
- Profile switch does not migrate/validate old algorithm states。
- Derived Memory may be absent from retrieval until separately indexed。

## 8. 声明边界

可声明 pluggable algorithm interface with canonical-independent state and baseline/MemoryBank-style/Zep-style implementations。

不可声明 plugins are externally equivalent to named products, recall always reinforces, or lifecycle/derived mutations are transactionally versioned/reindexed。

## 9. 缺口

Capability registry/versioning, plugin state migration, missing-ID/error reporting, lifecycle transition transaction, mandatory derived edges/versions/projection, operation metrics and deterministic plugin contract tests。
