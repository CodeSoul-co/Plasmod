# 15. Architecture Decision Records

> Language: [中文](../15-architecture-decision-records.md) | English

---

This chapter records cross-module decisions that define Plasmod's current architecture. Each record states the context, decision, consequences, rejected alternatives, and invariant that implementations must preserve.

---

## 15.1. ADR-0001: Event and WAL as the Causal Source

| Field | Decision record |
|---|---|
| Status | Accepted |
| Context | Agent state, memory, artifacts, and relations evolve together. Directly overwriting objects loses causal order and weakens replay. |
| Decision | Business mutations enter as Events. WAL assigns the durable causal sequence, and materialization derives canonical objects and projections. |
| Consequences | The system can distinguish accepted from visible, replay a range, and associate mutations with source Events. Callers must handle accepted-but-not-yet-visible outcomes. |
| Rejected alternative | Direct canonical CRUD as the only write path. It cannot reconstruct an Event sequence or provide equivalent replay semantics. |
| Current exception | Canonical CRUD remains available for management and compatibility, but it does not acquire WAL semantics automatically. |
| Invariant | Visibility progress must not advance before the required projection stage succeeds. |

---

## 15.2. ADR-0002: Canonical State Is Authoritative

| Field | Decision record |
|---|---|
| Status | Accepted |
| Context | ANN and lexical indexes are effective candidate generators but do not fully represent versions, relations, policy, lifecycle, or provenance. |
| Decision | ObjectStore, EdgeStore, and VersionStore retain authoritative state. Retrieval indexes are disposable projections connected by object ID. |
| Consequences | Incompatible vector spaces can be reindexed; query execution must hydrate candidate IDs and apply canonical rules. |
| Rejected alternative | Treating vector rows as the sole database record. This makes replay, latest-state semantics, graph provenance, and governance incomplete. |
| Invariant | Retrieval projection data cannot overwrite or invent canonical object content. |

---

## 15.3. ADR-0003: Go Runtime with a C++ Retrieval Library

| Field | Decision record |
|---|---|
| Status | Accepted, build-dependent |
| Context | API, WAL, consistency, and storage benefit from the Go runtime, while mature ANN implementations are primarily available in C++. |
| Decision | Go owns business contracts. CGO calls the stable C ABI of `libplasmod_retrieval`; C++ owns physical index operations. |
| Consequences | Native builds require CMake, CGO, ABI management, shared-library packaging, and license review. A stub path is available when native retrieval is not built. |
| Rejected alternatives | Reimplement every ANN engine in Go, or move the entire runtime into C++. Neither matches the current ownership boundary. |
| Invariant | Scope, RRF fusion, canonical hydration, policy, lifecycle, and evidence remain Go responsibilities. |

---

## 15.4. ADR-0004: Hot, Warm, and Cold Storage Tiers

| Field | Decision record |
|---|---|
| Status | Accepted |
| Context | Active agent objects require low latency, the full online working set requires durable canonical storage, and inactive data benefits from lower-cost archival. |
| Decision | HotObjectCache serves highly active objects, warm runtime storage and segments hold online data, and an S3-compatible cold store holds explicitly archived objects. |
| Consequences | Promotion, archive, reactivation, and purge require cross-tier coordination. Cold retrieval is opt-in through query semantics. |
| Rejected alternatives | Keep all objects permanently in memory, or synchronously make S3 part of every write transaction. |
| Invariant | Cold storage does not replace canonical backup or WAL, and archive is not equivalent to hard deletion. |

---

## 15.5. ADR-0005: Structured Evidence in Query Responses

| Field | Decision record |
|---|---|
| Status | Accepted |
| Context | An agent decision needs object identity, source, version, relation, and filtering context, not only a top-k similarity list. |
| Decision | `QueryResponse` can carry canonical objects, graph edges, versions, provenance, proof steps, applied filters, and retrieval/cache summaries. |
| Consequences | Query execution includes canonical hydration and bounded graph/version work after candidate retrieval. Production middleware must remove debug-only detail. |
| Rejected alternative | Return only candidate IDs and similarity scores as the complete query contract. That remains appropriate only for explicitly physical/internal paths. |
| Invariant | Proof and provenance entries must be derived from recorded facts or explicit planner/retrieval stages; they must not fabricate external evidence. |

---

## 15.6. ADR-0006: Native ANN Behind a Plasmod Adapter

| Field | Decision record |
|---|---|
| Status | Accepted, build-dependent |
| Context | Plasmod requires HNSW and optional IVF/DiskANN-style physical indexing without exposing third-party C++ object models to the agent database API. |
| Decision | `cpp/vendor` and `cpp/retrieval` are hidden behind the Plasmod C ABI and `dataplane/retrievalplane` bridge. |
| Consequences | Multiple index families can share one Go-facing boundary, but platform, ABI, source-revision, and license maintenance increase. |
| Rejected alternative | Bind each C++ engine directly into business-layer Go packages. That would spread backend-specific types and errors through the runtime. |
| Invariant | Third-party ANN internals are not presented as Plasmod-owned algorithms. |

---

## 15.7. ADR-0007: Candidate Fusion at the Go Result Layer

| Field | Decision record |
|---|---|
| Status | Accepted |
| Context | Lexical, dense, sparse, Hot, Warm, and Cold candidate scores use different scales and distance conventions. |
| Decision | The Go DataPlane fuses ranked candidate lists with RRF after backend-specific retrieval. |
| Consequences | Backends remain replaceable without requiring globally calibrated raw scores. Candidate depth, tie handling, source weights, and the RRF constant affect results and require stable configuration. |
| Rejected alternative | Add raw backend scores directly. The result would depend on incompatible metric ranges. |
| Invariant | RRF does not replace canonical version resolution, policy enforcement, lifecycle filtering, or relation constraints. |

---

## 15.8. ADR-0008: Vector-Only Ingest Is a Projection Interface

| Field | Decision record |
|---|---|
| Status | Accepted |
| Context | Precomputed vectors and bulk segment construction must avoid repeated embedding, but a vector row cannot represent Session, State, Artifact, Edge, Version, Policy, and provenance semantics. |
| Decision | `/v1/ingest/vectors` and warm-segment APIs remain physical retrieval-projection interfaces. Business writes continue to use Event and canonical ingest. |
| Consequences | Callers can reuse compatible vectors efficiently, but must provide stable object-ID mapping and a valid embedding compatibility tuple. |
| Rejected alternative | Promote vector-only ingest to the canonical source of truth. Such data cannot be fully reconstructed through Event replay. |
| Invariant | A query may use vector-only candidates, but canonical and evidence stages still own agent-native semantics when those stages are requested. |

---

## 15.9. ADR-0009: Explicit Consistency Modes

| Field | Decision record |
|---|---|
| Status | Accepted |
| Context | Agent workloads require different trade-offs between write acknowledgement, visibility latency, and background throughput. One implicit consistency behavior would make guarantees difficult to reason about. |
| Decision | The runtime exposes strict, bounded-staleness, and eventual visibility modes through the consistency controller. |
| Consequences | The required completion stage and timeout differ by mode. Metrics and responses must distinguish accepted, materialized, indexed, and visible progress. |
| Rejected alternative | Return one generic success response regardless of completed stage. |
| Invariant | A weaker mode may acknowledge earlier, but it must not report a stronger freshness guarantee than the completed stage supports. |

---

## 15.10. ADR-0010: Atomic Canonical Projection Requires Co-located Stores

| Field | Decision record |
|---|---|
| Status | Accepted |
| Context | Object, edge, and version records form one canonical mutation. Splitting them across independent storage transactions creates graph/version divergence. |
| Decision | The storage factory requires object, edge, and version stores to use the same backend for `ApplyCanonicalProjection`. |
| Consequences | Mixed per-store configurations are constrained. A future distributed backend must provide an equivalent atomic contract or explicitly revise the guarantee. |
| Rejected alternative | Best-effort writes to three unrelated stores while still claiming atomic canonical state. |
| Invariant | The runtime must reject a configuration that cannot satisfy the declared canonical transaction. |

---

## 15.11. ADR-0011: Canonical-first Projection with a Watermark Fence

| Field | Decision record |
|---|---|
| Status | Accepted |
| Context | Retrieval-first ordering can expose a candidate without a canonical object. Canonical-first ordering instead creates an internal window where authoritative state exists but indexing is incomplete. |
| Decision | The main callback commits the canonical write set before retrieval ingest and advances the visible watermark only after both succeed. Canonical objects persist `MutationLSN`; query uses `ReadWatermarkLSN` as a visibility fence. |
| Consequences | On retrieval failure, canonical snapshots remain available for same-LSN retry or reindex while normal queries hide the mutation. The two engines still do not form one ACID transaction. |
| Rejected alternatives | Retrieval-first ordering, or making canonical commit immediately visible before retrieval completion. |
| Invariant | An object whose `MutationLSN > ReadWatermarkLSN` must not be returned by the normal canonical query path. |

---

## 15.12. ADR-0012: Canonical Access and Evidence-safe Traversal

| Field | Decision record |
|---|---|
| Status | Accepted, partial security boundary |
| Context | One `scope` string cannot represent owner, hierarchical scope, agent/role grants, and share contracts. Filtering only seed candidates can leak private endpoints through graph expansion. |
| Decision | Memory, State, Artifact, Edge, and ObjectVersion persist `CanonicalAccess`. `/v1/query` applies access before hydration and revalidates nodes, edge endpoints, proof steps, and provenance after evidence assembly. Allowed reasons are returned as `AccessDecision`. |
| Consequences | Shared derivation binds typed ShareContract rules and enters through WAL. A trusted gateway must bind requester identity; raw CRUD and lifecycle routes still need a uniform write gate. |
| Rejected alternative | Rely only on retrieval metadata filters or a post-hoc contamination counter. |
| Invariant | Unauthorized objects and graph references must not appear in a normal QueryResponse; denied decisions must not disclose object existence. |

---

## 15.13. ADR Index

| ADR | Decision | Primary code boundary |
|---|---|---|
| 0001 | Event and WAL are the causal source | `eventbackbone`, ingest runtime |
| 0002 | Canonical state is authoritative | `storage`, `evidence`, `dataplane` |
| 0003 | Go runtime with C++ retrieval | `retrievalplane`, `cpp/` |
| 0004 | Hot/Warm/Cold tiering | `storage/tiered.go`, DataPlane |
| 0005 | Query returns structured evidence | `schemas/query.go`, `evidence` |
| 0006 | Native ANN stays behind an adapter | C ABI and CGO bridge |
| 0007 | Candidate fusion occurs in Go | DataPlane/RRF result layer |
| 0008 | Vector-only ingest is a projection API | Gateway vector and warm-segment routes |
| 0009 | Consistency mode is explicit | `worker/consistency` |
| 0010 | Canonical projection requires co-located stores | `storage/factory.go`, projection transaction |
| 0011 | Canonical-first projection uses a watermark fence | Runtime projection and query access |
| 0012 | Canonical access protects evidence traversal | schemas, semantic policy, Runtime access |

---

## 15.14. Adding or Revising an ADR

Create or revise an ADR when a change affects more than one module and alters an API guarantee, persistence boundary, recovery rule, default behavior, or ownership split.

Each ADR must include:

1. status and effective release or commit;
2. concrete context and problem;
3. the selected decision;
4. implementation and operational consequences;
5. rejected alternatives;
6. invariant enforced by code or tests;
7. migration impact when changing an accepted decision.

An ADR does not substitute for implementation. Chapter 14 must continue to report whether the corresponding capability is implemented, partial, experimental, or planned.
