# Query Schema

## 1. Purpose

This document defines the v1 query request and query response contracts for ANDB.

Its purpose is to give all collaborators a shared interface for:

- query input
- retrieval planning assumptions
- graph expansion seed format
- structured evidence return
- proof trace packaging

The current implementation structs live in [`src/internal/schemas/query.go`](../../src/internal/schemas/query.go). This document describes both the current field names and the intended semantic contract for v1.

## 2. Design Principle

ANDB query should not be treated as a plain search endpoint that returns only ranked chunks.

The contract should reflect the real goal:

**return structured evidence over canonical objects.**

That means the query interface must carry enough context for:

- retrieval planning
- filtering
- scope restriction
- relation expansion
- provenance return
- proof trace generation

## 3. Current Go Request Shape

Current `QueryRequest` fields:

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
- `relation_constraints`
- `response_mode`

Current `TimeWindow` fields:

- `from`
- `to`

These names are the implementation contract today.

## 4. Required Request Fields for v1

A practical v1 request should contain at least:

- `query_text`
- `agent_id`
- `session_id`
- `top_k`
- `response_mode`

Current example compatible with the running server:

```json
{
  "query_text": "What evidence suggests the current plan is blocked by a failed tool call?",
  "query_scope": "workspace",
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
  "memory_types": ["semantic", "episodic"],
  "relation_constraints": [],
  "response_mode": "structured_evidence"
}
```

## 5. Recommended Semantic Request Contract

The current implementation is intentionally narrow, but v1 semantics should be read as follows.

### 5.1 `query_text`

Type: `string`

Meaning:

- semantic retrieval anchor
- input to dense and sparse retrieval paths

### 5.2 `agent_id`

Type: `string`

Meaning:

- identifies the querying agent
- supports scope and visibility restriction

### 5.3 `session_id`

Type: `string`

Meaning:

- identifies the current execution context
- supports local-context and state-aware retrieval

### 5.4 `query_scope`

Type: `string`

Examples:

- `private`
- `session`
- `workspace`
- `shared`

Meaning:

- controls which namespace or visibility domain is searched

### 5.5 `tenant_id` (optional)

Type: `string`

Meaning:

- narrows retrieval to a tenant boundary when provided
- supports multi-tenant deployment patterns

### 5.6 `workspace_id` (optional)

Type: `string`

Meaning:

- narrows retrieval to a workspace boundary when provided
- improves routing and policy filtering precision

### 5.7 `top_k`

Type: `integer`

Meaning:

- initial retrieval candidate target
- applied before relation expansion

### 5.8 `time_window`

Type: `object`

Current fields:

- `from`
- `to`

Meaning:

- restricts the temporal range of query candidates

### 5.9 `object_types` (optional)

Type: `list[string]`

Meaning:

- restricts candidate search to selected canonical object families
- typical values include `memory`, `state`, `artifact`, `event`

### 5.10 `memory_types` (optional)

Type: `list[string]`

Meaning:

- applies a memory-level semantic filter when `memory` is included in object search
- typical values include `episodic`, `semantic`, `procedural`, `social`, `reflective`

### 5.11 `relation_constraints`

Type: `list[string]` in the current Go schema

Current implementation note:

- this is lighter than the richer semantic model originally proposed

Intended meaning:

- edge-type restrictions
- expansion hints
- graph-stage control parameters

If the project evolves this into a richer object later, that change should be reviewed carefully because it is a shared contract.

### 5.12 `response_mode`

Type: `string`

Recommended v1 values:

- `structured_evidence`
- `objects_only`

Current runtime note:

- demo scripts may still send `evidence`
- the server does not yet strongly validate mode values

The docs standardize on `structured_evidence` as the preferred target mode.

## 6. Future-Compatible Request Extensions

These are still semantically useful but are not yet present in the current Go request struct:

- richer `relation_constraints` object model
- `debug`

They should be treated as planned contract directions, not as implemented requirements today.

## 7. Query Response Design Principle

The default v1 response should be a structured evidence package rather than a bare ranking list.

It should ultimately contain:

- evidence objects
- relation edges
- provenance information
- version information
- applied filters
- proof trace

This is the contract that differentiates ANDB.

## 8. Current Go Response Shape

Current `QueryResponse` fields:

- `objects` as `[]string`
- `edges` as `[]Edge`
- `provenance` as `[]string`
- `versions` as `[]ObjectVersion`
- `applied_filters` as `[]string`
- `proof_trace` as `[]string`
- `chain_traces` as `{ "main", "memory_pipeline", "query", "collaboration" }` each `[]string` — ingest-time chains are usually empty on query; `query` is filled from `QueryChain` (proof/subgraph merge metadata)

Current implementation note:

- this is a bootstrap response shape
- object payloads are still object IDs rather than full evidence objects

## 9. Target Structured Response Schema

For the richer v1 contract, the response should include at least:

- `query_id`
- `status`
- `objects`
- `edges`
- `provenance`
- `versions`
- `applied_filters`
- `proof_trace`

Illustrative target example:

```json
{
  "query_id": "q_001",
  "status": "success",
  "objects": [
    {
      "object_id": "mem_001",
      "object_type": "memory",
      "summary": "Tool X failed due to token expiry.",
      "score": 0.91,
      "scope": "session",
      "version": 1,
      "source_refs": ["evt_008"]
    }
  ],
  "edges": [
    {
      "edge_id": "edge_013",
      "src_object_id": "mem_001",
      "dst_object_id": "evt_008",
      "edge_type": "derived_from",
      "weight": 1.0
    }
  ],
  "provenance": [
    {
      "object_id": "mem_001",
      "source_event_ids": ["evt_008"],
      "notes": "Derived from failed tool result."
    }
  ],
  "versions": [
    {
      "object_id": "mem_001",
      "object_type": "memory",
      "version": 1,
      "mutation_event_id": "evt_008"
    }
  ],
  "applied_filters": {
    "scope": "session",
    "time_window": {
      "from": "2026-03-16T00:00:00Z",
      "to": "2026-03-16T23:59:59Z"
    }
  },
  "proof_trace": {
    "retrieval_paths_used": ["dense", "filter"],
    "seed_object_ids": ["mem_001"],
    "expanded_edge_types": ["derived_from"],
    "assembly_steps": [
      "dense retrieval produced 8 candidates",
      "merged to 5 unique objects",
      "expanded 1 hop over derived_from edges"
    ]
  }
}
```

This richer response is the semantic target even though current code still uses simplified arrays.

## 10. Objects-Only Mode

ANDB may support a lighter debugging mode:

`response_mode = objects_only`

In that mode:

- `objects` remains required
- other fields may be empty or simplified
- this mode is for internal debugging or experiments, not the primary product contract

## 11. Error Response

Recommended error fields:

- `query_id`
- `status`
- `error_code`
- `message`

Example:

```json
{
  "query_id": "q_009",
  "status": "failed",
  "error_code": "INVALID_RELATION_CONSTRAINT",
  "message": "max_hops must be >= 1"
}
```

The current server still relies mostly on HTTP error responses rather than a fully standardized error envelope.

## 12. Minimal v1 Validation Rules

Request-side rules:

1. `query_text` must not be empty
2. `agent_id` must not be empty
3. `session_id` must not be empty
4. `top_k` must be positive
5. `response_mode` should be recognized
6. if provided, `object_types` and `memory_types` values should be recognized by the runtime
7. unsupported relation filters should be rejected or explicitly ignored

Response-side rules:

1. `objects` must always exist on successful responses
2. `edges`, `provenance`, `versions`, `applied_filters`, and `proof_trace` should remain present as categories even if empty
3. once `structured_evidence` is fully enforced, evidence mode should not collapse to a plain list without explanation

## 13. v1 Simplifications

The following are allowed in v1:

- rough scoring
- shallow provenance
- current-version-only metadata
- proof trace as execution notes
- 1-hop or 2-hop graph expansion only
- simplified request fields while the main flow is still being stabilized

These simplifications are acceptable only if the response still points toward the structured-evidence contract.

## 14. Summary

The ANDB query schema is not merely a search API contract. It is the semantic interface through which the database exposes reasoning-ready evidence.

For v1, the key requirement is:

**even if the implementation is lightweight, the contract should already reflect the structured-evidence philosophy.**
