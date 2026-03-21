# CogDB — Agent-Native Database for Multi-Agent Systems

CogDB (ANDB) is an agent-native database for multi-agent systems (MAS). It combines a tiered segment-oriented retrieval plane, an event backbone with an append-only WAL, a canonical object materialization layer, pre-computed evidence fragments, 1-hop graph expansion, and structured evidence assembly — all wired together as a single runnable Go server.

> **Core thesis:** agent memory, state, event, artifact, and relation should be modeled as first-class database objects, and query results should return structured evidence rather than only top-k text fragments.

## What is implemented

- Go server ([`src/cmd/server/main.go`](src/cmd/server/main.go)) with 10 HTTP routes
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

- [`src/internal/access`](src/internal/access): HTTP gateway, 10 routes including ingest, query, and canonical CRUD
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

> **Current implementation status:** Layers 1–4 and parts of 5–8 are fully implemented. `VectorRetrievalExecutor`, `LogDispatchWorker`, `TSO Worker`, `EmbeddingBuilderWorker`, `TensorProjectionWorker`, `AccessControlWorker`, `SubgraphExecutor`, and `SharedMemorySyncWorker` are planned for v1.x / v2+.

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
- Go HTTP API with 10 routes, Python SDK, and integration test suite

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
> **Last updated:** 2026-03-20
> **Status:** All Go internal tests pass (`go test ./src/internal/... exit 0`). Integration test suite passes end-to-end.
> **Note:** This section exists only on `integration/all-features-test` and is intentionally not present on `main`.

The following review checklist is intended for team members before merging `integration/all-features-test` → `main`.

> 👋 **To all members:** Please leave a comment or Slack thread when you complete your section checklist. Cross-dependency items are called out in the [Cross-Member Collaboration](#cross-member-collaboration) section below — check that table first if your work touches a shared interface.

---

### Member A — Schema & Query Filters (`feature/schema-a`)

**Scope merged:** Tenant/workspace filters, object/memory filter predicates on the query path.

| Item | Status | Notes |
|---|---|---|
| `QueryRequest` filter fields wired through `PolicyEngine` | ✅ | See `semantic/policy_engine.go` and `access/gateway.go` `/v1/query` handler |
| Tenant isolation: `workspace_id` and `tenant_id` filter at segment scan | ✅ | `coordinator/query_coordinator.go` applies filters before `DataPlane.Search` |
| Object-type filter (`memory` / `state` / `artifact`) | ✅ | `QueryRequest.ObjectTypes` applied in `semantic/planner.go` |
| **Review focus** | ⚠️ | Verify filter short-circuit when `ObjectTypes` is empty — should default to returning all types, not zero results |
| **Review focus** | ⚠️ | `workspace_id` propagation: ensure events written without `WorkspaceID` are still queryable under default namespace |
| Edge case: combined tenant + object-type + top_k filter | 🔲 | Needs integration test covering the 3-way combination |

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

---

### Member C — Graph Relations (`feature/graph-c`)

**Scope merged:** `GraphRelationWorker`, `GraphEdgeStore.BulkEdges`, 1-hop expansion in `evidence.Assembler`.

| Item | Status | Notes |
|---|---|---|
| `GraphRelationWorker.IndexEdge` wired in `MainChain` | ✅ | `chain.go` step 4: `derived_from` edge written per ingest |
| `BulkEdges(objectIDs)` 1-hop expansion in `Assembler.Build` | ✅ | Returns typed `[]schemas.Edge` in every `QueryResponse` |
| `GraphEdgeStore.DeleteEdge` contract defined | ✅ | Available but not yet called in any worker path |
| **Review focus** | ⚠️ | `EdgesFrom(id)` is O(n) scan — acceptable for current in-memory size, must be replaced with indexed lookup before scaling |
| **Review focus** | ⚠️ | Multi-hop BFS in `ProofTraceWorker` now default-caps at depth=8; verify there are no cycles in the test graph that could inflate trace size |
| **Review focus** | ⚠️ | `conflict_resolved` edges created by `ConflictMergeWorker` are currently not surfaced in `QueryResponse.Edges` — intentional? |
| Missing: edge TTL / expiry | 🔲 | Edges accumulate indefinitely; consider adding `expires_at` to `schemas.Edge` in v1.x |

---

### Member D — Worker Architecture Refactor

**Scope merged:** Worker split into 5 domain subpackages, `Create*` naming convention, multi-hop ProofTrace, DerivationLog integration.

| Item | Status | Notes |
|---|---|---|
| Worker subpackages: `ingestion` / `materialization` / `cognitive` / `indexing` / `coordination` | ✅ | Each worker in its own file |
| `nodes/` retains only contracts + Manager + DataNode/IndexNode/QueryNode | ✅ | `data_node.go`, `index_node.go`, `query_node.go` split out |
| All constructors renamed `Create*` | ✅ | `eventbackbone` package retains `New*` (not in scope) |
| `ProofTraceWorker.AssembleTrace` upgraded to BFS with configurable `maxDepth` | ✅ | Default cap = 8; pass `maxDepth=1` for legacy behaviour |
| `ToolTraceWorker` appends to `DerivationLog` on `tool_call`/`tool_result` | ✅ | Enables `ProofTraceWorker` to walk event → artifact causal path |
| `QueryChainInput.MaxDepth` field for caller-controlled trace depth | ✅ | Default 0 → resolves to 8 internally |
| **Review focus** | ⚠️ | `subscriber.go` event handlers run in goroutines without structured error reporting — add a dead-letter channel before production |
| **Review focus** | ⚠️ | `ExecutionOrchestrator` priority queues are unbounded (`queueCap` is per-level soft cap); add hard-limit + back-pressure signal |
| Missing: per-subpackage unit tests | 🔲 | `cognitive/`, `coordination/`, `indexing/`, `ingestion/`, `materialization/` subpackages have no `_test.go` yet |
| Missing: `ProofTraceWorker` BFS cycle detection test | 🔲 | Add test that seeds a cyclic graph and verifies BFS terminates |

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

#### Known Limitations & Next Steps

- Current S3 path provides local-delivery write/read validation — not the full production-grade Milvus-migration path
- `FIXME`-related areas in extended modules still need follow-up for full parity
- Next: unify `S3_* → minio.*` config mapping across all runtime modules; progressively replace dev-only snapshot export with full production writer path

---

## Cross-Member Collaboration

The table below lists every point where two members' work **must be confirmed together** before either side is considered done. Please tag the relevant member when you open a PR or reach this checkpoint.

| # | Interface / Touch Point | Owner | Needs confirmation from | What to verify |
|---|---|---|---|---|
| 1 | `schemas.QueryRequest` JSON shape | **A** | **B** | B's Python SDK `query()` kwargs still map 1:1 after A added workspace/object-type filters |
| 2 | `/v1/ingest` POST body | **A** | **B** | `workspace_id` field added by A — B must update `ingest_event()` in Python SDK |
| 3 | gRPC proto field names | **B** | **D** | If D adds a Go gRPC client stub, B must freeze proto field numbers and agree on service name first |
| 4 | `QueryResponse.edges` shape (`[]schemas.Edge`) | **C** | **B** | B's Python SDK parses `edges` — C changed edge structure; B must update response parsing |
| 5 | `ProofTraceWorker` BFS depth in `QueryResponse.proof_trace` | **D** | **B** | B's integration tests assert on `proof_trace` length; default depth=8 may increase trace size |
| 6 | `GraphEdgeStore.EdgesFrom` O(n) scan | **C** | **D** | D's BFS calls `EdgesFrom` in a loop — if C changes the store implementation, D's BFS performance changes |
| 7 | `ToolTraceWorker` → `DerivationLog` → `ProofTrace` chain | **D** | **C** | C's graph edges and D's derivation entries both feed `ProofTrace`; run joint test before merging |
| 8 | Worker `nodes/contracts.go` interface changes | **D** | **A, B, C** | Any interface signature change in contracts.go is a breaking change for all callers — announce in team chat first |

> 💬 **Suggested flow:** When you finish an item in the table above, post a short message in the team channel: _"#N ready for review by [member]"_. The receiving member should confirm within 24 h or flag a blocker.

---

## Pre-merge Checklist (all members)

> Run this checklist **together as a team** in a short sync call or shared doc before opening the merge PR to `main`.

- [ ] `go test ./src/internal/... -count=1 -timeout 30s` — all green (D runs this)
- [ ] `go test ./integration_tests/... -v -timeout 120s` — all green (A verifies filter tests)
- [ ] Python SDK tests pass: `cd integration_tests/python && python run_all.py` (B runs this)
- [ ] No new `TODO`/`FIXME` markers in committed code (each member self-checks their own files)
- [ ] `go vet ./...` passes
- [ ] Cross-member table above: all 8 items confirmed ✅
- [ ] Topology endpoint returns all expected worker types: `GET /v1/admin/topology` (D checks worker count)
- [ ] `QueryResponse.edges` non-empty after ingest→query round-trip (C verifies)
- [ ] `proof_trace` contains at least one `derivation:` step for `tool_call` events (D + B verify together)
- [ ] Squash-merge or fast-forward only — no merge bubbles on `main`
- [ ] Tag release commit with `v1.0.0-integration-rc1` before merging
