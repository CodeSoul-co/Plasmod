# Main Flow

## 1. Purpose

This document defines the main runtime flow of the ANDB v1 prototype. Its goal is to keep all contributors attached to one shared end-to-end path instead of building isolated pieces that cannot integrate.

The flow described here is both:

- the architectural target for v1
- the integration contract that current code should evolve toward

## 2. End-to-End Loop

The core ANDB loop is:

`event input -> event ingest -> canonical object materialization -> retrieval projection -> query planning -> multi-path retrieval -> graph expansion -> evidence assembly -> proof trace -> structured response`

This loop is the most important contract in the repository.

## 3. Why the Main Flow Must Be Frozen Early

If the flow is not defined early, the repository will drift in predictable ways:

- event payloads will stop matching object materialization needs
- retrieval will optimize for chunks rather than objects
- graph expansion will not know its seed contract
- response packaging will become inconsistent across modules
- experiments will benchmark the wrong interface

For ANDB, the main flow is not documentation after the fact. It is a design artifact.

## 4. Flow A: Ingest

### 4.1 Goal

Receive raw event input and convert it into a validated event envelope that becomes the source of downstream state change.

### 4.2 Current Entry Point

- HTTP route: `/v1/ingest/events`
- Gateway implementation: [`src/internal/access/gateway.go`](../../src/internal/access/gateway.go)
- Runtime entry: [`src/internal/worker/runtime.go`](../../src/internal/worker/runtime.go)

### 4.3 Input Shape

The current runtime ingests `schemas.Event`, defined in [`src/internal/schemas/canonical.go`](../../src/internal/schemas/canonical.go).

Typical event types include:

- `user_message`
- `assistant_message`
- `tool_call_issued`
- `tool_result_returned`
- `plan_updated`
- `critique_generated`

### 4.4 Steps

1. request reaches the access layer
2. request is decoded into an `Event`
3. event is appended to the WAL
4. append result produces an LSN / logical sequence
5. downstream consumers are notified through the in-memory bus

### 4.5 Current Runtime Reality

Today the runtime appends to WAL and immediately feeds the data plane. Full event validation and dedicated materialization workers are still shallow, but the write-first-into-WAL rule is already part of the design.

### 4.6 Output

- persisted event record in the in-memory WAL
- ingest acknowledgment
- trigger point for later materialization/indexing flow

## 5. Flow B: Materialization

### 5.1 Goal

Transform events into canonical objects and version-aware updates.

### 5.2 Why It Exists

Events are the source of truth for state change, but query execution should operate over object-centric forms rather than raw event streams alone.

### 5.3 Target Steps

1. load event envelope
2. determine which object types are affected
3. construct canonical objects
4. create or update `ObjectVersion`
5. generate typed edges where needed
6. persist canonical objects and relation records

### 5.4 Examples

- `user_message` -> `Event` + candidate `Memory`
- `tool_result_returned` -> `Event` + `Artifact` + optional `State`
- `plan_updated` -> `Event` + `State`
- `critique_generated` -> `Event` + reflective `Memory`

### 5.5 Current Runtime Reality

The repository does not yet have a full dedicated materialization module. The current data plane derives a memory-like retrieval object directly from ingested event text. That is a v1 bootstrap shortcut, not the final semantic boundary.

### 5.6 Output

- canonical objects
- object version hints
- edge records
- retrieval projection tasks

## 6. Flow C: Retrieval Projection

### 6.1 Goal

Prepare retrievable forms from canonical objects.

### 6.2 Why Projection Is Separate

Canonical objects represent semantic truth. Retrieval needs dense, sparse, and filterable projections derived from those objects.

### 6.3 Target Steps

1. choose retrievable objects
2. derive dense representation
3. derive sparse/lexical representation
4. extract filter attributes
5. store retrieval entries in the data plane

### 6.4 Current Runtime Reality

Today the runtime materialization path projects event text into a memory-like object ID (`mem_<event_id>`) and attaches:

- `tenant_id`
- `workspace_id`
- `agent_id`
- `session_id`

This is implemented through:

- [`src/internal/materialization/service.go`](../../src/internal/materialization/service.go)
- [`src/internal/dataplane/segment_adapter.go`](../../src/internal/dataplane/segment_adapter.go)

### 6.5 Output

- retrieval-ready object IDs
- searchable content representation
- metadata for filtering and namespace partitioning

## 7. Flow D: Query

### 7.1 Goal

Accept a structured query request and retrieve candidate evidence seeds.

### 7.2 Current Entry Point

- HTTP route: `/v1/query`
- Request type: `schemas.QueryRequest`
- Response type: `schemas.QueryResponse`

Current implementation:

- [`src/internal/access/gateway.go`](../../src/internal/access/gateway.go)
- [`src/internal/worker/runtime.go`](../../src/internal/worker/runtime.go)
- [`src/internal/schemas/query.go`](../../src/internal/schemas/query.go)

### 7.3 Target Request Semantics

The v1 contract is intended to carry:

- query text
- agent/session context
- scope restrictions
- temporal filters
- object and memory-type filters
- relation expansion constraints
- response mode

### 7.4 Current Steps

1. request reaches the query API
2. request is decoded into `QueryRequest`
3. runtime calls the embedded data plane
4. data plane performs search over segments
5. candidate object IDs are returned to response assembly

### 7.5 Current Runtime Reality

The current implementation is still lighter than the target contract:

- dense/sparse separation is not explicit yet
- filter application is represented in response notes more than in deep execution
- graph expansion is not yet active

But the contract shape already reserves space for those stages.

### 7.6 Output

- seed object IDs
- scanned segment information
- retrieval path/proof notes for response packaging

## 8. Flow E: Graph Expansion

### 8.1 Goal

Transform retrieved seed objects into a local evidence subgraph through typed relations.

### 8.2 Why It Matters

This is where ANDB diverges from ordinary chunk retrieval. Instead of returning only ranked fragments, the system should assemble related objects and edges that explain why the answer is supported.

### 8.3 Target Steps

1. accept seed objects from retrieval
2. load incoming and outgoing edges
3. apply hop, edge-type, scope, and confidence constraints
4. assemble a local evidence graph

### 8.4 v1 Constraint

In v1, expansion should remain constrained to 1-hop or 2-hop.

### 8.5 Current Runtime Reality

The response currently returns an empty edge list. This means the expansion stage is planned, schema-visible, and documentation-frozen, but not yet materially implemented.

## 9. Flow F: Response Assembly

### 9.1 Goal

Build the final structured response returned to the caller.

### 9.2 Target Response Content

The target v1 response includes:

- `objects`
- `edges`
- `provenance`
- `versions`
- `applied_filters`
- `proof_trace`

### 9.3 Current Runtime Reality

The current `QueryResponse` already includes these top-level categories, but uses simplified representations:

- `objects` is currently a list of object IDs
- `provenance` is currently a list of strings
- `applied_filters` is currently a list of filter names
- `proof_trace` is currently a list of execution notes

This is enough for bootstrap integration, but the richer schema described in the docs remains the intended v1 target.

## 10. Flow G: Benchmark and Experiment

### 10.1 Goal

Evaluate whether ANDB improves evidence-oriented retrieval over a simpler baseline.

### 10.2 Expected Tasks

- generate mock events
- ingest them through the public API
- run representative queries
- compare against a top-k-only baseline
- collect retrieval and response metrics

### 10.3 Current Assets

- [`scripts/seed_mock_data.py`](../../scripts/seed_mock_data.py)
- [`scripts/run_demo.py`](../../scripts/run_demo.py)
- [`scripts/benchmark.py`](../../scripts/benchmark.py)
- benchmark docs under [`docs/experiments`](../experiments)

## 11. Module Ownership Along the Flow

### 11.1 Access / API

Owns:

- route registration
- request decoding
- public contract exposure

### 11.2 Event Backbone / Runtime

Owns:

- WAL append semantics
- worker subscription path
- ingest/query orchestration

### 11.3 Materialization / Semantic Layer

Owns:

- event-to-object transformation
- edge generation
- version handling

### 11.4 Data Plane / Retrieval

Owns:

- retrieval projections
- search execution
- candidate return

### 11.5 Graph / Response

Owns:

- relation expansion
- evidence graph assembly
- proof trace packaging

### 11.6 Experiment Layer

Owns:

- seed scripts
- benchmark loops
- baseline comparison

## 12. What Must Stay Stable in v1

The following contracts should remain stable unless deliberately reviewed:

- event envelope shape
- canonical object schema
- query request shape
- query response categories
- candidate seed contract between retrieval and graph stages
- edge typing conventions needed for evidence assembly

## 13. What Can Remain Flexible in v1

The following can still vary internally:

- exact storage backend
- embedding backend
- sparse retrieval implementation
- graph storage representation
- in-process versus separated worker execution

As long as the shared contracts stay coherent.

## 14. Summary

All implementation work should connect back to this path:

`ingest -> materialize -> project -> retrieve -> expand -> assemble -> explain -> return`

That is the operational skeleton of ANDB v1.
