# Retrieval Schema

## 1. Purpose

This document defines the retrieval module contracts for ANDB. The retrieval module is responsible for finding relevant cognitive objects (memories, events, artifacts) through three parallel retrieval paths: dense vector search, sparse keyword matching, and attribute filtering.

The implementation structs live in [`src/retrieval/service/types.py`](../../src/retrieval/service/types.py) and [`src/retrieval/proto/retrieval.proto`](../../src/retrieval/proto/retrieval.proto).

## 2. Design Principle

The retrieval module follows a **three-way recall + RRF fusion** architecture:

- **Dense Retrieval**: Semantic similarity via vector search (Milvus ANN)
- **Sparse Retrieval**: Exact keyword matching (BM25 / sparse vectors)
- **Filter Retrieval**: Attribute-based filtering with governance awareness

Results are merged using Reciprocal Rank Fusion (RRF) to produce a unified candidate list.

## 3. RetrievalRequest

### 3.1 Current Fields

| Field | Type | Description |
|-------|------|-------------|
| `query_text` | string | Query text for semantic search |
| `query_vector` | []float32 | Pre-computed embedding (optional) |
| `tenant_id` | string | **Required**. Top-level isolation boundary |
| `workspace_id` | string | **Required**. Workspace isolation boundary |
| `agent_id` | string | Filter by agent |
| `session_id` | string | Filter by session |
| `scope` | string | private / session / workspace / global |
| `memory_type` | string | episodic / semantic / procedural |
| `top_k` | int | Number of candidates to return |
| `min_confidence` | float | Minimum confidence threshold |
| `min_importance` | float | Minimum importance threshold |
| `time_range` | TimeRange | Temporal filter |
| `enable_dense` | bool | Enable dense retrieval path (default true) |
| `enable_sparse` | bool | Enable sparse retrieval path (default true) |
| `enable_filter_only` | bool | Filter only mode (see Section 11) |

### 3.2 TimeRange

| Field | Type | Description |
|-------|------|-------------|
| `from_unix_ts` | int64 | Start timestamp |
| `to_unix_ts` | int64 | End timestamp |

## 4. Candidate

A single retrieval result object.

| Field | Type | Description |
|-------|------|-------------|
| `object_id` | string | Unique identifier |
| `object_type` | string | memory / event / artifact |
| `score` | float | RRF merged score |
| `agent_id` | string | Owner agent |
| `session_id` | string | Source session |
| `scope` | string | Visibility scope |
| `version` | int | Object version |
| `provenance_ref` | string | Provenance reference for member C |
| `content` | string | Object content |
| `summary` | string | Object summary |
| `confidence` | float | Confidence score |
| `importance` | float | Importance score |
| `level` | int | Distillation depth: 0=raw, 1=summary, 2=abstraction |
| `memory_type` | string | episodic / semantic / procedural |
| `verified_state` | string | Verification status (verified / unverified / disputed) |
| `salience_weight` | float | Salience weight for final reranking |
| `source_channels` | []string | ["dense", "sparse", "filter"] |
| `is_seed` | bool | Graph expansion seed marker |
| `seed_score` | float | Seed score for member C |

## 5. CandidateList

The retrieval response.

| Field | Type | Description |
|-------|------|-------------|
| `candidates` | []Candidate | Ranked candidate list |
| `total_found` | int | Total candidates before truncation |
| `retrieved_at_unix_ts` | int64 | Retrieval timestamp |
| `query_meta` | QueryMeta | Query execution metadata |

### 5.1 QueryMeta

| Field | Type | Description |
|-------|------|-------------|
| `latency_ms` | int64 | Execution latency |
| `dense_hits` | int | Candidates from dense path |
| `sparse_hits` | int | Candidates from sparse path |
| `filter_hits` | int | Candidates from filter path |
| `channels_used` | []string | Active retrieval channels |

## 6. RRF Fusion Algorithm

```
RRF_score(d) = Σ 1 / (k + rank_i(d))
```

Where:
- `k = 60` (standard constant)
- `rank_i(d)` is the rank of document d in path i

Candidates appearing in multiple paths accumulate scores.

## 7. Governance-Aware Filtering

The filter retriever enforces governance policies:

- `tenant_id` and `workspace_id`: **Mandatory** isolation boundaries
- `quarantine_flag == false`: Exclude quarantined objects
- `is_active == true`: Only return active memories
- `visibility_policy`: ACL-based access control
- `verified_state`: Filter or downrank unverified/disputed objects
- `salience_weight`: Applied after RRF fusion for final reranking

### 7.1 Salience Reranking

After RRF fusion, final score is computed as:

```
final_score = rrf_score * salience_weight
```

Candidates with `salience_weight < 1.0` are demoted; those with `salience_weight > 1.0` are promoted.

## 8. Required Milvus Collection Fields

The following scalar fields must exist in Milvus collections for filter retrieval to work:

```
agent_id
session_id
scope
memory_type
level
confidence
importance
is_active
quarantine_flag
valid_from
valid_to
version
provenance_ref
```

## 9. Interface Contracts

### 9.1 With Member A (Materialization)

- Embedding vectors stored in `embeddings` table with `vector_id`, `vector_context`, `original_text`, `dim`, `model_id`, `vector_ref`
- Scalar fields indexed in Milvus for filter queries

### 9.2 With Member C (Graph Expansion)

- `Candidate.object_id` serves as graph expansion seed node
- `Candidate.provenance_ref` enables provenance tracing
- `Candidate.is_seed` and `Candidate.seed_score` mark high-confidence seeds

### 9.3 With Member D (API Layer)

- Member D calls `Retriever.retrieve(request)` interface
- Returns `CandidateList` for response assembly

## 10. Service Definition (gRPC)

```protobuf
service RetrievalService {
    rpc Retrieve(RetrievalRequest) returns (CandidateList);
    rpc BatchRetrieve(BatchRetrievalRequest) returns (BatchRetrievalResponse);
}
```

## 11. Filter-Only Mode Behavior

When `enable_filter_only = true`:

| Aspect | Behavior |
|--------|----------|
| Dense retrieval | **Skipped** |
| Sparse retrieval | **Skipped** |
| Filter retrieval | **Executed** |
| RRF fusion | **Not executed** (single channel) |
| `Candidate.score` | Set to `1.0 * salience_weight` |
| `Candidate.source_channels` | `["filter"]` |
| Ordering | By `importance` descending, then `confidence` descending |

Use case: Attribute-based lookup without semantic search (e.g., "get all memories from session X").
