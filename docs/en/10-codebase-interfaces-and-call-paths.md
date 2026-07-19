# 10. Codebase, Interface Implementations, and Call Paths

> Language: [中文](../10-codebase-interfaces-and-call-paths.md) | English

---

This chapter maps system concepts to packages, files, types, fields, constructors, methods, helpers, runtime state, and tests.

---

## 10.1. Architecture To Code Map

This page is a quick index. Use the [System Design Reference](14-implementation-status-gaps-and-claim-boundaries.md) for constructor, method, typed-I/O, state, and maturity details.

| Architecture concept | Primary code | Supporting code |
|---|---|---|
| Event source of truth | `eventbackbone.WAL` | `worker.Runtime`, consistency tracker |
| Canonical objects | `schemas/canonical.go` | `storage.ObjectStore`, coordinators |
| Materialization | `materialization.Service` | `worker/nodes` materializers |
| Retrieval projection | `dataplane` | `retrievalplane`, C++ bridge |
| Evidence | `evidence` | graph edges, versions, policies |
| Query planning | `semantic.QueryPlanner` | dataplane + evidence assembler |
| Consistency | `worker/consistency.Controller` | checkpoint, tracker, queues |
| Tiered storage | `storage/tiered.go` | Badger, S3/MinIO |
| HTTP boundary | `access.Gateway` | auth and visibility middleware |
| Process wiring | `app.BuildServer` | server lifecycle and ports |
| Runtime coordination | `worker.Runtime`, `worker/chain` | consistency controller, NodeManager, partially wired Orchestrator |
| Memory evolution | `schemas.MemoryManagementAlgorithm`, cognitive dispatcher | algorithm state, reflection, tiering, audit |
| Governance | policy/ShareContract stores and policy engine | Runtime filters, reflection, decision log |
| Collaboration | `CollaborationChain`, agent collaboration adapter | communication/conflict/microbatch workers; partial transaction integration |
| Reconciliation | replay/reindex/reset/purge fragments | no unified active Reconciliation Manager |
| Scheduling | consistency queues, Orchestrator, NodeManager, counter scheduler | no unified resource-aware Intelligent Scheduler |

When changing a concept, inspect both its primary and supporting code rather than changing only a schema or route.

Further verification:

- [Interface Implementation Registry](14-implementation-status-gaps-and-claim-boundaries.md)
- [API to Engine Matrix](14-implementation-status-gaps-and-claim-boundaries.md)
- [Claim and Test Boundary](14-implementation-status-gaps-and-claim-boundaries.md)

---

## 10.2. Bootstrap And Runtime Wiring

### 10.2.1. Entry

`src/cmd/server/main.go`:

1. Calls `app.BuildServer`.
2. Registers deferred shutdown.
3. Calls `app.RunServers`.
4. Handles startup errors and termination signals.

### 10.2.2. BuildServer order

`src/internal/app/bootstrap.go` assembles components in this order:

1. clock, bus, WAL;
2. Runtime Storage and cold store;
3. semantic, materialization, evidence;
4. embedder and DataPlane.
5. node manager and active coordinator hub;
6. worker Runtime;
7. consistency controller/checkpoint;
8. Gateway, transport and optional gRPC.

Dependency direction is visible in construction: Gateway does not create Badger or native indexes directly, and workers do not read environment variables to choose HTTP listeners.

### 10.2.3. Shutdown

Shutdown stops HTTP/gRPC admission, Gateway background managers, consistency workers, Runtime, storage, WAL, and related resources. Every new background goroutine must join server shutdown rather than relying on forced process exit.

---

## 10.3. Artifact Creation

```text
artifact/tool result Event
  -> object descriptor + payload
  -> ArtifactIDOrDefault
  -> Artifact record
  -> produced_by/derived_from edges
  -> ObjectVersion
  -> optional retrieval projection for indexable text
```

Large content can be extracted to S3, while Artifact retains URI/hash/mime/provenance.

---

## 10.4. Batch Search

Gateway decodes `VectorWarmBatchQueryRequest` for `/v1/query/batch`, validates row dimensions and lineage, and invokes native batch search on a registered warm segment. Internal transport also provides warm-batch, serial-batch, and raw-batch variants.

Native batch search is only responsible for vector candidates; the Go layer still needs to apply scope, policy, canonical supplement and evidence per query.
The route therefore returns `VectorWarmBatchQueryResponse`, not the general QueryResponse with canonical Evidence.

Batch optimization must maintain single-request equivalence meaning and return independent errors/states for each.

---

## 10.5. Delete And Purge

```text
admin delete/purge request
  -> admin auth
  -> validate selector and scope
  -> hardDeleteManager task (where applicable)
  -> canonical/index/audit/outbox deletion stages
  -> optional cold deletion
  -> task status/metrics
```

Multi-store cleaning is not a single distributed transaction. Processors store task progress to distinguish between routing, running, cancelling, failing, and completion.

---

## 10.6. Event Ingest

```text
POST /v1/ingest/events
  -> access.Gateway.handleIngest
  -> decode/normalize schemas.Event
  -> write concurrency semaphore
  -> worker.Runtime ingest
  -> consistency.Controller submit(mode)
  -> WAL.Append -> LSN
  -> materialize canonical projection
  -> retrieval projection
  -> tracker/checkpoint advance
  -> HTTP status mapping and response
```

Backpressure, paused, accepted-not-visible, and projection-failure states map to 503; deadline and cancellation map to 504 and 408.

---

## 10.7. Evidence Assembly

```text
ranked object IDs
  -> load canonical objects
  -> GraphEdgeStore traversal
  -> ObjectVersion lookup
  -> PolicyRecord/ShareContract filters
  -> derivation/provenance lookup
  -> GraphNode + ProofStep
  -> QueryResponse
```

Production visibility filtering runs after response construction and may remove chain and debug fields. Evidence assembly must not leak policy-rejected objects through Edge expansion.

---

## 10.8. Materialization

```text
Event + LSN
  -> materialization.Service.MaterializeEvent
  -> deterministic IDs
  -> Event/Memory/checkpoint State/optional Artifact/Edge/ObjectVersion records
  -> keyed state/object/tool specialized workers when targeted
  -> RuntimeStorage canonical projection
  -> retrieval projection
```

Within one Badger backend, object, edge, and version writes can share a transaction. S3 and native-index writes are outside that transaction; the consistency Controller records, retries, or reports those failures.

---

## 10.9. Memory Algorithm Dispatch

```text
/v1/internal/memory/<operation>
  -> Gateway request decode
  -> active provider/profile lookup
  -> AlgorithmDispatchWorker/agent SDK service
  -> canonical Memory/algorithm state update
  -> policy/lifecycle effects
  -> response
```

Provider implementations are replaceable, but cannot bypass canonical storage, scope, or Event provenance.
The algorithm state will not be automatically converted and will be handled by upgraded logic.

---

## 10.10. Query Execution

```text
POST /v1/query
  -> Gateway.handleQuery
  -> schemas.QueryRequest validation/defaults
  -> semantic QueryPlanner/operators
  -> DataPlane query
     -> Hot lexical/canonical candidates
     -> Warm lexical/vector segment candidates
     -> optional Cold candidates
  -> merge/filter/rank
  -> evidence assembler
  -> visibility middleware
  -> QueryResponse
```

Objects named by `target_object_ids` may be supplemented from canonical storage. `query_status` therefore distinguishes retrieval hits from canonical supplementation.
supplemented result.

---

## 10.11. Relation Creation

```text
Event causality/parents/relation descriptor
  -> validate source/destination/type
  -> deterministic Edge
  -> GraphEdgeStore
  -> source and destination edge indexes
  -> evidence traversal/proof trace
```

Edge writing must retain scope and provenance. When creating Edge directly, Gateway will not automatically prove the existence or semantics of two-sided objects.

---

## 10.12. Replay

```text
POST /v1/admin/replay
  -> authenticate admin request
  -> choose WAL range/checkpoint
  -> WAL.Scan
  -> Runtime reprocess Event
  -> canonical materialization
  -> retrieval projection
  -> tracker/checkpoint advance
  -> replay summary
```

FileWAL scan errors must propagate to the caller. Replay correctness depends on deterministic IDs and stable materializer invariants.

---

## 10.13. Server Startup

```text
cmd/server.main
  -> app.BuildServer
     -> storage.BuildRuntimeStorage
     -> materialization/evidence/semantic constructors
     -> dataplane/embedder/retrieval constructors
     -> coordinator.NewHub
     -> worker.NewRuntime
     -> consistency.NewController
     -> access.NewGateway
  -> app.RunServers
     -> unified or split HTTP
     -> optional gRPC/transport
  -> signal
     -> shutdown in dependency-safe order
```

Port resolution lives in `app/ports.go`; storage selection lives in `storage/factory.go`. Diagnose startup problems in this construction order before investigating the upstream control plane.

---

## 10.14. State Update

```text
state_update/tool_result Event
  -> normalize actor/session + state key/value
  -> StateMaterializationWorker
  -> state_<agent>_<key>
  -> version increment
  -> AgentState + ObjectVersion
  -> query by scope/key/latest version
```

Failure to extract a State key or version must prevent creation of an invalid empty State. Direct POST to `/v1/states` bypasses the Event chain and should be limited to management or migration.

---

## 10.15. Tier Promotion And Archive

```text
Warm object/segment
  -> admin export/archive
  -> encode canonical object + index metadata
  -> ColdObjectStore (S3/MinIO)
  -> cold diagnostics/key indexes

Cold query include_cold=true
  -> cold candidate/object read
  -> merge with hot/warm
  -> optional cache promotion
```

Warm should not be deleted before archiving is complete. Promotion/cache does not change the canonical object ID and provenance.

---

## 10.16. Component Ownership

### 10.16.1. Plasmod-owned active core

- top-level `internal/app`, `access`, `schemas`, `storage`, `materialization`, `evidence`, `semantic`;
- top-level lightweight coordinator files;
- `worker/consistency` and Agent-native workers;
- Data plane glue, SDK, configuration and running script;
- `cpp/retrieval` Plasmod retrieval composition.

### 10.16.2. Upstream/vendored/compatibility areas

- `src/internal/platformpkg`: upstream platform-code snapshot with a separate license.
- `src/internal/coordinator/controlplane`: substantial upstream-compatible control-plane code.
- `src/internal/eventbackbone/streamplane`: upstream stream and flush components.
- `cpp/vendor`: third-party native retrieval source.

The presence of these directories does not mean that `app.BuildServer` creates a complete distributed coordination cluster. Follow constructors and interface injection to determine active usage.

### 10.16.3. Modification Principles

1. Place new agent-native logic in a Plasmod-owned package.
2. Avoid rewriting imported snapshots unless the active integration requires it.
3. Preserve source, license, and local-modification notices for upstream code.
4. Keep active wrappers separate from upstream APIs.
5. When updating a dependency, verify its actual use from the bootstrap path.

---

## 10.17. Call Graph

### 10.17.1. Write

```text
Gateway.handleIngest
  -> Runtime.SubmitIngestContext
  -> consistency.Controller
  -> WAL.Append
  -> materialization.Service
  -> RuntimeStorage canonical projection
  -> DataPlane.Ingest
  -> tracker/checkpoint
```

### 10.17.2. Read

```text
Gateway.handleQuery
  -> Gateway.ServiceQueryContext
  -> semantic planner
  -> DataPlane Query hot/warm/(cold)
  -> canonical supplement/filter
  -> evidence assembler
  -> visibility middleware
```

### 10.17.3. Recovery

```text
Gateway.handleAdminReplay -> WAL.Scan -> Runtime processing -> projection -> checkpoint
```

---

## 10.18. Config Index

| Reader | Active inputs |
|---|---|
| `app/ports.go` | HTTP/gRPC address and size envs |
| `storage/factory.go` | storage mode, data dir, WAL/checkpoint, S3 envs |
| `worker/consistency` | mode, queue, workers, retry, timeout, checkpoint envs |
| `dataplane` | embedder/retrieval envs |
| memory provider loader | `configs/memory_tiering.yaml`, `configs/algorithm_*.yaml` |
| access middleware | APP_MODE, admin key |

`configs/app.yaml`, `storage.yaml`, `retrieval.yaml`, and `graph.yaml` do not currently form the complete configuration source for `BuildServer`.

---

## 10.19. Interface Implementation Map

This page maintains a compact map; see complete method, constructor and link status [Interface Implementation Registry](14-implementation-status-gaps-and-claim-boundaries.md).

| Interface | Implementations/adapters |
|---|---|
| `eventbackbone.WAL` | FileWAL, InMemoryWAL |
| `storage.RuntimeStorage` | memory runtime bundle, Badger-backed runtime bundle |
| `storage.ObjectStore` | in-memory store, Badger object store, tiered wrapper |
| `storage.ColdObjectStore` | in-memory cold store, S3/MinIO store |
| `dataplane.EmbeddingGenerator` | TF-IDF, configured ONNX/provider adapter |
| `retrievalplane.SearchService` | CGO native bridge, unavailable stub |
| `semantic.QueryPlanner` | active semantic planner implementation |
| `consistency.CheckpointStore` | file checkpoint, in-memory/test stores |
| `transport.RuntimeAPI` | worker Runtime through Gateway/service adapter |
| `agent.MemoryManager` | BaselineMemoryManager HTTP adapter |
| `agent.LLMProvider`, `agent.MASProvider` | core contract only; no production implementation |
| `schemas.MemoryManagementAlgorithm` | baseline, MemoryBank-style, Zep-style plugins |
| `worker.IngestWorker.Accept` | PipelineIngestWorker; defined and tested, not wired into active Runtime |
| `schemas.GraphExpander` | no active implementation; worker chain uses SubgraphExecutorWorker |

Construction and final selection occur in `app.BuildServer` and `storage/factory.go`, not only in DataPlane constructors.

---

## 10.20. Interface Index

For the complete interface-to-implementation mapping and wiring status, see the [Interface Implementation Registry](14-implementation-status-gaps-and-claim-boundaries.md).

| Domain | Interfaces |
|---|---|
| Event | WAL, ErrorAwareWAL, Bus, WatermarkReader, DerivationLogger |
| Storage | SegmentStore, IndexStore, ObjectStore, GraphEdgeStore, SnapshotVersionStore, RuntimeStorage |
| Query | DataPlane, QueryPlanner, SearchService |
| Embedding | EmbeddingGenerator, Generator, BatchEmbeddingGenerator |
| Runtime | IngestWorker, materialization workers, CheckpointStore |
| Transport | RuntimeAPI |
| Agent SDK | MemoryManager, LLMProvider, MASProvider |
| Memory algorithms | MemoryManagementAlgorithm |
| Optional capabilities | BatchEmbeddingGenerator, ColdHNSWSearcher, ColdTierDiagnosticsProvider |

Specific signatures for each package `contracts.go` Or an interface declaration file.

---

## 10.21. Package Dependency Graph

```text
cmd/server
  -> internal/app
      -> access -> worker/coordinator/schemas/storage
      -> transport -> worker runtime API
      -> worker -> eventbackbone/materialization/dataplane/storage
      -> semantic -> schemas
      -> evidence -> storage/schemas
      -> dataplane -> storage/retrievalplane/schemas
      -> storage -> schemas/eventbackbone
```

`schemas` is a low-level package and does not depend on Gateway. `app` is the composition root. The C++ library enters the Go graph only through the retrievalplane bridge.

---

## 10.22. Route Map

The route map is derived from `access.Gateway.RegisterMgmtRoutes`, `RegisterAPIRoutes`, and `transport.NewServer`.

| Registry | Prefixes |
|---|---|
| Management | `/healthz`, `/v1/system`, `/v1/admin` |
| Application | `/v1/ingest`, `/v1/query`, canonical collections, `/v1/traces` |
| Runtime internal | `/v1/internal/memory`, task, plan, MAS, tool, agent, session |
| Transport internal | `/v1/internal/rpc`, `/v1/wal/stream` |

See the [Chapter 8 Route Index](08-api-schema-and-sdk-reference.md) for route, method, and stability details.

---

## 10.23. Storage Prefix Map

```text
seg|       retrieval segments
idx|       index metadata
obj|*|     canonical objects
edg|       graph edges
ver|       object versions
pol|       policy records
ctr|       share contracts
kpeS|      edge source index
kpeD|      edge destination index
```

The source file is `src/internal/storage/badger_stores.go`. The complete object-prefix table appears in [Storage Key Layout](#1040-storage-key-layout).

---

## 10.24. Test Map

| Behavior | Primary test areas |
|---|---|
| Route/method/error/auth/visibility | `src/internal/access/*_test.go` |
| Bootstrap/config/ports | `src/internal/app/*_test.go`, `config/*_test.go` |
| Event normalization and schema | `src/internal/schemas/*_test.go` |
| WAL/Bus/derivation | `src/internal/eventbackbone/*_test.go` |
| Badger/memory/S3/tiering | `src/internal/storage/*_test.go` |
| Materialization IDs/objects/edges | `src/internal/materialization/*_test.go`, `worker/materialization/*_test.go` |
| Runtime and end-to-end query | `src/internal/worker/runtime*_test.go`, `e2e_query_test.go` |
| Consistency modes/checkpoint | `src/internal/worker/consistency/*_test.go` |
| Retrieval/embedding | `src/internal/dataplane/*_test.go`, `retrievalplane` tests |
| Evidence/query planner | `src/internal/evidence/*_test.go`, `semantic/*_test.go` |
| SDK | `sdk/python/tests`, `sdk/nodejs/src/index.test.js` |

---

## 10.25. Type Index

| Domain | Main types | Source |
|---|---|---|
| Event | Event, EventIdentity, EventActor, EventAccess, EventMaterialization, EventRetrieval | `schemas/dynamic_event.go` |
| Canonical | Agent, Session, Memory, State/AgentState, Artifact, Edge, ObjectVersion | `schemas/canonical.go` |
| Governance | Policy, PolicyRecord, ShareContract | `schemas/canonical.go` |
| Retrieval | RetrievalSegment, WarmVectorsIngestRequest, VectorWarmBatchQueryRequest | `schemas/*retrieval*`, `vector_batch_query.go` |
| Query | QueryRequest, QueryResponse, GraphExpandRequest/Response | `schemas/query.go` |
| Evidence | GraphNode, ProofStep, EvidenceSubgraph | `schemas/canonical.go`, evidence package |
| Runtime | MaterializationResult, IngestRecord, consistency status/checkpoint | materialization/dataplane/worker packages |

The exact fields are marked with struct and JSON tags.

---

## 10.26. Package Index

| Package | Active role | Entry files |
|---|---|---|
| `internal/app` | Compose dependencies and start/stop servers | `bootstrap.go`, `ports.go`, `run.go` |
| `internal/access` | HTTP Gateway, security and visibility | `gateway.go`, `admin_auth.go`, `visibility.go` |
| `internal/schemas` | Event, canonical, query schema | `dynamic_event.go`, `canonical.go`, `query.go` |
| `internal/eventbackbone` | WAL, Bus, derivation log | `contracts.go`, file/memory WAL |
| `internal/worker` | Event runtime, materialization and consistency | `runtime.go`, `consistency/*`, `nodes/*` |
| `internal/materialization` | Derive default Memory, checkpoint State, optional Artifact, Edge, Version, and retrieval records | `service.go` |
| `internal/storage` | RuntimeStorage, Badger, S3, tier | `contracts.go`, `factory.go`, `badger_stores.go` |
| `internal/dataplane` | embedding, warm/cold retrieval | `contracts.go`, `vectorstore.go`, `retrievalplane/*` |
| `internal/semantic` | query planning/operator | `operators.go` |
| `internal/evidence` | Evidence/proof assembly | assembler files |
| `internal/coordinator` | active object/index/policy coordinators | `hub.go`, top-level coordinator files |
| `internal/transport` | Component RPC and WAL stream | `server.go` |

The following sections describe these packages in more detail.

---

## 10.27. app And access

`internal/app` reads environment configuration, constructs dependencies, selects unified or split listeners, and owns shutdown ordering.

`internal/access` is the HTTP boundary:

- `gateway.go` registers routes and handlers;
- `admin_auth.go` protects only the admin prefix;
- `visibility.go` filters response fields according to `APP_MODE`;
- the write semaphore controls admission;
- the hard-delete manager runs background purge tasks.

Handlers should do protocol conversion and error mapping, and should not re-implement storage, materialization or query operations.

---

## 10.28. coordinator

The active top-level `hub.go` composes lightweight object, memory, index, policy, and related coordinators around storage and module registration.

`coordinator/controlplane` is an upstream-compatible control-plane snapshot containing metadata, data, query, and access-proxy components.
`BuildServer` integrates the active core components but does not create a complete distributed cluster by default.

Core coordination changes should extend the active Hub contract first. Modify the imported control-plane lifecycle only when the active bootstrap explicitly adopts it.

---

## 10.29. dataplane And retrieval

DataPlane connects embedders, lexical/vector stores, tiered retrieval and query candidates.

With the `retrieval` build tag, `retrievalplane/bridge.go` links the C++ library. The stub build preserves pure-Go canonical and lexical paths. The native layer owns index/search operations; Go retains scope, policy, fusion, and evidence semantics.

A precomputed query or Event vector bypasses embedding generation. Every vector path must validate dimension and embedding family.

---

## 10.30. eventbackbone

Active core includes WAL contract, FileWAL, InMemoryWAL, Bus, watermark, derivation/policy decision log.

FileWAL is stored at `<dataDir>/wal.log`. InMemoryWAL exists only for the lifetime of the process.
The derivation log is stored at `<dataDir>/derivation.log`.

`eventbackbone/streamplane` is an upstream-compatible snapshot containing stream coordinator, node, and flush-pipeline code. The default single-process `BuildServer` does not fully enable that subsystem.

---

## 10.31. evidence And semantic

`semantic.QueryPlanner` converts QueryRequest into retrieval and filtering operations. After DataPlane returns candidates, `evidence` reads canonical objects, Edges, Versions, Policies, and derivations and builds GraphNode, ProofStep, and provenance records.

Evidence is structured query output, not an independent source of truth. The assembler must respect scope and policy and must not infer a source from a missing Edge.

---

## 10.32. schemas

`internal/schemas` is the type layer shared by APIs and persistence:

- `dynamic_event.go`: nested v0.4 Event plus legacy-alias normalization.
- `canonical.go`: objects, relations, versions, policies, and share contracts;
- `constants.go`: object, event, edge, and memory types;
- `query.go`: query/filter/evidence response;
- Other files: retrieval, governance, memory algorithm and extension type.

The schema package must not depend on Gateway or a concrete Badger implementation. Before changing JSON tags, inspect SDKs, WAL fixtures, storage codecs, and handler tests.

---

## 10.33. storage

`storage/factory.go` selects the disk or memory Runtime bundle. Disk mode combines Badger canonical stores, FileWAL, checkpoints, and optional S3 cold storage.

`contracts.go` defines RuntimeStorage; `badger_stores.go` implements prefixes and key codecs; `tiered.go` combines Hot, Warm, and Cold; `s3store.go` manages cold objects and edge indexes.

Object, Edge, and Version records can be committed atomically through a canonical-projection transaction when they use the same Badger backend. Operations that span S3 or a native index are outside that transaction.

---

## 10.34. transport And SDKs

`internal/transport/server.go` exposes batch ingest, warm query, segment registration, and WAL streams through a narrow `RuntimeAPI` interface, preventing transport from depending on the concrete Runtime type.

The Python SDK is the more complete application client. The Node SDK retains legacy package/class naming and limited functionality. SDK changes require HTTP contract tests; internal transport must not be treated as a public SDK endpoint.

---

## 10.35. Upstream Compatibility Areas

The following directories should first read and modify their license/source map:

- `src/internal/platformpkg`;
- `src/internal/coordinator/controlplane`;
- `src/internal/eventbackbone/streamplane`;
- `cpp/vendor`.

Maintenance requirements: retain copyright/license; record updated versions; place the Plasmod adapter on the border layer; avoid unrelated formatting that leads to massive
diff; active startup, storage, retrieval and shutdown verification after the upgrade.

---

## 10.36. worker And materialization

`worker.Runtime` receives Events and coordinates WAL, materialization, and projection. `worker/consistency.Controller` provides modes, queues, slots, retries, Tracker, and checkpoints.

`materialization.Service` handles general Event-to-Memory, checkpoint State, optional Artifact, Edge, and Version derivation. `worker/nodes` also contains contracts and implementations for keyed State, object, tool-trace, index, proof, algorithm-dispatch, and maintenance workers.

When adding a materializer, define deterministic IDs, replay re-entry, canonical transaction boundaries, projection-failure behavior, and the conditions for advancing the Tracker.

---

## 10.37. Repository Overview

Plasmod's core repository contains Go runtime, C++ retrieval, SDK, configuration, container and engineering documents.

| Path | Responsibility |
|---|---|
| `src/cmd/server` | Executable program input |
| `src/internal` | Core Go runtime |
| `cpp` | C++17 native retrieval library |
| `sdk/python` | Python HTTP SDK |
| `sdk/nodejs` | Node compatible with SDK, less capability |
| `configs` | Memory tier/provider configuration and reference configuration |
| `scripts` | Create, launch, and secure scripts |
| `docker-compose*.yml` | Split/unified container topology |
| `docs` | Current core engineering documentation |

Source-of-truth priority is: Makefile, CMakeLists, go.mod, and code-level configuration parsing before comments, example YAML, or older README text.

---

## 10.38. Repository Tree

```text
Plasmod/
├── src/
│   ├── cmd/server/main.go
│   └── internal/
│       ├── access/          # HTTP gateway, auth, response visibility
│       ├── app/             # dependency wiring and server lifecycle
│       ├── coordinator/     # active lightweight coordinators + upstream snapshot
│       ├── dataplane/       # embedding, retrieval, tiered query
│       ├── eventbackbone/   # WAL/bus/derivation + upstream streamplane
│       ├── evidence/        # evidence assembly
│       ├── materialization/ # event to canonical objects
│       ├── schemas/         # wire and canonical types
│       ├── semantic/        # query planning/operators
│       ├── storage/         # Badger, memory, S3, tiering
│       ├── transport/       # internal RPC/WAL stream
│       └── worker/          # runtime, materializers, consistency
├── cpp/                     # native retrieval and vendored source
├── sdk/
├── configs/
├── scripts/
├── docs/
├── Makefile
├── Dockerfile
└── docker-compose*.yml
```

Generated runtime and build directories such as `.andb_data`, `.gocache`, `cpp/build`, and `bin` are not source modules.

---

## 10.39. Source Of Truth Map

| Question | Source of truth |
|---|---|
| Event sequence and replayable facts | FileWAL/InMemoryWAL records and LSN |
| Current canonical object | RuntimeStorage ObjectStore |
| Canonical relationships | GraphEdgeStore |
| Version history | SnapshotVersionStore |
| Governance decisions | PolicyStore/PolicyRecord |
| Sharing agreements | ShareContractStore |
| Physical retrieval candidates | Retrieval segments and indexes in the derived plane |
| Consistency progress | tracker + checkpoint |
| Cold archive | explicitly archived S3/MinIO keys |
| Effective process configuration | environment resolution + effective-config endpoint |

The index is not a canonical source of truth. It can be rebuilt from canonical state or WAL, but WAL cannot reconstruct the causal history of direct CRUD mutations that never entered the log.

---

## 10.40. Storage Key Layout

Badger store uses a stable prefix to distinguish logical tables:

| Prefix | Record |
|---|---|
| `seg\|` | retrieval segment |
| `idx\|` | index metadata |
| `obj\|agent\|` | Agent |
| `obj\|session\|` | Session |
| `obj\|memory\|` | Memory |
| `obj\|state\|` | AgentState |
| `obj\|artifact\|` | Artifact |
| `obj\|event\|` | Event |
| `obj\|user\|` | User |
| `edg\|` | Edge |
| `ver\|` | ObjectVersion |
| `pol\|` | PolicyRecord |
| `ctr\|` | ShareContract |
| `kpeS\|` | source-oriented edge index |
| `kpeD\|` | destination-oriented edge index |

Definitions live in `src/internal/storage/badger_stores.go`. Some algorithm, audit, and outbox records use separate namespaces and must be accessed through their store APIs.

Changing a key prefix makes old data invisible to the new reader. Use a dual-read/dual-write transition or an offline conversion; never release a direct constant replacement without migration.
