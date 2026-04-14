# Query API

## Purpose

`POST /v1/query` is the main structured retrieval endpoint for the ANDB v1 prototype.
It accepts a `schemas.QueryRequest`, runs tiered retrieval, applies policy and
 time-window filtering, and returns a `schemas.QueryResponse`.

## Endpoint

- Method: `POST`
- Path: `/v1/query`
- Content-Type: `application/json`

## Request

Important request fields:

- `query_text`
- `query_scope`
- `session_id`
- `agent_id`
- `workspace_id` (optional)
- `top_k`
- `time_window`
- `object_types` (optional)
- `memory_types` (optional)
- `edge_types` (optional)
- `relation_constraints`
- `response_mode`
- `include_cold` (optional)

### `include_cold`

- Omitted or `false`: search hot and warm tiers only.
- `true`: extend retrieval to the archived cold tier as well.

When `include_cold=true`, the cold-tier mode priority is:

1. `hnsw`
2. `vector`
3. `lexical`

If a preferred mode cannot produce candidates, the runtime may fall back to the
next mode. That fallback is surfaced in `response.retrieval`.

### `object_types`

- Omitted or `[]`: treat as all queryable canonical kinds in v1: `memory`, `state`, and `artifact`.
- Non-empty: keep only IDs whose canonical kind matches the requested set.

## Example Request

```json
{
  "query_text": "parasite growth trend",
  "query_scope": "w_demo",
  "session_id": "sess_a",
  "agent_id": "agent_a",
  "workspace_id": "w_demo",
  "top_k": 10,
  "time_window": {
    "from": "2026-03-16T00:00:00Z",
    "to": "2026-03-16T23:59:59Z"
  },
  "object_types": ["memory", "artifact"],
  "memory_types": ["semantic"],
  "relation_constraints": [],
  "response_mode": "structured_evidence",
  "include_cold": true
}
```

## Response

The current response body is `schemas.QueryResponse`.

Important top-level fields:

- `objects`
- `nodes`
- `edges`
- `provenance`
- `versions`
- `applied_filters`
- `proof_trace`
- `evidence_cache`
- `retrieval`
- `query_status`
- `query_hint`

### `retrieval`

`retrieval` is the benchmark-facing summary added for experiment runs.

Fields:

- `tier`: final participating tier label such as `hot`, `hot+warm`, or `hot+warm+cold`
- `cold_search_mode`: `hnsw`, `vector`, `lexical`, or empty when cold was not used
- `cold_candidate_count`: number of cold-tier candidates produced before final fusion
- `cold_tier_requested`: whether the request set `include_cold=true`
- `cold_used_fallback`: whether cold retrieval fell back from a preferred mode
- `retrieval_hits`: number of retrieval-seed objects before canonical supplementation
- `canonical_adds`: number of canonical objects added after retrieval

### `evidence_cache`

`evidence_cache` summarizes fragment-cache lookups for returned objects.
When cold objects participate, `cold_hits` and `cold_misses` only count the
cold-sourced subset.

### `proof_trace`

`proof_trace` remains the detailed execution trace. When `include_cold=true`,
it may contain cold-specific steps such as:

- `cold_hnsw_search`
- `cold_embedding_fetch`
- `cold_rerank`
- `cold_lexical_search`

## Example Response

```json
{
  "objects": ["mem_evt_demo_001"],
  "edges": [],
  "provenance": ["event_projection", "retrieval_projection"],
  "versions": [],
  "applied_filters": ["scope", "visibility", "time_window", "time_window_bound"],
  "proof_trace": ["planner", "retrieval_search", "cold_hnsw_search", "response"],
  "evidence_cache": {
    "looked_up": 1,
    "hits": 1,
    "misses": 0,
    "cold_hits": 1,
    "cold_misses": 0
  },
  "retrieval": {
    "tier": "hot+warm+cold",
    "cold_search_mode": "hnsw",
    "cold_candidate_count": 10,
    "cold_tier_requested": true,
    "cold_used_fallback": false,
    "retrieval_hits": 10,
    "canonical_adds": 0
  }
}
```

## Errors

- Malformed JSON returns HTTP `400`.
- Unsupported methods return HTTP `405`.

The prototype does not yet expose a fully standardized error envelope for all
query failures.
