# 08. API, Schema, Configuration, and SDK Reference

> Language: [中文](../08-api-schema-and-sdk-reference.md) | English

---

This chapter defines the HTTP, gRPC, internal API, SDK, error, batching, configuration, and core-schema contracts.

---

## 08.1. Admin API

### 08.1.1. Authentication

Configure the admin key:

```bash
export PLASMOD_ADMIN_API_KEY='replace-with-a-secret'
```

Send `X-Admin-Key` or `Authorization: Bearer <key>`. The compatibility variable `ANDB_ADMIN_API_KEY` is still accepted, but new deployments should use the Plasmod variable.

If the key is not set, the server logs a warning and leaves admin routes unprotected.

### 08.1.2. Read-only Interfaces

- topology, storage, config/effective;
- metrics;
- GET of consistency/governance/runtime/provider mode;
- provider health;
- purge task status.

### 08.1.3. Mutation Interfaces

- S3 export/snapshot/cold purge;
- warm prebuild, embedding reindex;
- Data set/source delete and purge;
- data wipe, rollback, replay;
- POST of consistency/governance/runtime/provider mode

### 08.1.4. High-risk Operations

`data/wipe`, purge, cold-purge, rollback, and replay can modify large datasets or derived state.

1. Verify effective config and target instances;
2. Take a backup.
3. Restrict concurrent writes.
4. Save the request parameters and returned task ID.
5. Verify the results by query, trace, storage key and metrics.

The Admin API does not provide built-in multi-person approval, fine-grained RBAC, or audit archival. Add these controls at a trusted gateway or operations layer.

---

## 08.2. API Overview

### 08.2.1. HTTP Surface

Plasmod uses `net/http` to register three interface groups:

- Data/application interface: Event ingest, query, canonical object, trace;
- Internal runtime interfaces: memory algorithms, tasks, plans, MAS, and transport.
- Management interfaces: configuration, storage, replay, deletion, modes, and metrics.

Unified mode places management and data routes on `127.0.0.1:8080`. Split mode defaults to management on `9091` and data on `19530`.

### 08.2.2. Transport Surface

`src/internal/transport/server.go` provides internal HTTP RPC and WAL streaming. gRPC listens on `19531` by default. Its surface is smaller than the HTTP API; do not assume every HTTP route has a gRPC equivalent.

### 08.2.3. Content Type

Requests and successful responses primarily use `application/json`. Some errors use `http.Error` and therefore return plain text. Clients should check the HTTP status and `Content-Type` before decoding a response as JSON.

### 08.2.4. Stability Labels

- **Implemented**: registered and functional in the active bootstrap path.
- **Experimental**: implemented, but name or payload may change;
- **Partial**: the capability exists, but its contract is incomplete.
- **Not Confirmed**: the active code path does not provide enough evidence to rely on the capability.

`v1` is a route prefix, not a complete compatibility commitment; see [API Versioning](#083-api-versioning).

---

## 08.3. API Versioning

### 08.3.1. Current Version Boundary

- Public HTTP routes use `/v1`.
- Dynamic Events use schema `plasmod.dynamic_event.v0.4`.
- Canonical Go structs define their wire shape through JSON tags.
- Internal and transport APIs are coupled more closely to the repository commit.

`/v1` does not mean every field is frozen. Adding optional fields is generally backward compatible; renaming or deleting fields, changing types, or changing default consistency behavior requires explicit compatibility review.

### 08.3.2. Compatibility Rules

1. New clients emit canonical v0.4 nested fields.
2. The service may read legacy aliases, but new responses should not propagate them.
3. Optional application-specific data belongs in extensions or payload.
4. Internal API interoperability is guaranteed only for version-compatible components.
5. Persistent schema changes must account for replay of older WAL entries.
6. The SDK release must be marked with the corresponding service commit/tag.

See [Chapter 13: Extensibility, Compatibility, and Evolution](13-extensibility-compatibility-and-evolution.md) for the upgrade process.

---

## 08.4. Authentication

### 08.4.1. Built-in Capabilities

Admin middleware only matches `/v1/admin/`:

- primary env: `PLASMOD_ADMIN_API_KEY`;
- compatibility env: `ANDB_ADMIN_API_KEY`;
- headers: `X-Admin-Key` or Bearer token;
- key comparison uses a constant-time HMAC-based method to reduce timing leakage.

### 08.4.2. Capabilities Not Built In

- unified user authentication for the Data API;
- authentication for `/v1/internal/*`;
- fine-grained RBAC;
- TLS termination;
- token issuance and exchange;
- request quota.

### 08.4.3. Production Deployment

A trusted gateway/service mesh is placed in front of Plasmod:

1. TLS;
2. Verify the identity of the user or workload;
3. binding the identity to the tenant/workspace, refusing any overriding coverage of the client;
4. Isolate admin, internal, and transport ports.
5. Set body size, rate limit and audit logs;
6. The admin key is inserted into the secret manager and rotated regularly.

Canonical User, Policy, and ShareContract records are data and governance models; their presence does not mean that HTTP authentication is complete.

---

## 08.5. Binary And gRPC Transport

### 08.5.1. gRPC Server

gRPC is enabled by default on `0.0.0.0:19531` and can be disabled with `PLASMOD_GRPC_ENABLED=0`. The default maximum message size is 512 MiB; the effective logic lives in `src/internal/app/ports.go`.

The current gRPC service targets high-throughput and internal data transfer and does not mirror every HTTP route. Confirm supported methods from the protobuf definition rather than inferring capability from the open port.

The active Gateway and transport do not register a browser SSE endpoint. The WAL stream is an internal HTTP stream, not a browser EventSource. Binary transport refers primarily to protobuf/gRPC and row-major float-vector component protocols.

### 08.5.2. Internal HTTP RPC

`src/internal/transport/server.go` Provided by:

- batch ingest;
- unload segment;
- the warm query and its batch/raw variants;
- warm segment register;
- WAL stream.

These interfaces are directed to Plasmod components, not the preferred input for Agent applications.

Warm batch vectors use row-major layout: the service flattens two-dimensional HTTP vectors to `nq * dim`, and every row must have the same dimension. Internal flat requests carry explicit `nq`, `dim`, and `top_k` values. HTTP client and gRPC channel pooling manage connection reuse; the server does not create a durable session connection per agent.

### 08.5.3. Message Size

The big-vector batch needs to consider:

- gRPC max receive/send size;
- HTTP server/client body and timeout;
- gateway write concurrency semaphore;
- Native segment memory;
- Badger transaction size.

Increasing the message upper limit does not automatically increase throughput, but may increase memory peak.

---

## 08.6. Configuration Reference

### 08.6.1. Core Startup Configuration

| Variable | Default | Purpose |
|---|---|---|
| `PLASMOD_STORAGE` | `disk` | `disk` or `memory` |
| `PLASMOD_DATA_DIR` | `.andb_data` | Badger, WAL, checkpoint root directory |
| `PLASMOD_EMBEDDER` | resolved by configuration | `tfidf`, ONNX, or another provider |
| `PLASMOD_GRPC_ENABLED` | enabled | Whether to start gRPC |
| `PLASMOD_ADMIN_API_KEY` | empty | admin route key |
| `APP_MODE` | non-production default | response visibility and debug behavior |
| `PLASMOD_SKIP_VECTOR_INDEX` | false | skip vector projection globally |

### 08.6.2. Ports

Unified mode defaults to HTTP on `127.0.0.1:8080`. Split mode defaults to management on `0.0.0.0:9091` and API on `0.0.0.0:19530`; gRPC defaults to `0.0.0.0:19531`. See `src/internal/app/ports.go` for the exact variables and resolution logic.

### 08.6.3. Consistency

By default, strict and related default values include queue 4096, worker 4, retry 8, bounded lag 1s, query/shutdown timeout
30s and checkpoint flush 50ms. `worker/consistency` parses the environment and provides the runtime mode-management API.

### 08.6.4. S3/MinIO

Configurations include endpoints, buckets, access keys, secret keys, TLS, region/prefix. Sensitive values should not appear in logs, document examples, or
version control.

### 08.6.5. Actual YAML Status

The active bootstrap reads `configs/memory_tiering.yaml` and `configs/algorithm_*.yaml`.
`configs/app.yaml`, `storage.yaml`, `retrieval.yaml`, and `graph.yaml` are examples and cannot be treated as the runtime source of truth on their own;
Until `app.BuildServer` loads them explicitly, treat them as reference or compatibility configuration files.

Use `/v1/admin/config/effective` and startup logs to confirm effective values.

---

## 08.7. Error Model

### 08.7.1. HTTP Status

| Status | Current meaning |
|---:|---|
| `200`/other 2xx | The handler completed; callers must still inspect query or task status. |
| `400` | JSON, field, or normal runtime error |
| `401` | Admin key missing or invalid |
| `405` | Method not supported |
| `408` | Request context canceled |
| `503` | Backpressure, paused, accepted-not-visible, projection failure or runtime unavailable |
| `504` | Consistency or query wait exceeded its deadline |

Status mapping lives in `src/internal/access/gateway.go`. Some handlers return JSON errors; others return plain text.

### 08.7.2. Retry Classification

- 400/405: do not retry unchanged; correct the request.
- 401: correct the credentials.
- 503 backpressure: use exponential backoff.
- `503` accepted-not-visible: inspect status using the Event or object ID before resubmitting.
- `503` projection failure: inspect the embedder and native backend, then follow the replay procedure.
- `504`: the operation may complete later; inspect current state before retrying.
- Network disconnect: treat the submission outcome as unknown until status is checked.

### 08.7.3. Logical Query Status

Under HTTP 2xx, `query_status` may still be `no_retrieval_hits` or `no_retrieval_hits_supplemented`. The latter means retrieval produced no seed candidates but canonical listing supplemented the response.

---

## 08.8. Idempotency

### 08.8.1. Current Behavior

Public APIs do not provide a unified `Idempotency-Key` header, idempotency-record table, or exactly-once submission guarantee.

### 08.8.2. Event Ingestion

Callers should generate a stable `identity.event_id`; it is the primary key for tracing and application-level deduplication. After a network timeout:

1. Query the expected canonical object or trace.
2. Check the log/metrics of the service and the status of the WAL;
3. Re-send only when confirmed unprocessed or materialization can be safely re-introduced;
4. Resend with the same event ID rather than generating a new logical Event.

WAL append, projection, and direct CRUD have different replay and duplicate semantics. Do not generalize idempotency behavior from one route to every route.

### 08.8.3. Administrative Operations

Reindex, export, purge, and replay may create tasks or repeat scans. Callers should retain the task ID, scope, and checkpoint and inspect status before retrying.

### 08.8.4. Extension Requirements

Each new write interface must define its idempotency key, duplicate-detection scope, result-retention period, concurrent-duplicate behavior, and WAL/transaction boundaries.

---

## 08.9. Internal API

The Internal API connects the Agent SDK, algorithm providers, and runtime components. These routes are registered in the current Gateway but are not a stable public contract.

### 08.9.1. Memory Algorithm Bridge

- `POST /v1/internal/memory/recall`
- `POST /v1/internal/memory/ingest`
- `POST /v1/internal/memory/compress`
- `POST /v1/internal/memory/summarize`
- `POST /v1/internal/memory/decay`
- `POST /v1/internal/memory/share`
- `POST /v1/internal/memory/conflict/resolve`
- `POST /v1/internal/memory/stale`
- `POST /v1/internal/memory/conflict/inject`

`src/internal/access/gateway.go` decodes these requests and dispatches them to agent-SDK, semantic, or coordinator services. Algorithm profiles may come from baseline, MemoryBank-style, or Zep-style configuration, but the canonical schema does not change with the provider.

### 08.9.2. Task And Plan

- task: start, complete, tokens, claim, stage;
- plan: step, repair;
- session: context;
- tool: tool-state;
- agent: handoff;
- MAS: answer-consistency, aggregate.

These interfaces assume a trusted runtime caller. Admin authentication does not cover `/v1/internal/*`.

### 08.9.3. Transport RPC

`src/internal/transport/server.go` separately registers ingest-batch, segment-unload, warm-query, and warm-registration routes under `/v1/internal/rpc/*`. These are node-component protocols with payloads different from the Gateway API.

### 08.9.4. Usage Constraints

1. Expose the routes only on a private network.
2. Deploy client and server from the same commit.
3. Compare handler request structs before an upgrade.
4. Do not persist internal responses as long-term external contracts.
5. Wrap the routes behind a maintained adapter when application-level stability is required.

---

## 08.10. Pagination And Batching

### 08.10.1. Pagination

Canonical collection handlers do not currently share a `page_token` or `cursor` contract. Each GET route uses its own query parameters, so callers cannot assume stable snapshot pagination for large lists.

Large-scale exporting should use export/snapshot management capabilities or API direct extensions with a stable cursor, rather than a circular reading of unordered lists.

### 08.10.2. Query Batch

`POST /v1/query/batch` accepts `VectorWarmBatchQueryRequest`, not multiple general QueryRequest values. It requires `warm_segment_id`, `agent_mode`, and two-dimensional `vectors`; optional `source_ids` and `row_lineage` distribute row results to single-agent or multi-agent sources. Batching reduces native search-call overhead, but:

- does not constitute cross-query transactions;
- The route does not execute the general QueryRequest tenant/scope/evidence process;
- callers must inspect each entry in `rows` and read `by_source` when source fan-out is used.
- Batch size is restricted by HTTP body, memory, embedding and native search.

### 08.10.3. Vector Batch

`/v1/ingest/vectors` Accept two-dimensional vectors and optional object IDs.

- Each vector dimension is consistent;
- The number of object IDs is consistent with the number of vectors;
- The index parameters are consistent throughout the segment;
- If the batch fails, do not assume that the submission part will automatically roll back.

---

## 08.11. Public HTTP API

### 08.11.1. Event Ingest

```text
POST /v1/ingest/events
```

The body is the Event described in this chapter's schema reference. Successful responses include write/visibility status and LSN information. This is the preferred write entry point when WAL, replay, and materialization semantics are required.

### 08.11.2. Vector Ingest

```text
POST /v1/ingest/vectors
```

The primary fields are `vectors`, `object_ids`, `segment_id`, `index_type`, and IVF parameters. Supported index-type names are `HNSW`, `IVF_FLAT`, `IVF_PQ`, `IVF_SQ8`, and `DISKANN`; runtime availability depends on the native build.

This endpoint writes a physical retrieval segment; it does not replace Event and canonical-object ingest. A vector-only record cannot represent the complete semantics of an Agent object.

### 08.11.3. Query

```text
POST /v1/query
POST /v1/query/batch
```

`/v1/query` uses the general QueryRequest and QueryResponse schema defined below. `/v1/query/batch` is not a general query batch: it accepts `VectorWarmBatchQueryRequest` with `agent_mode`, `warm_segment_id`, `top_k`, two-dimensional `vectors`, and optional `source_ids`, `row_lineage`, and `search_raw`, then executes batch ANN against a registered warm segment.

### 08.11.4. Canonical Collections

```text
/v1/agents
/v1/sessions
/v1/memory
/v1/states
/v1/artifacts
/v1/edges
/v1/policies
/v1/share-contracts
```

GET uses query parameters for filtering, and POST accepts the corresponding canonical schema. These management and migration paths do not automatically produce the complete Event/WAL/Edge/Version chain.

### 08.11.5. Trace

```text
GET /v1/traces/{object_id}
```

`object_id` is a path suffix and must be URL-encoded. The response is assembled from object, edge, version, policy, and provenance records.

---

## 08.12. Route Index

### 08.12.1. Management

| Method | Route | Purpose | Status | Auth |
|---|---|---|---|---|
| GET | `/healthz` | Process health | Implemented | No built-in authentication |
| GET | `/v1/system/mode` | APP_MODE/deployment status | Implemented | No built-in authentication |
| GET | `/v1/admin/topology` | Topology summary | Implemented | Admin key |
| GET | `/v1/admin/storage` | Storage configuration summary | Implemented | Admin key |
| GET | `/v1/admin/config/effective` | Effective configuration | Implemented | Admin key |
| POST | `/v1/admin/s3/export` | Exported to the cold store | Implemented | Admin key |
| POST | `/v1/admin/s3/snapshot-export` | Quickly exported | Implemented | Admin key |
| POST | `/v1/admin/s3/cold-purge` | Clean up cold records | Implemented | Admin key |
| POST | `/v1/admin/warm/prebuild` | Preconstructed warm segment | Implemented | Admin key |
| POST | `/v1/admin/embeddings/reindex` | Embedding/index reconstructed | Implemented | Admin key |
| POST | `/v1/admin/dataset/delete` | Delete the logical data set | Implemented | Admin key |
| POST | `/v1/admin/dataset/purge` | Physical cleaning of data sets | Implemented | Admin key |
| GET/POST | `/v1/admin/dataset/purge/task` | Purge task Status/Control | Implemented | Admin key |
| POST | `/v1/admin/memory/delete-by-source` | Deleted by source | Implemented | Admin key |
| POST | `/v1/admin/memory/purge-by-source` | Cleaned by source | Implemented | Admin key |
| POST | `/v1/admin/data/wipe` | Clear data | Implemented | Admin key |
| POST | `/v1/admin/rollback` | Administrative rollback | Partial | Admin key |
| GET/POST | `/v1/admin/consistency-mode` | Check/switch consistency | Implemented | Admin key |
| POST | `/v1/admin/replay` | WAL replay | Implemented | Admin key |
| GET | `/v1/admin/metrics` | Operating indicators | Implemented | Admin key |
| GET/POST | `/v1/admin/governance-mode` | Models of governance | Implemented | Admin key |
| GET/POST | `/v1/admin/runtime-mode` | Runtime mode | Implemented | Admin key |
| GET/POST | `/v1/admin/memory/providers/mode` | Algorithm provider model | Experimental | Admin key |
| GET | `/v1/admin/memory/providers/health` | Provider Health | Experimental | Admin key |

### 08.12.2. Application Data API

| Method | Route | Purpose | Status |
|---|---|---|---|
| POST | `/v1/ingest/events` | Dynamic Event ingest | Implemented |
| POST | `/v1/ingest/vectors` | Precomputed vector segment ingest | Implemented |
| POST | `/v1/ingest/document` | Long document sections are written | Experimental |
| POST | `/v1/query` | Single query | Implemented |
| POST | `/v1/query/batch` | Warm-segment vector batch query | Implemented |
| GET/POST | `/v1/agents` | Agent canonical records | Implemented |
| GET/POST | `/v1/sessions` | Session canonical records | Implemented |
| GET/POST | `/v1/memory` | Memory canonical records | Implemented |
| GET/POST | `/v1/states` | AgentState canonical records | Implemented |
| GET/POST | `/v1/artifacts` | Artifact canonical records | Implemented |
| GET/POST | `/v1/edges` | Edge canonical records | Implemented |
| GET/POST | `/v1/policies` | Policy records | Implemented |
| GET/POST | `/v1/share-contracts` | Share contracts | Implemented |
| GET | `/v1/traces/{object_id}` | Evidence/provenance trace | Implemented |
| GET | `/v1/agent/list` | Agent by role/scale | Experimental |

`net/http.ServeMux` registers by path, and each handler validates the HTTP method. The table reflects current handler behavior; unsupported methods generally return `405`.

### 08.12.3. Internal Runtime API

| Routes | Purpose | Status |
|---|---|---|
| `/v1/internal/memory/*` | recall/ingest/compress/summarize/decay/share/conflict/stale | Experimental |
| `/v1/internal/task/*` | start/complete/tokens/claim/stage | Experimental |
| `/v1/internal/plan/*` | step/repair | Experimental |
| `/v1/internal/mas/*` | answer consistency/aggregate | Experimental |
| `/v1/internal/tool-state` | stateful tool query | Experimental |
| `/v1/internal/agent/handoff` | Agent handoff | Experimental |
| `/v1/internal/session/context` | Session context aggregation | Experimental |
| `/v1/internal/warm-segment/register` | Register a native warm segment | Experimental |

Although the last set of routes is present in the core Gateway, it is not a stable public API; production deployments should prevent direct access to the external network.

---

## 08.13. Canonical Objects

| Object | Primary ID | Purpose |
|---|---|---|
| Agent | `agent_id` | Runtime actor and capability directory |
| Session | `session_id` | A task/interaction scope |
| Event | `identity.event_id` | Immutable causal input |
| Memory | `memory_id` | Recallable agent memory |
| AgentState | `state_id` | Key/version status |
| Artifact | `artifact_id` | External or generated products |
| Edge | `edge_id` | Typed object relationships |
| ObjectVersion | object ID + version | Mutation history |
| User | `user_id` | Minimal identity-data object |
| Embedding | `vector_id` | Vector references and model information |
| PolicyRecord | policy/object | Object-governance decisions |
| ShareContract | `contract_id` | Explicit sharing agreement |
| RetrievalSegment | `segment_id` | Physical retrieval units |

Complete fields are defined in `src/internal/schemas/canonical.go`; object-type strings are defined in `schemas/constants.go`. Adding an object type requires coordinated changes to storage interfaces, Badger prefixes, Gateway, materializers, query/evidence paths, and documentation.

---

## 08.14. Dynamic Event v0.4

The canonical schema uses `schemas.Event` and the nested `schemas.DynamicEvent` groups.

| Group | Responsibility |
|---|---|
| `identity` | Event, tenant, workspace |
| `actor` | Agents, sessions and roles |
| `time` | event/ingest/visible time, logical timestamp |
| `event` | Event type, importance and so on |
| `object` | Target object type and selectable ID |
| `causality` | parent, causal refs, dependencies |
| `access` | visibility, consistency, ACL/policy references |
| `materialization` | enabled, targets, state prompts |
| `retrieval` | namespace, index text, embedding, query hints |
| `payload`/`data` | Business content and structured data |
| `runtime` | write and visibility status |
| `extensions` | Extended labels/fields |

Text extraction prefers `retrieval.index_text`, then text or content fields in the payload. Namespace resolution prefers retrieval namespace, then workspace, session, and finally `default`.

The compatibility layer accepts legacy flat fields, while canonical JSON output omits the corresponding `json:"-"` aliases.

---

## 08.15. Materialization Schema

The event `materialization` group describes the expected state of materialization and type of target. Common target:

- `memory`;
- `agent_state`;
- `artifact`;
- `relation`/`edge`;
- `object_version`;
- retrieval projection.

The active materializers do not yet treat `enabled` and `targets` as universal hard gates. Every Event ingest produces a default Memory, Memory ObjectVersion, and ingest-checkpoint State. Artifact and keyed AgentState objects are added when event, object, payload, and state-key conditions match. A target does not guarantee automatic creation of an unknown type, and `enabled=false` does not guarantee that the default canonical projection is skipped.

Memory IDs default to `mem_<event_id>`. Ingest-checkpoint State IDs use `state_<session_id>_<event_id>`, while keyed State IDs use `state_<agent_id>_<state_key>`. Artifact IDs prefer `object.object_id`. These rules are part of the persistence compatibility surface; changes require migration and replay validation.

Runtime status distinguishes acceptance, materialization, projection, and visibility. The consistency Controller's Tracker and checkpoint persist progress.

---

## 08.16. Memory Algorithm Schema

Memory algorithm works by stabilizing canonical memory fields and independent algorithm states, rather than defining new database master objects.

The relevant fields include:

- `memory_type`, `content`, `summary`;
- `confidence`, `importance`, `freshness_score`;
- `ttl`, `valid_from`, `valid_to`;
- `lifecycle_state`, `is_active`;
- `policy_tags`, `algorithm_state_ref`;
- Source event IDs and provenance ref.

Provider profiles are configured in `configs/algorithm_baseline.yaml`, `configs/algorithm_memorybank.yaml`, and `configs/algorithm_zep.yaml`. Algorithms can affect recall, decay, compression, and conflict resolution, but cannot bypass tenant/workspace scope, canonical persistence, or governance filtering.

---

## 08.17. Query Schema

Key `schemas.QueryRequest` fields:

- Text and ranking: `query_text`, `query_scope`, `top_k`.
- scope: `tenant_id`, `workspace_id`, `agent_id`, `session_id`;
- Types: `object_types`, `memory_types`, `edge_types`.
- Exact objects: `target_object_ids`.
- Time: `time_window.from/to`.
- Relationships: `relation_constraints`.
- Response: `response_mode`.
- Data sources: dataset/source/import batch selectors;
- Access, materialization, and runtime filters.
- `warm_segment_id`, `include_cold`, `embedding_vector`.

Responses include objects/nodes, edges, provenance, versions, applied filters, proof traces, chain traces, evidence-cache state, retrieval summaries, query status, and hints.

Standard response modes are `structured_evidence` and `objects_only`. Unknown-mode behavior is not a stable contract.

`POST /v1/query/batch` uses `schemas.VectorWarmBatchQueryRequest`, not an array of QueryRequest values. It returns `VectorWarmBatchQueryResponse` for native batch ANN against a warm segment.

---

## 08.18. Retrieval Schema

### 08.18.1. Event Retrieval

The Event retrieval group describes namespace, index text, embedding presence, dimension, vector, and materialized retrieval status. It instructs projection of a canonical Event; it is not the canonical object itself.

### 08.18.2. RetrievalSegment

Fields include segment ID, object type, namespace, time bucket, embedding family, storage/index ref, row count,
Min/max timestamp and tier.

### 08.18.3. Physical Index

The native layer accepts HNSW, IVF_FLAT, IVF_PQ, IVF_SQ8, and DISKANN when those implementations are built. Embedding family, dimension, and model ID form the segment compatibility boundary; equal dimensions do not imply the same embedding space.

### 08.18.4. Query Projection

A Query may provide `embedding_vector` to bypass the embedder. Otherwise, the data plane may call the configured embedder. Native ANN hits are merged with lexical and canonical candidates and then assembled into Evidence, so they are not the complete QueryResponse semantics.

---

## 08.19. SDK Reference

### 08.19.1. Python

Path: `sdk/python`; package: `plasmod-sdk`; module: `plasmod_sdk`; class: `PlasmodClient`.

The main method:

```python
PlasmodClient(base_url=None, timeout=None)
ingest_event(event_id, agent_id, session_id, event_type, payload, **extra)
ingest_vectors(vectors, segment_id="", object_ids=None, index_type="", ...)
query(query_text, query_scope="global", top_k=10, **filters)
get_consistency_mode()
set_consistency_mode(mode)
get(path)
post(path, body)
```

The default address is resolved from `PLASMOD_URI`, then `PLASMOD_BASE_URL`, and finally `http://127.0.0.1:19530`. Timeout is read from `PLASMOD_HTTP_TIMEOUT`. The SDK calls `requests.raise_for_status()`, so non-success responses raise exceptions.

`ingest_event` currently constructs a compatible flat Event. Use generic `post()` or extend the SDK when the complete nested v0.4 shape is required.

### 08.19.2. Node.js

Path: `sdk/nodejs`. The current package is named `andb-sdk-node`, is marked private, and exports `AndbClient`. Its public surface primarily covers consistency-mode operations. The legacy naming is still under compatibility migration, so maturity is Partial.

### 08.19.3. SDK Compatibility Principles

The SDK should not alter the ID, scope and consistency semantics on its own. New methods must generate requests from the Go schema JSON tag and add
server-side contract tests; updating SDK examples alone is insufficient.

---

## 08.20. WAL Stream

### 08.20.1. Interface

The transport server registers `/v1/wal/stream` to stream WAL records from a requested position.
`src/internal/eventbackbone`:

```go
type WAL interface {
    Append(Event) (LSN, error)
    Scan(from LSN, fn func(Record) bool)
    LatestLSN() LSN
}
```

Implementations that propagate scan errors also implement `ErrorAwareWAL`.

### 08.20.2. Semantics

- LSN represents log order, not wall-clock time;
- A stream record is an Event fact, not a snapshot of a materialized object.
- Consumers must handle interruptions, duplicates, and checkpoint-based resume.
- Completing a scan does not imply that retrieval projection has completed.
- FileWAL corruption must return an explicit error; a truncated tail must not be treated as a normal EOF.

### 08.20.3. Security Boundaries

WAL records may contain raw payloads and cross-scope events. Expose this route only to trusted nodes and protect it with network identity, TLS, and least privilege. It is not a public change-data-capture API.
