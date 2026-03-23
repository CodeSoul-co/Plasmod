# Query API

## Purpose

This document describes the structured query endpoint used by the ANDB v1 prototype.

The query path accepts a `QueryRequest`, plans a retrieval execution, performs candidate search, and assembles a lightweight evidence-oriented response.

## Endpoint

- Method: `POST`
- Path: `/v1/query`
- Content-Type: `application/json`

## Request Schema

The current server decodes the request into `schemas.QueryRequest` from `src/internal/schemas/query.go`.

Current fields:

- `query_text`
- `query_scope`
- `session_id`
- `agent_id`
- `tenant_id` (optional)
- `workspace_id` (optional)
- `top_k`
- `time_window`
- `object_types` (optional)
- `memory_types` (optional)
- `edge_types` (optional)
- `relation_constraints`
- `response_mode`

### `object_types`

- **Omitted or `[]`:** treated as **all queryable kinds** for v1: `memory`, `state`, and `artifact`. An empty list does **not** mean “match nothing”.
- **Non-empty:** only IDs whose canonical kind is in the list are kept after retrieval (inferred from ID prefixes: `mem_`, `state_`, `art_`). Unknown ID shapes are kept for forward compatibility.
- Unknown type strings in the request are ignored; if none remain after validation, the server falls back to the same **all three** defaults.
- `edge_types` are applied to graph/subgraph expansion and response edge filtering.

Current `time_window` fields:

- `from`
- `to`

## Example Request

```json
{
  "query_text": "hello",
  "query_scope": "w_demo",
  "session_id": "sess_a",
  "agent_id": "agent_a",
  "tenant_id": "t_demo",
  "workspace_id": "w_demo",
  "top_k": 5,
  "time_window": {
    "from": "2026-03-16T00:00:00Z",
    "to": "2026-03-16T23:59:59Z"
  },
  "object_types": ["memory", "state", "artifact"],
  "memory_types": ["semantic"],
  "relation_constraints": [],
  "response_mode": "structured_evidence"
}
```

## Success Response

The current runtime returns a simplified `QueryResponse`. It already preserves the main evidence categories, but some fields are still lightweight.

### Example

```json
{
  "objects": ["mem_evt_demo_001"],
  "edges": [],
  "provenance": ["event_projection", "retrieval_projection"],
  "versions": [],
  "applied_filters": ["scope", "visibility", "time_window", "time_window_bound"],
  "proof_trace": [
    "planner",
    "retrieval_search",
    "policy_filter",
    "response",
    "plan_partition:seg_123:growing"
  ]
}
```

## Field Interpretation

### `objects`

Current behavior:

- returns selected object IDs

Target behavior:

- should evolve into richer evidence objects

### `edges`

Current behavior:

- currently empty in the bootstrap runtime

Target behavior:

- typed evidence edges after graph expansion

### `provenance`

Current behavior:

- lightweight provenance stage markers

Target behavior:

- object-level provenance entries

### `applied_filters`

Current behavior:

- filters returned by the policy engine and query planner

### `proof_trace`

Current behavior:

- execution notes for planning, retrieval, and response assembly

## Error Behavior

Current server behavior:

- malformed JSON returns HTTP `400`
- unsupported methods return HTTP `405`

The current prototype does not yet expose a standardized structured error envelope for all query failures.

## Current Runtime Flow

The current path is:

`HTTP gateway -> query planner -> retrieval module -> evidence assembler -> QueryResponse`

Implementation entry points:

- `src/internal/access/gateway.go`
- `src/internal/worker/runtime.go`

## v1 Notes

The current response is intentionally lighter than the full structured-evidence target documented elsewhere, but it already preserves the categories required to evolve toward that target without breaking the main contract.
