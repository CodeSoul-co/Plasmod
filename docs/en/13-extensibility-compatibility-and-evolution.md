# 13. Extensibility, Compatibility, and System Evolution

> Language: [中文](../13-extensibility-compatibility-and-evolution.md) | English

---

This chapter defines the engineering contract for extending Plasmod. An extension is complete only when its schema, interfaces, implementation, bootstrap wiring, persistence, recovery behavior, tests, and documentation agree.

---

## 13.1. Extension Principles

Every extension must answer these questions before implementation:

| Question | Required answer |
|---|---|
| Causal source | Is the mutation represented by an Event and WAL record? |
| Authority | Which canonical object or store is the source of truth? |
| Identity | Is the ID deterministic and replay-safe? |
| Scope | Which tenant, workspace, agent, session, and sharing rules apply? |
| Projection | What retrieval record is derived, and how is it rebuilt? |
| Versioning | What creates ObjectVersion and validity changes? |
| Failure | What can complete partially, and how is that state exposed? |
| Recovery | Is the operation retryable, replayable, or repairable? |
| Deletion | How do soft delete, purge, archive, and reactivation behave? |
| Compatibility | Which API, storage, WAL, SDK, or configuration contract changes? |

Adding only a handler, struct, or index operation does not constitute a complete system feature.

### 13.1.1. Stable extension boundaries

| Boundary | Code surface | Appropriate extensions |
|---|---|---|
| Event schema | `src/internal/schemas/dynamic_event.go`, `constants.go` | new Event types, typed payload fields |
| Canonical model | `src/internal/schemas/canonical.go` | new stable object semantics |
| Materialization | `src/internal/materialization`, `src/internal/worker/materialization` | Event-to-object derivation |
| Runtime storage | `src/internal/storage/contracts.go` | canonical persistence backends |
| Retrieval DataPlane | `src/internal/dataplane/contracts.go` | candidate and segment behavior |
| Native retrieval | `src/internal/dataplane/retrievalplane`, `cpp/` | physical ANN implementations |
| Query semantics | `src/internal/schemas/query.go`, `src/internal/semantic` | typed filters and operators |
| Evidence | `src/internal/evidence` | hydration, graph, version, proof annotations |
| Governance | `src/internal/semantic/policy.go`, storage governance records | scope and policy decisions |
| Public adapters | `src/internal/access`, `src/internal/api/grpc`, `sdk/` | transport and SDK compatibility |

---

## 13.2. Add an Agent Framework Adapter

An adapter translates a framework lifecycle into Plasmod Event and Query contracts. It should not serialize an entire framework state as opaque text and call that integration complete.

### 13.2.1. Semantic mapping

| Framework concept | Plasmod representation |
|---|---|
| Conversation, run, or task | Session |
| User/environment observation | Event with observation semantics |
| Model or tool callback | Event, optionally ToolResult and State update |
| Durable memory | Memory derived through ingest/materialization |
| Environment mutation | AgentState with stable `state_key` |
| Plan, report, file, or patch | Artifact |
| Dependency, support, contradiction | Edge |
| Framework identity and tenancy | tenant/workspace/agent/session scope |

### 13.2.2. Adapter requirements

- stable IDs across retries;
- event time and logical-order preservation;
- typed payloads and schema versions;
- explicit precomputed-vector compatibility metadata;
- selected consistency and visibility requirement;
- retry with idempotency, timeout, and cancellation;
- shutdown flush and in-flight request handling;
- service and SDK version compatibility.

Framework adapters should live in an SDK or adapter package. The core composition root must not depend on a particular agent framework.

---

## 13.3. Add an Agent State Type

State types share the canonical AgentState model unless they require fundamentally different persistence, query, and lifecycle semantics.

1. Define the meaning of `state_type` and stable `state_key`.
2. Define the value codec and schema version.
3. Extend Event validation and State materialization.
4. Preserve deterministic State identity, normally derived from agent and key.
5. Define replace, merge, increment, and conflict semantics.
6. Define latest-version ordering independently of vector similarity.
7. Add replay, restart, and out-of-order update tests.
8. Add deletion, purge, trace, and SDK coverage.

State correctness is established by canonical version and logical ordering, not by whichever vector candidate ranks first.

---

## 13.4. Add a Canonical Object

### 13.4.1. Implementation checklist

| Area | Required change |
|---|---|
| Schema | struct, constants, validation, JSON compatibility |
| Storage contract | object-specific or generic `RuntimeStorage` operations |
| In-memory backend | CRUD/list semantics and not-found behavior |
| Badger backend | key prefix, codec, transaction participation |
| Cold representation | S3 key and serialization when archivable |
| Materialization | deterministic object, edge, and version derivation |
| Runtime/coordination | constructor wiring and lifecycle ownership |
| API | handler, gRPC mapping where applicable, error behavior |
| Query/evidence | filtering, hydration, graph/version/provenance output |
| Lifecycle | archive, restore, soft delete, purge, backup |
| SDK/docs | typed client model and numbered reference sections |

### 13.4.2. Test checklist

- in-memory and Badger CRUD parity;
- Badger close/reopen;
- object/edge/version transaction behavior;
- deterministic repeated materialization;
- replay after restart;
- scope and policy enforcement;
- old-data decoding;
- cold archive and rehydration;
- purge completeness.

A persisted key prefix is a storage-format contract. Do not rename it in a patch release without migration support.

---

## 13.5. Add an Event Type

1. Add the canonical Event type string in `schemas/constants.go`.
2. Extend `DynamicEvent.Normalize` and validation.
3. Specify payload, target-object, causality, parent, and scope requirements.
4. Map the Event to the appropriate materializer and worker path.
5. Define every derived object, edge, version, and deterministic ID.
6. Define whether vector projection is required, optional, or skipped.
7. Extend query filters, trace, replay, and audit behavior.
8. Add valid, invalid, duplicate, replay, and visibility tests.
9. Update HTTP/gRPC schema, SDK, and user guide.

Once an Event type is written to WAL, its string value is a compatibility contract. Rename it through aliasing and migration rather than deleting the old constant.

---

## 13.6. Add or Extend a Materializer

The materializer converts accepted Event semantics into canonical projection mutations.

### 13.6.1. Required contract

| Element | Requirement |
|---|---|
| Input | normalized, validated Event with assigned causal metadata |
| Output | typed objects, edges, versions, retrieval records, derivation metadata |
| Identity | deterministic for the same Event and target semantics |
| Mutation | written through the runtime storage transaction boundary |
| Projection | requested explicitly and tracked separately |
| Idempotency | duplicate Event replay does not create divergent objects |
| Failure | returns the failed phase and preserves replay information |

### 13.6.2. Wiring steps

1. Implement the typed derivation logic.
2. Register it in the active materialization service or worker.
3. Route the relevant Event type from ingest.
4. Write canonical mutations through `ApplyCanonicalProjection` where atomic object/edge/version behavior is required.
5. Request retrieval projection through the DataPlane.
6. Advance consistency state only after the required phase succeeds.
7. Add idempotency, replay, malformed input, and partial-failure tests.

The materializer must not advance the visible checkpoint on its own.

---

## 13.7. Add a Memory Algorithm

Memory algorithms implement algorithm-specific recall or lifecycle logic without becoming an alternative canonical store.

1. Define a provider/algorithm ID and typed configuration.
2. Implement the active memory algorithm contract in `src/internal/worker/cognitive`.
3. Read canonical Memory plus separate algorithm state.
4. Define supported operations: ingest, recall, reinforce, decay, compress, summarize, conflict, or archive recommendation.
5. Respect tenant scope, policy, TTL, and lifecycle state.
6. Register the provider in bootstrap and expose health/profile state.
7. Define state migration when switching active algorithms.
8. Add deterministic unit and dispatcher contract tests.

An algorithm must not write Badger keys directly or fabricate Event provenance. Suggested lifecycle transitions must flow through the runtime's canonical update path.

---

## 13.8. Add a Policy Rule

A policy rule receives a typed subject, actor/scope, operation, governance records, and runtime context. It returns a typed decision such as allow, deny, partial, mask, quarantine, weight, or TTL adjustment.

### 13.8.1. Implementation steps

1. Define rule ID, version, and typed configuration.
2. Select the exact read, write, share, or query hook.
3. Define deny precedence and policy composition.
4. Persist or reference the applicable PolicyRecord and ShareContract.
5. Record the decision, reason code, source, actor, and Event ID.
6. Ensure denied content is excluded from evidence, logs, and caches.
7. Test conflict, missing policy, default behavior, and scope leakage.
8. Expose only redacted effective configuration.

Security-sensitive failures must use an explicit fail-open or fail-closed policy. An accidental fallback is not a policy.

---

## 13.9. Add an Evidence Hook

An evidence hook may add graph nodes, edges, version/provenance data, proof steps, scores, or policy annotations.

Requirements:

- consume only objects already permitted for the query;
- remain deterministic for the same canonical snapshot and plan;
- limit graph depth and node/edge count;
- honor context cancellation and timeout;
- never mutate canonical state;
- avoid leaking denied content through explanations;
- define whether hook failure fails the query, marks it partial, or omits optional enrichment;
- return typed fields before production visibility middleware.

Every new hook requires evidence-package tests and an explicit response-contract stability level.

---

## 13.10. Add a Query Operator

1. Add a typed optional field to `QueryRequest` or the supported `query_ops` descriptor.
2. Parse and validate it in the semantic planner.
3. Define identical semantics for Hot, Warm, Cold, and canonical supplementation.
4. State whether it runs before candidate generation, after fusion, or during canonical filtering.
5. Record the operation in `applied_filters` or proof steps.
6. Preserve batch-query parity where the batch API supports structured semantics.
7. Update Python and other SDK models.
8. Test empty values, combinations, unsupported backends, and scope leakage.

An operator enforced only on ANN candidates can be bypassed by canonical supplementation or cold retrieval. Security and scope conditions require canonical enforcement.

---

## 13.11. Add a Retrieval Backend

The backend implements physical retrieval contracts while preserving Go-owned business semantics.

### 13.11.1. Backend contract

| Concern | Required definition |
|---|---|
| Index lifecycle | create, build, load, search, close, rebuild |
| Vector contract | metric, dimension, embedding family, normalization |
| Result | object ID, score/distance, candidate metadata |
| Batch | row identity, ordering, cancellation, concurrency |
| Persistence | segment and index metadata, compatible reopen |
| Mutation | delete, compaction, reindex |
| Errors | unavailable, invalid dimension, unsupported index, corruption |
| Ownership | handles, buffers, files, threads, shutdown |

Tenant policy, canonical hydration, RRF fusion, evidence, and version resolution stay in Go. An unsupported index must return a clear error rather than silently selecting a different algorithm.

---

## 13.12. Add a Storage Backend

Every backend must satisfy `RuntimeStorage` semantics, not merely compile against individual interfaces.

### 13.12.1. Required behavior

- object/edge/version atomic projection or an explicitly weaker rejected mode;
- stable key, ordering, pagination, and list semantics;
- durability and sync behavior;
- concurrent read/write behavior;
- not-found and duplicate error classes;
- backup, restore, soft delete, purge, and close;
- configuration snapshot and schema migration.

### 13.12.2. Wiring

Add an explicit mode to `storage/factory.go`, build one runtime bundle, and expose the resolved backend in `/v1/admin/storage`. Do not infer the backend from an opaque DSN.

Run the same contract suite against memory, Badger, and the new implementation, including empty lists, duplicate writes, canonical transactions, restart, and failure injection.

---

## 13.13. Schema and Payload Evolution

Use this placement order for new data:

1. business-specific, non-indexed content in `payload`;
2. optional typed extension fields for reusable but non-core semantics;
3. top-level Event or canonical fields only for stable cross-cutting semantics;
4. a new canonical object only when persistence, identity, query, and lifecycle differ materially.

New fields should be optional, have explicit defaults, and preserve decoding of old WAL JSON. Fields used for identity, scope, policy, ordering, persistent keys, or lifecycle cannot remain untyped free-form payload.

### 13.13.1. Dynamic Event

`schema_version` identifies the Event schema. A breaking interpretation requires a new version and a replay adapter. Legacy aliases may remain readable, but new output should use one canonical field name.

### 13.13.2. Canonical objects

Prefer additive optional fields. A change to identity, validity interval, version ordering, or storage key requires migration and compatibility fixtures.

### 13.13.3. Constants

Persisted Event types, object types, lifecycle states, relation types, and visibility modes are wire/storage values. Deprecate through aliases and migration windows.

---

## 13.14. API and SDK Compatibility

### 13.14.1. Public HTTP and gRPC

Changes to ingest, query, canonical collection, trace, and public gRPC messages require compatibility review. Prefer additive optional fields. Changing a field type, default, error status, or visibility behavior is a breaking change even if the route name remains unchanged.

### 13.14.2. Internal transport

`/v1/internal/*` and binary transport are version-coupled implementation surfaces. They need not carry the same long-term compatibility promise as public APIs, but rolling deployments still require an explicit compatibility window.

### 13.14.3. SDK release order

1. Deploy a server that accepts both old and new requests.
2. Release the SDK that sends the new field or operation.
3. Observe adoption and errors.
4. Deprecate the old form with a documented window.
5. Remove old behavior only in the declared breaking release.

Keep HTTP status, error body shape, typed `query_status`, and partial-result behavior synchronized across server and SDK documentation.

---

## 13.15. Configuration Deprecation

Some active environment and CMake variables still use the historical `ANDB_` prefix. They are real compatibility aliases and must remain documented until removed from code.

### 13.15.1. Deprecation process

1. Add the canonical new key and retain the old fallback.
2. Log a redacted deprecation warning when the old key is used.
3. Report only the canonical key in effective configuration.
4. Update Compose files, scripts, SDKs, and documentation.
5. Publish the removal release and migration procedure.
6. Remove the fallback only in the announced breaking release.

### 13.15.2. YAML boundary

A YAML field is active only when bootstrap actually loads and applies it. Example configuration files are not proof that a setting controls runtime behavior.

---

## 13.16. Storage Format Evolution

Storage-format changes include Badger key prefixes, value codecs, WAL records, checkpoints, derivation logs, native segment metadata, and S3 object layouts.

### 13.16.1. Versioning rules

- Store a format or schema version where decoding can change.
- Make readers tolerate supported older versions.
- Keep writers on one canonical current format.
- Never reinterpret existing bytes silently.
- Preserve a pre-migration backup until post-migration verification completes.

### 13.16.2. Migration patterns

| Pattern | Use when | Requirement |
|---|---|---|
| Read-old/write-new | Additive codec change | reader supports both forms |
| Offline rewrite | key or identity changes | writes stopped; rollback backup retained |
| Dual write | staged backend migration | divergence metrics and reconciliation |
| Rebuild projection | retrieval format changes | canonical source and embedding tuple available |
| WAL replay adapter | Event schema changes | deterministic old-to-new interpretation |

---

## 13.17. Upgrade and Migration Guide

### 13.17.1. Before upgrade

1. Record current commit/tag, Go/native dependencies, and redacted effective configuration.
2. Back up Badger, WAL, checkpoints, derivation log, and cold metadata.
3. Review schema, API, storage, and configuration changes.
4. Run the new binary against a copy of production data.
5. Verify old queries, traces, and replay before changing live data.

### 13.17.2. Upgrade

1. Stop admitting writes.
2. Wait for the required visible checkpoint.
3. Shut down the old process cleanly.
4. Run the documented offline migration if required.
5. Start the new version.
6. Verify health, storage, configuration, and provider status.
7. Submit one strict Event and query it.
8. Validate Memory, State, Artifact, Edge, Trace, and cold-tier paths.
9. Restore traffic gradually.

### 13.17.3. Rollback

Use the old binary directly only when it can read every format written by the new version. Otherwise restore the pre-upgrade backup and separately reconcile writes accepted during the upgrade window.

---

## 13.18. Extension Review Gate

An extension is ready to merge only when all applicable rows are complete.

| Area | Evidence |
|---|---|
| Design | source of truth, identity, scope, failure, recovery documented |
| Schema | typed definitions and backward-compatible decode tests |
| Interfaces | owner package and implementation registry updated |
| Wiring | active constructor/bootstrap path verified |
| Persistence | memory, Badger, and cold behavior tested as applicable |
| Correctness | idempotency, replay, ordering, partial failure tested |
| API/SDK | compatibility and error behavior documented |
| Operations | metrics, logs, backup, migration, troubleshooting updated |
| Claims | Chapter 14 maturity and claim boundary updated |
