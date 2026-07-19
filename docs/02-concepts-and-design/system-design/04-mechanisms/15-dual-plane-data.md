# 15. Dual-plane Data Mechanism

## 1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Mechanism |
| 目标 | canonical truth 与 disposable retrieval acceleration 分离 |
| 关键路径 | ingest/query/recovery |
| 成熟度 | 完整基础机制，持续 reconciliation 部分 |

## 2. Code entry

`materialization.MaterializationResult` 同时输出 canonical records 与 `dataplane.IngestRecord`；Runtime 把它们分别交给 DataPlane 和 RuntimeStorage；Query 先取 candidate IDs 再回连 stores/evidence。

## 3. Input/output

| Transform | Input | Output |
|---|---|---|
| canonicalization | Event | Memory/Artifact/Edge/Version |
| projection | Event/Memory text+embedding+metadata | IngestRecord/segments/index |
| hydration | candidate IDs | object-derived nodes/edges/versions/provenance |
| rebuild | canonical Memory scan | fresh retrieval plane |

## 4. Internal components

Canonical：WAL, RuntimeStorage, Badger/memory stores。Projection：Hot index, SegmentDataPlane, Vector/Sparse stores, native bridge, cold index, evidence cache。

## 5. Call relation

Ingest writes projection before canonical commit；query reads projection then canonical；admin reindex resets/re-ingests；delete/purge/archive coordinate multiple components manually。

## 6. State and synchronization

Connection key 是 object ID，compatibility key 是 embedding family + dimension。`flushDirty`/periodic flush 维护 warm native index；checkpoint tracks LSN visibility but not a full per-object projection generation。

## 7. Correctness

Canonical store is authority；projection may be dropped/rebuilt。However, DataPlane success before canonical failure can produce orphan candidate；canonical-only mutation can produce missing candidate。Current repair is replay/reindex/manual purge, not automatic scanner。

## 8. 声明边界

可声明 dual-plane architecture and rebuildable projection。

不可声明 synchronous equality at every instant, cross-plane ACID, automatic stale detection or zero-loss delete propagation。

## 9. 缺口

Add projection generation/object status, canonical commit token, tombstone propagation, checksum/divergence scan, repair plan and post-repair query verification。
