# 23. Adaptive Retrieval Engine

## 1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Engine |
| 原模块 | Retrieval Dataplane |
| 目标 | QueryPlan -> tiered/hybrid candidates with physical index acceleration |
| 关键路径 | 是 |
| 成熟度 | 完整基础 tiered/hybrid retrieval；intent/cost adaptation 部分 |

## 2. Code entry

| Item | Code |
|---|---|
| Go API | `src/internal/dataplane/contracts.go` |
| Active adapter | `tiered_adapter.go: TieredDataPlane` |
| Warm plane | `segment_adapter.go: SegmentDataPlane` |
| Logical index | `segmentstore/` |
| Vector/sparse | `vectorstore.go`, `sparsestore.go` |
| Native bridge | `dataplane/retrievalplane/bridge.go`, `cpp/retrieval` |
| Planner | `semantic.DefaultQueryPlanner` |
| Constructor | `NewTieredDataPlaneWithEmbedderAndConfig` |

## 3. Engine fields

| `TieredDataPlane` field | Meaning |
|---|---|
| `hot *segmentstore.Index` | fast in-memory lexical tier |
| `warm *SegmentDataPlane` | lexical/vector/sparse warm execution |
| `warmIngest func` | injectable/testable warm write path |
| `embedder` | configured embedding generator |
| cold lexical/vector/HNSW function fields | TieredObjectStore adapters |
| `rrfK` | fusion constant |

Warm plane internally owns segment index/planner/searcher/vector/sparse stores, embedder and registered warm segment mappings。

## 4. Interface and method surface

| Interface/API | Methods |
|---|---|
| `DataPlane` | `Ingest`, `Search`, `Flush` |
| Tiered extension | `BatchIngest`, reset/rebuild, hot/warm accessors |
| Warm segment | vector/flat ingest with index type, register/unload, text/vector search |
| Batch | plugin/raw/serial batch search and object-ID mapping |
| Native | index create/insert/search/release through bridge |

HTTP/transport mapping见 [API to Engine Matrix](../06-cross-reference/api-to-engine-matrix.md)。

## 5. Input/output

| Input | Fields used | Output |
|---|---|---|
| `IngestRecord` | ID/text/namespace/time/attributes/embedding/skip flag/family/dim | index/segment mutation |
| `SearchInput` | text/vector/TopK/namespace/time/types/cold | `SearchOutput` candidates/tier/trace/diagnostics |
| Warm batch | segment, NQ, TopK, flat vectors | IDs/distances/object ID rows |

## 6. Retrieval strategy

Hot lexical first；若不足 TopK 再查 warm；Cold only explicit。Warm can merge lexical and vector/sparse candidates via RRF/normalization。Precomputed query/event vectors bypass embedder。Native bridge unavailable时可退化到 lexical/Go path，具体能力取决于 build。

“adaptive” 当前主要是 tier fallback、candidate fusion、optional cold/native and early hot satisfaction；没有 general intent estimator、cost model 或 learned router。

## 7. Correctness and failure

- embedding family + dimension are compatibility boundaries；same dim is insufficient。
- `Search` returns value not error；backend failure classification is weak。
- object/memory type filters partly post-retrieval。
- native candidate does not carry canonical policy/evidence semantics。
- hot/warm writes happen together in TieredDataPlane, cold archive separate。

## 8. 声明边界

可声明 hybrid/tiered retrieval, optional native ANN, precomputed embeddings, RRF-style fusion and direct batch segment API。

不可声明 learned adaptive routing、all backends always enabled、ANN result equals final agent-visible answer or query plan executes every advanced operator type。

## 9. 缺口

Typed search error/partial result, score/reason metadata per candidate, policy-aware prefilter, intent/cost router, segment health/load feedback, cross-backend ranking contract and projection generation validation。
