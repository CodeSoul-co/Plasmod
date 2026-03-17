# Architecture Overview

## 1. Purpose

This document explains the overall architecture of the ANDB v1 prototype. It defines the system thesis, clarifies the architectural layers, and connects the conceptual design to the code currently present in this repository.

This is a framework document, not a final distributed-systems specification.

## 2. Architectural Thesis

The central thesis of ANDB is:

**a database for multi-agent systems should treat memory, state, event, artifact, and relation as canonical objects, and should produce retrieval-ready views through event-driven materialization.**

This shifts the design from:

- table-centric modeling
- direct overwrite updates
- chunk-first retrieval

toward:

- object-centric modeling
- event-driven state evolution
- structured evidence retrieval

## 3. Architectural Positioning

ANDB v1 is intentionally positioned between three traditions:

### 3.1 Segment-Oriented Retrieval Plane

We borrow segment-oriented retrieval ideas from systems like Milvus, but the first-party ANDB runtime now exposes its own module boundaries and naming.  All Milvus-derived terms have been renamed to CogDB conventions (`Partition→Shard`, `Row→ObjectRecord`, etc.).

The retrieval layer is now a **three-tier architecture**:
- **Hot tier** — `HotSegmentIndex` (bounded in-memory growing shards) for sub-millisecond recall of active-session objects
- **Warm tier** — `SegmentDataPlane` (full in-memory, all shards) for normal queries
- **Cold tier** — `ColdSegmentPlane` (disk-backed placeholder) for historical and time-travel queries

Current in-repo anchor points:

- [`src/internal/dataplane/tiered_adapter.go`](../../src/internal/dataplane/tiered_adapter.go)
- [`src/internal/dataplane/segment_adapter.go`](../../src/internal/dataplane/segment_adapter.go)
- [`src/internal/dataplane/segmentstore/engine.go`](../../src/internal/dataplane/segmentstore/engine.go)

### 3.2 Manu-Inspired Control and Event Plane

We borrow the idea that state change should be log-first, event-centric, and coordinated through an orchestration layer rather than through direct final-state mutations.

Current in-repo anchor points:

- [`src/internal/eventbackbone/wal.go`](../../src/internal/eventbackbone/wal.go)
- [`src/internal/coordinator/hub.go`](../../src/internal/coordinator/hub.go)

### 3.3 Agent-Native Semantic Layer

This is the part ANDB adds on top:

- canonical cognitive objects
- explicit relation edges
- provenance-aware response assembly
- version-aware object semantics
- structured evidence return
- **pre-computed evidence fragments** built at ingest time so query assembly is fast
- **1-hop graph expansion** over retrieved object IDs to populate the `Edges` field

Current in-repo anchor points:

- [`src/internal/schemas/canonical.go`](../../src/internal/schemas/canonical.go)
- [`src/internal/semantic/objects.go`](../../src/internal/semantic/objects.go)
- [`src/internal/materialization/service.go`](../../src/internal/materialization/service.go)
- [`src/internal/evidence/assembler.go`](../../src/internal/evidence/assembler.go)
- [`src/internal/evidence/cache.go`](../../src/internal/evidence/cache.go)
- [`src/internal/materialization/pre_compute.go`](../../src/internal/materialization/pre_compute.go)

## 4. Three Architectural Perspectives

The architecture should be read through three overlapping perspectives.

### 4.1 Execution-Oriented System Layers

These layers explain runtime behavior:

- Access Layer
- Coordinator Layer
- Event Backbone
- Worker Layer
- Storage/Data Plane

### 4.2 Semantic Modeling Layers

These layers explain the meaning of what is stored:

- Base semantic objects
- Policy/governance semantics
- Adaptation and future specialization

### 4.3 Representation Layers

These layers explain how objects appear to the retrieval system:

- Canonical record layer
- Retrieval projection layer
- Reasoning structure layer

These are different views of the same system, not separate products.

## 5. Main Architectural Principle

The most important principle is:

**ANDB stores canonical objects and derived retrieval views, not only raw embeddings or raw message chunks.**

That implies:

1. events are the source of truth for state evolution
2. canonical objects are the semantic unit of storage and retrieval
3. retrieval-ready representations are projections, not the canonical truth itself
4. graph structure is part of evidence construction
5. query results should be evidence packages, not only ranking lists

## 6. Canonical Objects as the Core Unit

The v1 object set is:

- `Agent`
- `Session`
- `Event`
- `Memory`
- `State`
- `Artifact`
- `Edge`
- `ObjectVersion`

The current object contract is defined in [`src/internal/schemas/canonical.go`](../../src/internal/schemas/canonical.go). That file is the implementation source of truth for current field names. The schema docs define the semantic intent and the stability rules around those fields.

## 7. Separation of Concerns

### 7.1 Access Layer

Responsibilities:

- receive HTTP requests
- decode request payloads
- expose health and public endpoints
- route ingest and query calls into the runtime

Current code:

- [`src/internal/access/gateway.go`](../../src/internal/access/gateway.go)

### 7.2 Coordinator Layer

Responsibilities:

- coordinate schema and object model registration
- host policy/version/object scheduling concerns
- provide a control-plane integration point between runtime modules

Current code:

- [`src/internal/coordinator`](../../src/internal/coordinator)

In v1, this is still lightweight and mostly in-process.

### 7.3 Event Backbone

Responsibilities:

- append events to WAL before downstream mutation
- provide clock/sequence semantics
- expose subscriber-oriented flow for later materialization and indexing workers
- `Scan(fromLSN)` and `LatestLSN()` enable bounded-staleness replay and recovery

Current code:

- [`src/internal/eventbackbone/wal.go`](../../src/internal/eventbackbone/wal.go)
- [`src/internal/eventbackbone/pubsub.go`](../../src/internal/eventbackbone/pubsub.go)
- [`src/internal/eventbackbone/tso.go`](../../src/internal/eventbackbone/tso.go)
- [`src/internal/eventbackbone/watermark.go`](../../src/internal/eventbackbone/watermark.go)
- [`src/internal/eventbackbone/derivation_log.go`](../../src/internal/eventbackbone/derivation_log.go)
- [`src/internal/eventbackbone/policy_decision_log.go`](../../src/internal/eventbackbone/policy_decision_log.go)

In v1, this backbone is in-memory and intentionally simplified.

### 7.4 Worker Layer

Responsibilities:

- ingest submission: WAL → `MaterializeEvent` → canonical object persistence → pre-compute → retrieval plane
- query execution: tiered search → graph expansion → evidence assembly
- 14 specialised worker types (data, index, query, memory-extraction, consolidation, graph-relation, proof-trace, etc.)

Current code:

- [`src/internal/worker/runtime.go`](../../src/internal/worker/runtime.go)
- [`src/internal/worker/nodes/`](../../src/internal/worker/nodes/)

The `Runtime.SubmitIngest` now performs the full materialization loop: canonical `Memory` + `ObjectVersion` + typed `Edge` records are persisted to their stores on every ingest call.

### 7.5 Storage/Data Plane

Responsibilities:

- manage object projections for retrieval
- execute search over embedded segments/indexes
- support candidate lookup for later expansion

Current code:

- [`src/internal/dataplane`](../../src/internal/dataplane)

In v1, this is not yet a full external storage stack.

## 8. Canonical Records, Retrieval Projections, and Reasoning Structures

### 8.1 Canonical Record Layer

This layer stores object-level truth:

- object identity
- typed fields
- timestamps and version markers
- provenance anchors
- ownership and scope references

In current code, the canonical shapes are expressed through Go structs under [`src/internal/schemas`](../../src/internal/schemas).

### 8.2 Retrieval Projection Layer

This layer stores search-ready derived forms:

- dense text/vector representations
- sparse terms and lexical signals
- filterable attributes such as tenant, workspace, agent, and session

Today, the embedded data plane derives a memory-like projection directly from ingested event text and stores attributes like tenant, workspace, agent, and session.

### 8.3 Reasoning Structure Layer

This layer stores graph-like structures needed for evidence assembly:

- typed edges
- derivation links
- support/contradiction relations
- version and lineage hints

The v1 response contract already reserves space for these structures even though runtime expansion is still shallow.

## 9. Why Framework-First Matters

ANDB should not be developed as a pile of unrelated modules because the modules are tightly coupled by semantic contracts.

Examples:

- retrieval depends on canonical object shape
- evidence assembly depends on candidate object identity
- version metadata affects response semantics
- graph expansion depends on edge typing
- ingest/materialization determines what retrieval can search

That is why the repository freezes:

- shared schemas
- main-flow semantics
- request/response contracts
- module boundaries

before aggressive feature expansion.

## 10. v1 Architectural Focus

The v1 prototype validates this path:

`event → WAL → object materialization → canonical store → retrieval projection → tiered search → 1-hop graph expansion → pre-computed evidence assembly → structured response`

**Current implementation state:**

| Capability | Status |
|---|---|
| Event ingest (WAL-first) | ✅ |
| Canonical Memory + Version + Edge materialization | ✅ |
| Tiered retrieval (hot/warm/cold) | ✅ |
| Pre-computed EvidenceFragment at ingest | ✅ |
| 1-hop graph expansion in query response | ✅ |
| Structured QueryResponse (objects/edges/provenance/trace) | ✅ |
| 9-coordinator Hub | ✅ |
| 14-type worker node contracts | ✅ |
| HTTP API (10 routes) | ✅ |

The following remain intentionally simplified:

- distributed runtime
- durable persistence (cold tier is in-memory simulation)
- deep (2+ hop) graph expansion
- full governance enforcement
- full logical time and publication semantics

## 11. What Success Looks Like in v1

v1 is successful if the repository demonstrates:

1. canonical object modeling
2. event-first ingestion
3. retrieval over derived object projections
4. evidence-oriented response structure
5. a coherent foundation for graph and provenance expansion

Feature count is less important than proving this end-to-end design holds together.

## 12. Relationship to Future Versions

Future versions may add:

- policy-aware retrieval
- stronger visibility semantics
- richer object lineage and rollback
- conflict/merge handling
- tensor memory operators
- distributed orchestration and persistence

v1 should establish a skeleton that can absorb those capabilities without replacing the core abstraction.

## 13. Summary

ANDB v1 is best understood as:

**an event-driven, object-centric, retrieval-aware database skeleton for multi-agent cognition and structured evidence return.**

Its purpose is not to finish the whole system, but to make the architectural thesis concrete and runnable.
