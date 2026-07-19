# 6. Query and Evidence Chain

## 1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Chain |
| 目标 | 将 Query 转成候选对象并返回 objects、relations、versions、provenance、policy 和 proof |
| 关键路径 | 是 |
| 成熟度 | 完整基础 structured evidence；高级 operator、ACL 和 hydration 仍部分 |

## 2. Entry and interfaces

| Layer | Entry/interface |
|---|---|
| HTTP | `POST /v1/query`, `Gateway.ServiceQueryContext` |
| Runtime | `ExecuteQueryContext`, `executeQuery` |
| Planner | `QueryPlanner.Build` |
| Retrieval | `DataPlane.Search` through `NodeManager.DispatchQuery` |
| Assembly | `Assembler.Build` |
| Reasoning | `QueryChain.Run` |
| Response | `schemas.QueryResponse` + visibility middleware |

## 3. Stages

| # | Stage | Concrete behavior |
|---|---|---|
| 1 | read consistency | strict/bounded waits；eventual skips |
| 2 | plan | defaults TopK, namespace, time/types/tier and carries descriptor filters |
| 3 | candidate retrieval | Hot/Warm, optional Cold；lexical/vector/sparse/native depending config |
| 4 | candidate correction | CJK fallback, type/selector/target filters, inactive filtering |
| 5 | canonical supplement | State/Artifact/other requested types from ObjectStore |
| 6 | policy filters | `PolicyEngine.ApplyQueryFilters` generates filters unless minimal mode |
| 7 | evidence assembly | edges, versions, provenance, policy annotations, cache fragments, proof skeleton |
| 8 | QueryChain | prefetch graph nodes/edges, BFS proof trace, one-hop subgraph, edge merge |
| 9 | provenance/metrics | embedding provenance, evidence support, contamination observation |
| 10 | output filtering | APP_MODE visibility strips internal/debug fields in production |

## 4. Inputs and outputs

`QueryRequest` 与 `QueryResponse` 的完整字段见 [Object and Message Registry](../06-cross-reference/object-and-message-registry.md)。

| Output section | Source |
|---|---|
| `objects` | retrieval IDs + canonical supplements |
| `nodes` | QueryChain hydrated Memory/Event/Artifact graph nodes |
| `edges` | assembler BulkEdges + subgraph merge |
| `versions` | latest version lookup |
| `provenance` | Memory source refs, State/Artifact event refs, Edge refs, versions, embedding annotations |
| `proof_trace` | assembler stages/cache fragments + BFS/derivation steps |
| `applied_filters` | PolicyEngine descriptor output |
| `retrieval` | tier/cold diagnostics and hit/add counts |

## 5. Retrieval details

- Default search is hot/warm；Cold 只在 `include_cold=true` 时参与。
- Go 层负责 candidate merge/filter/evidence；C++ native engine 只负责物理 ANN。
- RRF/score normalization 在 dataplane/retrieval implementation 内；默认 RRF 常数由代码定义。
- `objects_only` fast path 跳过 evidence/QueryChain。
- `/v1/query/batch` 是 direct warm ANN，不属于本 evidence chain。

## 6. Data and state

Query 通常只读 canonical/retrieval stores；会更新 metrics、hot cache observation 或 evidence cache stats。它不承诺把 proof response 持久化。

Inactive memories 被过滤；cold-origin archived IDs 在 explicit cold query 中可被保留。Quarantine/retracted 当前在 Assembler 中主要生成 annotation，完整 deny/mask enforcement 不是集中式完成。

## 7. Correctness and failure

- `query_status` 区分真实 retrieval hit 与 canonical supplement。
- Candidate ID hydration 依赖 deterministic prefix + ObjectStore；未知 ID 默认推断 memory，存在误分类风险。
- Graph expansion 是基于已有 Edge，不证明 graph completeness。
- `DataPlane.Search` 无 error 返回，backend failure 可表现为空结果。
- Evidence cache miss 退化为 query-time evidence，不应改变 canonical answer contract。

## 8. 声明边界

可声明：hybrid/tiered candidate retrieval、canonical supplement、Edge/Version/Provenance/Proof structured response。

不可声明：所有 policy 都在 EvidenceAssembler 强制执行、所有对象都完整 hydrate 到 payload、graph/proof 是形式化证明或 evidence completeness 已量化。

## 9. 缺口

1. typed retrieval error；
2. full object hydration contract 或明确只返回 ID；
3. policy deny/mask 与 annotation 分离；
4. advanced response mode executor 接线；
5. configurable graph depth/edge filters from QueryRequest；
6. evidence completeness/confidence formula 和 tests。
