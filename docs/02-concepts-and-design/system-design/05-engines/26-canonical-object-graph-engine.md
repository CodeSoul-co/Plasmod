# 26. Canonical Object Graph Engine

## 1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Engine |
| 原模块 | Canonical Object Store / RuntimeStorage |
| 目标 | 持久化并查询 canonical objects、relations、versions、policies、contracts、audit 和 algorithm state |
| 关键路径 | 是 |
| 成熟度 | 完整单进程/memory/Badger core；graph query/constraints 部分 |

## 2. Code entry

| Item | Code |
|---|---|
| Interfaces | `src/internal/storage/contracts.go` |
| Memory implementation | `storage/memory.go` |
| Persistent implementation | `badger_stores.go`, `composite.go` |
| Factory | `BuildRuntimeFromEnv` |
| Canonical transaction | `ApplyCanonicalProjection` |
| Graph adapters | `schemas/graph_*`, `worker/indexing/graph.go` |
| Coordinators | Object/Memory/Version/Policy coordinators |

## 3. Engine fields and stores

The Engine is an aggregate boundary, not one `CanonicalObjectGraphEngine` struct。

| RuntimeStorage accessor | Record family |
|---|---|
| `Segments`, `Indexes` | retrieval metadata |
| `Objects` | Agent/Session/Event/Memory/State/Artifact/User |
| `Edges` | graph relations + src/dst indexes |
| `Versions` | object histories/latest |
| `Policies` | append-only PolicyRecord |
| `Contracts` | ShareContract |
| `Audits` | append-only AuditRecord |
| `AlgorithmStates` | per memory/algorithm state |
| `HotCache` | volatile object cache |

`CanonicalProjection` fields：Memory, State, Artifact, Versions, Edges, flags for base edges。

## 4. Interfaces and API surface

| Interface | Main methods |
|---|---|
| ObjectStore | put/get/list per object type, delete Memory |
| GraphEdgeStore | put/get/delete/from/to/bulk/list/prune |
| SnapshotVersionStore | put/history/latest |
| PolicyStore | append/get/list |
| ShareContractStore | put/get/by-scope/list |
| AuditStore | append/get/list/delete target |
| AlgorithmStateStore | put/get/list |
| RuntimeStorage | accessors, canonical projection, base-edge helpers |

External direct API includes canonical collection routes；Event/Query paths use Engine through Runtime/evidence。See [API to Engine Matrix](../06-cross-reference/api-to-engine-matrix.md)。

## 5. Input/output and data model

Input：canonical structs and mutations。Output：hydrated structs, edge neighborhoods, versions, policies/contracts/audits and lists。Full object fields are in [Object and Message Registry](../06-cross-reference/object-and-message-registry.md)。

## 6. Internal composition and indexes

Badger uses typed key prefixes for object/edge/version/policy/contract and source/destination edge secondary indexes。Memory implementation uses maps + locks。ObjectModelRegistry describes type PK/versionable/indexable metadata but does not enforce all stores automatically。

## 7. Correctness/failure

- Same Badger backend can atomically apply canonical projection。
- Store interfaces mostly omit `error` on writes, limiting backend failure propagation。
- Direct CRUD can bypass Event/WAL/version/projection/audit。
- Edge endpoints have no mandatory foreign-key or schema constraint validation。
- Latest version depends on store ordering；rollback API is partial。

## 8. 声明边界

可声明 canonical multi-object graph with version/policy/share/audit state and memory/Badger implementations。

不可声明 graph database query language、referential integrity、distributed transaction、arbitrary historical snapshot or every mutation Event-sourced。

## 9. 缺口

Error-returning mutation interface, transaction abstraction for all canonical writes, referential/type constraints, graph traversal service, version-at-time snapshots, Event-only public mutation policy and storage contract tests across implementations。
