# 9. Canonical State vs Retrieval Projection

## 1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Data Perspective |
| 问题 | 权威状态与检索加速视图为何分离、如何连接和恢复 |
| 成熟度 | 完整的基本双平面，自动 divergence reconciliation 部分 |

## 2. 代码入口

| Direction / concern | Package / file | Interface / method |
|---|---|---|
| canonical contracts | `src/internal/storage/contracts.go` | `ObjectStore`, `GraphEdgeStore`, `SnapshotVersionStore`, `RuntimeStorage` |
| canonical commit | `src/internal/storage/canonical_projection.go`, Badger stores | `ApplyCanonicalProjection` |
| event derivation | `src/internal/materialization/service.go` | `MaterializeEvent` |
| projection write/read | `src/internal/dataplane/contracts.go` | `DataPlane.Ingest`, `Search`, `Flush` |
| vector projection | `src/internal/dataplane/vectorstore.go` | `AddText(s)`, `AddVector`, `Build`, `Search` |
| tier connection | `src/internal/storage/tiered.go`, `src/internal/dataplane/tiered_adapter.go` | tier read/write and `IncludeCold` search |
| rebuild | `src/internal/worker/runtime.go`, access admin handlers, index workers | `ReindexEmbeddings`, prebuild and replay methods |

## 3. 输入与输出

| Flow | Input | Output | Connection key |
|---|---|---|---|
| canonical mutation | canonical object/edge/version bundle | persisted object graph | object ID + version |
| projection mutation | `IngestRecord` | lexical/vector/sparse/native record | `ObjectID` |
| query projection | `SearchInput` | candidate object IDs and scores | `ObjectID` |
| hydration | candidate IDs + requested types | canonical GraphNode/object/version/policy | typed object ID |
| rebuild | canonical Memory list or WAL entries | regenerated projection/index | object ID + embedding family/dim |
| delete/archive | object ID + mode | tombstone/eviction/cold movement | same object ID across tiers |

## 4. 内部组成

| Canonical plane | Projection plane |
|---|---|
| Event/WAL, Memory, State, Artifact | lexical records, vector/sparse records |
| Edge, ObjectVersion | hot/warm/cold segment metadata |
| PolicyRecord, ShareContract, Audit | native index handles and candidate views |
| MemoryAlgorithmState | evidence fragment cache |

Object ID 是主要连接键；namespace、object/memory type、event time、embedding family/dim 是检索兼容 metadata。

转换与 hydration：

| Direction | Function/path |
|---|---|
| Event -> both planes | `MaterializeEvent` produces Memory + `IngestRecord` |
| canonical -> projection rebuild | `Runtime.ReindexEmbeddings`, warm prebuild/index workers |
| projection -> canonical | candidate IDs -> ObjectStore/Edge/Version lookups |
| cold -> response | explicit `include_cold`, then merge/filter/evidence |

## 5. 调用关系

Event write 由 consistency projection callback 驱动 materializer、DataPlane 和 canonical store；query 先读 projection，再从 canonical stores hydrate 并构造 evidence。Admin replay/reindex 从 WAL 或 canonical state 重建 projection。

当前主写回调的顺序是 `DataPlane.Ingest` 先于 `ApplyCanonicalProjection`，两者不共享 transaction。State 的 keyed canonical materialization 主要由 subscriber worker 异步完成，因此不能把 Memory projection visible 推导为 State 已 materialized。

## 6. 数据与状态

Projection 中存 text/vector/sparse terms、attributes、segment/tier/index metadata，不应当成为完整 policy/provenance/version 真值。Canonical store 中保存对象语义，不保证每个对象都已进入 ANN index。

| Plane | Persistent state | In-memory state |
|---|---|---|
| canonical | Badger objects/edges/versions/policies/audit/algorithm state | memory backend equivalents |
| projection | segment/index metadata、可选 native/cold vector state | lexical postings、buffers、hot cache |
| connection metadata | object ID、embedding family/dim、generation/segment refs | query trace/cache entries |

## 7. 正确性

- Runtime projection success precedes canonical commit in main write callback；两者无跨引擎 transaction。
- inactive warm Memory 会从普通 query filter 中移除；explicit cold result 可保留 archived object。
- delete/purge 要分别清理 canonical、segment/index、cache、cold records。
- archive/export 必须在删除 warm 前确认 cold write；当前由操作路径而非统一 invariant manager 执行。

### Recovery model

Canonical plane 是恢复依据，WAL 是重新派生依据；projection 可通过 replay/reindex 重建。当前无持续 divergence scanner/checksum checkpoint，因此“可重建”不等于“自动发现并修复所有漂移”。

## 8. 声明边界

可声明 authoritative canonical state + disposable retrieval acceleration view。

不可声明 projection 与 canonical 始终同步、共享 ACID transaction，或任意 projection hit 都已有完整 canonical/evidence 数据。

## 9. 缺口

- 缺少 projection generation/checkpoint 与 per-object projection status；
- 缺少持续 stale/divergence scanner 和 checksum；
- 缺少统一 deletion/archive/reactivation tombstone propagation；
- 缺少 projection write 失败后的自动 canonical-driven repair；
- 需要 dual-plane fault-injection、rebuild equivalence 和 stale-read contract tests。
