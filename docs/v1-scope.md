# v1 Scope

## 1. Purpose

This document defines the scope of the ANDB v1 prototype.

The main rule is:

**v1 is a validation prototype, not a production-ready database platform.**

The purpose of this document is to keep the repository aligned around a narrow but meaningful validation target.

## 2. What v1 Is

v1 is a framework-level research prototype for an agent-native database. It is intended to validate that the following end-to-end thesis is workable:

- events can be ingested as the source of state change
- canonical objects can be materialized from events
- retrieval can operate over object-centric projections
- graph expansion can assemble structured evidence
- query responses can return more than isolated top-k chunks

## 3. What v1 Is Not

v1 is not:

- a production-grade distributed database
- a full cloud-native control plane
- a complete governance engine
- a full tensor execution runtime
- a finalized enterprise integration platform
- a complete conflict resolution system
- the final future-proof API for all later versions

Every major implementation choice should be checked against that distinction.

## 4. Core Validation Questions

The repository should use v1 to answer these questions:

### Q1

Can memory, state, event, and artifact be modeled as canonical objects instead of plain rows or raw chunk records?

### Q2

Can event-driven flow produce retrieval-ready object views?

### Q3

Can retrieval combine multiple signals over canonical objects rather than over chunks only?

### Q4

Can relation expansion assemble a minimal evidence subgraph?

### Q5

Can the response expose structured evidence with provenance and version hints?

If these are validated, v1 succeeds.

## 5. Must-Have Capabilities in v1

### 5.1 Event Ingest

v1 must support:

- event input
- event envelope decoding
- write-first event persistence into the event backbone
- ingest acknowledgment

Current anchors:

- [`src/internal/access/gateway.go`](../src/internal/access/gateway.go)
- [`src/internal/worker/runtime.go`](../src/internal/worker/runtime.go)
- [`src/internal/eventbackbone/wal.go`](../src/internal/eventbackbone/wal.go)

### 5.2 Canonical Object Materialization

v1 must support the concept and contract for generating at least:

- `Event`
- `Memory`
- `State`
- `Artifact`

Current reality:

- `Event` is explicitly ingested
- retrieval projection currently derives a memory-like object directly from event text
- full dedicated materialization workers are still to be expanded

### 5.3 Simplified Version Support

v1 must support at least:

- version fields on mutable objects
- mutation-event linkage
- the presence of object-version metadata in the response contract

This does not require a complete logical-time or publication model.

### 5.4 Retrieval Over Canonical-Object Projections

v1 must support:

- retrieval over object-derived representations
- metadata-aware filtering hooks
- candidate return suitable for later graph expansion

The long-term target includes dense, sparse, and filter-based retrieval. The current implementation is lighter, but the contract should move in that direction.

### 5.5 Relation Layer

v1 must support:

- typed edges in the canonical model
- relation-aware response structure
- at least a constrained 1-hop or 2-hop expansion design

Full graph execution can remain shallow during bootstrap, but the schema and flow cannot ignore relations.

### 5.6 Evidence Assembly

v1 must assemble or at least meaningfully scaffold a structured local evidence package from retrieval results.

At minimum, the response path should preserve:

- object identity
- edges category
- provenance category
- versions category
- proof trace category

### 5.7 Structured Response

The query response must move toward returning:

- objects
- edges
- provenance
- version hints
- applied filters
- proof trace notes

Even if some fields are still simplified in the current runtime, the contract should remain evidence-oriented.

### 5.8 Benchmark and Demo Support

v1 must support:

- mock data ingest
- runnable query demo
- basic testability
- baseline-oriented benchmark planning

Current anchors:

- [`scripts/seed_mock_data.py`](../scripts/seed_mock_data.py)
- [`scripts/run_demo.py`](../scripts/run_demo.py)
- [`scripts/benchmark.py`](../scripts/benchmark.py)

## 6. Optional or Weakly Supported in v1

The following can remain minimal:

### 6.1 Scope and Visibility

Basic scope fields can exist without a full governance engine.

### 6.2 Policy Execution

Policy references and policy coordinators can exist without complete enforcement.

### 6.3 Artifact Depth

Artifacts can be linked simply without external federation.

### 6.4 Proof Trace Depth

Proof trace can be explanatory and shallow rather than formally complete.

### 6.5 Time Semantics

`visible_time` and `logical_ts` can exist as contract fields before the full runtime semantics are implemented.

## 7. Explicit Non-Goals for v1

### 7.1 Full Logical Time Model

Do not build:

- TSO-grade global semantics beyond current lightweight support
- full visibility publication engine
- bounded staleness engine
- complete time-travel query model

### 7.2 Full Governance Runtime

Do not build:

- full ACL engine
- full TTL enforcement engine
- quarantine workflow engine
- production audit pipeline

### 7.3 Full Conflict and Merge Runtime

Do not build:

- fact arbitration engine
- shared plan merge runtime
- CRDT-style merge engine

### 7.4 Full Tensor Memory Engine

Do not build:

- generalized subtensor execution
- tensor-native storage engine
- full tensor operator runtime

### 7.5 Full Distributed Runtime

Do not build:

- elastic worker autoscaling
- production scheduler framework
- HA deployment architecture

### 7.6 Full Enterprise Federation

Do not build:

- large connector suites
- cross-system policy federation
- enterprise orchestration stack

## 8. Acceptable Simplifications

The following are explicitly acceptable in v1:

1. in-process workers instead of independent services
2. in-memory or lightweight storage
3. shallow graph store behavior
4. rough scoring
5. proof trace as execution notes
6. shallow versioning
7. approximate visibility filtering
8. bootstrap response objects that are less rich than the final target

These are acceptable only if the architecture stays extensible.

## 9. Success Criteria

v1 is successful if the repository can demonstrate:

1. a collaborator can ingest mock events through the public API
2. the system can project or materialize canonical-object-oriented records
3. a query can retrieve candidate objects from the data plane
4. the response preserves evidence-oriented structure
5. the docs, code, and tests agree on the main flow
6. benchmark work can compare ANDB-style response with a simpler baseline

## 10. Scope Discipline Rules

### Rule 1

No new top-level feature should be added unless it directly improves the v1 validation loop.

### Rule 2

Any feature requiring major infrastructure should be challenged.

### Rule 3

If a capability can be represented as a field now and implemented later, prefer that path.

### Rule 4

Correct abstraction is more important than complete functionality in v1.

## 11. Relationship to Later Versions

Later versions may extend v1 with:

- policy-aware retrieval
- rollback/time-travel query
- visibility-aware retrieval
- share contracts
- conflict merge
- tensor slicing and aggregation
- distributed scaling
- external enterprise connectors

These should layer onto the v1 skeleton rather than forcing v1 to absorb them prematurely.

## 12. Summary

The scope of v1 is deliberately narrow in runtime ambition but strong in abstraction.

Its job is to prove that the following system is real and implementable:

**event-driven + object-centric + retrieval-aware + graph-assembled + structured-evidence-returning**

Anything that does not directly serve that loop should be postponed.
