# Agent-Native Database for Multi-Agent Systems

ANDB (CogDB) is a v1 prototype of an agent-native database for multi-agent systems (MAS). The repository combines a tiered segment-oriented retrieval plane, an event backbone with append-only WAL, a canonical object materialization layer, pre-computed evidence fragments, 1-hop graph expansion, and structured evidence assembly — all wired together as a single runnable Go server.

The core thesis is simple:

**agent memory, state, event, artifact, and relation should be modeled as first-class database objects, and query results should return structured evidence rather than only top-k text fragments.**

## Project Status

This repository is in the **runnable-prototype** stage.  The main ingest/query path is fully wired end-to-end.

What is implemented today:

- Runnable Go server in [`src/cmd/server/main.go`](src/cmd/server/main.go) with 11 HTTP routes (incl. `GET /v1/admin/storage`)
- Append-only WAL with `Scan` and `LatestLSN` for replay and watermark tracking
- `MaterializeEvent` → `MaterializationResult` that produces a canonical `Memory`, `ObjectVersion`, and typed `Edge` records at ingest time
- Three-tier data plane: **hot** (in-memory LRU cache) → **warm** (full segment index) → **cold** (archived tier), all behind a unified `DataPlane` interface
- Pre-computed `EvidenceFragment` cache populated at ingest, merged into proof traces at query time
- 1-hop graph expansion via `GraphEdgeStore.BulkEdges` in the `Assembler.Build` path
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

- [`src/internal/access`](src/internal/access): HTTP gateway, 11 routes including ingest, query, canonical CRUD, and `GET /v1/admin/storage`
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
For endpoint details and ACK fields, see `docs/api/ingest.md`.

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

## Review Notes (Current Prototype)

- `GET /healthz` previously returned `Content-Type: text/plain` — fixed to `application/json` in `access/gateway.go`.
- The HTTP gateway returns plain-text errors for malformed JSON and method mismatches; a structured error envelope would improve integration debugging.
- The public API surface is small and stable; `response_mode: structured_evidence` is the canonical value — demo scripts have been updated to match.
- For repeated integration runs, consider exposing a lightweight dev-only reset endpoint or adding idempotency semantics for seed data to avoid state accumulation.

To run only the Go internal module tests:

```bash
go test ./src/internal/... -count=1 -timeout 30s
```

All 12 native packages have their own `*_test.go` file.  See [`docs/contributing.md §11`](docs/contributing.md) for the module-level test specification.

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

## Collaboration Principles

This repository follows a framework-first development model:

1. freeze the main flow before scaling modules
2. freeze shared schemas before parallel implementation
3. validate the end-to-end path before optimizing internals
4. keep v1 focused on proving the architectural thesis

If you are starting implementation work, read [`docs/v1-scope.md`](docs/v1-scope.md) and [`docs/contributing.md`](docs/contributing.md) first.

## Near-Term Milestone

The implemented v1 prototype can already demonstrate:

- event ingest through the public API (`POST /v1/ingest/events`)
- `MaterializeEvent` → canonical `Memory`, `ObjectVersion`, and `Edge` records written to stores
- tiered retrieval (hot → warm → cold) over canonical-object projections
- 1-hop graph expansion in every `QueryResponse`
- pre-computed `EvidenceFragment` cache merged into `ProofTrace` at query time

Next milestones:

- benchmark comparison against simple top-k return
- time-travel queries using WAL `Scan` replay
- multi-agent session isolation and scope enforcement

## Long-Term Direction

Later versions may extend ANDB with:

- policy-aware retrieval and visibility enforcement
- stronger version/time semantics
- share contracts and governance objects
- richer graph reasoning and proof replay
- tensor memory operators
- cloud-native distributed orchestration

v1 does not aim to complete that roadmap. It aims to establish the right abstraction and the right main path.
