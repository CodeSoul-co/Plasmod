# Retrieval Schema

Implementation: `src/retrieval/service/types.py`, `src/retrieval/proto/retrieval.proto`

---

The implementation structs live in [`src/internal/retrieval/service/types.py`](../../src/internal/retrieval/service/types.py) and [`src/internal/retrieval/proto/retrieval.proto`](../../src/internal/retrieval/proto/retrieval.proto).

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
[Query Layer]
        |
        | RetrievalRequest
        v
+---[ Retriever.retrieve(request) ]---+
|                                      |
|  Step 1: FilterRetriever.filter()    |   <-- runs FIRST, produces whitelist
|        |                             |
|        v                             |
|  Step 2: Dense + Sparse in parallel  |   <-- constrained to whitelist
|        |             |               |
|        v             v               |
|  Step 3: Merger.merge()              |
|    3a. Deduplicate by object_id      |
|    3b. Accumulate RRF scores         |
|    3c. Safety filter                 |
|    3d. Confidence/importance filter  |
|    3e. Rerank                        |
|    3f. Truncate to top_k             |
|    3g. Mark seeds                    |
|        |                             |
+--------+-----------------------------+
         |
         | CandidateList
         v
[Query Layer: Response Assembly]
```

---

## 2. RetrievalRequest (Input)

Source: Query Layer constructs this and passes to `Retriever.retrieve(request)`.

### 2.1 All Fields

| Field | Type | Default | Required | Used In | Description |
|-------|------|---------|----------|---------|-------------|
| `query_id` | `str` | `""` | Yes | Tracing | Unique query ID, passed through to response for end-to-end tracing |
| `query_text` | `str` | `""` | Yes | Dense, Sparse | Natural language query. Dense uses it for embedding lookup. Sparse converts it to BM25 sparse vector via `_text_to_sparse_vector()` |
| `query_vector` | `Optional[List[float]]` | `None` | No | Dense | Pre-computed dense embedding from Query Layer. If provided, Dense skips re-vectorization and searches directly with this vector. If `None`, Dense returns empty |
| `tenant_id` | `str` | `""` | Yes | Dense, Sparse, Filter | Top-level isolation boundary. All three paths build filter expression `tenant_id == "{value}"`. Mandatory for data isolation |
| `workspace_id` | `str` | `""` | Yes | Dense, Sparse, Filter | Workspace isolation boundary. All three paths build filter expression `workspace_id == "{value}"`. Mandatory for data isolation |
| `agent_id` | `Optional[str]` | `None` | No | Dense, Sparse, Filter | If set, all three paths append `agent_id == "{value}"` to filter expression |
| `session_id` | `Optional[str]` | `None` | No | Dense, Sparse, Filter | If set, all three paths append `session_id == "{value}"` to filter expression |
| `scope` | `Optional[str]` | `None` | No | Dense, Sparse, Filter | Allowed values: `private`, `session`, `workspace`, `global`. If set, all three paths append `scope == "{value}"` to filter expression |
| `memory_types` | `Optional[List[str]]` | `None` | No | Dense, Sparse, Filter | List of memory types to include. Allowed values per item: `episodic`, `semantic`, `procedural`. If set, all three paths append `memory_type in ["episodic", "semantic"]` (Milvus IN expression). If `None`, no memory_type filter is applied |
| `object_types` | `Optional[List[str]]` | `None` | No | Filter | List of object types to include. Allowed values per item: `memory`, `event`, `artifact`, `state`. If set, Filter appends `object_type in ["memory", "event"]`. Only used in Filter path, not in Dense/Sparse |
| `top_k` | `int` | `10` | Yes | Merger, Retriever | Number of final candidates to return. Merger truncates to this value after reranking. If `for_graph=True`, effective top_k becomes `top_k * 2`. Each sub-retriever also uses `top_k` as its internal search limit |
| `min_confidence` | `float` | `0.0` | No | Filter, Merger | Used in two places: (1) Filter builds `confidence >= {value}` in Milvus expression; (2) Merger post-merge filters out candidates where `candidate.confidence < min_confidence` |
| `min_importance` | `float` | `0.0` | No | Filter, Merger | Used in two places: (1) Filter builds `importance >= {value}` in Milvus expression; (2) Merger post-merge filters out candidates where `candidate.importance < min_importance` |
| `time_range` | `Optional[TimeRange]` | `None` | No | Filter | Temporal bounds. If `time_range.from_ts` is set, Filter appends `valid_from <= {unix_ts}`. If `time_range.to_ts` is set, Filter appends `valid_to >= {unix_ts}`. Not used in Dense/Sparse |
| `as_of_ts` | `Optional[datetime]` | `None` | No | Filter, Merger | Time-travel constraint. Filter appends `valid_from <= {unix_ts}`. Merger safety filter removes candidates where `visible_time > as_of_ts` or `valid_from > as_of_ts` |
| `min_version` | `Optional[int]` | `None` | No | Filter, Merger | Version constraint. Filter appends `version >= {value}`. Merger safety filter removes candidates where `version < min_version` |
| `exclude_quarantined` | `bool` | `True` | No | Merger | If `True`, Merger safety filter removes candidates where `quarantine_flag == True` |
| `exclude_unverified` | `bool` | `False` | No | Merger | If `True`, Merger safety filter removes candidates where `verified_state == "unverified"` |
| `enable_dense` | `bool` | `True` | No | Retriever | If `False`, Retriever skips Dense search entirely, `dense_results = []` |
| `enable_sparse` | `bool` | `True` | No | Retriever | If `False`, Retriever skips Sparse search entirely, `sparse_results = []` |
| `enable_filter` | `bool` | `True` | No | Retriever | If `False`, Retriever skips Filter retrieval entirely, `filter_results = []` |
| `enable_filter_only` | `bool` | `False` | No | Retriever | If `True`, Retriever skips Dense and Sparse entirely. Returns filter results ordered by `(importance desc, confidence desc)` with `score = 1.0 * salience_weight`. RRF fusion is not executed |
| `for_graph` | `bool` | `False` | No | Merger, Retriever | Graph expansion mode for Graph Expansion Layer. Merger uses `top_k * 2` instead of `top_k`. Candidates must include `source_event_ids` |

### 2.2 TimeRange

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `from_ts` | `Optional[datetime]` | `None` | Start of time window. Filter uses this as `valid_from <= {unix_ts}` |
| `to_ts` | `Optional[datetime]` | `None` | End of time window. Filter uses this as `valid_to >= {unix_ts}` |

---

## 3. Candidate (Output per object)

A single retrieval result. Each sub-retriever (Dense, Sparse, Filter) produces `List[Candidate]`. Merger deduplicates by `object_id` and computes scores.

### 3.1 All Fields

#### 3.1.1 Identity Fields

| Field | Type | Default | Written By | Read By | Description |
|-------|------|---------|------------|---------|-------------|
| `object_id` | `str` | (required) | Dense, Sparse, Filter read from Milvus `object_id` field | Merger uses as dedup key; Graph Expansion Layer uses as graph seed node | Unique identifier of the cognitive object |
| `object_type` | `str` | `"memory"` | Dense, Sparse, Filter read from Milvus `object_type` field | Query Layer for response formatting | Allowed values: `memory`, `event`, `artifact`. Describes what kind of cognitive object this is |

#### 3.1.2 Score Fields

| Field | Type | Default | Written By | Read By | Description |
|-------|------|---------|------------|---------|-------------|
| `score` | `float` | `0.0` | Dense writes `hit.distance` (cosine/IP similarity). Sparse writes `hit.distance`. Filter writes `0.0`. Merger **overwrites** with accumulated RRF score: `score = sum(1/(k+rank_i))` across all channels. In filter-only mode, Retriever sets `score = 1.0 * salience_weight` | Merger uses for reranking formula input; Benchmark Layer for evaluation | RRF merged score before reranking. This is the raw fusion score |
| `final_score` | `float` | `0.0` | Merger computes: `final_score = score * max(importance, 0.01) * max(freshness_score, 0.01) * max(confidence, 0.01)`. In filter-only mode, Retriever computes same formula | Query Layer for final ranking; Merger for seed marking; Benchmark Layer for evaluation | Final reranked score. Candidates are sorted by this value descending. This is the score that determines output order |
| `dense_score` | `float` | `0.0` | Merger sets to `1/(k+rank)` for the candidate's rank in the Dense result list. `0.0` if candidate was not found by Dense | Benchmark Layer for per-channel evaluation | The RRF contribution from Dense path only |
| `sparse_score` | `float` | `0.0` | Merger sets to `1/(k+rank)` for the candidate's rank in the Sparse result list. `0.0` if candidate was not found by Sparse | Benchmark Layer for per-channel evaluation | The RRF contribution from Sparse path only |

#### 3.1.3 Metadata Fields (from Milvus)

These fields are read from Milvus by each sub-retriever and passed through unchanged to the output.

| Field | Type | Default | Source (Milvus column) | Used In | Description |
|-------|------|---------|----------------------|---------|-------------|
| `agent_id` | `str` | `""` | `agent_id` | Passed through to output | The agent that created this object |
| `session_id` | `str` | `""` | `session_id` | Passed through to output | The session where this object was created |
| `scope` | `str` | `""` | `scope` | Passed through to output | Visibility scope: `private`, `session`, `workspace`, `global` |
| `version` | `int` | `0` | `version` | Merger safety filter checks `version >= min_version`; passed through to output | Object version number. Higher version supersedes lower |
| `provenance_ref` | `str` | `""` | `provenance_ref` | Graph Expansion Layer reads for provenance tracing | Reference to the provenance chain for this object |
| `content` | `str` | `""` | `content` | Query Layer reads for response assembly | Full content text of the object |
| `summary` | `str` | `""` | `summary` | Query Layer reads for response assembly | Summarized content of the object |
| `level` | `int` | `0` | `level` | Passed through to output | Distillation depth. `0` = raw event, `1` = summary, `2` = abstraction |
| `memory_type` | `str` | `""` | `memory_type` | Passed through to output | Type of memory: `episodic`, `semantic`, `procedural` |
| `verified_state` | `str` | `""` | `verified_state` | Merger checks if `exclude_unverified`; passed through to output | Verification status: `verified`, `unverified`, `disputed` |

#### 3.1.4 Scoring Input Fields (from Milvus, used in reranking)

These fields are read from Milvus and used by Merger in the reranking formula.

| Field | Type | Default | Source (Milvus column) | Used In | Description |
|-------|------|---------|----------------------|---------|-------------|
| `confidence` | `float` | `0.0` | `confidence` | Merger reranking formula: `final_score = score * importance * freshness_score * confidence`. Merger post-merge filter: removes if `< min_confidence`. Filter pre-filter: Milvus expression `confidence >= {min_confidence}` | Confidence score of this object. Range 0.0-1.0. May be overridden by `policy_records.confidence_override` (Materialization Layer writes this) |
| `importance` | `float` | `0.0` | `importance` | Merger reranking formula: multiplied into `final_score`. Merger post-merge filter: removes if `< min_importance`. Filter pre-filter: Milvus expression `importance >= {min_importance}`. Filter-only mode: primary sort key (descending) | Importance score of this object. Range 0.0-1.0. Materialization Layer computes this |
| `freshness_score` | `float` | `1.0` | `freshness_score` | Merger reranking formula: multiplied into `final_score` | Freshness score computed by Materialization Layer using a time-decay function. Range 0.0-1.0. `1.0` means maximally fresh, decays toward `0.0` over time. All three retrievers read this from Milvus |
| `salience_weight` | `float` | `1.0` | `salience_weight` | Filter-only mode: `score = 1.0 * salience_weight`. Not used in normal RRF reranking formula but available for governance override | Governance weight from `policy_records` table. `1.0` = neutral, `>1.0` = promoted, `<1.0` = demoted. Materialization Layer writes this from policy_records |

#### 3.1.5 Version and Time Fields (from Milvus, used in safety filter)

| Field | Type | Default | Source (Milvus column) | Used In | Description |
|-------|------|---------|----------------------|---------|-------------|
| `valid_from` | `Optional[datetime]` | `None` | `valid_from` | Merger safety filter: removes if `valid_from > as_of_ts` (when `as_of_ts` is set). Filter pre-filter: Milvus expression `valid_from <= {as_of_ts}` and `valid_from <= {time_range.from_ts}` | Timestamp when this version of the object became active |
| `valid_to` | `Optional[datetime]` | `None` | `valid_to` | Filter pre-filter: Milvus expression `valid_to >= {time_range.to_ts}` | Timestamp when this version was superseded by a newer version. `None` means this is the current version |
| `visible_time` | `Optional[datetime]` | `None` | `visible_time` | Merger safety filter: removes if `visible_time > now` (not yet visible). Merger safety filter: removes if `visible_time > as_of_ts` (time-travel) | Timestamp when this object became visible to queries. Allows delayed-publish semantics |

#### 3.1.6 Governance Fields (from Milvus, used in safety filter)

| Field | Type | Default | Source (Milvus column) | Used In | Description |
|-------|------|---------|----------------------|---------|-------------|
| `quarantine_flag` | `bool` | `False` | `quarantine_flag` | Merger safety filter: removes if `True` (when `exclude_quarantined=True`, which is the default) | Whether this object has been quarantined by governance policy |
| `visibility_policy` | `str` | `""` | `visibility_policy` | Passed through to output, available for Query Layer ACL enforcement | Visibility policy: `public`, `private`, `workspace` |
| `is_active` | `bool` | `True` | `is_active` | Merger safety filter: removes if `False`. All three retrievers read this from Milvus | Active flag. Inactive objects are soft-deleted and excluded from results |
| `ttl` | `Optional[datetime]` | `None` | `ttl` | Merger safety filter: removes if `ttl < now` (expired) | TTL expiry timestamp. After this time the object is considered expired |

#### 3.1.7 Channel Tracking Fields (set by Merger)

| Field | Type | Default | Written By | Read By | Description |
|-------|------|---------|------------|---------|-------------|
| `source_channels` | `List[str]` | `[]` | Merger sets this during dedup. Each channel that found this object appends its name: `"dense"`, `"sparse"`, `"filter"`. In filter-only mode, Retriever sets `["filter"]` | Benchmark Layer for channel analysis; Query Layer for debugging | List of retrieval channels that contributed this candidate |

#### 3.1.8 Graph Expansion Fields (for Graph Expansion Layer)

| Field | Type | Default | Written By | Read By | Description |
|-------|------|---------|------------|---------|-------------|
| `is_seed` | `bool` | `False` | Merger: sets `True` if `final_score >= seed_threshold` (default threshold `0.7`) | Graph Expansion Layer: uses seed candidates as starting nodes for graph traversal | Whether this candidate is a seed for graph expansion |
| `seed_score` | `float` | `0.0` | Merger: sets to `final_score` when `is_seed=True` | Graph Expansion Layer: uses to prioritize seed traversal order | The score used for seed ranking, equal to `final_score` when marked as seed |
| `source_event_ids` | `List[str]` | `[]` | All three retrievers read from Milvus `source_event_ids` field | Graph Expansion Layer: traverses these event IDs to expand the evidence graph. Required when `for_graph=True` | List of event IDs that contributed to the creation of this object. Written by Materialization Layer at materialization time |

---

## 4. CandidateList (Output)

Returned by `Retriever.retrieve(request)` to Query Layer.

### 4.1 All Fields

| Field | Type | Default | Written By | Description |
|-------|------|---------|------------|-------------|
| `candidates` | `List[Candidate]` | `[]` | Merger (normal mode) or Retriever (filter-only mode) | Ranked list of candidates, sorted by `final_score` descending, truncated to `top_k` (or `top_k*2` if `for_graph`) |
| `total_found` | `int` | `0` | Merger: set to count of unique object_ids after dedup but before safety filter and truncation. Retriever (filter-only): set to `len(filter_results)` | Total number of candidates found before truncation. Useful for pagination |
| `retrieved_at` | `Optional[datetime]` | `None` | Merger or Retriever: set to `datetime.now()` at response time | Timestamp when retrieval was executed |
| `query_meta` | `Optional[QueryMeta]` | `None` | Merger or Retriever: constructed with hit counts and timing | Query execution metadata for debugging and monitoring |

### 4.2 QueryMeta

| Field | Type | Default | Written By | Description |
|-------|------|---------|------------|-------------|
| `latency_ms` | `int` | `0` | Merger: `int((end_time - start_time).total_seconds() * 1000)`. Retriever (filter-only): set to `0` | Execution latency of the merge step in milliseconds |
| `dense_hits` | `int` | `0` | Merger: `len(dense_results)` input count. Retriever (filter-only): `0` | Number of candidates returned by Dense retriever before merge |
| `sparse_hits` | `int` | `0` | Merger: `len(sparse_results)` input count. Retriever (filter-only): `0` | Number of candidates returned by Sparse retriever before merge |
| `filter_hits` | `int` | `0` | Merger: `len(filter_results)` input count. Retriever (filter-only): `len(filter_results)` | Number of candidates returned by Filter retriever before merge |
| `channels_used` | `List[str]` | `[]` | Merger: appends channel name if its result list is non-empty. Retriever (filter-only): `["filter"]` | Which retrieval channels were actually active and returned results |

---

## 5. Field Flow by Component

### 5.1 Filter Retriever (`filter.py`)

**Input**: `RetrievalRequest`

**Fields used to build Milvus filter expression**:

| Request Field | Milvus Expression Generated | Condition |
|---------------|---------------------------|-----------|
| `tenant_id` | `tenant_id == "{value}"` | Always (if non-empty) |
| `workspace_id` | `workspace_id == "{value}"` | Always (if non-empty) |
| `agent_id` | `agent_id == "{value}"` | If not `None` |
| `session_id` | `session_id == "{value}"` | If not `None` |
| `scope` | `scope == "{value}"` | If not `None` |
| `memory_types` | `memory_type in ["episodic", "semantic"]` | If not `None` |
| `object_types` | `object_type in ["memory", "event"]` | If not `None` |
| `min_confidence` | `confidence >= {value}` | If `> 0` |
| `min_importance` | `importance >= {value}` | If `> 0` |
| `time_range.from_ts` | `valid_from <= {unix_ts}` | If set |
| `time_range.to_ts` | `valid_to >= {unix_ts}` | If set |
| `as_of_ts` | `valid_from <= {unix_ts}` | If set |
| `min_version` | `version >= {value}` | If not `None` |
| `top_k` | `limit={top_k}` | Search limit |

**Output fields read from Milvus**: `object_id`, `object_type`, `agent_id`, `session_id`, `scope`, `version`, `provenance_ref`, `content`, `summary`, `confidence`, `importance`, `level`, `memory_type`, `verified_state`, `salience_weight`, `freshness_score`, `source_event_ids`, `is_active`

**Output**: `List[Candidate]` with all above fields populated, `score=0.0`

### 5.2 Dense Retriever (`dense.py`)

**Input**: `RetrievalRequest` (requires `query_vector` to be non-None)

**Fields used to build Milvus filter expression**:

| Request Field | Milvus Expression Generated | Condition |
|---------------|---------------------------|-----------|
| `tenant_id` | `tenant_id == "{value}"` | Always (if non-empty) |
| `workspace_id` | `workspace_id == "{value}"` | Always (if non-empty) |
| `agent_id` | `agent_id == "{value}"` | If not `None` |
| `session_id` | `session_id == "{value}"` | If not `None` |
| `scope` | `scope == "{value}"` | If not `None` |
| `memory_types` | `memory_type in ["episodic", "semantic"]` | If not `None` |

**Search parameters**: `metric_type=IP`, `nprobe=10`, `anns_field=vector`

**Output fields read from Milvus**: `object_id`, `object_type`, `agent_id`, `session_id`, `scope`, `version`, `provenance_ref`, `content`, `summary`, `confidence`, `importance`, `level`, `memory_type`, `verified_state`, `salience_weight`, `freshness_score`, `is_active`, `source_event_ids`, `valid_from`, `valid_to`, `visible_time`, `quarantine_flag`, `visibility_policy`, `ttl`

**Output**: `List[Candidate]` with `score=hit.distance` (cosine/IP similarity score from Milvus), all safety-filter fields populated from Milvus

### 5.3 Sparse Retriever (`sparse.py`)

**Input**: `RetrievalRequest` (requires `query_text` to be non-empty)

**Tokenization**: `query_text` -> lowercase -> whitespace split -> FNV-1a hash to sparse index -> TF weight

**Fields used to build Milvus filter expression**: Same as Dense (tenant_id, workspace_id, agent_id, session_id, scope, memory_types)

**Search parameters**: `metric_type=IP`, `anns_field=sparse_vector`

**Output fields read from Milvus**: Same as Dense (all 24 fields including safety-filter fields)

**Output**: `List[Candidate]` with `score=hit.distance`, all safety-filter fields populated from Milvus

### 5.4 Merger (`merger.py`)

**Input**: `dense_results: List[Candidate]`, `sparse_results: List[Candidate]`, `filter_results: List[Candidate]`, `request: RetrievalRequest`

**Step-by-step field mutations**:

| Step | Action | Fields Written |
|------|--------|---------------|
| 3a. Dedup | Merge by `object_id`. If duplicate, keep first seen, accumulate score | `object_id` as key |
| 3b. RRF | For each channel, compute `1/(k+rank)` and add to `score` | `score` (overwritten with RRF sum), `dense_score`, `sparse_score`, `source_channels` |
| 3c. Safety filter | Remove candidates failing governance checks | Reads: `quarantine_flag`, `ttl`, `visible_time`, `is_active`, `valid_from`, `version`. Reads from request: `as_of_ts`, `min_version` |
| 3d. Threshold filter | Remove low confidence/importance | Reads: `confidence`, `importance`. Reads from request: `min_confidence`, `min_importance` |
| 3e. Rerank | `final_score = score * max(importance, 0.01) * max(freshness_score, 0.01) * max(confidence, 0.01)` | `final_score` |
| 3f. Truncate | Sort by `final_score` desc, take first `effective_k` | Sort order changed |
| 3g. Seed marking | If `final_score >= seed_threshold`: `is_seed=True`, `seed_score=final_score` | `is_seed`, `seed_score` |

**Output**: `CandidateList` with `candidates`, `total_found`, `retrieved_at`, `query_meta`

### 5.5 Retriever (`retriever.py`) - Filter-Only Mode

When `enable_filter_only=True`, Dense and Sparse are skipped entirely.

**Field mutations on each filter result candidate**:

| Field | Value Set |
|-------|-----------|
| `score` | `1.0 * salience_weight` |
| `final_score` | `score * max(importance, 0.01) * max(freshness_score, 0.01) * max(confidence, 0.01)` |
| `source_channels` | `["filter"]` |

**Sort order**: `(importance desc, confidence desc)`

**Truncation**: `top_k` (or `top_k * 2` if `for_graph=True`)

---

## 6. RRF Fusion Algorithm

```
RRF_score(d) = Σ 1 / (k + rank_i(d))
```

- `k = 60` (standard constant, configurable in `Merger.__init__`)
- `rank_i(d)` is the 1-indexed rank of document `d` in channel `i`
- Channels: `dense`, `sparse`, `filter`
- A candidate appearing in all 3 channels at rank 1 gets: `1/61 + 1/61 + 1/61 = 0.04918`
- A candidate appearing only in dense at rank 1 gets: `1/61 = 0.01639`

Per-channel scores are stored separately:
- `dense_score = 1/(k + rank_in_dense)` or `0.0` if not in dense
- `sparse_score = 1/(k + rank_in_sparse)` or `0.0` if not in sparse
- `score = dense_score + sparse_score + filter_rrf_score` (accumulated)

---

## 7. Reranking Formula

```
final_score = score * max(importance, 0.01) * max(freshness_score, 0.01) * max(confidence, 0.01)
```

- `score` is the RRF merged score from Step 3b
- `importance` comes from Milvus, written by Materialization Layer
- `freshness_score` comes from Milvus, computed by Materialization Layer using time-decay
- `confidence` comes from Milvus, may be overridden by `policy_records.confidence_override`
- `max(x, 0.01)` prevents zero-multiplication (a field at `0.0` would zero out the entire score)

---

## 8. Safety Filter Rules

Applied in Merger after RRF merge, before reranking.

| # | Condition | Field Checked | Comparison | When Applied |
|---|-----------|---------------|------------|-------------|
| 1 | Quarantined | `candidate.quarantine_flag` | `== True` | Always (when `exclude_quarantined=True`) |
| 2 | TTL expired | `candidate.ttl` | `< datetime.now()` | Only if `candidate.ttl` is not `None` |
| 3 | Not yet visible | `candidate.visible_time` | `> datetime.now()` | Only if `candidate.visible_time` is not `None` |
| 4 | Inactive | `candidate.is_active` | `== False` | Always |
| 5 | Time-travel (visible) | `candidate.visible_time` | `> request.as_of_ts` | Only if `request.as_of_ts` is set AND `candidate.visible_time` is not `None` |
| 6 | Time-travel (version) | `candidate.valid_from` | `> request.as_of_ts` | Only if `request.as_of_ts` is set AND `candidate.valid_from` is not `None` |
| 7 | Version too old | `candidate.version` | `< request.min_version` | Only if `request.min_version` is not `None` |

Any candidate matching any of the above conditions is **removed** from the result set.

---

## 9. Required Milvus Collection Fields

All fields that our retrieval module reads from Milvus, listed exhaustively.

### 9.1 Fields read by ALL three retrievers (Dense, Sparse, Filter)

| Milvus Column | Data Type | Used For |
|---------------|-----------|----------|
| `object_id` | VARCHAR | Candidate identity, dedup key |
| `object_type` | VARCHAR | Candidate identity |
| `agent_id` | VARCHAR | Filter expression + output |
| `session_id` | VARCHAR | Filter expression + output |
| `scope` | VARCHAR | Filter expression + output |
| `version` | INT32 | Filter expression + safety filter + output |
| `provenance_ref` | VARCHAR | Output for Graph Expansion Layer |
| `content` | VARCHAR | Output |
| `summary` | VARCHAR | Output |
| `confidence` | FLOAT | Filter expression + reranking + threshold filter |
| `importance` | FLOAT | Filter expression + reranking + threshold filter |
| `level` | INT32 | Output |
| `memory_type` | VARCHAR | Filter expression + output |
| `verified_state` | VARCHAR | Output + governance check |
| `salience_weight` | FLOAT | Output + filter-only mode scoring |

### 9.2 Safety-filter and governance fields (read by ALL three retrievers)

| Milvus Column | Data Type | Used For |
|---------------|-----------|----------|
| `freshness_score` | FLOAT | Reranking formula |
| `is_active` | BOOL | Safety filter |
| `source_event_ids` | ARRAY(VARCHAR) | Graph expansion for Graph Expansion Layer |
| `valid_from` | INT64 | Time-range filter + time-travel filter |
| `valid_to` | INT64 | Time-range filter |
| `visible_time` | INT64 | Safety filter (not-yet-visible check) |
| `quarantine_flag` | BOOL | Safety filter |
| `ttl` | INT64 | Safety filter (TTL expiry check) |
| `visibility_policy` | VARCHAR | Passed through for Query Layer ACL |

### 9.4 Vector Fields (not scalar, used for search)

| Milvus Column | Data Type | Used By |
|---------------|-----------|---------|
| `vector` | FLOAT_VECTOR | Dense retriever ANN search |
| `sparse_vector` | SPARSE_FLOAT_VECTOR | Sparse retriever BM25 search |

### 9.5 Isolation Fields (required for tenant isolation)

| Milvus Column | Data Type | Used By |
|---------------|-----------|---------|
| `tenant_id` | VARCHAR | All three retrievers, mandatory filter |
| `workspace_id` | VARCHAR | All three retrievers, mandatory filter |

---

## 10. Interface Contracts

### 10.1 With Materialization Layer

| What Materialization Layer Writes | Where | What Retrieval Layer Reads |
|---------------------|-------|-------------------|
| Dense embedding | Milvus `vector` field | Dense retriever searches against this |
| Sparse embedding | Milvus `sparse_vector` field | Sparse retriever searches against this |
| `freshness_score` | Milvus `freshness_score` column | Merger reranking formula multiplier |
| `source_event_ids` | Milvus `source_event_ids` column | Filter retriever reads, passed to Graph Expansion Layer |
| `confidence` | Milvus `confidence` column | Reranking formula + threshold filter |
| `importance` | Milvus `importance` column | Reranking formula + threshold filter |
| `is_active` | Milvus `is_active` column | Safety filter |
| `salience_weight` | Milvus `salience_weight` column (from policy_records) | Filter-only scoring |
| `valid_from`, `valid_to`, `visible_time` | Milvus columns | Time-travel and safety filter |
| `quarantine_flag`, `ttl`, `visibility_policy` | Milvus columns | Safety filter |

**Constraint**: Materialization Layer and Query Layer must use the same `model_id` for embedding. Query embedding dimension must match document embedding dimension.

### 10.2 With Graph Expansion Layer

| What Retrieval Layer Outputs | How Graph Expansion Layer Uses It |
|--------------------|--------------| 
| `Candidate.object_id` | Starting node for graph traversal |
| `Candidate.source_event_ids` | Edge list: traverses these event IDs to expand the evidence subgraph |
| `Candidate.is_seed` | Only expands candidates where `is_seed=True` |
| `Candidate.seed_score` | Prioritizes seed expansion order by this score |
| `Candidate.provenance_ref` | Traces provenance chain from this reference |
| `for_graph=True` in request | Tells Retrieval Layer to return `top_k*2` candidates and ensure `source_event_ids` is populated |

### 10.3 With Query Layer

| What Query Layer Provides | What Retrieval Layer Returns |
|----------------|--------------------|
| `RetrievalRequest` with all fields | `CandidateList` with ranked candidates |
| `query_vector` (optional, pre-computed embedding) | If provided, Dense uses directly; if not, Dense returns empty (Query Layer must provide) |
| `query_text` | Sparse converts to BM25 sparse vector internally |
| `Retriever.retrieve(request)` call | Single request execution |
| `Retriever.batch_retrieve(requests)` call | Parallel execution of multiple requests, returns `List[CandidateList]` |

### 10.4 With Benchmark Layer

| What Retrieval Layer Outputs | How Benchmark Layer Uses It |
|--------------------|---------------|
| `Candidate.dense_score` | Per-channel evaluation of Dense recall quality |
| `Candidate.sparse_score` | Per-channel evaluation of Sparse recall quality |
| `Candidate.score` | Overall RRF score evaluation |
| `Candidate.final_score` | End-to-end ranking quality evaluation |
| `Candidate.source_channels` | Channel coverage analysis |
| `QueryMeta.dense_hits` | Dense retriever hit count |
| `QueryMeta.sparse_hits` | Sparse retriever hit count |
| `QueryMeta.filter_hits` | Filter retriever hit count |
| `QueryMeta.latency_ms` | Performance benchmarking |

---

## 11. Filter-Only Mode

When `RetrievalRequest.enable_filter_only = True`:

| Step | What Happens |
|------|-------------|
| 1 | Retriever calls `FilterRetriever.filter(request)` |
| 2 | Dense retriever is **not called** |
| 3 | Sparse retriever is **not called** |
| 4 | Merger is **not called** (no RRF fusion) |
| 5 | Each candidate gets `score = 1.0 * salience_weight` |
| 6 | Each candidate gets `final_score = score * max(importance, 0.01) * max(freshness_score, 0.01) * max(confidence, 0.01)` |
| 7 | Each candidate gets `source_channels = ["filter"]` |
| 8 | Candidates sorted by `(importance desc, confidence desc)` |
| 9 | Truncated to `top_k` (or `top_k * 2` if `for_graph=True`) |
| 10 | Returned as `CandidateList` with `query_meta.channels_used = ["filter"]` |

---

## 12. Graph Mode

When `RetrievalRequest.for_graph = True`:

| Step | What Happens |
|------|-------------|
| 1 | Normal retrieval flow executes (Filter -> Dense+Sparse -> Merge) |
| 2 | Merger uses `effective_k = top_k * 2` instead of `top_k` |
| 3 | Each candidate must have `source_event_ids` populated (Filter retriever reads this from Milvus) |
| 4 | Merger marks seeds: candidates with `final_score >= seed_threshold` get `is_seed=True` and `seed_score=final_score` |
| 5 | Graph Expansion Layer receives the `CandidateList` and expands the graph using `source_event_ids` from seed candidates |

---

## 13. Service Definition (gRPC)

```protobuf
service RetrievalService {
    rpc Retrieve(RetrievalRequest) returns (CandidateList);
    rpc BatchRetrieve(BatchRetrievalRequest) returns (BatchRetrievalResponse);
}

message BatchRetrievalRequest {
    repeated RetrievalRequest requests = 1;
}

message BatchRetrievalResponse {
    repeated CandidateList results = 1;
}
```
