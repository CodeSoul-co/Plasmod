# 01. Project Overview, Requirements, and System Boundaries

> Language: [中文](../01-project-overview-and-requirements.md) | English

---

This chapter defines the problem Plasmod solves, its intended users, supported use cases, capability boundaries, terminology, and traceable requirements. All implementation claims are grounded in the active bootstrap path and repository tests.

---

## 01.1. Capability Map

Status labels describe the current bootstrap path and test coverage. They are not long-term compatibility guarantees.

| Capability | User Entry Point | Core Objects | Primary Implementation | Status |
|---|---|---|---|---|
| Ingest agent events | `POST /v1/ingest/events`, gRPC | Event | `access.Gateway`, `worker.Runtime`, WAL | Implemented |
| Persist and scan the WAL | Runtime/Admin | Event, LSN | `eventbackbone.FileWAL` / `InMemoryWAL` | Implemented |
| Materialize events | Event ingest | Memory/State/Artifact/Edge/Version | `materialization.Service`, worker materializers | Implemented |
| Query memories and canonical objects | `POST /v1/query` | Memory and related objects | planner, TieredDataPlane, Assembler | Implemented |
| Query the latest state | `/v1/states` or query selector | AgentState | ObjectStore, state materializer | Partial |
| Artifact management | `/v1/artifacts`, Event ingest | Artifact | object materializer, ObjectStore | Implemented |
| Relation/Edge | `/v1/edges`, query/trace | Edge | GraphEdgeStore, evidence assembler | Implemented |
| Trace and provenance | `GET /v1/traces/{id}` | Edge/Version/derivation | Gateway, Evidence, logs | Implemented |
| Strict/Bounded/Eventual | Event/query + admin mode | LSN/visibility | consistency.Controller/Tracker | Implemented |
| Replay | `/v1/admin/replay` | Event/Object | Runtime + WAL scan | Implemented |
| Rollback | `/v1/admin/rollback` | Version/Object | Gateway/Storage | Partial |
| Hot/Warm/Cold memory | config/query | Memory | TieredObjectStore/TieredDataPlane | Implemented |
| S3/MinIO cold tier | env + compose/admin | archived objects | S3ColdStore | Conditional |
| Hybrid retrieval | query | retrieval projection | lexical + CGO ANN + RRF | Conditional |
| Warm vector batch | HTTP internal/binary/gRPC | RetrievalSegment | transport + retrievalplane | Experimental |
| Governance policy | `/v1/policies`, query trace | PolicyRecord | PolicyEngine/PolicyStore | Partial |
| Share contract | `/v1/share-contracts` | ShareContract | ContractStore/PolicyEngine | Partial |
| Memory lifecycle | internal memory routes | Memory/AlgorithmState | cognitive workers | Experimental |
| Python SDK | `sdk/python` | HTTP objects | `PlasmodClient` | Partial |
| Node SDK | `sdk/nodejs` | consistency mode | `AndbClient` | Partial |
| gRPC | `:19531` | core ingest/query/vector | `PlasmodAPIService` | Implemented, limited surface |

### 01.1.1. Status Definitions

- **Implemented**: wired into the active server and covered by current tests.
- **Partial**: present, but missing part of the expected contract, such as pagination, complete ACL enforcement, object coverage, or stable SDK mapping. Direct CRUD paths may also bypass Event/WAL semantics.
- **Conditional**: requires a build tag, native library, external service, or additional configuration.
- **Experimental**: available for evaluation, but not a stable production contract.

---

## 01.2. Documentation Map

| Section | Content | Intended Readers |
|---|---|---|
| [00 Documentation Home](README.md) | Reading order, role-based paths, and status labels | Everyone |
| [01 Requirements](01-project-overview-and-requirements.md) | Problems, use cases, functional/non-functional requirements and tracking | Product, architecture, core development |
| [02 Concepts and Design](02-system-architecture-and-design.md) | The source of truth, writing/reading paths, consistency, failure models, and [30 system design verification](14-implementation-status-gaps-and-claim-boundaries.md) | Architecture and core development |
| [03 Getting Started](09-getting-started-and-user-guide.md) | Install, start, first request, verify, clean | New users |
| [04 User Guide](09-getting-started-and-user-guide.md) | Use each core capability according to user tasks | App developers |
| [05 API and Reference](08-api-schema-and-sdk-reference.md) | HTTP/gRPC/binary, schema, configuration and error | Integrated developers |
| [06 Codebase](10-codebase-interfaces-and-call-paths.md) | Storage, packaging, interfaces, call chains, storage keys | Core developers |
| [07 Dependencies](11-dependencies-build-and-development.md) | Native stack, Badger, S3, embedders, and upgrade boundaries | Build and maintenance engineers |
| [08 Development](11-dependencies-build-and-development.md) | Build, debug, test, contribute, and make common changes | Contributors |
| [09 Operations](12-deployment-operations-and-troubleshooting.md) | Deployment, security, monitoring, recovery, and troubleshooting | Operators |
| [10 Extensibility](13-extensibility-compatibility-and-evolution.md) | Add schemas, materializers, operators, and backends | Extension authors |
| [11 Evolution](14-implementation-status-gaps-and-claim-boundaries.md) | Maturity, compatibility, migration, and limitations | Release and maintenance engineers |

### 01.2.1. Code Sources of Truth

- Bootstrap: `src/cmd/server/main.go`, `src/internal/app/bootstrap.go`
- HTTP: `src/internal/access/gateway.go`
- gRPC: `src/internal/api/grpc/proto/plasmod/v1/plasmod.proto`
- Event schema: `src/internal/schemas/dynamic_event.go`
- canonical schema: `src/internal/schemas/canonical.go`
- runtime: `src/internal/worker/runtime.go`, `runtime_consistency.go`
- storage: `src/internal/storage/contracts.go`, `factory.go`
- retrieval: `src/internal/dataplane/`, `src/internal/dataplane/retrievalplane/`, `cpp/`
- consistency: `src/internal/worker/consistency/`

### 01.2.2. Detailed Design References

| What to Verify | Reference |
|---|---|
| 3 Architectures + 4 Chains + 5 Perspectives + 8 Mechanisms + 10 Engines | [System Design Reference](14-implementation-status-gaps-and-claim-boundaries.md) |
| Canonical/Event/Query/Worker fields and typed I/O | [Object and Message Registry](14-implementation-status-gaps-and-claim-boundaries.md) |
| Interfaces, implementations, constructors, and wiring status | [Interface Implementation Registry](14-implementation-status-gaps-and-claim-boundaries.md) |
| Concrete route-to-Chain and route-to-Engine mappings | [API to Engine Matrix](14-implementation-status-gaps-and-claim-boundaries.md) |
| Synchronous/asynchronous stages, failure windows, and recovery | [Execution State and Failure Matrix](14-implementation-status-gaps-and-claim-boundaries.md) |
| Supported claims and overclaim boundaries | [Claim and Test Boundary](14-implementation-status-gaps-and-claim-boundaries.md) |

---

## 01.3. Positioning and Boundaries

### 01.3.1. Difference from Vector + Metadata

In a vector-plus-metadata design, the vector row is usually the primary record, while updates, versions, state transitions, and relationships are encoded by the application. Plasmod instead models Event, Memory, AgentState, Artifact, Edge, and ObjectVersion as canonical objects. Dense, sparse, and lexical indexes are retrieval projections that can be rebuilt from canonical data.

This distinction does not imply that Plasmod's ANN kernel is inherently faster than a dedicated vector database. Plasmod's value is the combination of agent-object semantics, explicit visibility, recovery, versioning, and evidence construction. ANN functionality is provided through the Plasmod bridge and third-party engines.

### 01.3.2. Relationship to Conventional Vector Databases

Plasmod can accept precomputed vectors and build warm segments, or generate vectors through TF-IDF and configured local or external embedders. It also maintains canonical objects, the WAL, relationships, and versions. Dedicated vector databases generally provide more mature distributed ANN execution, cluster management, and ecosystem integration; Plasmod does not claim feature parity in those areas.

### 01.3.3. Relationship to Agent Frameworks

Plasmod is not an agent planner, LLM client, or tool-execution framework. An agent framework decides when to emit events, how to execute tasks, and how to consume query results. Plasmod receives structured runtime data and provides persistence, materialization, retrieval, visibility control, and recovery. `src/internal/agent/` contains integration abstractions; it is not a complete agent-framework runtime.

### 01.3.4. Relationship to MemoryBank- and Zep-style Algorithms

MemoryBank-style, Zep-style, and baseline profiles live under `src/internal/worker/cognitive/`. The algorithm dispatcher can influence memory lifecycle operations, recall, and graph processing. These profiles are pluggable strategies, not replacements for canonical storage. Algorithm state is stored through `MemoryAlgorithmStateStore`; canonical Memory remains owned by the Plasmod storage layer.

### 01.3.5. Plasmod Responsibilities

- Event acceptance, WAL ordering, and replay input.
- Projection from Event to canonical objects.
- canonical object, edge, version, policy, contract and algorithm state storage.
- Retrieval projection, query planning and structured evidence assembly.
- Consistency admission, visibility, waiting, checkpoint and recovery.
- Hot/warm/cold routing and optional S3/MinIO access.
- HTTP, internal binary/SSE, gRPC and SDK interface boundaries.

### 01.3.6. External Component Responsibilities

- LLM reasoning, tool execution, and agent orchestration.
- Availability of configured embedding providers such as OpenAI, Cohere, Vertex AI, and Hugging Face.
- The S3 service owns its durability, replication, IAM, and lifecycle guarantees.
- Internal algorithms and ABIs of Knowhere, FAISS, DiskANN, ONNX Runtime, llama.cpp, and TensorRT.
- TLS, WAF, complete identity system and network isolation.

### 01.3.7. Explicit Non-goals and Unsupported Guarantees

- It does not provide universal ACID services across all objects and external systems.
- There is no guarantee of exactly-once side effects of all materializers.
- The default runtime is not a validated production-grade distributed cluster.
- No default user-level authentication is provided; only admin shared-key middleware.
- Not every YAML example is loaded by the active bootstrap path.
- There is no guarantee of binary compatibility between every platform, GPU, ANN backend and index file.

See [Constraints and Non-goals](#016-constraints-and-non-goals) for the detailed boundaries.

---

## 01.4. Project Overview

### 01.4.1. One-sentence Positioning

Plasmod is an agent-native database core. It accepts events, materializes Memory, AgentState, Artifact, Edge, and ObjectVersion as first-class canonical objects, and returns structured evidence containing objects, versions, relationships, provenance, and proof traces.

### 01.4.2. Problems Addressed

Long-running agents need more than similar-text retrieval. They must be able to determine which event produced a fact, which version is current, how objects relate, who may observe an object, when a write becomes visible, and how state is recovered after a restart. A flat collection of text, vectors, and metadata does not express these constraints directly.

Plasmod's core pathways are thus divided into:

1. Gateway receives Event or canonical object requests.
2. The Event is appended to the WAL and receives a monotonically increasing LSN.
3. The consistency controller schedules projection according to strict, bounded-staleness, or eventual mode.
4. Materialization derives Memory, AgentState, Artifact, Edge, and ObjectVersion records from the Event.
5. The canonical store persists object facts; the retrieval plane maintains rebuildable lexical and vector projections.
6. The query planner retrieves and filters candidates; the evidence assembler adds relationships, versions, provenance, and proof steps.

The active entry points are `src/internal/access/gateway.go`, `src/internal/worker/runtime_consistency.go`, `src/internal/worker/runtime.go`, `src/internal/materialization/`, `src/internal/storage/`, and `src/internal/evidence/assembler.go`.

### 01.4.3. Primary Users

- Agent framework developers: connect runtime events, sessions, state, and artifacts to a unified storage model.
- Agent application developers: write and query via HTTP, gRPC or Python SDK.
- Platform operators: configure persistence, S3/MinIO, visibility modes, recovery, and administrative APIs.
- Core developers: extend schemas, materializers, query operators, storage backends, and retrieval backends.

### 01.4.4. Current Core Capabilities

- Dynamic Event v0.4 input with compatibility for the legacy flat event input.
- File-backed and in-memory WAL implementations, LSN scans, recovery checkpoints, and an administrative replay API.
- Canonical storage for Memory, State, Artifact, Edge, Version, Policy, and ShareContract records.
- Three runtime consistency modes: strict, bounded staleness, and eventual visibility.
- Hot/warm/cold object path; Badger persistence; S3/MinIO cold store optional.
- Lexical retrieval with optional dense/sparse ANN; fallback when the C++ bridge is unavailable.
- Structured evidence responses, one-hop edge expansion, versions, and proof traces.
- Unified/split HTTP listening and independent gRPC listening.
- Python SDK; the Node SDK currently covers only consistency-mode controls.

### 01.4.5. Current Status

Core event ingest, query, canonical storage, Badger persistence, WAL, and consistency control are **Implemented**. Authorization isolation, general authentication, pagination, cross-object transactions, complete SDK coverage, and production backup orchestration are **Partial**. Optional ANN backends, GPU and TensorRT paths, and MemoryBank/Zep-style profiles are **Experimental** or conditional; their presence must not be interpreted as availability in every build.

### 01.4.6. Shortest Reading Path

Continue with [Positioning and Boundaries](#013-positioning-and-boundaries), [Capability Map](#011-capability-map), [System Architecture and Design](02-system-architecture-and-design.md), and [Getting Started](09-getting-started-and-user-guide.md).

---

## 01.5. Glossary

| Term | Engineering Definition |
|---|---|
| Event | A structured fact or action from an agent runtime. `schemas.Event` uses the Dynamic Event v0.4 canonical JSON form. |
| WAL | The accepted order and replay source for events. `Append` returns an LSN; `Scan` reads from a specified LSN. |
| LSN | A monotonically increasing log sequence number assigned by the WAL; it is not wall-clock time. |
| Canonical Object | A durable, authoritative object representation, such as Memory, AgentState, or Artifact. |
| Memory | A knowledge object materialized from an Event, with type, scope, version, lifecycle, and source-event metadata. |
| AgentState | The current value for an agent/session/state key. The Go type remains `State`; `AgentState` is an alias. |
| Artifact | An external or inline result produced by an agent, such as text, a report, code, or a tool result. |
| Edge | A typed, directed relationship between two canonical objects. |
| ObjectVersion | An object version or snapshot that records the mutation event and validity interval. |
| Materialization | The process that converts an accepted Event into canonical objects, edges, and versions. |
| Canonical Projection | The normalized write set for an Event: Event, Memory, checkpoint State, optional Artifact, Edges, and Versions. |
| Retrieval Projection | A rebuildable lexical, dense, or sparse index derived from canonical objects; it is not a source of truth. |
| Evidence | A query hit combined with edges, versions, provenance, proof steps, and filtering annotations. |
| Proof Trace | An explainability chain containing planner, retrieval, policy, tier, graph, and derivation steps. |
| Watermark | The highest LSN whose projection the runtime has completed and may expose as visible. |
| Strict | The write call waits until the projection for its LSN is visible. |
| Bounded Staleness | The write is queued under a freshness SLA, and reads may wait for the required watermark. |
| Eventual | The WAL accepts the Event before asynchronous projection; reads guarantee eventual progress only. |
| Hot | The in-process cache and fast index for recent or high-salience objects. |
| Warm | The canonical object store and primary retrieval segments. |
| Cold | The S3/MinIO or in-memory archive for explicitly archived objects. |
| RRF | Reciprocal Rank Fusion combines ranked candidate lists from multiple retrieval sources. |
| Vector-only mode | A conditional mode that disables graph, policy, and provenance work; it does not represent full Plasmod semantics. |
| Source of Truth | The authoritative basis for recovery and conflict decisions: Event/WAL for causal order and the canonical store for current object facts. |

---

## 01.6. Constraints and Non-goals

### 01.6.1. Consistency and Transactions

- Event ingest has WAL and projection consistency controls, but canonical direct CRUD routes are not all through WAL.
- It does not provide universal ACID services across Badger, S3, external embedder and native index.
- Asynchronous materializers and external side effects are not guaranteed to execute exactly once. LSNs, checkpoints, deterministic IDs, and retries reduce duplicate effects.

### 01.6.2. System Boundaries

- Plasmod is not an agent framework, LLM gateway, tool executor or workflow scheduler.
- Plasmod does not implement the internal algorithms of S3, Badger, Knowhere, FAISS, DiskANN, ONNX Runtime or TensorRT.
- The upstream compatibility code under `coordinator/controlplane`, `eventbackbone/streamplane`, and `platformpkg` does not mean that the active `BuildServer` enables a complete distributed control plane.

### 01.6.3. API and Security

- There is currently no single user authentication, OAuth/OIDC, RBAC or TLS termination.
- `PLASMOD_ADMIN_API_KEY` protects only `/v1/admin/*`; public and internal data routes still require network-boundary protection.
- Canonical collection APIs do not yet share one pagination/cursor contract.
- `/v1/internal/*` is not a stable public API.

### 01.6.4. Storage and Deployment

- Default starts as a single process; default directories in disk mode are `.andb_data`.
- The S3 cold tier is an optional archive backend; it does not replace canonical Badger storage or WAL.
- The cross-version migration tools for index, WAL and Badger formats are incomplete.
- Docker Compose uses development credentials and cannot be directly considered a production security configuration.

### 01.6.5. SDKs and Optional Capabilities

- Python SDK only covers part of HTTP; Node SDK currently mainly covers consistency mode.
- gRPC only covers health, event/vector ingest and query/batch query.
- ANN, GPU, ONNX, GGUF, TensorRT and some index types depend on the build tag/native library/platform.
- The existence of a MemoryBank/Zep profile does not mean that all paper algorithms or external product behaviour is fully replicated.

---

## 01.7. Functional Requirements

The requirements use a compact, traceable format. See the [Requirements Traceability Matrix](#0110-requirements-traceability-matrix) for code and test mappings.

### 01.7.1. FR-ING-001 Event Acceptance

#### 01.7.1.1. Requirement
The system must accept Dynamic Event v0.4 and the legacy flat event input. It must generate an event ID when the caller omits one.

#### 01.7.1.2. Rationale
Provide one canonical representation while preserving a migration path for existing clients.

#### 01.7.1.3. Inputs
`schemas.Event` JSON.

#### 01.7.1.4. Expected Behavior
Normalize the input, append it to the WAL, and return the event ID, LSN, and projection/visibility status.

#### 01.7.1.5. Failure Behavior
Invalid JSON, unsupported consistency settings, and write backpressure return non-2xx responses.

#### 01.7.1.6. Acceptance Criteria
Gateway and Dynamic Event tests pass.

#### 01.7.1.7. Current Status
Implemented.

#### 01.7.1.8. Related Code
`schemas/dynamic_event.go`, `access/gateway.go`, `worker/runtime_consistency.go`.

### 01.7.2. FR-ORD-001 Event Ordering and Replay

#### 01.7.2.1. Requirement
Every accepted Event must receive an LSN and be scannable by LSN. Disk mode must use the file-backed WAL.

#### 01.7.2.2. Rationale
Unified order for recovery and visibility watermarks.

#### 01.7.2.3. Inputs
Event and `from_lsn`.

#### 01.7.2.4. Expected Behavior
`Append`, `Scan`, and `LatestLSN` must satisfy the WAL contract.

#### 01.7.2.5. Failure Behavior
Persistent WAL decoding and I/O errors must be propagated to the caller.

#### 01.7.2.6. Acceptance Criteria
WAL recovery and corruption tests pass.

#### 01.7.2.7. Current Status
Implemented.

#### 01.7.2.8. Related Code
`eventbackbone/contracts.go`, `wal.go`, `wal_file.go`.

### 01.7.3. FR-MAT-001 Canonical Materialization

#### 01.7.3.1. Requirement
The system must derive the appropriate Memory, AgentState, or Artifact from an Event and persist the related Edge and ObjectVersion records.

#### 01.7.3.2. Rationale
Preserve agent-object semantics instead of reducing an object to an unexplained vector row.

#### 01.7.3.3. Inputs
A normalized Event.

#### 01.7.3.4. Expected Behavior
When the stores share one Badger backend, a canonical projection atomically commits its object, edge, and version set.

#### 01.7.3.5. Failure Behavior
A failed projection must not advance the visible checkpoint. The Controller retries according to configuration.

#### 01.7.3.6. Acceptance Criteria
Materialization, canonical-projection, and consistency tests pass.

#### 01.7.3.7. Current Status
Implemented. Some direct CRUD paths do not use this Event-first path.

#### 01.7.3.8. Related Code
`materialization/service.go`, `storage/canonical_projection.go`, worker materializers.

### 01.7.4. FR-STA-001 State Updates

#### 01.7.4.1. Requirement
Updates to the same agent/session/state key must point to a stable State ID and an incremental version.

#### 01.7.4.2. Rationale
Latest-state queries require deterministic overwrite and version semantics.

#### 01.7.4.3. Inputs
state update/change/checkpoint Event.

#### 01.7.4.4. Expected Behavior
The state materializer updates State and creates an ObjectVersion at checkpoints.

#### 01.7.4.5. Failure Behavior
When the state key is missing, no State is produced. Equivalent updates must not generate competing State IDs.

#### 01.7.4.6. Acceptance Criteria
State-materialization tests pass.

#### 01.7.4.7. Current Status
Implemented; restoration of the state-key map across processes remains Partial.

#### 01.7.4.8. Related Code
`worker/materialization/state.go`.

### 01.7.5. FR-RET-001 Retrieval and Structured Queries

#### 01.7.5.1. Requirement
The system must support filters/operators such as query text, scope, object/memory type, time window, target IDs, cold tier and precomputed embedding.

#### 01.7.5.2. Rationale
Agent queries require semantic retrieval and exact selectors to coexist.

#### 01.7.5.3. Inputs
`schemas.QueryRequest`.

#### 01.7.5.4. Expected Behavior
Planner generates SearchInput; dataplane returns candidates; assembler produces objects, edges, versions, provenance and proof trace.

#### 01.7.5.5. Failure Behavior
An invalid selector or backend error returns a clear error; a zero hit is not a transport failure.

#### 01.7.5.6. Acceptance Criteria
Query, tiered-adapter, and evidence tests pass.

#### 01.7.5.7. Current Status
Core retrieval and evidence assembly are Implemented; scope and policy enforcement are Partial.

#### 01.7.5.8. Related Code
`semantic/operators.go`, `worker/runtime.go`, `evidence/assembler.go`.

### 01.7.6. FR-CON-001 Configurable Consistency

#### 01.7.6.1. Requirement
The system must support strict, bounded-staleness, and eventual visibility, and allow Event and Query requests to override the default mode.

#### 01.7.6.2. Rationale
Different agent workloads require an explicit trade-off among latency, throughput, and freshness.

#### 01.7.6.3. Inputs
runtime config, Event access, Query access consistency.

#### 01.7.6.4. Expected Behavior
The Controller manages admission, queues, retries, watermarks, checkpoints, and query waits.

#### 01.7.6.5. Failure Behavior
Queue-full, paused, deadline, accepted-not-visible, and projection-failure conditions must remain distinguishable.

#### 01.7.6.6. Acceptance Criteria
Consistency Controller and Tracker tests pass.

#### 01.7.6.7. Current Status
Implemented.

#### 01.7.6.8. Related Code
`worker/consistency/`, `runtime_consistency.go`.

### 01.7.7. FR-GOV-001 Governance and Sharing

#### 01.7.7.1. Requirement
The system should persist PolicyRecord, ShareContract, and AuditRecord objects and apply the supported policy subset during query and evidence construction.

#### 01.7.7.2. Rationale
Multi-agent data needs traceable sharing and lifecycle decisions.

#### 01.7.7.3. Inputs
policy, contract, memory operation.

#### 01.7.7.4. Expected Behavior
Append policy and audit records, resolve them by object and scope, and expose governance decisions in traces.

#### 01.7.7.5. Failure Behavior
The basic policy engine must not be treated as a complete authentication or authorization system.

#### 01.7.7.6. Acceptance Criteria
Governance storage and policy-engine tests pass for the implemented subset.

#### 01.7.7.7. Current Status
Partial.

#### 01.7.7.8. Related Code
`semantic/policy.go`, storage policy/contract/audit stores.

### 01.7.8. FR-OPS-001 Deletion, Purge, and Recovery

#### 01.7.8.1. Requirement
The system must distinguish logical deletion, hard purge, data wipe, and replay. Cross-tier deletion should remove objects, edges, segment references, and cold data as applicable.

#### 01.7.8.2. Rationale
Lifecycle operations must be auditable, and removed objects must not remain retrievable.

#### 01.7.8.3. Inputs
Dataset, source, or memory selector plus admin credentials.

#### 01.7.8.4. Expected Behavior
Batch deletion, background hard-delete processing, audit/outbox records, and status queries.

#### 01.7.8.5. Failure Behavior
Partial failures must be observable and must not be reported as complete success.

#### 01.7.8.6. Acceptance Criteria
Purge, hard-delete, wipe, and replay tests pass.

#### 01.7.8.7. Current Status
Implemented/Partial, depending on the backend and operation.

#### 01.7.8.8. Related Code
`access/hard_delete_manager.go`, `storage/purge_warm.go`, admin handlers.

### 01.7.9. FR-SDK-001 SDKs and Transport

#### 01.7.9.1. Requirement
Core ingest/query should be available via HTTP and gRPC; SDK fields must be consistent with the schema.

#### 01.7.9.2. Rationale
Allow clients to integrate without importing internal Go packages.

#### 01.7.9.3. Inputs
JSON, protobuf or row-major binary payload.

#### 01.7.9.4. Expected Behavior
Each transport maps to the same Gateway service methods.

#### 01.7.9.5. Failure Behavior
Protocol errors must be mapped to transport-level errors rather than causing panics.

#### 01.7.9.6. Acceptance Criteria
gRPC, framing, Python, and Node tests pass.

#### 01.7.9.7. Current Status
HTTP Implemented; gRPC limited; SDK Partial.

#### 01.7.9.8. Related Code
`gateway_rpc.go`, `api/grpc/`, `transport/`, `sdk/`.

---

## 01.8. Non-functional Requirements

This section defines engineering requirements; it does not report benchmark results.

| ID | Category | Requirement | Current Evidence / Boundary |
|---|---|---|---|
| NFR-PERF-001 | Performance | Writes must be subject to admission control; a full retrieval-index rebuild must not run synchronously for every write. | Gateway semaphore, consistency queue, background flush. |
| NFR-FRESH-001 | Freshness | The system must distinguish between WAL accepted, object visible and retrieval visible, and expose watermark/lag. | consistency tracker/controller. |
| NFR-COR-001 | Correctness | A shared durable backend for canonical objects, edges, and versions must support atomic projection. | The factory binds object/edge/version stores to one backend; Badger commits the transaction. |
| NFR-DUR-001 | Durability | The WAL and canonical store of disk mode must be read after the process restarts. | FileWAL + Badger; requires an external backup strategy. |
| NFR-AVL-001 | Availability | Recovery errors must produce an observable failure state; shutdown must stop admission, drain workers, and close storage. | Controller lifecycle with `ServerBundle.Shutdown`. |
| NFR-SCL-001 | Scalability | Queue, worker, write semaphore and batch interfaces must be configurable; single implementations cannot be described as verified distributed clusters. | env config; the current core starts as a single process. |
| NFR-SEC-001 | Security | Admin routes must use the shared key or deployment-level reverse-proxy protection; production responses must not expose debug/raw fields. | Admin auth and visibility middleware. |
| NFR-ISO-001 | Tenant isolation | Tenant, workspace, and session identifiers must propagate through Events, queries, and canonical objects; each service path must document where scope filtering is enforced. | Schema coverage is complete; enforcement is partial. |
| NFR-OBS-001 | Observability | It must provide health, admin metrics, topology, storage/config status and identifiable errors. | HTTP management routes and runtime stats. |
| NFR-MNT-001 | Maintainability | Source of truth, projection, third-party ownership and extension registration must be documented. | This documentation system and package contracts. |
| NFR-COMP-001 | Compatibility | API, schema, WAL, storage key, embedding family and native ABI changes must have a migration/rollback specification. | Evolution docs; the current versioning capability is incomplete. |
| NFR-PORT-001 | Portability | The pure-Go lexical path must remain available when the native bridge is absent; native and GPU capabilities are declared per platform. | Retrieval stub, build tags, and CMake options. |

### 01.8.1. Validation Principles

Functional tests demonstrate behavior; they do not replace capacity testing, fault injection, security audits, or platform certification. Performance, availability, and scalability claims require independent validation and must not appear in core functional documentation as established facts without that evidence.

---

## 01.9. Problem Statement

### 01.9.1. Limitations of Flat Memory Stores

When agent memory is only a collection of text, vectors, and metadata, similarity search is straightforward, but version history, causal chains, state replacement, sharing scope, and recovery order are not. Applications are then forced to maintain separate event logs, state tables, and provenance graphs, fragmenting write acknowledgement, retrieval hits, and authoritative facts across different systems.

| Missing semantic | Consequence of vector + metadata only | Plasmod authoritative record |
|---|---|---|
| mutation order | concurrent updates and replay cannot be distinguished | Event + WAL LSN |
| current state | a similar hit is not necessarily the current value | State |
| history | overwrites lose old values and validity boundaries | ObjectVersion snapshot + validity interval |
| causality | metadata references are not a traversable graph | Edge + source/target object ID |
| visibility | an index hit may precede complete persistence | mutation LSN + visible watermark |
| recovery | a damaged index has no verifiable rebuild source | WAL + canonical objects/versions |

The engineering invariant is: **the canonical plane is authoritative; the retrieval plane is a disposable acceleration view**. Candidate IDs must hydrate back to canonical objects, and a projection must never overwrite canonical truth.

### 01.9.2. Dynamic State Challenges

Tool results, plan updates, and checkpoints continuously mutate agent state. Overwriting a metadata field loses the mutation event, version, visibility time, and recovery basis. Plasmod identifies one mutable state key by:

```text
(tenant_id, workspace_id, agent_id, session_id, state_key)
```

`CanonicalStateID` hashes this tuple into a stable ID. Every mutation increments `State.version` and writes an `ObjectVersion` containing a complete `Snapshot`, `MutationEventID`, `MutationLSN`, and `ValidFrom/ValidTo` boundaries.

| Input condition | State behavior |
|---|---|
| new state key | create version 1 |
| new mutation event | close the previous version and append the next version |
| retry of the same WAL LSN | idempotently complete projection without another version |
| replay of an old event already in history | keep current state; do not roll it back |
| same event ID submitted under a new LSN | return a duplicate result without another canonical mutation |

Before canonical commit, Runtime serializes version resolution with `stateProjectionMu`. `ApplyCanonicalProjection` then writes Event, Memory, State, Artifact, Edge, and Version records as one canonical write set. With Badger, object, edge, and version records share one transaction; the memory backend preserves the logical write set but is not a cross-process transaction.

### 01.9.3. Multi-agent Scope and Provenance

When multiple agents share a workspace, a memory may be private, session-scoped, team-scoped, or shared. A `scope` string alone cannot express tenant/workspace/team/session, owner, visible agents, roles, policy tags, share contracts, and derivation permissions.

Memory, State, Artifact, Edge, and ObjectVersion records persist the same `CanonicalAccess` structure:

| Field | Purpose |
|---|---|
| `tenant_id`, `workspace_id`, `team_id`, `session_id` | hierarchical scope |
| `owner_agent_id` | owner authorization and provenance preservation |
| `visibility` | private/session/team/workspace/tenant/public/restricted |
| `visible_to_agents`, `visible_to_roles` | explicit read grants |
| `policy_tags` | governance labels |
| `share_contract_id` | binds a derived object to an explicit agreement |

`POST /v1/query` builds a principal from `requester_agent_id` and `requester_roles`, then applies a canonical access gate before hydration. Allowed reasons are returned through `QueryResponse.access_decisions`. After evidence assembly, Node, Edge, ProofStep, and provenance references are filtered again so graph expansion from a visible seed cannot disclose a private endpoint.

`POST /v1/internal/memory/share` no longer performs a direct object copy. Runtime validates ownership and, when supplied, the ShareContract tenant/workspace/scope plus source `derive` and target `read` permissions. It then emits a derived Event through the regular WAL, materialization, canonical projection, and retrieval-visibility path, creating a shared Memory, ObjectVersion, and `derived_from` Edge.

### 01.9.4. Plasmod Engineering Response

- Event records causal input, actor, scope, and mutation intent.
- WAL/LSN records acceptance order, retry identity, replay position, and visibility progress.
- Canonical Object preserves current facts together with `CanonicalAccess` and `MutationLSN`.
- ObjectVersion preserves a complete snapshot, mutation event, validity interval, and access context.
- Edge and derivation records preserve `derived_from`, supports, contradicts, conflict, and other relations.
- Retrieval Projection is created after canonical commit; a failed projection is retried from the same WAL entry and remains hidden before the visible watermark.
- Evidence Response returns selected objects, access decisions, versions, edges, provenance, and proof steps together.

These semantics must remain consistent across runtime, storage, query, and recovery paths rather than being reconstructed ad hoc by an SDK or agent framework.

### 01.9.5. Write and Visibility Protocol

```text
Normalize Event
-> Append WAL and assign LSN
-> Derive canonical write set
-> ApplyCanonicalProjection
-> Ingest retrieval projection
-> advance visible watermark
-> ACK according to consistency mode
```

Canonical-before-retrieval ordering is deliberate. If retrieval ingest fails, WAL and canonical snapshots remain repair sources, while the controller does not advance the visible watermark. The query access gate rejects objects whose `mutation_lsn > read_watermark_lsn`. Strict ACK waits for the complete projection callback at that LSN; bounded and eventual modes use the controller's configured wait boundary.

### 01.9.6. Query and Evidence Protocol

```text
QueryRequest + requester principal
-> retrieval/canonical candidates
-> canonical existence and access gate
-> hydration and graph expansion
-> endpoint access revalidation
-> version/provenance/proof assembly
-> QueryResponse(objects, access_decisions, read_watermark_lsn, evidence)
```

Denied candidates are not listed in `access_decisions`, avoiding object-existence disclosure. The response explains only why returned objects were allowed, such as owner, scope, explicit grant, or share contract.

### 01.9.7. Failure and Recovery Boundaries

| Failure window | Implemented behavior | Guarantee boundary |
|---|---|---|
| before WAL append | request fails without an LSN | no canonical mutation |
| after WAL append, before canonical commit | controller retries the same LSN | materialization and State updates are idempotent |
| after canonical commit, before retrieval ingest | canonical snapshot exists, watermark is not advanced | normal queries hide that mutation; the same LSN can retry |
| duplicate event ID under a new LSN | duplicate result reports the original LSN | no duplicate version or State rollback |
| lost projection/index | replay or rebuild from WAL/canonical data | automatic full reconciliation remains partial |

### 01.9.8. Claims That Remain Out of Scope

This implementation establishes canonical write/query/share invariants in the core Runtime; it is not a complete IAM system and does not authorize every management route. `requester_agent_id` is still caller-supplied and must be bound by an authenticated gateway. Raw canonical CRUD and internal admin routes remain a trusted management plane. Mask/partial responses, uniform write ACLs for every direct lifecycle mutation, cross-node State serialization, and automatic full reconciliation remain future work. Governance and Recovery therefore remain **Partial** overall; schema presence alone is not evidence of complete security or distributed transactions.

---

## 01.10. Requirements Traceability Matrix

| Requirement | Design | Module/API | Test | Status |
|---|---|---|---|---|
| FR-ING-001 | Event-first, write path | `schemas/dynamic_event.go`; `POST /v1/ingest/events` | dynamic event/gateway tests | Implemented |
| FR-ORD-001 | Source of truth, failure model | `eventbackbone/*wal*`; admin replay | WAL tests | Implemented |
| FR-MAT-001 | Event-to-object, canonical projection | materialization + storage projection | materialization/projection tests | Implemented |
| FR-STA-001 | Canonical object/version model | state materializer; `/v1/states` | state/runtime tests | Implemented/Partial |
| FR-RET-001 | Query path, evidence | semantic/dataplane/evidence; `/v1/query` | query/evidence/tiered tests | Implemented |
| FR-CON-001 | Consistency model | worker/consistency; admin mode | controller/tracker tests | Implemented |
| FR-GOV-001 | Security/policy model | semantic policy + policy/contract/audit stores | governance tests | Partial |
| FR-OPS-001 | Failure/recovery/lifecycle | admin delete/purge/wipe/replay | access/storage/runtime tests | Implemented/Partial |
| FR-SDK-001 | Transport model | HTTP/gRPC/binary + SDK | gRPC/framing/SDK tests | Partial |
| NFR-FRESH-001 | Consistency/watermark | tracker/controller | consistency tests | Implemented |
| NFR-COR-001 | Canonical atomicity | storage factory/projection | Badger projection tests | Implemented for shared Badger backend |
| NFR-SEC-001 | Security model | admin auth/visibility | auth/visibility tests | Partial |
| NFR-PORT-001 | Dependency/build model | retrieval stub/build tags | standard and tagged builds | Partial |

### 01.10.1. Maintenance Rules

When adding or changing a feature, update its requirement, design documentation, route/schema reference, code mapping, and at least one test together. If an interface exists but is not wired into the active bootstrap path, mark it **Not Confirmed**, not **Implemented**.

---

## 01.11. Stakeholders and Use Cases

### 01.11.1. UC-001: Framework Writes Runtime Events

#### 01.11.1.1. Actor
Agent framework developer.

#### 01.11.1.2. Goal
Write the observation, tool result, state update, or artifact as an Event and get the accepted/visible status.

#### 01.11.1.3. Preconditions
The service is healthy. The Event includes at least an actor, event type, and payload, and the selected consistency mode is valid.

#### 01.11.1.4. Main Flow
The framework calls `/v1/ingest/events`. Gateway normalizes the input to v0.4, the WAL assigns an LSN, the Controller executes or schedules projection, and the response reports event, object, and visibility status.

#### 01.11.1.5. Alternative Flow
Eventual mode may return after WAL acceptance while projection completes in the background. A full queue, paused runtime, or failed projection returns a distinguishable retryable error.

#### 01.11.1.6. Data Written
Event, Memory/State/Artifact, Edge, ObjectVersion, retrieval projection.

#### 01.11.1.7. Data Queried
Query, canonical CRUD, trace.

#### 01.11.1.8. Consistency Requirement
Event-level `access.consistency` overrides the runtime default.

#### 01.11.1.9. Failure Expectation
The response must distinguish an unaccepted write from a WAL-accepted Event that is not yet visible.

#### 01.11.1.10. Related API
`POST /v1/ingest/events`, `GET/POST /v1/admin/consistency-mode`.

### 01.11.2. UC-002: Tool-Use Agent Updates State

#### 01.11.2.1. Actor
Tool-use agent.

#### 01.11.2.2. Goal
Read the latest state of the assigned agent/session after a continuous tool result and state update.

#### 01.11.2.3. Preconditions
The state Event contains `object.state_key` or compatible payload fields.

#### 01.11.2.4. Main Flow
The agent writes a state update. The state materializer derives a stable State ID and advances the version for the agent/state-key pair. The client then reads `/v1/states` or uses a structured query selector.

#### 01.11.2.5. Alternative Flow
In eventual mode, the client waits and retries, or selects strict visibility for a decision-critical read.

#### 01.11.2.6. Data Written
Event, State, derivation records, and ObjectVersion checkpoints.

#### 01.11.2.7. Data Queried
State list/latest selector.

#### 01.11.2.8. Consistency Requirement
Critical decision paths use strict; bounded/eventual when tolerating old values.

#### 01.11.2.9. Failure Expectation
A client must not treat "not found" and "accepted but not yet materialized" as the same condition.

#### 01.11.2.10. Related API
`POST /v1/ingest/events`, `GET /v1/states`, `POST /v1/query`.

### 01.11.3. UC-003: Research Agent Retrieves Evidence

#### 01.11.3.1. Actor
Research agent.

#### 01.11.3.2. Goal
Query memories and retrieve their source events, relationships, versions, provenance, and proof traces.

#### 01.11.3.3. Preconditions
The query scope and object filters are valid, and the relevant objects have entered canonical storage and the applicable retrieval projection.

#### 01.11.3.4. Main Flow
The client calls `/v1/query`; the planner creates SearchInput, the tiered data plane retrieves candidates, and the assembler adds edges, versions, provenance, and proof steps.

#### 01.11.3.5. Alternative Flow
Set `target_object_ids` to use the canonical selector, or set `include_cold` to include the archive tier.

#### 01.11.3.6. Data Written
No; the evidence cache can be pre-computed at the ingest stage.

#### 01.11.3.7. Data Queried
Memory, Edge, ObjectVersion, PolicyRecord, EvidenceFragment.

#### 01.11.3.8. Consistency Requirement
The query mode decides whether to wait for the visible watermark.

#### 01.11.3.9. Failure Expectation
The client must use `query_status` to distinguish a valid zero-hit result from an incomplete canonical-hydration or retrieval stage.

#### 01.11.3.10. Related API
`POST /v1/query`, `GET /v1/traces/{object_id}`.

### 01.11.4. UC-004: Multi-Agent Sharing and Conflict Resolution

#### 01.11.4.1. Actor
Multi-agent runtime.

#### 01.11.4.2. Goal
Express ownership, visibility, share contract and conflict relationships.

#### 01.11.4.3. Preconditions
Agent, workspace and contract identification are stable.

#### 01.11.4.4. Main Flow
Write a shared Memory or ShareContract, apply access filters during queries, and record conflicts through internal memory routes plus Edge and AuditRecord objects.

#### 01.11.4.5. Alternative Flow
Only basic visibility/ACL judgments are used for unconfigured contracts.

#### 01.11.4.6. Data Written
Memory, ShareContract, PolicyRecord, Edge, AuditRecord.

#### 01.11.4.7. Data Queried
Scope-aware query and trace.

#### 01.11.4.8. Consistency Requirement
Use strict visibility for sharing decisions that must observe the latest policy and contract state.

#### 01.11.4.9. Failure Expectation
The current ACL support is basic. Deployments still require identity authentication and tenant-boundary protection above Plasmod.

#### 01.11.4.10. Related API
`/v1/share-contracts`, `/v1/policies`, internal memory share/conflict routes.

### 01.11.5. UC-005: Operator Recovers the Service
#### 01.11.5.1. Actor
AI platform operator.

#### 01.11.5.2. Goal
Reset projection from durable WAL and checkpoint after service interruption.

#### 01.11.5.3. Preconditions
`PLASMOD_STORAGE=disk` is configured, the data directory and WAL are readable, and version and embedding settings are compatible.

#### 01.11.5.4. Main Flow
`BuildServer` opens Badger and FileWAL. The consistency Controller loads checkpoints, scans subsequent WAL entries, replays unfinished projections, and exposes healthy status only after initialization succeeds.

#### 01.11.5.5. Alternative Flow
Run replay preview before applying admin replay. When the embedding family is incompatible, perform a controlled reindex.

#### 01.11.5.6. Data Written
Checkpoint, canonical projections, and retrieval indexes.

#### 01.11.5.7. Data Queried
Admin storage, configuration, consistency, and replay status.

#### 01.11.5.8. Consistency Requirement
During recovery, data whose LSN has not reached the visible watermark must not be reported as visible.

#### 01.11.5.9. Failure Expectation
WAL decoding, checkpoint, and projection errors must either prevent startup or return an explicit recovery failure.

#### 01.11.5.10. Related API
`/v1/admin/replay`, `/v1/admin/consistency-mode`, `/v1/admin/storage`.
