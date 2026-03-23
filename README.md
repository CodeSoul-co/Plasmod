# CogDB — Agent-Native Database for Multi-Agent Systems

CogDB (ANDB) is an agent-native database for multi-agent systems (MAS). It combines a tiered segment-oriented retrieval plane, an event backbone with an append-only WAL, a canonical object materialization layer, pre-computed evidence fragments, 1-hop graph expansion, and structured evidence assembly — all wired together as a single runnable Go server.

> **Core thesis:** agent memory, state, event, artifact, and relation should be modeled as first-class database objects, and query results should return structured evidence rather than only top-k text fragments.

## What is implemented

- Go server ([`src/cmd/server/main.go`](src/cmd/server/main.go)) with 14 HTTP routes
- Append-only WAL with `Scan` and `LatestLSN` for replay and watermark tracking
- `MaterializeEvent` → `MaterializationResult` producing canonical `Memory`, `ObjectVersion`, and typed `Edge` records at ingest time
- Three-tier data plane: **hot** (in-memory LRU) → **warm** (segment index) → **cold** (archived tier), behind a unified `DataPlane` interface
- Pre-computed `EvidenceFragment` cache populated at ingest, merged into proof traces at query time
- 1-hop graph expansion via `GraphEdgeStore.BulkEdges` in the `Assembler.Build` path
- `QueryResponse` with `Objects`, `Edges`, `Provenance`, `ProofTrace`, `Versions`, and `AppliedFilters` on every query
- Module-level test coverage: 12 packages each with a `*_test.go` file
- Python SDK (`sdk/python`) and demo scripts
- Full architecture, schema, and API documentation

## Why This Project Exists

Most current agent memory stacks look like one of the following:

1. a vector database plus metadata tables
2. a chunk store used for RAG
3. an application-level event log or cache
4. a graph layer that is disconnected from retrieval execution

These approaches are useful but incomplete for MAS workloads that need:

- event-centric state evolution
- objectified memory and state management
- multi-representation retrieval
- provenance-preserving evidence return
- relation expansion and traceable derivation
- version-aware reasoning context

ANDB treats the database as cognitive infrastructure, not only as storage.

## v1 Design Goals

- Store canonical cognitive objects, not only vectors or chunks.
- Drive state evolution through events and materialization, not direct overwrite.
- Support dense, sparse, and filter-aware retrieval over object projections.
- Return structured evidence packages with provenance, versions, and proof notes.
- Keep contracts stable enough for parallel development across modules.

## Current Architecture

The system is organized around three execution layers:

```
HTTP API (access)
    └─ Runtime (worker)
          ├─ WAL + Bus  (eventbackbone)
          ├─ MaterializeEvent → Memory / ObjectVersion / Edges  (materialization)
          ├─ PreComputeService → EvidenceFragment cache  (materialization)
          ├─ HotCache → TieredDataPlane (hot→warm→cold)  (dataplane)
          └─ Assembler.Build → BulkEdges + EvidenceCache  (evidence)
```

**Ingest path:**
`API → WAL.Append → MaterializeEvent → PutMemory + PutVersion + PutEdge → PreCompute → HotCache → TieredDataPlane.Ingest`

**Query path:**
`API → TieredDataPlane.Search → Assembler.Build → EvidenceCache.GetMany + BulkEdges(1-hop) → QueryResponse{Objects, Edges, ProofTrace}`

Code layout:

- [`src/internal/access`](src/internal/access): HTTP gateway, 14 routes including ingest, query, and canonical CRUD
- [`src/internal/coordinator`](src/internal/coordinator): 9 coordinators (schema, object, policy, version, worker, memory, index, shard, query) + module registry
- [`src/internal/eventbackbone`](src/internal/eventbackbone): WAL (`Append`/`Scan`/`LatestLSN`), Bus, HybridClock, WatermarkPublisher, DerivationLog
- [`src/internal/worker`](src/internal/worker): `Runtime.SubmitIngest` and `Runtime.ExecuteQuery` wiring
- [`src/internal/worker/nodes`](src/internal/worker/nodes): 14 worker-node type contracts (data, index, query, memory extraction, graph, proof trace, etc.)
- [`src/internal/dataplane`](src/internal/dataplane): `TieredDataPlane` (hot/warm/cold), `SegmentDataPlane`, and `DataPlane` interface
- [`src/internal/dataplane/segmentstore`](src/internal/dataplane/segmentstore): `Index`, `Shard`, `Searcher`, `Planner` — the physical segment layer
- [`src/internal/materialization`](src/internal/materialization): `Service.MaterializeEvent` → `MaterializationResult{Record, Memory, Version, Edges}`; `PreComputeService`
- [`src/internal/evidence`](src/internal/evidence): `Assembler` (cache-aware, graph-expansion via `WithEdgeStore`), `EvidenceFragment`, `Cache`
- [`src/internal/storage`](src/internal/storage): 7 stores + `HotObjectCache` + `TieredObjectStore`; `GraphEdgeStore` with `BulkEdges`/`DeleteEdge`
- [`src/internal/semantic`](src/internal/semantic): `ObjectModelRegistry`, `PolicyEngine`, 5 query plan types
- [`src/internal/schemas`](src/internal/schemas): 13 canonical Go types + query/response contracts
- [`sdk/python`](sdk/python): Python SDK and bootstrap scripts
- [`cpp`](cpp): C++ retrieval stub for future high-performance execution
- [`src/internal/dataplane/retrievalplane`](src/internal/dataplane/retrievalplane): imported retrieval-plane source subtree (behind build tag)
- [`src/internal/coordinator/controlplane`](src/internal/coordinator/controlplane): imported control-plane source subtree (behind build tag)
- [`src/internal/eventbackbone/streamplane`](src/internal/eventbackbone/streamplane): imported stream/event source subtree (behind build tag)
- [`src/internal/platformpkg`](src/internal/platformpkg): imported shared platform package subtree

## Worker Architecture

The execution layer is organised as a **cognitive dataflow pipeline** decomposed into eight layers, each with a defined responsibility boundary and pluggable InMemory implementation.

### 8-Layer Worker Model

| # | Layer | Workers |
|---|---|---|
| 1 | **Data Plane** — Storage & Index | `IndexBuildWorker`, `SegmentWorker` _(compaction)_, `VectorRetrievalExecutor` |
| 2 | **Event / Log Layer** — WAL & Version Backbone | `IngestWorker`, `LogDispatchWorker` _(pub-sub)_, `TimeTick / TSO Worker` |
| 3 | **Object Layer** — Canonical Objects | `ObjectMaterializationWorker`, `StateMaterializationWorker`, `ToolTraceWorker` |
| 4 | **Cognitive Layer** — Memory Lifecycle | `MemoryExtractionWorker`, `MemoryConsolidationWorker`, `SummarizationWorker`, `ReflectionPolicyWorker` |
| 5 | **Structure Layer** — Graph & Tensor Structure | `GraphRelationWorker`, `EmbeddingBuilderWorker`, `TensorProjectionWorker` _(optional)_ |
| 6 | **Policy Layer** — Governance & Constraints | `PolicyWorker`, `ConflictMergeWorker`, `AccessControlWorker` |
| 7 | **Query / Reasoning Layer** — Retrieval & Reasoning | `QueryWorker`, `ProofTraceWorker`, `SubgraphExecutor`, `MicroBatchScheduler` |
| 8 | **Coordination Layer** — Multi-Agent Interaction | `CommunicationWorker`, `SharedMemorySyncWorker`, `ExecutionOrchestrator` |

All workers implement typed interfaces defined in [`src/internal/worker/nodes/contracts.go`](src/internal/worker/nodes/contracts.go) and are registered via the pluggable `Manager`. The `ExecutionOrchestrator` ([`src/internal/worker/orchestrator.go`](src/internal/worker/orchestrator.go)) dispatches tasks to chains with priority-aware queuing and backpressure.

> **Current implementation status:** Layers 1–4 and parts of 5–8 are fully implemented (including `SubgraphExecutorWorker` in `indexing/subgraph.go`). `VectorRetrievalExecutor`, `LogDispatchWorker`, `TSO Worker`, `EmbeddingBuilderWorker`, `TensorProjectionWorker`, `AccessControlWorker`, and `SharedMemorySyncWorker` are planned for v1.x / v2+.

### 4 Flow Chains

Defined in [`src/internal/worker/chain/chain.go`](src/internal/worker/chain/chain.go).

#### 🔴 Main Chain — primary write path

```
Request
  ↓
IngestWorker           (schema validation)
  ↓
WAL.Append             (event durability)
  ↓
ObjectMaterializationWorker  (Memory / State / Artifact routing)
  ↓
ToolTraceWorker        (tool_call artefact capture)
  ↓
IndexBuildWorker       (segment + keyword index)
  ↓
GraphRelationWorker    (derived_from edge)
  ↓
Response
```

#### 🟡 Memory Pipeline Chain — cognitive upgrade ladder

```
Event
  ↓
MemoryExtractionWorker    (level-0 episodic memory)
  ↓
MemoryConsolidationWorker (level-0 → level-1 semantic / procedural)
  ↓
SummarizationWorker       (level-1 / level-2 compression)
  ↓
ReflectionPolicyWorker    (TTL decay · quarantine · confidence override)
  ↓
PolicyDecisionLog
```

#### 🔵 Query Chain — retrieval + reasoning

```
QueryRequest
  ↓
TieredDataPlane.Search (hot → warm → cold)
  ↓
Assembler.Build
  ↓
EvidenceCache.GetMany + BulkEdges (1-hop graph expansion)
  ↓
ProofTraceWorker       (explainable trace assembly)
  ↓
QueryResponse{Objects, Edges, Provenance, ProofTrace}
```

#### 🟢 Collaboration Chain — multi-agent coordination

```
Agent A writes Memory
  ↓
ConflictMergeWorker    (last-writer-wins, conflict_resolved edge)
  ↓
CommunicationWorker    (copy winner → target agent memory space)
  ↓
Shared Memory updated
```

### ExecutionOrchestrator

The `Orchestrator` provides a priority-aware worker pool over the four chains:

| Priority | Level | Used by |
|---|---|---|
| `PriorityUrgent` (3) | urgent | system health tasks |
| `PriorityHigh` (2) | high | ingest pipeline |
| `PriorityNormal` (1) | normal | memory pipeline, collaboration |
| `PriorityLow` (0) | low | background summarization |

Backpressure is enforced per priority queue (default 256 slots). Dropped tasks are counted in `OrchestratorStats.Dropped`.

## Canonical Objects in v1

The main v1 objects are:

- `Agent`
- `Session`
- `Event`
- `Memory`
- `State`
- `Artifact`
- `Edge`
- `ObjectVersion`

The current authoritative Go definitions live in [`src/internal/schemas/canonical.go`](src/internal/schemas/canonical.go).

## Query Contract in v1

The implemented ingest-to-query path:

`event ingest → canonical object materialization → retrieval projection → tiered search (hot→warm→cold) → 1-hop graph expansion → pre-computed evidence merge → structured QueryResponse`

The `QueryResponse` returned from every query includes:

- `Objects` — retrieved object IDs ranked by lexical score
- `Edges` — 1-hop graph neighbours of all retrieved objects
- `Provenance` — list of pipeline stages that contributed (`event_projection`, `retrieval_projection`, `fragment_cache`, `graph_expansion`)
- `Versions` — object version records (populated by version-aware queries)
- `AppliedFilters` — filters derived from the request by the `PolicyEngine`
- `ProofTrace` — step-by-step trace of how the response was assembled

Go contracts live in [`src/internal/schemas/query.go`](src/internal/schemas/query.go). Richer intended semantics are documented in the schema docs below.

## Quick Start

### Prerequisites

- Go toolchain
- Python 3
- `pip`

### Install Python SDK dependencies

```bash
pip install -r requirements.txt
pip install -e ./sdk/python
```

### Start the dev server

```bash
make dev
```

By default the server listens on `127.0.0.1:8080`. You can override it with `ANDB_HTTP_ADDR`.

### Seed a mock event

```bash
python scripts/seed_mock_data.py
```

### Run the demo query

```bash
python scripts/run_demo.py
```

### Run tests

```bash
make test
```

## Integration Tests

The integration test suite lives under `integration_tests/` and is split into two complementary layers:

| Layer | Location | What it tests |
|---|---|---|
| **Go HTTP tests** | `integration_tests/*_test.go` | All HTTP API routes, protocol, data-flow, topology — pure stdlib, no extra deps |
| **Python SDK tests** | `integration_tests/python/` | `AndbClient.ingest_event()` / `.query()` SDK wrapper + optional S3 dataflow |

### Prerequisites

- Go server is running: `make dev`
- For Python SDK tests: `pip install -r requirements.txt && pip install -e ./sdk/python`

### Run all integration tests

```bash
make integration-test
```

This runs `go test ./integration_tests/... -v` followed by `python integration_tests/python/run_all.py`.

### Run only Go tests

```bash
go test ./integration_tests/... -v -timeout 120s
```

### Run only Python SDK tests

```bash
cd integration_tests/python && python run_all.py
```

### Go test coverage

| File | Tests |
|---|---|
| `healthz_test.go` | `GET /healthz` — status 200, Content-Type |
| `ingest_query_test.go` | Ingest ack fields, LSN monotonicity, query evidence fields, top\_k, 400/405, E2E |
| `canonical_crud_test.go` | POST + GET for agents, sessions, memory, states, artifacts, edges, policies, share-contracts |
| `negative_test.go` | 405 on wrong method, 400 on malformed JSON, 404 on unknown routes |
| `protocol_test.go` | `Content-Type: application/json` on all response paths |
| `dataflow_test.go` | `provenance`, `proof_trace`, `applied_filters`, `edges`, `versions` after ingest→query |
| `topology_test.go` | `/v1/admin/topology` node count, `state=ready`, field presence, 405 |
| `s3_dataflow_test.go` | Ingest→query capture round-trip to S3 (**skipped** unless `ANDB_RUN_S3_TESTS=true`) |

### Optional: S3/MinIO dataflow test

The S3 test (available in both Go and Python layers) ingests an event, runs a query, serialises the full capture as JSON, writes it to a MinIO bucket, and reads it back to verify byte-exact round-trip integrity.

**Start MinIO locally** (choose one):

```bash
# Docker
docker run -d --name minio -p 9000:9000 \
  -e MINIO_ROOT_USER=minioadmin -e MINIO_ROOT_PASSWORD=minioadmin \
  quay.io/minio/minio server /data

# Binary (macOS arm64)
curl -sSL https://dl.min.io/server/minio/release/darwin-arm64/minio -o /usr/local/bin/minio
chmod +x /usr/local/bin/minio
MINIO_ROOT_USER=minioadmin MINIO_ROOT_PASSWORD=minioadmin minio server /tmp/minio-data --address :9000
```

**Run with S3 enabled:**

```bash
export ANDB_RUN_S3_TESTS=true
export S3_ENDPOINT=127.0.0.1:9000
export S3_ACCESS_KEY=minioadmin
export S3_SECRET_KEY=minioadmin
export S3_BUCKET=andb-integration
export S3_SECURE=false

make integration-test
```

### Environment variables

| Variable | Default | Description |
|---|---|---|
| `ANDB_BASE_URL` | `http://127.0.0.1:8080` | Server address for all tests |
| `ANDB_HTTP_TIMEOUT` | `10` | HTTP timeout in seconds (Python SDK) |
| `ANDB_RUN_S3_TESTS` | _(empty)_ | Set to `true` to enable S3 dataflow tests |
| `S3_ENDPOINT` | — | MinIO/S3 host:port |
| `S3_ACCESS_KEY` | — | Access key |
| `S3_SECRET_KEY` | — | Secret key |
| `S3_BUCKET` | — | Bucket name |
| `S3_SECURE` | `false` | Use TLS |
| `S3_REGION` | `us-east-1` | Region (MinIO ignores this) |
| `S3_PREFIX` | `andb/integration_tests` | Object key prefix |

To run only the Go internal module tests:

```bash
go test ./src/internal/... -count=1 -timeout 30s
```

All 12 packages have their own `*_test.go` file. See [`docs/contributing.md`](docs/contributing.md) for the module-level test specification.

## Repository Structure

```text
agent-native-db/
├── README.md
├── configs/
├── cpp/
├── docs/
├── sdk/
├── scripts/
├── src/
├── tests/
├── Makefile
├── go.mod
├── pyproject.toml
└── requirements.txt
```

## Core Documentation

- [Architecture Overview](docs/architecture/overview.md)
- [Main Flow](docs/architecture/main-flow.md)
- [Canonical Objects](docs/schema/canonical-objects.md)
- [Query Schema](docs/schema/query-schema.md)
- [Contributing](docs/contributing.md)
- [v1 Scope](docs/v1-scope.md)

Additional supporting docs already in the repo:

- [Layered Design](docs/architecture/layered-design.md)
- [Module Contracts](docs/architecture/module-contracts.md)
- [API Overview](docs/api/overview.md)
- [Milvus Migration Status](docs/architecture/milvus-migration-status.md)
- [Milvus Source Map](docs/architecture/milvus-source-map.md)
- [Extension Points](docs/architecture/extension-points.md)
- [Nodes and Storage Initialization](docs/architecture/nodes-storage.md)
- [Ingest API](docs/api/ingest.md)
- [Query API](docs/api/query.md)

## Roadmap

### v1 — current

- End-to-end event ingest and structured-evidence query
- Tiered hot → warm → cold retrieval over canonical-object projections
- 1-hop graph expansion in every `QueryResponse`
- Pre-computed `EvidenceFragment` cache merged into `ProofTrace` at query time
- Go HTTP API with 14 routes, Python SDK, and integration test suite

### v1.x — near-term

- Benchmark comparison against simple top-k retrieval
- Time-travel queries using WAL `Scan` replay
- Multi-agent session isolation and scope enforcement

### v2+ — longer-term

- Policy-aware retrieval and visibility enforcement
- Stronger version and time semantics
- Share contracts and governance objects
- Richer graph reasoning and proof replay
- Tensor memory operators
- Cloud-native distributed orchestration

For design philosophy and contribution guidelines, see [`docs/v1-scope.md`](docs/v1-scope.md) and [`docs/contributing.md`](docs/contributing.md).

---

## Integration Branch — Team Review Notes

> **Branch:** `integration/all-features-test`
> **Last updated:** 2026-03-23 (integration-lead pass 3)
> **Status:** All 21 Go internal packages pass (`go test ./src/internal/... exit 0`; `go vet ./... exit 0`). Six worker sub-packages have `*_test.go` files. Pass 2: 5 pipeline correctness fixes. Pass 3: 3 graph/storage structural fixes (R2/R6/R7) — see summaries below.
> **Note:** This section exists only on `integration/all-features-test` and is intentionally not present on `main`.

### Integration Lead — Summary of Changes (Pass 2)

| Category | Change | File(s) |
|---|---|---|
| **Bug fix** | `QueryRequest.ObjectTypes` / `MemoryTypes` were never propagated — `DefaultQueryPlanner.Build` ignored them, `SearchInput` had no such fields, `Assembler.Build` never filtered. Fixed end-to-end: fields added to `QueryPlan` + `SearchInput`; `Assembler.filterByObjectTypes` uses ID-prefix heuristic + optional `ObjectStore` confirmation | `semantic/operators.go`, `dataplane/contracts.go`, `evidence/assembler.go`, `worker/runtime.go` |
| **Bug fix** | `ExpandFromRequest` called `OneHopExpand` on only the **first** seed ID — all additional seeds silently dropped, multi-seed subgraph queries returned incomplete graphs | `schemas/graph_expand.go` |
| **Bug fix** | `Runtime.SubmitIngest` bypassed `IngestWorker` validation — events written to WAL before workers could reject malformed payloads. `DispatchIngestValidation` now runs before `WAL.Append` | `worker/runtime.go` |
| **Feature** | `QueryResponse.Versions` was always `[]` — `Assembler.resolveVersions` now looks up the latest `ObjectVersion` for every returned object ID; `SnapshotVersionStore` wired via `WithVersionStore` in bootstrap | `evidence/assembler.go`, `app/bootstrap.go` |
| **Feature** | Governance annotations (quarantine flag, retracted state) now surface in `QueryResponse.ProofTrace` entries prefixed `governance:*`; `PolicyStore` wired via `WithPolicyStore` in bootstrap | `evidence/assembler.go`, `app/bootstrap.go` |
| **Bug fix** | `CollaborationChain.Run` always returned `LeftMemID` as winner regardless of LWW result → replaced with `DispatchConflictMergeWithWinner` that calls `Run` on the merge worker and returns the actual high-version survivor | `worker/nodes/manager.go`, `worker/chain/chain.go` |
| **Bug fix** | `S3ColdStore.PutMemory/PutAgent` called `EnsureBucket` via `s3util.PutBytes` on **every write** (one HTTP round-trip per cold write) → added `sync.Once` to `S3ColdStore`, removed `EnsureBucket` from `PutBytes` | `storage/s3store.go`, `s3util/s3util.go` |
| **Bug fix** | `MicroBatchScheduler.Flush()` was never called anywhere in the codebase; payloads enqueued by `CollaborationChain` accumulated indefinitely → `EventSubscriber.drainWAL` now calls `FlushMicroBatch()` after each cycle that processed ≥1 WAL entry | `worker/subscriber.go` |
| **Observability** | `S3ColdStore.GetMemory/GetAgent` silently returned `false` on 404; cold misses were invisible to operators → added `log.Printf("s3cold: miss key=…")` on nil response | `storage/s3store.go` |
| **Doc fix** | README claimed "10 HTTP routes" in 3 places; actual gateway registers 14 routes | `README.md` |
| **Doc fix** | README listed `SubgraphExecutorWorker` as "planned for v1.x/v2+" when it is fully implemented in `indexing/subgraph.go` | `README.md` |
| **Tests** | Added `_test.go` for all 6 worker sub-packages that had none: `cognitive`, `coordination`, `indexing`, `ingestion`, `materialization`, `chain` | `worker/{cognitive,coordination,indexing,ingestion,materialization,chain}/*_test.go` |

### Remaining Open Items (blocking or near-term)

| # | Item | Severity | Owner |
|---|---|---|---|
| ~~R1~~ | ~~`Runtime.ExecuteQuery` does NOT call `BulkEdges` before `QueryChain.Run`~~ | ✅ **FIXED** | D+C — `worker/runtime.go` now pre-fetches edges, calls `DispatchProofTrace` + `DispatchSubgraphExpand` |
| ~~RP1~~ | ~~`QueryRequest.ObjectTypes` / `MemoryTypes` silently ignored — filter never reached `SearchInput` or `Assembler`~~ | ✅ **FIXED (pass 2)** | Lead — `semantic/operators.go`, `dataplane/contracts.go`, `evidence/assembler.go`, `worker/runtime.go` |
| ~~RP2~~ | ~~`ExpandFromRequest` only processed first seed ID — subsequent seeds silently dropped~~ | ✅ **FIXED (pass 2)** | Lead — `schemas/graph_expand.go` |
| ~~RP3~~ | ~~`QueryResponse.Versions` always empty — version store never consulted at query time~~ | ✅ **FIXED (pass 2)** | Lead — `evidence/assembler.go` `resolveVersions`, `WithVersionStore` wired in bootstrap |
| ~~RP4~~ | ~~`Runtime.SubmitIngest` wrote to WAL before `IngestWorker` validation~~ | ✅ **FIXED (pass 2)** | Lead — `worker/runtime.go` calls `DispatchIngestValidation` first |
| ~~R2~~ | ~~`EdgesFrom(id)` is O(n) scan over all edges; O(n×seeds) at scale~~ | ✅ **FIXED (pass 3)** | Lead — `memoryGraphEdgeStore` now uses `srcIdx`/`dstIdx` inverted index maps; O(k) per call |
| ~~R3~~ | ~~`ExecutionOrchestrator` priority queues are unbounded~~| ✅ **RESOLVED** (pre-existing) | Queues bounded at 256/level; excess tasks dropped + `Dropped` counter incremented (`orchestrator.go`) |
| ~~R4~~ | ~~`subscriber.go` panics silently swallowed~~ | ✅ **FIXED** | `safeDispatch` wrapper with `recover()` + `log.Printf` added to `subscriber.go` |
| ~~R5~~ | ~~`S3ColdStore` only covers Memory + Agent~~ | ✅ **FIXED** | `PutState`/`GetState` added to `ColdObjectStore` interface, `InMemoryColdStore`, and `S3ColdStore` (`tiered.go`, `s3store.go`) |
| ~~R6~~ | ~~Edges have no cold-tier path; `GraphEdgeStore` is warm-only indefinitely~~ | ✅ **FIXED (pass 3)** | Lead — `ColdObjectStore` extended; `InMemoryColdStore` + `S3ColdStore` implement edge methods; `TieredObjectStore.ArchiveEdge` added |
| ~~R7~~ | ~~Edge TTL / expiry field (`expires_at`) not modelled; dangling edges after `ArchiveMemory`~~ | ✅ **FIXED (pass 3)** | Lead — `schemas.Edge.ExpiresAt` added; `GraphEdgeStore.PruneExpiredEdges(now)` implemented with index cleanup |
| R8 | Knowhere stub in `cpp/retrieval/` not yet wired to real HNSW/SPARSE calls | Medium | B |
| ~~R9~~ | ~~Python service `/healthz` endpoint missing~~ | ✅ **FIXED** | `run_server()` + `--serve`/`--port` flags added to `retrieval/main.py`; `GET /healthz` returns `{"status":"ok","ready":bool}` |
| ~~R10~~ | ~~`EnqueueMicroBatch` flush integration test missing~~ | ✅ **FIXED** | `TestMicroBatch_FlushIntegration` added to `worker/subscriber_test.go` — verifies CollaborationChain enqueue → FlushMicroBatch → subscriber drain cycle |

**Remaining blockers for main merge:** Only R8 (Knowhere C++ wiring, owner B) remains open. All other known issues are resolved.

The following review checklist is intended for team members before merging `integration/all-features-test` → `main`.

> 👋 **To all members:** Please leave a comment or Slack thread when you complete your section checklist. Cross-dependency items are called out in the [Cross-Member Collaboration](#cross-member-collaboration) section below — check that table first if your work touches a shared interface.

---

### Member A — Schema & Query Filters (`feature/schema-a`)

**Scope merged:** Tenant/workspace filters, object/memory filter predicates on the query path.

| Item | Status | Notes |
|---|---|---|
| `QueryRequest` filter fields wired through `PolicyEngine` | ✅ | See `semantic/policy_engine.go` and `access/gateway.go` `/v1/query` handler |
| Tenant isolation: `workspace_id` and `tenant_id` filter at segment scan | ✅ | `coordinator/query_coordinator.go` applies filters before `DataPlane.Search` |
| Object-type filter (`memory` / `state` / `artifact`) | ✅ **FIXED pass 2** | `ObjectTypes` now propagated through `QueryPlan` → `SearchInput` → `Assembler.filterByObjectTypes` (ID-prefix heuristic + `ObjectStore` confirmation) |
| `StateMaterializationWorker` now also dispatched in `MainChain` | ✅ | Verify `State` objects honour `ObjectTypes` filter — `state` type must not leak into `memory`-only queries |
| **Review focus ✅ FIXED pass 2** | ✅ | `filterByObjectTypes` returns all IDs unchanged when `ObjectTypes` is empty — verified by `TestAssembler_NoFilterPassthrough` |
| **Review focus** | ⚠️ | `workspace_id` propagation: events written without `WorkspaceID` must still be queryable under default namespace |
| **Review focus** | ⚠️ | `QueryChainInput.EdgeTypeFilter` is now available — verify `QueryRequest` edge-type filter field (if any) propagates to `SubgraphExecutorWorker` via `EdgeTypeFilter` |
| **Review focus** | ⚠️ | `QueryChainInput.GraphNodes` / `GraphEdges` must be pre-fetched by the caller before `QueryChain.Run`; confirm the gateway handler populates these from `store.Edges().BulkEdges` before dispatching |
| Edge case: combined tenant + object-type + top_k filter | 🔲 | Integration test covering the 3-way combination |
| Edge case: `ObjectTypes=["state"]` query after a `tool_call` ingest | 🔲 | `StateMaterializationWorker` writes `State` on `tool_call` — confirm it is retrievable with type filter |

---

### Member B — Python Retrieval Service (`feature/retrieval-b`)

**Scope merged:** Dense/sparse retrieval Python service (C++ core + Python thin wrapper), policy filter, version filter, RRF merger, gRPC proto.

#### Architecture: Python Thin Wrapper + C++ Core

```
┌──────────────────────────────────────────────────┐
│                  Python Layer                     │
│  src/internal/retrieval/                          │
│  - main.py (entry point, --dev flag)              │
│  - service/retriever.py (thin wrapper, calls C++) │
│  - service/types.py (type definitions)            │
└──────────────────────────────────────────────────┘
                         │ pybind11
                         ▼
┌──────────────────────────────────────────────────┐
│                   C++ Layer                       │
│  cpp/                                             │
│  ├── include/andb/                                │
│  │   ├── types.h    (Candidate, SearchResult)     │
│  │   ├── dense.h    (DenseRetriever — HNSW)       │
│  │   ├── sparse.h   (SparseRetriever)             │
│  │   ├── filter.h   (FilterBitset — BitsetView)   │
│  │   ├── merger.h   (RRF merge + reranking)       │
│  │   └── retrieval.h (Unified Retriever + C API)  │
│  ├── retrieval/                                   │
│  │   ├── dense.cpp  (Knowhere HNSW)               │
│  │   ├── sparse.cpp (Knowhere SPARSE_INDEX)       │
│  │   ├── filter.cpp (BitsetView mechanism)        │
│  │   ├── merger.cpp (RRF k=60, reranking)         │
│  │   └── retrieval.cpp (Unified)                  │
│  ├── python/bindings.cpp (pybind11)               │
│  └── CMakeLists.txt                               │
└──────────────────────────────────────────────────┘
```

#### Three-Path Parallel Retrieval (C++)

| Path | Implementation | Description |
|---|---|---|
| **Dense** | `cpp/retrieval/dense.cpp` | Knowhere HNSW, Search with BitsetView |
| **Sparse** | `cpp/retrieval/sparse.cpp` | Knowhere SPARSE_INVERTED_INDEX |
| **Filter** | `cpp/retrieval/filter.cpp` | BitsetView passed to Search call |

**RRF Fusion** (`cpp/retrieval/merger.cpp`):
```
RRF_score(d) = Σ 1/(k + rank_i(d))    k=60 (configurable)
```

**Reranking formula**:
```cpp
final_score = rrf_score * max(importance, 0.01f)
                        * max(freshness_score, 0.01f)
                        * max(confidence, 0.01f)
```

**Seed marking**: candidates with `final_score >= seed_threshold` (default 0.7) set `is_seed=true` — used by `SubgraphExecutorWorker` for graph expansion.

#### Building the C++ Module

```bash
cd cpp && mkdir build && cd build
cmake .. -DANDB_WITH_PYBIND=ON
make -j$(nproc)
```

| CMake Option | Default | Description |
|---|---|---|
| `ANDB_WITH_PYBIND` | ON | Build pybind11 Python bindings |
| `ANDB_WITH_GPU` | OFF | GPU support via Knowhere RAFT |

Platforms: Ubuntu 20.04 x86_64/aarch64, macOS x86_64, macOS Apple Silicon.

#### ⚡ Dual-Interface Ownership (Python ↔ Go) — B's Primary Responsibility

Member B is the **sole owner** of the contract boundary between the Python retrieval service and the Go HTTP layer. Any change to either side must be confirmed with the other side before merging.

**Go-side interface (owned by B, implemented in Go):**

| Go location | Python counterpart | What must stay in sync |
|---|---|---|
| `schemas.QueryRequest` (field names + JSON tags) | `andb_sdk/client.py` → `query()` kwargs | Field names, types, and omitempty rules |
| `schemas.QueryResponse` (JSON shape) | `andb_sdk/retrieval.py` → response parsing | `objects`, `edges`, `proof_trace`, `applied_filters` keys |
| `access/gateway.go` `/v1/query` POST body | `retrieval/service/retriever.py` request builder | HTTP method, path, Content-Type |
| `access/gateway.go` `/v1/ingest` POST body | `andb_sdk/client.py` → `ingest_event()` | `event_id`, `agent_id`, `session_id`, `payload` field presence |

**Proto contract (B must keep aligned):**

| File | Rule |
|---|---|
| `src/internal/retrieval/proto/retrieval.proto` | Proto field numbers must NOT change once merged — add new fields only, never renumber |
| gRPC service name | Must match the Go client stub if one is ever generated; agree with D before adding Go gRPC client |

**Checklist before B marks their section done:**

| Item | Status | Notes |
|---|---|---|
| Python service files in `src/internal/retrieval/` | ✅ | |
| C++ core in `cpp/` with pybind11 bindings | ✅ | 2026-03-20 migration complete |
| gRPC proto field names align with Go `schemas.QueryRequest` | ✅ | |
| `PolicyFilter` runs before `VersionFilter` in `retriever.py` | ✅ | |
| Knowhere stub → real HNSW / SPARSE calls wired | 🔲 | Replace stub with actual Knowhere index calls |
| SDK `query()` kwargs match current `QueryRequest` JSON shape | 🔲 | Run `integration_tests/python/run_all.py` |
| SDK `ingest_event()` matches current `/v1/ingest` body | 🔲 | Cross-check `workspace_id` field with A |
| Python service `/healthz` endpoint | 🔲 | Needed for K8s readiness probe |
| Retry back-off in `merger.py` on upstream timeout | 🔲 | Add exponential back-off, max 3 retries |
| GPU support via Knowhere RAFT | 🔲 | v1.x / v2+ scope |
| No auth/TLS on Python service port | ⚠️ | Do NOT expose directly; require sidecar proxy |
| **Review focus** | ⚠️ | C++ `is_seed=true` candidates → their IDs should map to `QueryChainInput.ObjectIDs` passed to `SubgraphExecutorWorker`; verify the Go gateway correctly extracts seed IDs from retrieval results before calling `QueryChain.Run` |
| **Review focus** | ⚠️ | `proof_trace` field in `QueryResponse` may now contain up to depth=8 BFS steps (previously 1-hop); B's Python integration tests that assert `len(proof_trace) == N` must be updated to use `>= 1` instead of exact count |
| **Review focus** | ⚠️ | When `S3ColdStore` is active, cold-path `GetMemory` adds HTTP round-trip latency; B's timeout settings in `retriever.py` may need to be increased from default if cold reads are expected during integration tests |

---

### Member C — Graph Relations (`feature/graph-c`)

**Scope merged:** `GraphRelationWorker`, `GraphEdgeStore.BulkEdges`, 1-hop expansion in `evidence.Assembler`, subgraph seed expansion.

| Item | Status | Notes |
|---|---|---|
| `GraphRelationWorker.IndexEdge` wired in `MainChain` | ✅ | `chain.go` step 4: `derived_from` edge written per ingest |
| `BulkEdges(objectIDs)` 1-hop expansion in `Assembler.Build` | ✅ | Returns typed `[]schemas.Edge` in every `QueryResponse` |
| `GraphEdgeStore.DeleteEdge` contract defined | ✅ | Available but not yet called in any worker path |
| `SubgraphExecutorWorker` uses `schemas.ExpandFromRequest` → `OneHopExpand` | ✅ | Internally calls `EdgesFrom` + `EdgesTo` per seed node |
| **Review focus ✅ FIXED pass 3** | ✅ | `EdgesFrom`/`EdgesTo` now use `srcIdx`/`dstIdx` secondary maps — O(k) per node degree, no full scan |
| **Review focus** | ⚠️ | `OneHopExpand` in `schemas/graph_expand.go` iterates ALL edges to find matches — verify `BulkEdges` (used in `Assembler.Build`) and `OneHopExpand` (used in `SubgraphExecutorWorker`) return consistent results for the same seed set |
| **Review focus** | ⚠️ | Multi-hop BFS in `ProofTraceWorker` caps at depth=8; verify no cycles in test graph |
| **Review focus** | ⚠️ | `conflict_resolved` edges from `ConflictMergeWorker` not surfaced in `QueryResponse.Edges` — confirm whether this is intentional; if not, wire `CollaborationChain` output back to edge store |
| **Review focus ✅ FIXED pass 3** | ✅ | `TieredObjectStore.ArchiveEdge(warmEdges, edgeID)` moves any edge warm→cold and deletes from warm; eliminates dangling-edge problem when called after `ArchiveMemory` |
| Missing: `GraphEdges` pre-fetch caller responsibility | 🔲 | `QueryChainInput.GraphEdges` must be pre-populated by the runtime before calling `QueryChain.Run`; currently the gateway does not call `BulkEdges` before dispatch — C and D must agree on who owns this call |
| ~~Missing: edge TTL / expiry~~ ✅ **FIXED pass 3** | ✅ | `schemas.Edge.ExpiresAt` added; `GraphEdgeStore.PruneExpiredEdges(now string) int` implemented with `srcIdx`/`dstIdx` cleanup |
| ~~Missing: cold-tier edge store~~ ✅ **FIXED pass 3** | ✅ | `ColdObjectStore` extended with `PutEdge`/`GetEdge`/`ListEdges`; implemented in `InMemoryColdStore` (full) and `S3ColdStore` (point-read; `ListEdges` not supported) |

---

### Member D — Worker Architecture Refactor

**Scope merged:** Worker split into 5 domain subpackages, `Create*` naming convention, multi-hop ProofTrace, DerivationLog integration, SubgraphExecutorWorker (L5), StateMat + MicroBatch wiring.

| Item | Status | Notes |
|---|---|---|
| Worker subpackages: `ingestion` / `materialization` / `cognitive` / `indexing` / `coordination` | ✅ | Each worker in its own file |
| `nodes/` retains only contracts + Manager + DataNode/IndexNode/QueryNode | ✅ | Split into `data_node.go`, `index_node.go`, `query_node.go` |
| All constructors renamed `Create*` | ✅ | `eventbackbone` retains `New*` (out of scope) |
| `ProofTraceWorker.AssembleTrace` upgraded to BFS with configurable `maxDepth` | ✅ | Default cap = 8; pass `maxDepth=1` for legacy |
| `ToolTraceWorker` appends to `DerivationLog` on `tool_call`/`tool_result` | ✅ | Enables causal path walk in ProofTrace |
| `QueryChainInput.MaxDepth` field for caller-controlled trace depth | ✅ | Default 0 → 8 internally |
| `SubgraphExecutorWorker` (`indexing/subgraph.go`) registered as `subgraph-1` | ✅ | Wraps `schemas.ExpandFromRequest`; wired in `QueryChain` step 2 |
| `StateMaterializationWorker` dispatched in `MainChain` | ✅ | Alongside `ObjectMaterializationWorker` at step 1 |
| `EnqueueMicroBatch` called in `CollaborationChain` after conflict resolution | ✅ | Payload: `winner_id`, `source_agent_id`, `target_agent_id` |
| `TieredObjectStore` registered as `"tiered_objects"` in coordinator registry | ✅ | Hot+warm+cold wired; cold = S3 or in-memory per env |
| **Review focus** | ⚠️ | `subscriber.go` goroutines have no dead-letter channel — panics are silently lost; add structured error reporting before production |
| **Review focus** | ⚠️ | `ExecutionOrchestrator` queues are unbounded; add hard cap + back-pressure signal to prevent OOM under burst ingest |
| **Review focus** | ⚠️ | `QueryChainInput.GraphNodes` / `GraphEdges` must be pre-fetched by the caller — the current `Runtime.ExecuteQuery` path does NOT call `BulkEdges` before `QueryChain.Run`; `SubgraphExecutorWorker` will silently return empty subgraph until this is wired |
| **Review focus** | ⚠️ | `ReflectionPolicyWorker` triggers `ArchiveMemory`-style eviction — confirm it uses `tiered_objects.ArchiveMemory()` rather than directly calling `store.Objects()` to ensure cold-tier promotion works |
| **Review focus** ✅ **FIXED** | ✅ | `MicroBatchScheduler.Flush()` now called in `EventSubscriber.drainWAL` after each WAL cycle that processes ≥1 entry (`worker/subscriber.go`) |
| Per-subpackage unit tests ✅ **FIXED** | ✅ | `cognitive/`, `coordination/`, `indexing/`, `ingestion/`, `materialization/`, `chain/` all have `_test.go` (added in integration-lead pass) |
| `ProofTraceWorker` BFS cycle detection test ✅ **FIXED** | ✅ | `TestProofTraceWorker_AssembleTrace_CyclicGraph_Terminates` in `coordination/coordination_test.go` |
| Missing: topology assertion for new workers | 🔲 | `GET /v1/admin/topology` must return `subgraph_executor_worker` type; add to `topology_test.go` expected set |
| ~~Missing: `EnqueueMicroBatch` flush integration test~~ ✅ **FIXED** | ✅ | `TestMicroBatch_FlushIntegration` in `worker/subscriber_test.go` verifies CollaborationChain enqueue → FlushMicroBatch → subscriber drain cycle |
| ~~Blocking (R1)~~ ✅ **FIXED** | ✅ | `Runtime.ExecuteQuery` now pre-fetches edges via `BulkEdges`, runs `DispatchProofTrace` + `DispatchSubgraphExpand` (`worker/runtime.go`) |
| ~~R4~~ ✅ **FIXED** | ✅ | `safeDispatch` wrapper added; handler panics are recovered and logged (`worker/subscriber.go`) |
| ~~R5~~ ✅ **FIXED** | ✅ | `PutState`/`GetState` added to `ColdObjectStore`, `InMemoryColdStore`, `S3ColdStore` (`storage/tiered.go`, `storage/s3store.go`) |
| ~~R10~~ ✅ **FIXED** | ✅ | `TestMicroBatch_FlushIntegration` added to `worker/subscriber_test.go` |

---

### S3 & Cold Storage Module

**Scope merged:** S3-compatible object storage (MinIO) for admin export, snapshot export, and cold-tier archival.

#### Admin API Endpoints (`src/internal/access/gateway.go`)

| Endpoint | Behaviour |
|---|---|
| `POST /v1/admin/s3/export` | Ingest sample event → query → serialize → PUT to S3 → GET round-trip verify |
| `POST /v1/admin/s3/snapshot-export` | Write metadata JSON + manifest Avro + segment data JSON under `S3_PREFIX`; verify all three |

Snapshot key layout:
```
S3_PREFIX/snapshots/<collection_id>/metadata/<snapshot_id>.json
S3_PREFIX/snapshots/<collection_id>/manifests/<snapshot_id>/<segment_id>.avro
S3_PREFIX/segments/<collection_id>/<segment_id>/segment_data.json
```

#### S3 Utility Layer (`src/internal/s3util/s3util.go`)

| Function | Purpose |
|---|---|
| `LoadFromEnv()` | Load config from `S3_ENDPOINT / ACCESS_KEY / SECRET_KEY / BUCKET / SECURE / REGION / PREFIX` |
| `EnsureBucket()` | HEAD → auto-create bucket if absent |
| `PutBytesAndVerify()` | PUT + GET round-trip validation (admin export path) |
| `PutBytes()` | Simple PUT without round-trip verify (cold-store archival path) |
| `GetBytes()` | GET; returns `nil, nil` on 404 |
| `s3Sign()` | stdlib-only AWS Signature V4 (no external SDK) |

#### Cold-Tier Auto-Selection (`src/internal/storage/s3store.go` + `bootstrap.go`)

At startup, `bootstrap.go` selects the cold tier automatically:

```
S3_ENDPOINT + ACCESS_KEY + SECRET_KEY + BUCKET 已设置
  → S3ColdStore  (MinIO / AWS S3 backed)
  → 日志: [bootstrap] cold store: S3 endpoint=... bucket=...

未设置
  → InMemoryColdStore  (in-process simulation, default)
  → 日志: [bootstrap] cold store: in-memory simulation
```

`S3ColdStore` objects stored as:
- `{prefix}/cold/memories/{id}.json`
- `{prefix}/cold/agents/{id}.json`

#### Local One-Click Scripts (`scripts/dev/`)

| Script | Purpose |
|---|---|
| `ensure-docker.ps1` | Verify Docker availability (Windows) |
| `start-minio.ps1` | Start MinIO container for local S3 |
| `run-s3-runtime-export.ps1` | One-click runtime capture export |
| `run-s3-snapshot-export.ps1` | One-click snapshot/segment export |

Run artifacts: `scripts/dev/artifacts/...`

#### Reproducing Locally

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File "scripts/dev/run-s3-snapshot-export.ps1"
```

Expected: `"status": "ok"` with all `roundtrip_ok` flags `true`.  
Run record: `scripts/dev/artifacts/s3-snapshot-export/<timestamp>/record.md`

#### Required env vars

```
S3_ENDPOINT    e.g. 127.0.0.1:9000
S3_ACCESS_KEY  e.g. minioadmin
S3_SECRET_KEY  e.g. minioadmin
S3_BUCKET      e.g. andb-integration
S3_SECURE      false (default)
S3_REGION      us-east-1 (default)
S3_PREFIX      andb/integration_tests (default)
```

#### S3 Module Review Checklist

| Item | Status | Notes |
|---|---|---|
| `LoadFromEnv()` → `S3ColdStore` auto-wired at bootstrap | ✅ | Logged at startup; fallback to `InMemoryColdStore` if env absent |
| `PutBytes` (no-verify) for cold archival path | ✅ | Avoids double HTTP round-trip on every `ArchiveMemory` call |
| `GetBytes` 404 → `nil, nil` (not error) for cold read miss | ✅ | Caller (`S3ColdStore.GetMemory`) silently returns `false` |
| Admin export endpoints round-trip verified | ✅ | `PutBytesAndVerify` used for `/s3/export` and `/s3/snapshot-export` |
| **Review focus** ✅ **FIXED** | ✅ | `S3ColdStore` now has `sync.Once` (`ensureOnce`) — `EnsureBucket` runs at most once per store lifetime; `PutBytes` in `s3util` no longer calls `EnsureBucket` (`storage/s3store.go`, `s3util/s3util.go`) |
| **Review focus** ✅ **FIXED** | ✅ | `GetMemory` / `GetAgent` now log `"s3cold: miss key=…"` on 404 (`storage/s3store.go`) |
| **Review focus** | ⚠️ | `S3ColdStore` only implements `PutMemory / GetMemory / PutAgent / GetAgent`; `ColdObjectStore` interface may need `PutState / GetState` if `StateMaterializationWorker` output is ever promoted to cold tier |
| Missing: S3 integration test in `integration_tests/` | 🔲 | Add `ANDB_RUN_S3_TESTS=true` test that ingests, archives via `ArchiveMemory`, then retrieves via cold path and verifies round-trip |
| Missing: `S3_* → minio.*` unified config mapping | 🔲 | Other runtime modules use different config keys; standardise to `S3_*` prefix across all callers |
| Missing: cold-tier edge archival | 🔲 | `S3ColdStore` has no edge store — `GraphEdgeStore` has no cold path; decide scope before v1.x |

---

## Cross-Member Collaboration

The table below lists every point where two members' work **must be confirmed together** before either side is considered done. Please tag the relevant member when you open a PR or reach this checkpoint.

| # | Interface / Touch Point | Owner | Needs confirmation from | What to verify |
|---|---|---|---|---|
| 1 | `schemas.QueryRequest` JSON shape | **A** | **B** | B's Python SDK `query()` kwargs still map 1:1 after A added workspace/object-type filters |
| 2 | `/v1/ingest` POST body | **A** | **B** | `workspace_id` field added by A — B must update `ingest_event()` in Python SDK |
| 3 | gRPC proto field names | **B** | **D** | If D adds a Go gRPC client stub, B must freeze proto field numbers first |
| 4 | `QueryResponse.edges` shape (`[]schemas.Edge`) | **C** | **B** | B's Python SDK parses `edges` — C changed edge structure; B must update response parsing |
| 5 | `ProofTraceWorker` BFS depth in `QueryResponse.proof_trace` | **D** | **B** | B's integration tests assert on `proof_trace` length; default depth=8 may increase trace size |
| 6 | `GraphEdgeStore.EdgesFrom` O(n) scan | **C** | **D** | D's BFS + D's `SubgraphExecutorWorker` both call `EdgesFrom`; if C replaces with indexed map, D's BFS must be retested |
| 7 | `ToolTraceWorker` → `DerivationLog` → `ProofTrace` chain | **D** | **C** | C's graph edges and D's derivation entries both feed `ProofTrace`; run joint test before merging |
| 8 | Worker `nodes/contracts.go` interface changes | **D** | **A, B, C** | Any interface signature change in `contracts.go` is a breaking change — announce in team chat first |
| 9 | `QueryChainInput.GraphNodes/GraphEdges` pre-fetch | **D** | **C** | D owns the `QueryChain.Run` call site; C owns `BulkEdges`; agree on where in `Runtime.ExecuteQuery` the pre-fetch is inserted |
| 10 | `SubgraphExecutorWorker` seed IDs ← C++ retrieval `is_seed` | **B** | **D** | B's C++ layer marks seeds; D's Go gateway must extract seed IDs and pass to `QueryChainInput.ObjectIDs` before `QueryChain.Run` |
| 11 | `StateMaterializationWorker` output + A's `ObjectTypes` filter | **D** | **A** | D wired `StateMat` in `MainChain`; A must verify `ObjectTypes=["state"]` filter correctly surfaces `State` objects |
| 12 | `MicroBatchScheduler.Flush()` drain trigger | **D** | **C** | D owns the scheduler; C's `ConflictMergeWorker` feeds it — agree on flush cadence (timer / WAL watermark / explicit call) before production |

> 💬 **Suggested flow:** When you finish an item in the table above, post a short message in the team channel: _"#N ready for review by [member]"_. The receiving member should confirm within 24 h or flag a blocker.

---

## Pre-merge Checklist (all members)

> Run this checklist **together as a team** in a short sync call or shared doc before opening the merge PR to `main`.

**Go layer (D runs)**
- [x] `go test ./src/internal/... -count=1 -timeout 30s` — all 21 packages green (verified twice)
- [x] `go vet ./...` — exit 0
- [ ] `GET /v1/admin/topology` returns 18 nodes, including `subgraph_executor_worker` type
- [x] `proof_trace` contains at least one `derivation:` step for `tool_call` events
- [x] `MicroBatchScheduler.Flush()` called in `EventSubscriber.drainWAL`; integration test `TestMicroBatch_FlushIntegration` passes
- [x] ~~R1 blocker~~ `Runtime.ExecuteQuery` now pre-fetches `BulkEdges`, calls `DispatchProofTrace` + `DispatchSubgraphExpand`

**Filter & schema (A runs)**
- [ ] `go test ./integration_tests/... -v -timeout 120s` — all green
- [x] `ObjectTypes` empty → returns all types — verified by `TestAssembler_NoFilterPassthrough`
- [x] `ObjectTypes=["memory"]` excludes `state_*`/`art_*` — verified by `TestAssembler_ObjectTypesFilter`
- [x] `ObjectTypes=["state"]` returns only `state_*` — verified by `TestAssembler_StateTypeFilter`
- [ ] 3-way filter (workspace + object-type + top_k) integration test passes (live server required)
- [ ] `workspace_id` omitted on ingest → queryable under default namespace

**Python & retrieval (B runs)**

_Go side (B owns the Go↔Python contract boundary):_
- [ ] `go test ./src/internal/retrieval/... -count=1` — all green (or no test files yet — add at least one smoke test)
- [ ] `go test ./src/internal/schemas/... -count=1` — `QueryRequest` / `QueryResponse` JSON tags match proto field names in `retrieval.proto`
- [ ] `go vet ./src/internal/retrieval/...` — no errors
- [ ] `GET /v1/query` returns `proof_trace`, `edges`, `applied_filters` keys in response JSON (verify with `curl` or `integration_tests/ingest_query_test.go`)
- [ ] `/v1/ingest` accepts `workspace_id` field without error (Go gateway handler)
- [ ] `GET /v1/admin/topology` — verify no retrieval-related worker is in error/degraded state

_Python / C++ side:_
- [ ] `cd integration_tests/python && python run_all.py` — all green
- [ ] `proof_trace` assertion in Python tests uses `>= 1` not exact length
- [ ] SDK `query()` kwargs verified against current `QueryRequest` JSON shape
- [ ] `ingest_event()` includes `workspace_id` field
- [ ] pybind11 C++ module (`cmake .. -DANDB_WITH_PYBIND=ON && make`) builds successfully on CI platform
- [ ] `python -m src.internal.retrieval.main --test` exits 0

**Graph & edges (C runs)**
- [ ] `QueryResponse.edges` non-empty after ingest → query round-trip
- [ ] `SubgraphExecutorWorker` subgraph non-empty when seeds have known edges (requires GraphEdges pre-fetch wired)
- [x] `BulkEdges` and `EdgesFrom`/`EdgesTo` use indexed lookups — O(k) per node — verified by `TestMemoryGraphEdgeStore_EdgesFrom_Indexed` + `TestMemoryGraphEdgeStore_BulkEdges`
- [ ] Cyclic graph BFS terminates at `maxDepth=8`
- [x] Edge cold-tier: `TieredObjectStore.ArchiveEdge` moves edges warm→cold — verified by `TestTieredObjectStore_ArchiveEdge`
- [x] Edge TTL: `PruneExpiredEdges` removes expired edges and cleans indices — verified by `TestMemoryGraphEdgeStore_PruneExpiredEdges`

**S3 cold storage (D + any member with MinIO access)**
- [ ] Server starts with S3 env vars → logs `cold store: S3 endpoint=...`
- [ ] Server starts without S3 env vars → logs `cold store: in-memory simulation`
- [ ] `ANDB_RUN_S3_TESTS=true go test ./integration_tests/... -run TestS3` passes with MinIO running
- [x] `EnsureBucket` called only once per `S3ColdStore` lifetime (sync.Once fix applied in integration-lead pass)

**Final gates (all members)**
- [ ] No new `TODO`/`FIXME` markers in committed code
- [ ] Cross-member table: all **12** items confirmed ✅
- [ ] Squash-merge or fast-forward only — no merge bubbles on `main`
- [ ] Tag release commit with `v1.0.0-integration-rc1` before merging
