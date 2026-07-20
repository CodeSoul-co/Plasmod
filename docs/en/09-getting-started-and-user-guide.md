# 09. Installation, Startup, and User Guide

> Language: [中文](../09-getting-started-and-user-guide.md) | English

---

This chapter covers environment setup, builds, service startup, the first Event and query, core user operations, verification, shutdown, and cleanup.

---

## 09.1. First Event And Query

The following example uses Dynamic Event v0.4 and sends the Event through the WAL-backed ingest path.

### 09.1.1. Ingest an Event

```bash
curl -fsS -X POST http://127.0.0.1:8080/v1/ingest/events \
  -H 'Content-Type: application/json' \
  -d '{
    "schema_version": "plasmod.dynamic_event.v0.4",
    "identity": {
      "event_id": "evt_quickstart_001",
      "tenant_id": "tenant-quickstart",
      "workspace_id": "workspace-quickstart"
    },
    "actor": {
      "agent_id": "agent-quickstart",
      "session_id": "session-quickstart"
    },
    "time": {
      "event_time": 1767225600000,
      "logical_ts": 1
    },
    "event": {
      "event_type": "user_message",
      "importance": 0.8
    },
    "object": {
      "object_type": "memory"
    },
    "access": {
      "consistency": "strict",
      "visibility": "workspace"
    },
    "materialization": {
      "enabled": true,
      "targets": ["memory", "object_version"]
    },
    "retrieval": {
      "index_text": "The user prefers dark mode",
      "has_embedding": false
    },
    "payload": {
      "text": "The user prefers dark mode."
    }
  }'
```

The default Memory ID is `mem_` plus `event_id`, so this Event produces `mem_evt_quickstart_001`. Do not assume that `object.object_id` overrides this rule for Memory; explicit object IDs are honored by the Artifact path through `ArtifactIDOrDefault`.

With `has_embedding=false` and `index_text` present, the Event skips vector indexing but still enters canonical storage and lexical retrieval. This is appropriate for installation verification without an external embedder.

### 09.1.2. Query an Object by Exact Identifier

```bash
curl -fsS -X POST http://127.0.0.1:8080/v1/query \
  -H 'Content-Type: application/json' \
  -d '{
    "query_text": "dark mode preference",
    "tenant_id": "tenant-quickstart",
    "workspace_id": "workspace-quickstart",
    "session_id": "session-quickstart",
    "agent_id": "agent-quickstart",
    "target_object_ids": ["mem_evt_quickstart_001"],
    "object_types": ["memory"],
    "top_k": 10,
    "response_mode": "structured_evidence"
  }'
```

The main parts of the response include:

- `objects`: the canonical objects returned by the query.
- `edges`, `versions`, `provenance`: the evidence associated with those objects.
- `proof_trace`: the explanatory retrieval and relationship steps.
- `retrieval_summary`: tiers, modes, candidate counts, and applied filters.
- `query_status`: whether retrieval completed, returned no hits, or used canonical supplementation.

### 09.1.3. Query a Trace

```bash
curl -fsS http://127.0.0.1:8080/v1/traces/mem_evt_quickstart_001
```

The Trace API is not the same as the original application log.

---

## 09.2. Install From Source

### 09.2.1. Install Dependencies

In the repository root directory, execute:

```bash
go mod download
```

The Python SDK is a standalone editable package:

```bash
python3 -m venv .venv
source .venv/bin/activate
pip install -e ./sdk/python
```

### 09.2.2. Minimal Startup

```bash
PLASMOD_STORAGE=disk \
PLASMOD_DATA_DIR=.andb_data \
PLASMOD_EMBEDDER=tfidf \
PLASMOD_GRPC_ENABLED=0 \
go run ./src/cmd/server
```

The settings mean:

- `disk` selects Badger persistence and the file-backed WAL.
- `.andb_data` stores canonical objects, edges, versions, WAL, and consistency checkpoints.
- `tfidf` avoids an external embedding dependency.
- disabling gRPC leaves unified HTTP on `127.0.0.1:8080`.

Do not assume that `configs/storage.yaml` selects the active backend. The current `app.BuildServer` resolves storage primarily from environment variables.

### 09.2.3. Enable Native Retrieval

```bash
make cpp
make build
./bin/plasmod
```

`make cpp` currently requests FAISS support. Follow [Chapter 11: Dependencies, Build, and Development](11-dependencies-build-and-development.md) and keep native/CGO failures separate from Go object-storage failures.

### 09.2.4. Use the Development Script

```bash
cp .env.example .env
make dev
```

`make dev` invokes `scripts/dev_up.sh`, reads `.env`, and enables the `retrieval` build tag when the native library is present. `.env.example` is a template; confirm effective configuration through startup logs and the admin configuration endpoint.

---

## 09.3. Prerequisites

### 09.3.1. Use the Go Data Path Only

- Go `1.25.x`, as specified by `go.mod`.
- Git;
- macOS or Linux;
- a writable data directory.

This path uses the Go retrieval stub and does not require a C++ build. It is suitable for verifying canonical objects, WAL, queries, and evidence-chain behavior.

### 09.3.2. Enable Native Retrieval

We also need:

- CMake `3.20+`;
- Compiler that supports C++17;
- OpenMP;
- FAISS and its transitive dependencies when the corresponding option is enabled;
- an enabled CGO toolchain.

`cpp/CMakeLists.txt` defines the native library. `src/internal/dataplane/retrievalplane/bridge.go` uses the `retrieval` build tag for the CGO bridge. When `cpp/build/libplasmod_retrieval.dylib` or the corresponding `.so` exists, `make build` adds the tag automatically.

### 09.3.3. Docker Path

- Docker Desktop or compatible Docker Engine;
- Docker Compose v2;
- At least enough disk space to build mirrors and save Badger/MinIO data volumes.

### 09.3.4. Pre-start Checks

```bash
go version
docker version
docker compose version
cmake --version
```

Skip only the checks for components that are deliberately disabled, such as Docker or native retrieval.

---

## 09.4. Python SDK Quickstart

### 09.4.1. Installation

```bash
python3 -m venv .venv
source .venv/bin/activate
pip install -e ./sdk/python
```

The package is `plasmod-sdk`, the import module is `plasmod_sdk`, and the client class is `PlasmodClient`.

### 09.4.2. Ingest and Query

```python
from plasmod_sdk import PlasmodClient

client = PlasmodClient(base_url="http://127.0.0.1:8080")

client.ingest_event(
    event_id="evt_python_001",
    agent_id="agent-python",
    session_id="session-python",
    event_type="user_message",
    payload={"text": "The user prefers concise answers."},
    tenant_id="tenant-quickstart",
    workspace_id="workspace-quickstart",
    access={"consistency": "strict", "visibility": "workspace"},
)

result = client.query(
    query_text="answer style preference",
    tenant_id="tenant-quickstart",
    workspace_id="workspace-quickstart",
    session_id="session-python",
    agent_id="agent-python",
    top_k=10,
)
print(result)
```

The SDK is an HTTP client. It does not run Plasmod locally or create the server's persistence directories.
[SDK Reference in Chapter 8](08-api-schema-and-sdk-reference.md).

---

## 09.5. Quickstart

### 09.5.1. Startup

```bash
PLASMOD_STORAGE=disk PLASMOD_DATA_DIR=.andb_data \
PLASMOD_EMBEDDER=tfidf PLASMOD_GRPC_ENABLED=0 \
go run ./src/cmd/server
```

### 09.5.2. Check Service Health

```bash
curl -fsS http://127.0.0.1:8080/healthz
```

### 09.5.3. Ingest and Query

Run the Event and query commands from [First Event and Query](#091-first-event-and-query). When a strict write succeeds, `mem_evt_quickstart_001` has passed the current canonical visibility gate.

### 09.5.4. Inspect a Trace

```bash
curl -fsS http://127.0.0.1:8080/v1/traces/mem_evt_quickstart_001
```

See [Verify Installation](#098-verify-installation) for the complete checks.

---

## 09.6. Run With Docker

### 09.6.1. Split-port Mode

```bash
docker compose up -d --build
docker compose ps
```

The default service input is:

- Data API: `http://127.0.0.1:19530`.
- Management API: `http://127.0.0.1:9091`.
- gRPC: `127.0.0.1:19531`;
- MinIO API: `http://127.0.0.1:9000`;
- MinIO Console: `http://127.0.0.1:9001`.

Health checks:

```bash
curl -fsS http://127.0.0.1:9091/healthz
```

### 09.6.2. Unified HTTP Mode

```bash
docker compose -f docker-compose.unified.yml up -d --build
curl -fsS http://127.0.0.1:8080/healthz
```

Unified mode registers management and data routing on an HTTP monitor, while gRPC still uses separate ports.

### 09.6.3. Data Persistence

Compose mounts the Plasmod data directory at `/data`; MinIO uses a separate volume. Removing containers does not remove volumes. `docker compose down -v` explicitly removes the Compose volumes and their persistent data.

### 09.6.4. Administrative API Authentication

Production-like deployments must set `PLASMOD_ADMIN_API_KEY`. Use it in requests as follows:

```bash
curl -H "X-Admin-Key: $PLASMOD_ADMIN_API_KEY" \
  http://127.0.0.1:9091/v1/admin/config/effective
```

When the management key is not set, the current implementation only records warnings and will not automatically reject management requests, so the default Compose configuration cannot be used as a security configuration.

---

## 09.7. Stop, Reset And Cleanup

### 09.7.1. Start from Source

When running in the foreground, press `Ctrl-C`. `app.RunServers` initiates HTTP/gRPC shutdown, waits for active work, and closes resources.

Confirmation process has stopped:

```bash
pgrep -af 'plasmod|src/cmd/server'
```

When you need to clear your local data, stop the service and delete the directory you have explicitly set:

```bash
rm -rf .andb_data
```

The operation deletes WAL, canonical records, versions and checkpoints, which cannot be restored.

### 09.7.2. Docker

Stop but keep the data:

```bash
docker compose down
```

Compose volumes is deleted:

```bash
docker compose down -v
```

Do not manually delete Badger files while the service is still writing. For production data, prioritize backup, snapshot or controlled purge; see
[Backup and Restoration Statement of Chapter 12](12-deployment-operations-and-troubleshooting.md).

---

## 09.8. Verify Installation

The following sequence of checks can distinguish between process, persistence, materialization and retrieval problems.

### 09.8.1. Service Liveness

```bash
curl -fsS http://127.0.0.1:8080/healthz
```

Docker split mode changes the address to `http://127.0.0.1:9091/healthz`.

### 09.8.2. Successful Event Ingestion

Run [First Event and Query](#091-first-event-and-query). If the response is not 2xx, inspect the response body first.
Strictly consistent overtime, projection failure and common test errors use different status codes.

### 09.8.3. Object Is Queryable

The query response should contain an object with ID `mem_evt_quickstart_001`. Including `target_object_ids` verifies canonical selection even when vector retrieval returns no candidates.

### 09.8.4. WAL and Data Directories Exist

```bash
test -f .andb_data/wal.log
test -d .andb_data
```

In disk mode, consistency checkpoints and Badger data are also stored.

### 09.8.5. Effective Configuration

After setting up the management key:

```bash
curl -fsS -H "X-Admin-Key: $PLASMOD_ADMIN_API_KEY" \
  http://127.0.0.1:8080/v1/admin/config/effective
```

This endpoint reports the effective configuration of the running process and is therefore more authoritative than a static YAML file.

### 09.8.6. Data Remains Readable After Restart

Keep `PLASMOD_DATA_DIR` unchanged, stop the service cleanly, restart with the same command, and repeat the Query and Trace checks. Objects, versions, and WAL progress should remain; `wal.log` and `LatestLSN` must not reset. Data loss after restart is expected with `PLASMOD_STORAGE=memory`.

---

## 09.9. Deletion And Purge

Before deleting data, decide whether the requirement is query hiding, logical deletion, index removal, or physical purge.

### 09.9.1. Administrative Entry Points

The Admin API provides:

- dataset delete, purge, and purge-task status;
- Memory delete or purge by source;
- complete data wipe;
- S3 Cold purge;
- rollback-related operations.

These routes live under `/v1/admin/*` and should be protected with `PLASMOD_ADMIN_API_KEY`.

### 09.9.2. Semantic Differences

- Delete: normally removes the object from standard query results while retaining selected audit records.
- Purge: physically removes canonical, index, or Cold records according to the operation.
- Wipe: clears broad runtime state for development or controlled recovery.
- Rollback: restores a projection or State; it does not delete the original Event.

### 09.9.3. Operation Order

1. Query the target scope and count.
2. Create a backup or snapshot.
3. Enable admin authentication and request logging.
4. Perform logical deletion first where appropriate.
5. Verify that normal queries no longer expose the targets.
6. Purge only when required by policy or capacity constraints.
7. Verify object, Edge, Version, index, and Cold-key cleanup.

There is currently no single distributed transaction across Badger, S3 and all recovery backends. Large-scale purge requires task state verification of completeness.

---

## 09.10. Ingest Events

### 09.10.1. Purpose

`POST /v1/ingest/events` records an agent-runtime change as a durable Dynamic Event v0.4 fact and drives materialization of Memory, State, Artifact, Edge, and ObjectVersion records.

### 09.10.2. Minimal Input

Provide at least:

- `schema_version`;
- `identity.event_id`, `tenant_id`, `workspace_id`;
- `actor.agent_id`, `session_id`;
- `time.event_time`, `logical_ts`;
- `event.event_type`;
- `access.consistency`, `visibility`;
- `materialization.enabled`, `targets`;
- `payload` or `data`.

Field definitions come from `src/internal/schemas/dynamic_event.go`. The compatibility layer accepts legacy flat fields, but new clients should emit the nested v0.4 form shown above.

### 09.10.3. Write Stages

1. Gateway decodes and validates Event.
2. Runtime adds Event to WAL and gets LSN.
3. The consistency Controller schedules work according to strict, bounded, or eventual mode.
4. Materializer writes canonical objects, edges and versions.
5. Retrieval projection updates query structures; Event configuration may skip vector projection.
6. Tracker updates committed/projected/visible status and returns results.

### 09.10.4. Consistency Selection

- `strict`: wait for this Event to reach the strict visibility threshold.
- `bounded`: project asynchronously within `freshness_sla_ms` or the configured bound.
- `eventual`: accept the Event and project asynchronously without an immediate freshness guarantee.

Event-level `access.consistency` overrides the service default. A successful response means the selected Plasmod visibility gate was satisfied; it does not prove completion in downstream external systems.

### 09.10.5. Embedding Selection

- Precomputed: provide the embedding vector and dimension.
- Server-generated: provide `retrieval.index_text` and set `has_embedding=true`.
- Canonical plus lexical only: provide `index_text` and set `has_embedding=false`.
- Disable vector indexing globally with `PLASMOD_SKIP_VECTOR_INDEX=1`.

The pre-calculated vector must be consistent with the dimension and semantics of the configured retrieval space. Plasmod will not prove that the same dimensional vectors produced by different models are comparable.

### 09.10.6. Error Handling

- Validation failure: correct the fields before retrying.
- `503`: write may be accepted but not visible, or projection may have failed; inspect status and recover using the same `event_id`.
- `504`: the consistency wait timed out; inspect object/trace status before retrying.
- `408`: the request context was canceled; treat submission outcome as unknown until checked.
- Other 5xx: Check WAL, Badger, Embedding and native retrieval logs.

The current interface has no `Idempotency-Key` protocol. `event_id` is the primary application deduplication and trace identifier, but callers must inspect trace and WAL status before deciding to resubmit.

---

## 09.11. Manage Agent State

### 09.11.1. Data Model

The canonical type is `schemas.AgentState`, with compatibility alias `State`. The canonical object-type constant is `agent_state`; `state` remains an alias. Fields identify the agent, state key, value, version, and timestamps.

### 09.11.2. Event-driven Updates

State changes should be submitted as `state_update`, `state_change`, or a related tool-result Event with a State key and value. Both primary Runtime materialization and the specialized State worker derive:

```text
state_<sha256-prefix(tenant, workspace, agent, session, state_key)>
```

This produces a stable, scope-safe ID. The next version is read from canonical State/ObjectVersion stores. Retrying the same mutation event does not increment the version, and replaying an older event does not roll current State backward.

When no explicit State key exists, an ordinary Event updates the scope's stable `last_memory_id` State to the default Memory ID. It does not create a new checkpoint ID for every Event. `materialization.targets` is not yet a universal hard gate for this default State projection.

### 09.11.3. Direct State API

A direct `POST /v1/states` is not equivalent to an Event-driven update: it does not automatically create the source Event, WAL causality, or derived relationships.

### 09.11.4. Query the Latest State

When querying latest State, provide tenant, workspace, agent, session, and State key and use the same `CanonicalStateID` algorithm. Use `/v1/query` with a trust-bound `requester_agent_id` and roles when governance filtering is required.

### 09.11.5. Current Limitations

- There is no cross-process transaction lock that serializes arbitrary direct CRUD updates.
- `state` and `agent_state` are compatibility names; new clients should use canonical `agent_state`.

---

## 09.12. Manage Agents And Sessions

### 09.12.1. Agent

Canonical `schemas.Agent` stores Agent ID, name, type, capabilities, runtime status, tenant, and workspace information.

The HTTP collection is `/v1/agents`: GET reads or lists, and POST directly stores canonical registry records. If Agent creation must be replayable business history, represent it as an Event as well or use Event ingest as the primary path.

### 09.12.2. Session

`schemas.Session` groups Events, memories, States, and Artifacts within one session scope. The collection endpoint is `/v1/sessions`.

Recommended practices:

- Session ID is generated and maintained stable during upstream runtime;
- Each event carries the same tenant/workspace/agent/session scope;
- Do not use Session ID as a substitute for tenant isolation;
- The end of the session can be updated, but do not physically remove the chain of evidence immediately.

### 09.12.3. Query Scope

`QueryRequest` can filter by `tenant_id`, `workspace_id`, `agent_id`, and `session_id` together. Begin with the strongest isolation boundary, then narrow to Agent and Session.

### 09.12.4. Lifecycle Boundaries

Agent and Session canonical CRUD does not currently provide unified ETags, optimistic locking, or cursor pagination. Use Event plus ObjectVersion when an auditable update chain is required.

---

## 09.13. Manage Artifacts

Artifact represents a program, report, code modification, tool product, or other agent output that can be independently addressed.
`src/internal/schemas/canonical.go`.

### 09.13.1. Create

Create an Artifact through Event ingest when replay and provenance are required:

1. Set `event.event_type` to the relevant artifact or tool-result type.
2. Set `object.object_type` to `artifact`.
3. Provide a stable business ID in `object.object_id` when available.
4. Set `materialization.targets` to `artifact` and `object_version` as intent metadata.
5. Provide parents/dependencies in causality;
6. Put content or an external-location descriptor in the payload.

Current Artifact creation is driven by artifact-like object/Event types, tool Events, or URI/body payload conditions. `materialization.targets` alone does not force arbitrary creation. `ArtifactIDOrDefault` prefers a valid explicit object ID; its fallback differs from the Memory ID rule.

### 09.13.2. Direct Access

`/v1/artifacts` provides canonical CRUD for migration and management. Callers using this direct path must create related Edge, Version, and Event records themselves; otherwise trace may see an isolated object.

### 09.13.3. Update

Do not overwrite provenance when updating an Artifact. Create a new Event and ObjectVersion, and connect versions with `updates`, `derived_from`, or `supersedes` Edges as appropriate.

### 09.13.4. Content Storage

Plasmod can store structured payloads and object metadata; large files should usually be stored in object storage, while Artifact saves URIs, hashes,
media type and provenance. The file upload lifecycle should not depend solely on the search index.

---

## 09.14. Memory Lifecycle

Plasmod breaks down the lifecycle of Memory into writing, retrieving, compressing/summarizing, decreasing, sharing, conflict handling, layering and deleting.

### 09.14.1. Creation and Classification

Event materialization creates `mem_<event_id>` by default. Memory type can represent episodic, semantic, procedural, social, reflective, factual, profile, affective-state, or preference/constraint memory.

### 09.14.2. Algorithm Operations

Internal runtime routes include recall, ingest, compress, summarize, decay, share, conflict resolve and stale markup.
These `/v1/internal/memory/*` interfaces are intended for controlled integration. Their stability is Experimental, and they must not be exposed directly to untrusted networks.

Algorithms are configured by `configs/memory_tiering.yaml` and `configs/algorithm_*.yaml`. They may change scores, importance, or lifecycle decisions, while canonical Memory, Edge, and Event remain database records.

### 09.14.3. Tier Transitions

- Hot: limited cache within the process;
- Warm: main canonical store and retrieval layer;
- Cold: explicitly archived to S3/MinIO or memory cold store.

A write does not automatically copy the object to every tier.

### 09.14.4. Delete

Logical deletion differs from source/dataset deletion and physical purge. When auditing or replaying, make sure to keep WAL, Edge,
Version and scope of cold archives.

---

## 09.15. Query Memories

### 09.15.1. Query Entry Point

- General query: `POST /v1/query`.
- Warm-segment precomputed-vector batch query: `POST /v1/query/batch`.
- Direct Memory collection access: `/v1/memory`.
- Internal runtime recall: `/v1/internal/memory/recall` (Experimental).

### 09.15.2. Primary Filter Dimensions

`schemas.QueryRequest` supports:

- tenant, workspace, agent, session scope;
- `object_types`, `memory_types`, `edge_types`;
- time window;
- `target_object_ids`;
- relation constraints;
- dataset/source/batch selectors;
- access, materialization, runtime filters;
- `top_k`, `response_mode`;
- `include_cold`;
- precomputed query embedding.

These general filters apply only to `/v1/query`. `/v1/query/batch` uses the separate `VectorWarmBatchQueryRequest` and searches a warm segment directly without automatically assembling canonical Evidence.

### 09.15.3. Execution Stages

1. Hot cache candidate;
2. Warm canonical/lexical/vector candidate;
3. Read Cold only when `include_cold=true`.
4. Combine, filter and sort;
5. Evidence assembler adds edges, versions, policies, provenance and proof trace.

Cold data is therefore not searched by default. Callers that require archived data must request it explicitly and accept the additional I/O.

### 09.15.4. Response Modes

`QueryResponse` can include:

- `objects`, `nodes`;
- `edges`, `versions`, `provenance`;
- `filters`;
- `proof_trace`, `chain_traces`;
- `retrieval_summary`;
- `query_status` and `hint`.

Clients must inspect `query_status`; HTTP 200 alone does not prove that every retrieval and evidence stage participated. Production visibility middleware may remove internal debug fields.

### 09.15.5. Latest Memory

Latest-Memory queries must define scope, memory type, and time ordering. The highest vector-similarity result is not necessarily the newest object.

---

## 09.16. Query Relations

### 09.16.1. Edge Model

`schemas.Edge` is a canonical relationship that connects a source and destination and carries edge type, scope, time, and optional properties. Common types include:

- `caused_by`, `derived_from`;
- `summarizes`, `updates`;
- `uses_tool`, `tool_produces`;
- `belongs_to_session`, `owned_by_agent`, `shared_with`.

### 09.16.2. Write

Event causality, parent references, relation types, and materialization settings can generate Edges. `/v1/edges` also supports direct canonical management, but callers that write Edges directly must validate both endpoint objects and their scope.

### 09.16.3. Query

It can be:

- Use `/v1/edges` to read known relationships.
- Use `edge_types` in `/v1/query` to constrain relationships.
- Use `/v1/traces/{id}` to request the evidence graph around an object.

### 09.16.4. Interpret Results

An Edge is a recorded claim, not proof that the claim is correct. Interpret `supports` and `contradicts` with their source objects, ObjectVersion, PolicyRecord, and ProofStep context. Evidence assembly structures these records; it does not independently verify real-world truth.

---

## 09.17. Replay

Replay reads Events from WAL and re-executes runtime materialization and projection to rebuild derived state after an interruption.

### 09.17.1. Prerequisites

- Use `PLASMOD_STORAGE=disk`;
- `<dataDir>/wal.log` is readable and intact;
- schemas and materializers are compatible with the records;
- target canonical and retrieval backends are writable;
- the replay range has an explicit LSN boundary.

### 09.17.2. Entry Point

The management entry point is `/v1/admin/replay`. Internal stream transport also supports node-to-node WAL transfer; it is not an application-facing event-subscription API.

### 09.17.3. Correctness Checks

1. Record `LatestLSN`, the visible checkpoint, and target-object status before replay.
2. Pause conflicting writes or define how they will be reconciled.
3. Replay the selected range.
4. Verify object, Edge, ObjectVersion, and Trace counts.
5. Verify the consistency checkpoint.
6. Query the target and confirm the expected version.

Replay cannot reconstruct direct CRUD history that never entered WAL, nor can it restore payloads that were physically deleted and are no longer retained in WAL.

---

## 09.18. Runtime Modes

Plasmod exposes several independent runtime modes; they must not be collapsed into one generic "production mode."

### 09.18.1. APP_MODE

- `test`: responses may include `_debug` and internal fields.
- `prod`: visibility middleware removes debug, raw, log, chain-trace, and related internal fields.

This setting changes the response surface; it does not enable TLS or end-user authentication.

### 09.18.2. Storage Mode

- `disk`: Badger plus FileWAL, recoverable across process restarts.
- `memory`: in-process stores plus InMemoryWAL, intended for tests and temporary runs.

### 09.18.3. Consistency Mode

- `strict_visible`/`strict`;
- `bounded_staleness`/`bounded`;
- `eventual_visibility`/`eventual`.

The admin API can change the service default, and individual Events can override it.

### 09.18.4. Governance and Memory Provider Mode

Use the Admin API to inspect or adjust governance mode, Runtime mode, memory-provider mode, and provider health.
Changing coordination and strategic behavior without migrating existing data formats.

### 09.18.5. Unified and Split Server

Unified mode uses one HTTP listener; split mode separates management and data listeners. Both use the same core Runtime and are not independent databases.

---

## 09.19. Sharing And Visibility

### 09.19.1. Scope

Both Event and Query carry tenant, workspace, agent, and session scope. `access.visibility` expresses the object's visibility level.
ShareContract and PolicyRecord express clearer shared/governance decisions.

### 09.19.2. Recommended Rules

1. Tenant is the highest isolation boundary.
2. workspace indicates the scope of collaboration;
3. Agent and session scopes narrow access within a workspace.
4. Private memory should not be isolated by name only;
5. Shared memory should be created by a ShareContract or an auditable Edge;
6. Queries must have scope and not rely on retrieval-sensitive results.

### 09.19.3. ShareContract

`schemas.ShareContract` stores provider, recipient, subject scope, authorization conditions, and lifecycle. Writing directly to `/v1/share-contracts` only persists the canonical contract; applications must still ensure the relevant policy is enforced on query paths.

### 09.19.4. Current Security Boundary

Plasmod has a scope filter, policy records and admin key, but not a complete IAM product.
End-user authentication middleware is not built in. Production deployments must provide TLS, identity verification, tenant binding, and rate limiting through a trusted gateway.

---

## 09.20. Tiered Storage

### 09.20.1. Three-tier Responsibilities

| Layer | Current Implementation | Useful |
|---|---|---|
| Hot | `HotObjectCache` | Process-local access to recently active objects; default capacity 2,000 |
| Warm | `storage.ObjectStore` plus retrieval indexes | Normal persistence and query |
| Cold | S3/MinIO or in-memory cold store | Explicit archive and historical reads |

### 09.20.2. Write Behavior

A normal Event writes to Warm and may update Hot; it is not automatically copied to Cold. Archival requires an explicit lifecycle or administrative operation.

### 09.20.3. Query Behavior

Queries search Hot and Warm by default. Set `include_cold=true` when historical completeness requires reading Cold and the additional I/O is acceptable.

### 09.20.4. S3 Key Space

Cold backend distinguishes between memories, embeddings, agents, states, artifacts, edges, and edge indexes under the configuration prefix.
Do not change the key arbitrarily by external programs; the consistency of the JSON object and the index key is maintained by Plasmod.

### 09.20.5. Configuration

MinIO/S3 endpoint, bucket, credentials, TLS, and prefix are resolved by environment variables and the storage factory. The presence or reachability of `configs/storage.yaml` or an S3 endpoint does not prove that the active process uses S3; inspect effective configuration and startup logs.

---

## 09.21. Trace And Provenance

### 09.21.1. Trace API

```text
GET /v1/traces/{object_id}
```

The interface collects canonical object, Edge, ObjectVersion, PolicyRecord, provenance and proof steps around the target object.

### 09.21.2. Difference Between Provenance and Application Logs

- Provenance: how objects are generated by an Event, parent object, and derivative relationship;
- Proof trace: which steps can be explained in this assembly of evidence;
- Chain trace: selective internal tracking in the query link;
- Application logs: Process running events, not canonical database records.

In production mode, visibility middleware removes `debug`, `raw`, `log`, and `chain_traces`. Test-mode response fields must not become a production client contract.

### 09.21.3. Prerequisites for Complete Tracing

1. Writing from Event Entry through WAL;
2. The event carries a stable ID and a parent dependence;
3. Materialization created ObjectVersion and Edge;
4. Remove the strategy to retain the necessary tombstone/provenance;
5. Scope allows current inquiries to see both ends of the relationship.

When directly storing an isolated canonical object, the Trace API can only return the existing records, which cannot be inferred from non-existent history.
