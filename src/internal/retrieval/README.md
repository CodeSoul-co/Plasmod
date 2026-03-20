# Retrieval Module (Member B)

## Overview

The Retrieval Module is responsible for multi-signal candidate retrieval over canonical cognitive objects stored in Milvus. It implements a three-path parallel retrieval architecture with RRF (Reciprocal Rank Fusion) merging, safety filtering, and reranking.

**Location**: `src/retrieval/`

**Schema Documentation**: `docs/schema/retrieval-schema.md`

---

## Current Features

### 1. Three-Path Parallel Retrieval

| Path | Implementation | Description |
|------|---------------|-------------|
| **Dense** | `service/dense.py` | Vector similarity search using Milvus ANN (IP metric) |
| **Sparse** | `service/sparse.py` | BM25-style sparse vector search with FNV-1a hashing |
| **Filter** | `service/filter.py` | Attribute-based scalar filtering with Milvus expressions |

### 2. RRF Fusion

**Implementation**: `service/merger.py`

```
RRF_score(d) = Σ 1/(k + rank_i(d))
```

- `k = 60` (configurable)
- Accumulates scores across all three channels
- Per-channel scores tracked: `dense_score`, `sparse_score`

### 3. Safety Filtering

**Implementation**: `service/merger.py` → `Merger._safety_filter()`

| Rule | Field Checked | Condition |
|------|--------------|-----------|
| 1 | `quarantine_flag` | `== True` → remove |
| 2 | `ttl` | `< now` → remove (expired) |
| 3 | `visible_time` | `> now` → remove (not yet visible) |
| 4 | `is_active` | `== False` → remove |
| 5 | `visible_time` | `> as_of_ts` → remove (time-travel) |
| 6 | `valid_from` | `> as_of_ts` → remove (time-travel) |
| 7 | `version` | `< min_version` → remove |

### 4. Reranking Formula

```python
final_score = score * max(importance, 0.01) * max(freshness_score, 0.01) * max(confidence, 0.01)
```

### 5. Seed Marking (for Graph Expansion)

- Candidates with `final_score >= 0.7` are marked as seeds
- Sets `is_seed = True` and `seed_score = final_score`
- Graph Expansion Layer uses these seeds for 1-hop traversal

### 6. Filter-Only Mode

When `enable_filter_only = True`:
- Skips Dense and Sparse retrieval
- Returns filter results with `score = 1.0 * salience_weight`
- Sorted by `(importance desc, confidence desc)`

### 7. Graph Mode

When `for_graph = True`:
- Returns `top_k * 2` candidates instead of `top_k`
- Ensures `source_event_ids` is populated for graph traversal

---

## Module Structure

```
src/retrieval/
├── README.md              # This file
├── main.py                # Entry point (--dev for debug mode)
├── requirements.txt       # Python dependencies
├── proto/
│   └── retrieval.proto    # gRPC service definition
└── service/
    ├── __init__.py
    ├── types.py           # RetrievalRequest, Candidate, CandidateList, QueryMeta
    ├── interfaces.py      # Abstract base classes for retrievers
    ├── retriever.py       # Unified Retriever orchestrator
    ├── dense.py           # MilvusDenseRetriever
    ├── sparse.py          # MilvusSparseRetriever
    ├── filter.py          # MilvusFilterRetriever
    ├── merger.py          # Merger (RRF + safety filter + rerank)
    └── version_filter.py  # Version constraint utilities
```

---

## Interface Contracts

### Input: RetrievalRequest (from Query Layer)

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `query_id` | `str` | Yes | Unique query ID for tracing |
| `query_text` | `str` | Yes | Natural language query |
| `query_vector` | `List[float]` | No | Pre-computed dense embedding |
| `tenant_id` | `str` | Yes | Tenant isolation boundary |
| `workspace_id` | `str` | Yes | Workspace isolation boundary |
| `agent_id` | `str` | No | Agent filter |
| `session_id` | `str` | No | Session filter |
| `scope` | `str` | No | Visibility scope (private/session/workspace/global) |
| `memory_types` | `List[str]` | No | Memory type filter (episodic/semantic/procedural) |
| `object_types` | `List[str]` | No | Object type filter (memory/event/artifact/state) |
| `top_k` | `int` | Yes | Number of candidates to return (default: 10) |
| `min_confidence` | `float` | No | Minimum confidence threshold |
| `min_importance` | `float` | No | Minimum importance threshold |
| `time_range` | `TimeRange` | No | Temporal bounds (from_ts, to_ts) |
| `as_of_ts` | `datetime` | No | Time-travel constraint |
| `min_version` | `int` | No | Minimum version constraint |
| `exclude_quarantined` | `bool` | No | Exclude quarantined objects (default: True) |
| `enable_dense` | `bool` | No | Enable dense retrieval (default: True) |
| `enable_sparse` | `bool` | No | Enable sparse retrieval (default: True) |
| `enable_filter` | `bool` | No | Enable filter retrieval (default: True) |
| `enable_filter_only` | `bool` | No | Filter-only mode (default: False) |
| `for_graph` | `bool` | No | Graph expansion mode (default: False) |

### Output: CandidateList (to Query Layer / Graph Expansion Layer)

| Field | Type | Description |
|-------|------|-------------|
| `candidates` | `List[Candidate]` | Ranked candidates, sorted by `final_score` desc |
| `total_found` | `int` | Total candidates before truncation |
| `retrieved_at` | `datetime` | Retrieval timestamp |
| `query_meta` | `QueryMeta` | Execution metadata |

### Candidate Fields

| Category | Fields |
|----------|--------|
| **Identity** | `object_id`, `object_type` |
| **Scores** | `score`, `final_score`, `dense_score`, `sparse_score` |
| **Metadata** | `agent_id`, `session_id`, `scope`, `version`, `provenance_ref`, `content`, `summary`, `level`, `memory_type`, `verified_state` |
| **Scoring Input** | `confidence`, `importance`, `freshness_score`, `salience_weight` |
| **Version/Time** | `valid_from`, `valid_to`, `visible_time` |
| **Governance** | `quarantine_flag`, `visibility_policy`, `is_active`, `ttl` |
| **Channel Tracking** | `source_channels` |
| **Graph Expansion** | `is_seed`, `seed_score`, `source_event_ids` |

### QueryMeta Fields

| Field | Type | Description |
|-------|------|-------------|
| `latency_ms` | `int` | Merge step latency |
| `dense_hits` | `int` | Dense retriever hit count |
| `sparse_hits` | `int` | Sparse retriever hit count |
| `filter_hits` | `int` | Filter retriever hit count |
| `channels_used` | `List[str]` | Active channels that returned results |

---

## Integration with Other Layers

### Upstream: Materialization Layer

| What Materialization Writes | Where | What We Read |
|----------------------------|-------|--------------|
| Dense embedding | Milvus `vector` field | Dense retriever searches |
| Sparse embedding | Milvus `sparse_vector` field | Sparse retriever searches |
| `freshness_score` | Milvus column | Reranking formula |
| `source_event_ids` | Milvus column | Graph expansion |
| `confidence`, `importance` | Milvus columns | Reranking + threshold filter |
| `is_active`, `quarantine_flag`, `ttl` | Milvus columns | Safety filter |
| `salience_weight` | Milvus column | Filter-only scoring |

**Constraint**: Materialization Layer and Query Layer must use the same `model_id` for embeddings.

### Downstream: Graph Expansion Layer

| What We Output | How Graph Expansion Uses It |
|----------------|----------------------------|
| `Candidate.object_id` | Starting node for graph traversal |
| `Candidate.source_event_ids` | Edge list for evidence subgraph expansion |
| `Candidate.is_seed` | Only expands candidates where `is_seed=True` |
| `Candidate.seed_score` | Prioritizes seed expansion order |
| `Candidate.provenance_ref` | Traces provenance chain |
| `for_graph=True` | Tells us to return `top_k*2` candidates |

### Downstream: Query Layer

| What Query Layer Provides | What We Return |
|--------------------------|----------------|
| `RetrievalRequest` | `CandidateList` with ranked candidates |
| `query_vector` (pre-computed) | Dense uses directly |
| `query_text` | Sparse converts to BM25 sparse vector |

### Downstream: Benchmark Layer

| What We Output | How Benchmark Uses It |
|----------------|----------------------|
| `dense_score`, `sparse_score` | Per-channel evaluation |
| `score`, `final_score` | Ranking quality evaluation |
| `source_channels` | Channel coverage analysis |
| `QueryMeta.*_hits` | Hit count metrics |
| `QueryMeta.latency_ms` | Performance benchmarking |

---

## Milvus Collection Requirements

All 24 fields must exist in the Milvus collection:

**Isolation**: `tenant_id`, `workspace_id`

**Identity**: `object_id`, `object_type`

**Metadata**: `agent_id`, `session_id`, `scope`, `version`, `provenance_ref`, `content`, `summary`, `level`, `memory_type`, `verified_state`, `salience_weight`

**Scoring**: `confidence`, `importance`, `freshness_score`

**Safety**: `is_active`, `quarantine_flag`, `ttl`, `valid_from`, `valid_to`, `visible_time`, `visibility_policy`

**Graph**: `source_event_ids`

**Vectors**: `vector` (FLOAT_VECTOR), `sparse_vector` (SPARSE_FLOAT_VECTOR)

---

## Changelog

### 2026-03-20 (Evening)

- **Added `benchmark_retrieve()` interface**: Returns ALL candidates without truncation for Benchmark Layer analysis. Supports baseline comparisons (dense-only, sparse-only, full-fusion).

- **Added `rrf_score` field to Candidate**: Preserves RRF score before reranking for benchmark analysis.

- **Added error codes module** (`service/errors.py`): Defines `RetrievalErrorCode` (200/400/404/500/503) and validation utilities aligned with week-3 design doc.

- **Enhanced `--dev` mode logging**: Detailed request/result logging including per-candidate score breakdown (rrf_score, dense_score, sparse_score, importance, freshness, confidence).

- **Added `benchmark_retrieve()` to RetrievalService**: Exposed in main.py for Benchmark Layer integration.

### 2026-03-20 (Morning)

- **Rebased to main** (`3850557`): Synced with latest main branch
- **Removed test files from git**: Local dev test files (`test_*.py`) excluded from tracking

### 2026-03-19

- **Fixed safety filter bug**: Dense and Sparse now read all 24 fields from Milvus including safety-filter fields (`quarantine_flag`, `is_active`, `ttl`, `visible_time`, `valid_from`, `freshness_score`, `source_event_ids`, `visibility_policy`). Previously these fields were only read by Filter, causing safety filter to silently pass quarantined/inactive/expired objects from Dense/Sparse channels.

- **Replaced member references with module names**: All "member A/B/C/D/E" references in documentation replaced with proper module names (Materialization Layer, Retrieval Layer, Graph Expansion Layer, Query Layer, Benchmark Layer).

- **Rewrote retrieval-schema.md**: Exhaustive field listing with:
  - Every field's type, default, required status
  - Written-by and read-by for each field
  - Field flow traced through Filter, Dense, Sparse, Merger, Retriever
  - Milvus filter expressions documented per component
  - Safety filter rules numbered with exact conditions
  - Interface contracts as explicit tables
  - No ambiguous language ("etc." removed)

---

## Running the Module

### Development Mode

```bash
cd src/retrieval
python main.py --dev
```

### Dependencies

```bash
pip install -r requirements.txt
```

Required packages:
- `pymilvus` - Milvus Python SDK
- `grpcio`, `grpcio-tools` - gRPC support
- `protobuf` - Protocol buffers

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `MILVUS_URI` | `http://localhost:19530` | Milvus server URI |
| `MILVUS_COLLECTION` | `cognitive_objects` | Collection name |

---

## Error Codes

Defined in `service/errors.py`, aligned with week-3 design doc:

| Code | Name | Description | Query Layer Action |
|------|------|-------------|--------------------|
| 200 | OK | Normal response | Process candidates |
| 400 | BAD_REQUEST | Missing required fields | Check request format |
| 404 | NOT_FOUND | No candidates found (valid) | Return empty to agent |
| 500 | INTERNAL_ERROR | Milvus connection failed, etc. | Log error, return degraded |
| 503 | SERVICE_UNAVAILABLE | Timeout | Log, may retry |

---

## TODO / Future Work

- [ ] Add `owner_type` filter support (private/public/partial)
- [ ] Implement `relation_constraints` from query-schema.md
- [ ] Add C++ high-performance retrieval implementation (see `cpp/` directory)
- [ ] Distributed retrieval with sharding
- [ ] Caching layer for hot queries
- [x] Add `benchmark_retrieve()` interface (completed 2026-03-20)
- [x] Add `rrf_score` field for benchmark analysis (completed 2026-03-20)
- [x] Add error codes definition (completed 2026-03-20)
- [x] Enhance `--dev` mode logging (completed 2026-03-20)
