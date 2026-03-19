# Retrieval Schema

## 1. Purpose

This document defines the retrieval module contracts for ANDB. The retrieval module is responsible for finding relevant cognitive objects (memories, events, artifacts) through three parallel retrieval paths: dense vector search, sparse keyword matching, and attribute filtering.

The implementation structs live in [`src/retrieval/service/types.py`](../../src/retrieval/service/types.py) and [`src/retrieval/proto/retrieval.proto`](../../src/retrieval/proto/retrieval.proto).

## 2. Design Principle

The retrieval module follows a **filter-first whitelist + three-way recall + RRF fusion** architecture:

1. **Filter Retrieval** runs first to produce a whitelist of valid object_ids
2. **Dense Retrieval** and **Sparse Retrieval** run in parallel within the whitelist
3. Results are merged using **RRF fusion**
4. **Safety filter** removes quarantined, expired, inactive objects
5. **Reranking** computes `final_score = rrf * importance * freshness * confidence`

## 3. RetrievalRequest

### 3.1 Current Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `query_id` | string | Yes | Unique query ID for tracing |
| `query_text` | string | Yes | Natural language query for dense+sparse |
| `query_vector` | []float32 | No | Pre-computed embedding from D (skip re-vectorization) |
| `tenant_id` | string | Yes | Top-level isolation boundary |
| `workspace_id` | string | Yes | Workspace isolation boundary |
| `agent_id` | string | Yes | Querying agent, determines visibility |
| `session_id` | string | Yes | Current session context |
| `scope` | string | Yes | private / session / workspace / global |
| `memory_types` | []string | No | Filter by memory types (list), empty = all |
| `object_types` | []string | No | Filter by object types (memory/event/artifact/state) |
| `top_k` | int | Yes | Number of candidates to return (default 10) |
| `min_confidence` | float | No | Minimum confidence threshold |
| `min_importance` | float | No | Minimum importance threshold |
| `time_range` | TimeRange | No | Temporal filter (valid_from/valid_to overlap) |
| `as_of_ts` | timestamp | No | Time-travel: only return objects where valid_from <= as_of_ts |
| `min_version` | int | No | Only return version >= min_version |
| `exclude_quarantined` | bool | No | Exclude quarantined objects (default true) |
| `exclude_unverified` | bool | No | Exclude unverified objects |
| `enable_dense` | bool | No | Enable dense retrieval path (default true) |
| `enable_sparse` | bool | No | Enable sparse retrieval path (default true) |
| `enable_filter` | bool | No | Enable attribute filter path (default true) |
| `enable_filter_only` | bool | No | Filter only mode (see Section 11) |
| `for_graph` | bool | No | Graph mode: return top_k*2, must include source_event_ids |

### 3.2 TimeRange

| Field | Type | Description |
|-------|------|-------------|
| `from_unix_ts` | int64 | Start timestamp |
| `to_unix_ts` | int64 | End timestamp |

## 4. Candidate

A single retrieval result object (CandidateObject in design doc).

| Field | Type | Description |
|-------|------|-------------|
| `object_id` | string | Unique identifier |
| `object_type` | string | memory / event / artifact |
| `score` | float | RRF merged score (before reranking) |
| `final_score` | float | After reranking: rrf * importance * freshness * confidence |
| `dense_score` | float | Per-channel RRF score from dense path (for member E) |
| `sparse_score` | float | Per-channel RRF score from sparse path (for member E) |
| `agent_id` | string | Owner agent |
| `session_id` | string | Source session |
| `scope` | string | Visibility scope |
| `version` | int | Object version |
| `provenance_ref` | string | Provenance reference for member C |
| `content` | string | Object content |
| `summary` | string | Object summary |
| `confidence` | float | Confidence score (may be overridden by policy_records) |
| `importance` | float | Importance score |
| `freshness_score` | float | Computed by member A, read from memories table |
| `level` | int | Distillation depth: 0=raw, 1=summary, 2=abstraction |
| `memory_type` | string | episodic / semantic / procedural |
| `verified_state` | string | verified / unverified / disputed |
| `salience_weight` | float | From policy_records, governance override multiplier |
| `valid_from` | timestamp | When this version became active |
| `valid_to` | timestamp | When this version was superseded |
| `visible_time` | timestamp | When object became visible |
| `quarantine_flag` | bool | Whether object is quarantined |
| `visibility_policy` | string | public / private / workspace |
| `is_active` | bool | Active flag |
| `ttl` | timestamp | TTL expiry time |
| `source_channels` | []string | ["dense", "sparse", "filter"] |
| `is_seed` | bool | Graph expansion seed marker |
| `seed_score` | float | Seed score for member C |
| `source_event_ids` | []string | Required when for_graph=true (for member C graph traversal) |

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

## 6. Execution Flow

```
Filter runs first -> produces whitelist of valid object_ids
    |
    v
Dense (in whitelist)  +  Sparse (in whitelist)   [parallel]
    |                       |
    v                       v
        Three-way RRF merge
            |
            v
        Safety filter (quarantine / ttl / visible_time / is_active)
            |
            v
        Rerank: final_score = rrf * importance * freshness * confidence
            |
            v
        Truncate to top_k (or top_k*2 if for_graph)
            |
            v
        Mark seeds (for member C)
```

## 7. RRF Fusion Algorithm

```
RRF_score(d) = Σ 1 / (k + rank_i(d))
```

Where:
- `k = 60` (standard constant)
- `rank_i(d)` is the rank of document d in path i (1-indexed)

Candidates appearing in multiple paths accumulate scores.

## 8. Reranking Formula

After RRF fusion, the final score is computed as:

```
final_score = rrf_score * importance * freshness_score * confidence
```

- `importance`, `freshness_score`, `confidence` come from the memories table (member A computes them)
- If `policy_records` has `confidence_override`, it replaces `confidence`
- `salience_weight` from `policy_records` is available for governance-level adjustments

## 9. Safety Filter (Post-Merge)

After RRF merge, these objects are removed:

| Condition | Source Field | Action |
|-----------|-------------|--------|
| Quarantined | `quarantine_flag = true` | Remove |
| TTL expired | `ttl < now` | Remove |
| Not yet visible | `visible_time > now` | Remove |
| Inactive | `is_active = false` | Remove |
| Time-travel | `valid_from > as_of_ts` | Remove (if as_of_ts set) |
| Version too old | `version < min_version` | Remove (if min_version set) |

## 10. Required Milvus Collection Fields

```
object_id
object_type
agent_id
session_id
scope
memory_type
confidence
importance
freshness_score
is_active
version
valid_from
valid_to
visible_time
provenance_ref
source_event_ids
salience_weight
```

## 11. Interface Contracts

### 11.1 With Member A (Materialization)

- Embedding vectors stored in `embeddings` table
- `freshness_score` computed by A using decay function, B reads it directly
- `source_event_ids` must be written by A at materialization time
- Same `model_id` must be used for query and document embeddings

### 11.2 With Member C (Graph Expansion)

- `for_graph=true` returns `top_k*2` candidates with `source_event_ids`
- C traverses `source_event_ids` to expand the evidence graph
- `is_seed` and `seed_score` mark high-confidence starting points

### 11.3 With Member D (Query Worker)

- D calls `Retriever.retrieve(request)` or `Retriever.batch_retrieve(requests)`
- Returns `CandidateList` for response assembly
- D may pre-compute `query_vector` and pass it to skip re-vectorization

### 11.4 With Member E (Benchmarks)

- `dense_score` and `sparse_score` provide per-channel scores for evaluation
- `benchmark_retrieve` returns full candidate list without truncation

## 12. Service Definition (gRPC)

```protobuf
service RetrievalService {
    rpc Retrieve(RetrievalRequest) returns (CandidateList);
    rpc BatchRetrieve(BatchRetrievalRequest) returns (BatchRetrievalResponse);
}
```

## 13. Filter-Only Mode Behavior

When `enable_filter_only = true`:

| Aspect | Behavior |
|--------|----------|
| Dense retrieval | **Skipped** |
| Sparse retrieval | **Skipped** |
| Filter retrieval | **Executed** |
| RRF fusion | **Not executed** (single channel) |
| `Candidate.score` | Set to `1.0 * salience_weight` |
| `Candidate.final_score` | Computed with reranking formula |
| `Candidate.source_channels` | `["filter"]` |
| Ordering | By `importance` descending, then `confidence` descending |
| Truncation | `top_k` (or `top_k*2` if `for_graph`) |

Use case: Attribute-based lookup without semantic search (e.g., "get all memories from session X").

## 14. Graph Mode Behavior

When `for_graph = true`:

| Aspect | Behavior |
|--------|----------|
| Candidate count | `top_k * 2` instead of `top_k` |
| `source_event_ids` | **Must** be populated for each candidate |
| Use case | Member C uses these as seed nodes for graph expansion |
