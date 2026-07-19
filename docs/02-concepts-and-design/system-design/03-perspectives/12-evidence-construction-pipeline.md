# 12. Evidence Construction Pipeline

## 1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Pipeline Perspective |
| 问题 | Retrieval hit 如何变成可解释、可追溯 response |
| 成熟度 | 完整基础 pipeline；completeness/confidence 与强治理部分缺失 |

## 2. 代码入口

| Concern | Package / file | Constructor / method |
|---|---|---|
| query entry | `src/internal/access/gateway.go`, `src/internal/worker/runtime.go` | query handler, `Runtime.ExecuteQuery` |
| query plan | `src/internal/semantic/` | `NewDefaultQueryPlanner`, `QueryPlanner.Build` |
| retrieval | `src/internal/dataplane/` | `DataPlane.Search` |
| evidence skeleton | `src/internal/evidence/assembler.go` | `NewAssembler`/`NewCachedAssembler`, `Build` |
| graph/proof completion | `src/internal/worker/chain/chain.go` | `QueryChain.Execute` |
| graph worker | `src/internal/worker/indexing/subgraph.go` | `Expand` |
| proof worker | `src/internal/worker/coordination/` | proof trace constructor/dispatch |

## 3. 输入与输出

| Stage | Typed input | Output |
|---|---|---|
| planning | `schemas.QueryRequest` | `schemas.QueryPlan` |
| candidate search | `dataplane.SearchInput` | `dataplane.SearchOutput` |
| assembly | candidate IDs + query context | nodes, incident edges, latest versions, provenance, policy annotations |
| graph expansion | `GraphExpandRequest` + hydrated nodes/edges | `GraphExpandResponse` |
| proof | seeds + subgraph | `[]ProofStep` |
| packaging | retrieval/evidence/consistency metadata | `schemas.QueryResponse` |

## 4. 内部组成

### Stage to function map

| Stage | Function/component | Output |
|---|---|---|
| Candidate retrieval | `DataPlane.Search` | object IDs/tier/segment trace |
| Type/memory filter | Runtime + `Assembler.filterByObjectTypes` | filtered IDs |
| Object hydration | Runtime/QueryChain ObjectStore lookups | GraphNode/type/provenance data |
| Graph expansion | `GraphEdgeStore.BulkEdges`, Subgraph worker | typed Edge/subgraph |
| Version resolution | `Assembler.resolveVersions` | latest ObjectVersion |
| Provenance integration | `resolveProvenance`, embedding attachment, derivation worker | event/ref strings |
| Policy annotation | policy filters + `governanceAnnotations` | applied filters/proof steps |
| Proof construction | assembler skeleton/cache + ProofTraceWorker BFS | ProofStep list |
| Packaging | `QueryResponse` | evidence-bearing response |

### Evidence schema

| Record | Key fields |
|---|---|
| GraphNode | object ID/type/label/properties |
| Edge | source/type/relation/destination/weight/provenance/time |
| ObjectVersion | object/version/mutation event/valid interval/tag |
| ProofStep | step/depth/source/edge/target/weight/operation/description |
| EvidenceSubgraph | seeds/nodes/edges/proof/provenance |

### Cache behavior

Ingest `PreComputeService` 可将 EvidenceFragment 放入 bounded in-memory cache。Query hit 合并 fragment；miss 时仍读取 Edge/Version/Policy 构建 delta evidence。Cache 不持久，版本变化后没有统一 invalidation generation，正确性必须依赖 canonical lookup 而非 cache-only answer。

## 5. 调用关系

Gateway 解析 QueryRequest，Runtime 执行 consistency read gate 和 plan，NodeManager/DataPlane 获取 candidates；Runtime 做类型/作用域/状态过滤及 canonical supplement；Assembler hydrate canonical data；QueryChain 再扩展 subgraph 并生成 proof；visibility middleware 最后可能剥离 debug/chain 字段。

`/v1/query/batch` 是 warm vector batch direct path，不接受一组完整 `QueryRequest`，也不执行本 evidence pipeline。它不能作为 Query & Evidence Chain 的等价批量接口。

## 6. 数据与状态

- 输入 projection state：candidate IDs、score、tier/segment/index trace；
- canonical state：Memory/State/Artifact/Event、Edge、ObjectVersion、PolicyRecord；
- provenance state：source Event/ref、derivation log、embedding annotation；
- in-memory state：bounded evidence fragment cache 和 query-local hydrated graph；
- 输出状态：`QueryResponse` 中 objects、subgraph、proof、retrieval/consistency metadata；query 本身通常不修改 canonical state。

Graph/version/provenance rules：

- Assembler 读取 returned IDs 的 incident edges；QueryChain 默认 one-hop subgraph + proof worker 最多内部 cap 8 hop。
- Version resolution 当前选择 latest；Query time-window 不等于 historical version resolution。
- Provenance 来自 Edge ref、Version mutation Event、Memory/State/Artifact source fields 和 embedding annotations。
- supporting/contradicting/derived relation 能以 EdgeType 表达，但没有统一 evidence rank/confidence formula。

## 7. 正确性

Cache miss 仍回读 canonical stores；因此 cache 是优化而非权威。当前 version resolver 选择 latest，不提供 query-time historical snapshot；graph expansion 和 proof 有 hop/node/edge cap。Policy annotation 不保证每个 object/edge 已经过统一 deny/mask enforcement，必须与 Runtime 过滤路径一并审查。

Policy and proof boundary：

Policy annotation 说明对象状态；它不总是强制删除对象。Proof trace 是可解释执行/关系轨迹，不是形式验证证明。Production visibility middleware 还可能删除 debug/chain fields。

## 8. 声明边界

可声明结构化 nodes/edges/versions/provenance/proof package 和 cache-assisted assembly。

不可声明 evidence completeness 已计算、proof 可完整 replay、policy annotation 等价 enforcement、confidence/support score 有统一统计含义。

## 9. 缺口

- 缺少 version-aware cache generation/invalidation；
- 缺少 historical/valid-at version resolver；
- `schemas.GraphExpander` 的两参数接口没有 active implementation，实际 worker 使用三参数 `SubgraphExecutorWorker.Expand`；
- 缺少统一 evidence ranker、completeness/confidence 定义；
- 缺少 policy-safe graph traversal 和 edge-level visibility enforcement；
- 缺少 full pipeline replayability 与 stale-cache contract tests。
