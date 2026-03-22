# CogDB — Agent-Native Database for Multi-Agent Systems

CogDB (ANDB) is an agent-native database for multi-agent systems (MAS). It combines a tiered segment-oriented retrieval plane, an event backbone with an append-only WAL, a canonical object materialization layer, pre-computed evidence fragments, 1-hop graph expansion, and structured evidence assembly — all wired together as a single runnable Go server.

> **Core thesis:** agent memory, state, event, artifact, and relation should be modeled as first-class database objects, and query results should return structured evidence rather than only top-k text fragments.

## What is implemented

<<<<<<< HEAD
- Go server ([`src/cmd/server/main.go`](src/cmd/server/main.go)) with 14 HTTP routes
=======
## Project Status

This repository is in the **runnable-prototype** stage.  The main ingest/query path is fully wired end-to-end.

What is implemented today:

- Runnable Go server in [`src/cmd/server/main.go`](src/cmd/server/main.go) with 11 HTTP routes (incl. `GET /v1/admin/storage`)
>>>>>>> a2c07755 (feat(storage): add memory and Badger backends with hybrid composition)
- Append-only WAL with `Scan` and `LatestLSN` for replay and watermark tracking
<<<<<<< HEAD
- `MaterializeEvent` → `MaterializationResult` producing canonical `Memory`, `ObjectVersion`, and typed `Edge` records at ingest time
- Three-tier data plane: **hot** (in-memory LRU) → **warm** (segment index) → **cold** (archived tier), behind a unified `DataPlane` interface
=======
- `MaterializeEvent` → `MaterializationResult` that produces a canonical `Memory`, `ObjectVersion`, typed `Edge` records, and optional `State` / `Artifact` (+ versions) at ingest time
- Three-tier data plane: **hot** (in-memory LRU cache) → **warm** (full segment index) → **cold** (archived tier), all behind a unified `DataPlane` interface
>>>>>>> 0c64d888 (feat(worker): extract PipelineIngestWorker and wire ingest module registry)
- Pre-computed `EvidenceFragment` cache populated at ingest, merged into proof traces at query time
- 1-hop graph expansion via `GraphEdgeStore.BulkEdges` in the `Assembler.Build` path
<<<<<<< HEAD
- `QueryResponse` with `Objects`, `Edges`, `Provenance`, `ProofTrace`, `Versions`, and `AppliedFilters` on every query
- Module-level test coverage: 12 packages each with a `*_test.go` file
- Python SDK (`sdk/python`) and demo scripts
- Full architecture, schema, and API documentation
=======
- `QueryResponse` returns `Objects`, `Edges`, `Provenance`, and `ProofTrace` in every response
- Module-level test coverage: 12 packages each with their own `*_test.go` file
- Python SDK bootstrap and demo scripts
- Architecture, schema, and milestone documentation

**Storage backends (design):** Memory vs Badger vs hybrid per sub-store — see [`STORAGE_BACKEND.md`](STORAGE_BACKEND.md) (implementation may be in progress).

What is still intentionally lightweight in v1:

- Distributed runtime and persistence
- Full policy/governance execution
- Deep proof construction beyond 1-hop
- Production-grade indexing and optimization
>>>>>>> a2c07755 (feat(storage): add memory and Badger backends with hybrid composition)

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
          ├─ PipelineIngestWorker.Accept  (ingest pipeline; see below)
          │     └─ WAL + Bus  (eventbackbone)
          │     └─ MaterializeEvent → Memory / ObjectVersion / Edges / optional State·Artifact  (materialization)
          │     └─ PreComputeService → EvidenceFragment cache  (materialization)
          │     └─ nodeManager.DispatchIngest → TieredDataPlane.Ingest  (dataplane + worker nodes)
          └─ Assembler.Build → BulkEdges + EvidenceCache  (query path only)
```

**Ingest path (code):** `API → Runtime.SubmitIngest → PipelineIngestWorker.Accept` — internally:

`validate(event_id) → WAL.Append → MaterializeEvent → PutMemory/PutState/PutArtifact + PutVersion + PutEdge → PreCompute → HotCache (if salient) → DispatchIngest(data/index nodes) → DataPlane.Ingest`

The active pipeline is also registered on the coordinator **module registry** as `ingest_worker` (see `app.BuildServer`). When a `WorkerScheduler` is attached, each successful `Accept` records a `WorkerTypeIngest` dispatch/complete pair for metrics.

**Query path:**
`API → TieredDataPlane.Search → Assembler.Build → EvidenceCache.GetMany + BulkEdges(1-hop) → QueryResponse{Objects, Edges, ProofTrace}`

Code layout:

<<<<<<< HEAD
- [`src/internal/access`](src/internal/access): HTTP gateway, 14 routes including ingest, query, and canonical CRUD
=======
- [`src/internal/access`](src/internal/access): HTTP gateway, 11 routes including ingest, query, canonical CRUD, and `GET /v1/admin/storage`
>>>>>>> a2c07755 (feat(storage): add memory and Badger backends with hybrid composition)
- [`src/internal/coordinator`](src/internal/coordinator): 9 coordinators (schema, object, policy, version, worker, memory, index, shard, query) + module registry
- [`src/internal/eventbackbone`](src/internal/eventbackbone): WAL (`Append`/`Scan`/`LatestLSN`), Bus, HybridClock, WatermarkPublisher, DerivationLog
- [`src/internal/worker`](src/internal/worker): `Runtime.SubmitIngest` (delegates to `IngestWorker.Accept`), `PipelineIngestWorker`, `Runtime.ExecuteQuery`, `Runtime.IngestWorker()` / registry key `ingest_worker`
- [`src/internal/worker/nodes`](src/internal/worker/nodes): 14 worker-node type contracts (data, index, query, memory extraction, graph, proof trace, etc.)
- [`src/internal/dataplane`](src/internal/dataplane): `TieredDataPlane` (hot/warm/cold), `SegmentDataPlane`, and `DataPlane` interface
- [`src/internal/dataplane/segmentstore`](src/internal/dataplane/segmentstore): `Index`, `Shard`, `Searcher`, `Planner` — the physical segment layer
- [`src/internal/materialization`](src/internal/materialization): `Service.MaterializeEvent` → `MaterializationResult` (record, memory, version, edges, optional state/artifact); `PreComputeService`
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

### Validate ingest path locally (dev helper)

Use the week2 mock batch to validate ingest materialization quickly:

```bash
powershell -ExecutionPolicy Bypass -File scripts/dev/run-mock-events-week2.ps1
```

This posts events from `scripts/dev/mock-events-week2.json` and prints ingest acknowledgements.
For endpoint details, ACK fields (`memory_id`, optional `state_id` / `artifact_id`), runtime order, and implementation pointers (`PipelineIngestWorker`, `ingest_worker` registry), see [`docs/api/ingest.md`](docs/api/ingest.md).

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

This subsection covers (a) **local runtime storage: memory vs disk** (recent change) and (b) **optional S3/MinIO round-trip tests**. The S3/MinIO flow is separate from where ANDB keeps its canonical objects on the machine.

#### The two storage interfaces — memory vs disk

Here **「两个接口」** means **two implementations of the same `RuntimeStorage` contract**: one keeps data **in process memory**, one persists via **Badger on disk** (same public HTTP API either way).

| 接口 / 实现 | 数据放哪 | 说明 |
|-------------|----------|------|
| **内存** | 进程内 map（`MemoryRuntimeStorage`） | 默认；快；**重启即丢** |
| **磁盘** | Badger 目录 `ANDB_DATA_DIR`（默认 `.andb_data`） | `ANDB_STORAGE=disk` 或 hybrid 里把某个子 store 设为 `disk`；**可跨重启保留** |

测试或磁盘空间紧张时可用 **`ANDB_BADGER_INMEMORY=true`**：仍走 Badger 代码路径，但 **不落本地文件**（数据只在内存里的 Badger 实例中）。

**混合：** `ANDB_STORAGE=hybrid` + `ANDB_STORE_*` 可指定每个子 store（segments、objects 等）单独用内存或磁盘。详见 [`STORAGE_BACKEND.md`](STORAGE_BACKEND.md)。

**查看当前解析结果：** `GET /v1/admin/storage`（v1 仍 **不** 持久化 WAL，`wal_persistence` 为 `false`）。

#### S3/MinIO — dev-only admin routes (not the memory/disk split)

Optional **object export to S3-compatible storage** uses **two `POST` admin handlers** (SigV4; MinIO is the usual local server). These are **only** for validating upload/read-back to **remote** object storage — they do **not** choose between “save in RAM” vs “save on local disk” (that is entirely `RuntimeStorage` + env vars above).

| Endpoint | What it does |
|----------|----------------|
| **`POST /v1/admin/s3/export`** | Sample ingest + query in-process → one capture JSON **PUT** to bucket → **GET** back → `roundtrip_ok` |
| **`POST /v1/admin/s3/snapshot-export`** | Writes snapshot-style keys under `S3_PREFIX` (metadata, Avro manifest, segment JSON) → read back each → per-artifact `roundtrip_ok` |

The **Go integration test** `s3_dataflow_test.go` and the **Python** layer follow the same pattern as **`/v1/admin/s3/export`**: ingest → query → upload capture JSON → read back for byte-level verification. Enable them with `ANDB_RUN_S3_TESTS=true` after MinIO is up.

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
| `ANDB_STORAGE` | `memory` | Runtime store mode: `memory`, `disk`, or `hybrid` (see [`STORAGE_BACKEND.md`](STORAGE_BACKEND.md)) |
| `ANDB_DATA_DIR` | `.andb_data` | Badger on-disk directory when any sub-store uses `disk` |
| `ANDB_STORE_*` | _(inherit mode)_ | Per-store override (`SEGMENTS`, `INDEXES`, `OBJECTS`, `EDGES`, `VERSIONS`, `POLICIES`, `CONTRACTS`) — each `memory` or `disk`; meaningful when `ANDB_STORAGE=hybrid` |
| `ANDB_BADGER_INMEMORY` | _(empty)_ | Set `true` to use in-RAM Badger (no mmap files); useful when the temp disk is full |
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

<<<<<<< HEAD
- End-to-end event ingest and structured-evidence query
- Tiered hot → warm → cold retrieval over canonical-object projections
=======
1. freeze the main flow before scaling modules
2. freeze shared schemas before parallel implementation
3. validate the end-to-end path before optimizing internals
4. keep v1 focused on proving the architectural thesis

If you are starting implementation work, read [`docs/v1-scope.md`](docs/v1-scope.md) and [`docs/contributing.md`](docs/contributing.md) first.

## Near-Term Milestone

The implemented v1 prototype can already demonstrate:

- event ingest through the public API (`POST /v1/ingest/events`)
- `MaterializeEvent` → canonical `Memory`, `ObjectVersion`, `Edge` records, and optional `State` / `Artifact` written to stores
- tiered retrieval (hot → warm → cold) over canonical-object projections
>>>>>>> 0c64d888 (feat(worker): extract PipelineIngestWorker and wire ingest module registry)
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
> **Last updated:** 2026-03-24
> **Status:** All 21 Go internal packages pass (`go test ./src/internal/... exit 0`; `go vet ./... exit 0`). Six worker sub-packages have `*_test.go` files. Pass 2: 5 pipeline correctness fixes. Pass 3: 3 graph/storage structural fixes (R2/R6/R7). Pass 4: cold tier bug fix (TieredDataPlane ↔ TieredObjectStore integration).
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
| **Bug fix** | `S3ColdStore.PutMemory/PutAgent` called `EnsureBucket` via `PutBytes` on **every write** (one HTTP round-trip per cold write) → added `sync.Once` to `S3ColdStore`, removed eager bucket ensure from `PutBytes` | `storage/s3store.go`, `storage/s3util.go` |
| **Bug fix** | `MicroBatchScheduler.Flush()` was never called anywhere in the codebase; payloads enqueued by `CollaborationChain` accumulated indefinitely → `EventSubscriber.drainWAL` now calls `FlushMicroBatch()` after each cycle that processed ≥1 WAL entry | `worker/subscriber.go` |
| **Observability** | `S3ColdStore.GetMemory/GetAgent` silently returned `false` on 404; cold misses were invisible to operators → added `log.Printf("s3cold: miss key=…")` on nil response | `storage/s3store.go` |
| **Doc fix** | README claimed "10 HTTP routes" in 3 places; actual gateway registers 14 routes | `README.md` |
| **Doc fix** | README listed `SubgraphExecutorWorker` as "planned for v1.x/v2+" when it is fully implemented in `indexing/subgraph.go` | `README.md` |
| **Tests** | Added `_test.go` for all 6 worker sub-packages that had none: `cognitive`, `coordination`, `indexing`, `ingestion`, `materialization`, `chain` | `worker/{cognitive,coordination,indexing,ingestion,materialization,chain}/*_test.go` |

### Remaining Open Items (blocking or near-term)

| ID | Description | Owner | Status |
|---|---|---|---|
| **E1** | `TieredDataPlane.Ingest` never wrote to cold tier; `Flush()` never called | S3/Cold Module | ✅ Fixed |
| **E2** | `TieredObjectStore` (with `S3ColdStore`) completely bypassed in `SubmitIngest` — cold tier orphaned | S3/Cold Module | ✅ Fixed |
| **E3** | C++ Knowhere retrieval module (`retrievalplane`) never imported from query path — pure lexical search only | Member B | 🔲 Pending |
| **D1** | `subscriber.go` panic handler uses `log.Printf` instead of structured dead-letter channel | Member D | 🔲 Pending |

The following review checklist is intended for team members before merging `integration/all-features-test` → `main`.

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
| **Review focus ✅ FIXED pass 3** | ✅ | `workspace_id` propagation: events without `WorkspaceID` now materialize to `default` namespace; covered by `TestRuntime_Query_DefaultNamespace_WhenWorkspaceMissing` |
| **Review focus ✅ FIXED pass 3** | ✅ | `QueryRequest.edge_types` added and propagated into `GraphExpandRequest.EdgeTypes`; runtime also filters response edges by requested types |
| **Review focus ✅ FIXED pass 3** | ✅ | Runtime pre-fetches `GraphEdges` (`store.Edges().BulkEdges`) and `GraphNodes` (`prefetchGraphNodes`) before `DispatchSubgraphExpand` |
| Edge case: combined tenant + object-type + top_k filter | ✅ **FIXED pass 3** | Covered by `TestRuntime_Query_TenantObjectTypeTopK_AndToolCallState` (memory-only + `top_k=2`) |
| Edge case: `ObjectTypes=["state"]` query after a `tool_call` ingest | ✅ **FIXED pass 3** | Covered by `TestRuntime_Query_TenantObjectTypeTopK_AndToolCallState` (tool_call + state payload + state-only query) |

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
| `ANDB_WITH_KNOWHERE` | ON | Real Knowhere HNSW/SPARSE index (downloads zilliztech/knowhere v2.3.12; requires OpenBLAS) |
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
| Knowhere C++ engine compiled | ✅ | `ANDB_WITH_KNOWHERE=ON` in `cpp/CMakeLists.txt` |
| **FIXED E3** | 🔲 | C++ Knowhere retrieval (`retrievalplane`) is never imported from Go query path — active search uses `segmentstore` lexical only; `segment_adapter.go` must be updated to call `retrievalplane.NewRetriever()` when `CGO_ENABLED=1` |
| SDK `query()` kwargs match current `QueryRequest` JSON shape | 🔲 | Run `integration_tests/python/run_all.py` |
| SDK `ingest_event()` matches current `/v1/ingest` body | 🔲 | Cross-check `workspace_id` field with A |
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
| **Review focus** | ⚠️ | `OneHopExpand` iterates passed-in edges slice — verify `BulkEdges` and `OneHopExpand` return consistent results for the same seed set |
| **Review focus** | ⚠️ | `conflict_resolved` edges from `ConflictMergeWorker` not surfaced in `QueryResponse.Edges` — confirm whether intentional |
| Missing: `GraphEdges` pre-fetch caller responsibility | 🔲 | `QueryChainInput.GraphEdges` must be pre-populated before `QueryChain.Run`; C and D must agree on ownership |

---

### Member D — Worker Architecture Refactor

**Scope merged:** Worker split into 5 domain subpackages, `Create*` naming convention, multi-hop ProofTrace, DerivationLog integration, SubgraphExecutorWorker (L5), StateMat + MicroBatch wiring.

| Item | Status | Notes |
|---|---|---|
| **D1 — FIX** | 🔲 | `subscriber.go` panic handler uses `log.Printf` instead of structured dead-letter channel — replace with `sub.ErrorCh` dead-letter reporting before production |
| **Review focus** | ⚠️ | `ReflectionPolicyWorker` eviction — confirm uses `tiered_objects.ArchiveMemory()` not direct `store.Objects()` |
| Missing: `GraphEdges` pre-fetch in `QueryChain.Run` path | 🔲 | `QueryChainInput.GraphEdges` must be pre-populated; C and D must agree on ownership |

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

#### S3 Utility Layer (`src/internal/storage/s3util.go`)

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
| **Review focus** ✅ **FIXED** | ✅ | `S3ColdStore` now has `sync.Once` (`ensureOnce`) — `EnsureBucket` runs at most once per store lifetime; `PutBytes` no longer calls `EnsureBucket` (`storage/s3store.go`, `storage/s3util.go`) |
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
- [x] `go test ./integration_tests/... -v -timeout 120s` — all green
- [x] `ObjectTypes` empty → returns all types — verified by `TestAssembler_NoFilterPassthrough`
- [x] `ObjectTypes=["memory"]` excludes `state_*`/`art_*` — verified by `TestAssembler_ObjectTypesFilter`
- [x] `ObjectTypes=["state"]` returns only `state_*` — verified by `TestAssembler_StateTypeFilter`
- [x] 3-way filter (workspace + object-type + top_k) integration test passes (live server required)
- [x] `workspace_id` omitted on ingest → queryable under default namespace

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
