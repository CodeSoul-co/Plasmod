# 17. Evidence Construction Mechanism

## 1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Mechanism |
| 目标 | Candidate -> canonical object -> graph/version/provenance/policy -> proof package |
| 成熟度 | 完整基础，ranking/completeness 部分 |

## 2. Code entry and APIs

`Assembler.Build`, `QueryChain.Run`, ProofTraceWorker, SubgraphExecutorWorker, `GET /v1/traces/{id}`, `POST /v1/query`。

## 3. Input/output

Input：SearchInput/SearchOutput, policy filter descriptions, canonical stores。Output：QueryResponse objects/nodes/edges/versions/provenance/proof/cache/retrieval metadata。

## 4. Internal components

| Component | Function |
|---|---|
| fragment cache/precompute | amortize ingest-time evidence metadata |
| object hydrator logic | infer/load Memory/Event/Artifact/State data |
| edge expander | incident edges and one-hop subgraph |
| proof worker | BFS edge + derivation trace |
| version resolver | latest version |
| provenance resolver | source events/edge refs/mutation events |
| policy annotator | quarantine/retracted/filter proof steps |

## 5. Relations and version behavior

EdgeType can represent derived/support/conflict/share relationships；proof uses stored edges, not inferred missing edges。Version resolution is latest-only in current assembler；historical time-window does not select an exact historical snapshot。

## 6. Cache/state

Evidence cache is bounded in-memory and disposable；canonical evidence inputs live in Edge/Version/Policy/derivation stores。Cache hit/miss appears in response stats。

## 7. Correctness

- Duplicate edges are merged by EdgeID in QueryChain。
- Annotation is not always enforcement。
- Unknown ID type inference can default to memory。
- No formula for evidence completeness/confidence/support score。
- Proof trace can mix execution descriptions and graph derivation steps。

## 8. 声明边界

可声明 structured, traceable evidence assembly from canonical records。

不可声明 formal proof, complete provenance, calibrated confidence or version-time correctness for arbitrary historical queries。

## 9. 缺口

Need typed evidence node contract, policy-safe traversal, historical version resolver, cache generation/invalidation, rank/confidence/completeness definition and replay verifier。
