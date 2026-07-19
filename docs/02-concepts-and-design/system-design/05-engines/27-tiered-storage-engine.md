# 27. Tiered Storage Engine

## 1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Engine |
| 原模块 | Tiered Storage |
| 目标 | 在 Hot/Warm/Cold/Archive 之间读取、提升、归档、清理和重建对象/embedding |
| 关键路径 | ingest promotion/query fallback/archive/delete |
| 成熟度 | 完整基础三层实现；autonomous migration/resource optimization 部分 |

## 2. Code entry

| Item | Code |
|---|---|
| Object tiering | `storage/tiered.go` |
| Hot policy | `hot_cache_policy.go`, memory tier config |
| Cold implementations | `InMemoryColdStore`, `S3ColdStore` |
| Retrieval tiering | `dataplane.TieredDataPlane` |
| Constructor | `NewTieredObjectStoreWithThreshold`, `NewTieredDataPlaneWithEmbedderAndConfig` |
| API | S3 export/snapshot/cold purge, delete/purge, query include_cold |

## 3. Engine fields

| Type/field | Meaning |
|---|---|
| `HotEntry` | object ID/type/payload, salience, access count, last access, insert time |
| `HotObjectCache.mu/entries/order/maxSize/policy` | bounded concurrent cache and eviction policy |
| `TieredObjectStore.hot` | Hot cache |
| `.warm` | canonical ObjectStore |
| `.warmEdge` | warm graph edge store |
| `.cold` | S3 or in-memory cold store |
| `.embedder` | archive/reindex embedding generation |
| `.hotThreshold` | promotion threshold |
| `TieredDataPlane` fields | hot index, warm plane, embedder, cold search functions, RRF K |

## 4. Methods/input/output

| Operation | Input | Output/side effect |
|---|---|---|
| Hot `Put/Get/Contains/Evict/Clear/Len` | object metadata | volatile cache state |
| promote/get | Memory/ID + salience | Hot/Warm read/write |
| `ArchiveMemory` | Memory ID | cold Memory/embedding/edges and warm/hot transition per method |
| cold search | text/vector/TopK | object IDs + diagnostics |
| soft delete cleanup | Memory ID | hot eviction |
| hard delete | Memory ID | hot/warm/cold/edge cleanup |
| export/purge | selectors | S3/cold mutations/admin summary |

## 5. Placement logic

Hot default capacity 2000；cache hotness combines salience, recency and access count, while extended `HotCachePolicy` config exposes weighted recency/frequency/semantic/tier class controls。Warm is canonical/default serving tier。Cold is written on explicit archive/reflection, not every ingest。Cold is queried only when requested。

Lifecycle and tier are not identical：archived lifecycle can guide cold placement, but Hot/Warm/Cold is physical state and may temporarily diverge。

## 6. Calls and sync/async

Runtime promotes synchronously after canonical commit；flush loop asynchronously persists retrieval index；reflection may archive asynchronously；admin export/purge runs request/task-specific logic；query cold path synchronous when requested。

## 7. Correctness/failure

- Cold backend selected only when required S3 env is valid, otherwise in-memory simulation。
- Archive spans object/edge/embedding stores and lacks distributed transaction。
- Soft delete leaves cold aligned until hard purge；query filtering must avoid serving stale hot payload。
- S3/network errors are operation-level and retry is caller/admin responsibility。
- Hot cache is disposable and not authoritative。

## 8. 声明边界

可声明 real Hot/Warm/Cold routing, bounded hot cache, S3/MinIO cold backend, explicit archive/retrieval/delete operations。

不可声明 fully autonomous tier optimizer, zero-copy migration, transactional movement or automatic reactivation/prefetch based on learned cost。

## 9. 缺口

Persistent tier metadata/state machine, migration job/status/retry, warm-delete-after-cold-verify invariant, resource pressure feedback, rehydration API, per-object location diagnostics and cross-tier reconciliation tests。
