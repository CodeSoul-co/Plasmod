# 24. Evidence Assembly Engine

## 1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Engine |
| 原模块 | Evidence Assembler |
| 目标 | Candidate IDs + query context -> structured evidence response |
| 关键路径 | structured `/v1/query` |
| 成熟度 | 完整基础 assembly；ranking/completeness/historical version 部分 |

## 2. Code entry

| Item | Code |
|---|---|
| Package | `src/internal/evidence` |
| Main files | `assembler.go`, `cache.go`, `fragment.go` |
| Constructors | `NewAssembler`, `NewCachedAssembler` |
| Wiring | `WithEdgeStore`, `WithVersionStore`, `WithObjectStore`, `WithPolicyStore` |
| Main method | `Build(SearchInput, SearchOutput, filters)` |
| Post assembly | `worker/chain.QueryChain`, Runtime embedding provenance |

## 3. Engine fields

| Field | Type | Purpose |
|---|---|---|
| `cache` | `*evidence.Cache` | ingest precomputed fragment lookup |
| `edgeStore` | `GraphEdgeStore` | one-hop incident edges |
| `versionStore` | `SnapshotVersionStore` | latest versions |
| `objectStore` | `ObjectStore` | type/memory subtype and source hydration |
| `policyStore` | `PolicyStore` | quarantine/retracted annotations |

## 4. Input/output fields

| Input | Used for |
|---|---|
| SearchInput object/memory types | post-filter |
| SearchOutput IDs/tier/cold/segments | response objects, proof steps, diagnostics |
| filters | `AppliedFilters` |
| stores/cache | edges/versions/provenance/policy/cache proof |

Output `QueryResponse` fields are enumerated in [Object and Message Registry](../06-cross-reference/object-and-message-registry.md)。

## 5. Internal components

| Suggested component | Actual function/status |
|---|---|
| Object Hydrator | prefix inference + store lookup，部分 |
| Graph Builder | `expandEdges` + QueryChain，完整基础 |
| Version Resolver | latest-only `resolveVersions` |
| Provenance Resolver | `resolveProvenance` + Runtime attachment |
| Policy Annotator | `governanceAnnotations`，annotation only |
| Proof Constructor | planner/retrieval/policy/response steps + fragments |
| Evidence Ranker | no independent ranker |
| Evidence Cache | bounded in-memory cache |
| Response Packager | QueryResponse construction |

## 6. Calls, sync and state

Runtime calls Assembler after retrieval/filter；then QueryChain augments nodes/edges/proof。All synchronous。Cache is mutable in-memory；Assembler itself has fixed dependency fields and no per-request state。

## 7. Correctness/failure

- Missing optional store yields empty corresponding section rather than error。
- Unknown ID defaults to memory type heuristic。
- Policy violation is annotated, not necessarily filtered。
- Latest version selection ignores historical query time。
- Cache miss is tolerated；cache staleness has no generation invalidation。

## 8. 声明边界

可声明 assembly of objects IDs, edges, latest versions, provenance and proof trace from canonical records。

不可声明 complete hydration, formal proof, calibrated evidence score, complete ACL enforcement or historical version correctness。

## 9. 缺口

Explicit `EvidencePackage` schema, typed hydrator, version-at-time resolver, policy decision enforcement, cache invalidation, rank/support/confidence/completeness algorithms and strict missing-dependency error mode。
