# CogDB — Agent-Native Database for Multi-Agent Systems
> **Branch:** `dev` (integration) · **Pass 8** (2026-03-27)

CogDB (ANDB) is an agent-native database for multi-agent systems (MAS). It combines a tiered segment-oriented retrieval plane, an event backbone with an append-only WAL, a canonical object materialization layer, pre-computed evidence fragments, 1-hop graph expansion, and structured evidence assembly — all wired together as a single runnable Go server.

> **Core thesis:** agent memory, state, event, artifact, and relation should be modeled as first-class database objects, and query results should return structured evidence rather than only top-k text fragments.

## What is implemented

- Go server ([`src/cmd/server/main.go`](src/cmd/server/main.go)) with 14 HTTP routes, graceful shutdown via `context.WithCancel`
- Append-only WAL with `Scan` and `LatestLSN` for replay and watermark tracking
- `MaterializeEvent` → `MaterializationResult` producing canonical `Memory`, `ObjectVersion`, and typed `Edge` records at ingest time
- Synchronous object materialization: `ObjectMaterializationWorker`, `ToolTraceWorker`, and `StateCheckpoint` called in `SubmitIngest` so State/Artifact/Version objects are immediately queryable
- Supplemental canonical retrieval in `ExecuteQuery`: State/Artifact IDs fetched from ObjectStore alongside retrieval-plane results
- Event store: `ObjectStore` supports Event CRUD; `QueryChain.Run` routes `evt_`/`art_` IDs to load Event/Artifact GraphNodes
- Three-tier data plane: **hot** (in-memory LRU) → **warm** (segment index) → **cold** (archived tier), behind a unified `DataPlane` interface
- Dual storage backends: in-memory (default) and Badger-backed persistent storage (`ANDB_STORAGE=disk`), with per-store hybrid mode; `GET /v1/admin/storage` reports resolved config
- Pre-computed `EvidenceFragment` cache populated at ingest, merged into proof traces at query time
- 1-hop graph expansion via `GraphEdgeStore.BulkEdges` in the `Assembler.Build` path
- `QueryResponse` with `Objects`, `Edges`, `Provenance`, `ProofTrace`, `Versions`, `AppliedFilters`, and `chain_traces` (`main` / `memory_pipeline` / `query` / `collaboration` string slots; query path fills `query` from `QueryChain`) on every query
- `QueryChain` (post-retrieval reasoning): multi-hop BFS proof trace + 1-hop subgraph expansion, merged deduplicated into response
- 19 worker nodes registered: 3 data-plane + 16 domain workers (Ingest, ObjectMat, StateMat, ToolTrace, MemExtract, MemConsolidate, Summarize, ReflectionPolicy, IndexBuild, GraphRelation, ProofTrace, SubgraphExecutor, ConflictMerge, Communication, MicroBatch, AlgorithmDispatch)
- Safe DLQ: panic recovery with overflow buffer (capacity 256) + structured `OverflowBuffer()` + `OverflowCount` metrics — panics are never silently lost
- `AlgorithmDispatchWorker` wired with no-op default algorithm; pluggable via `MemoryManagementAlgorithm` interface
- Module-level test coverage: 22 packages with `*_test.go`; 13 Go integration test files covering CRUD, chains, dataflow, topology, protocol
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
- [`src/internal/dataplane/retrievalplane`](src/internal/dataplane/retrievalplane): CGO bridge boundary — `bridge_stub.go` (default, no CGO) + `contracts.go` (Retriever/SearchService interfaces)
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

#### 🟡 Memory Pipeline Chain — six-layer cognitive management

The memory pipeline implements the six-layer memory management architecture from the design specification.  Every path honours the core principle: **upper-layer agents may only consume `MemoryView`; they never access the raw object store or index directly.**

The pipeline separates **fixed generic infrastructure** from **algorithm-owned pipeline workers**:

- `AlgorithmDispatchWorker` and `GraphRelationWorker` are fixed nodes present in every deployment (`worker/cognitive/`).
- Everything else — extraction, consolidation, summarization, governance — is owned by the algorithm and lives under `worker/cognitive/<algo>/`.  Different algorithms may implement these stages completely differently, or omit stages they do not need.

**Materialization path — write-time (generic design):**

```
Event / Interaction
  ↓
[algo pipeline: materialization workers]   ← algorithm-specific
    e.g. raw event → level-0 memory → level-1 consolidation → level-2 summary
  ↓
GraphRelationWorker                        ← fixed
    relation binding: owned_by · derived_from · scoped_to · observed_by
  ↓
AlgorithmDispatchWorker [ingest]           ← fixed
    algo.Ingest() → MemoryAlgorithmState persisted
    AlgorithmStateRef set on Memory
  ↓
[algo pipeline: governance workers]        ← algorithm-specific
    e.g. TTL / quarantine / confidence / salience rules
    → PolicyDecisionLog + AuditStore
```

**Materialization path — write-time (baseline algorithm concrete example):**

```
Event / Interaction
  ↓
baseline.MemoryExtractionWorker       level-0 episodic memory, LifecycleState=active
  ↓
baseline.MemoryConsolidationWorker    level-0 → level-1 semantic/procedural
  ↓
baseline.SummarizationWorker          level-1/level-2 compression
  ↓
GraphRelationWorker
  ↓
AlgorithmDispatchWorker [ingest]
  ↓
baseline.ReflectionPolicyWorker
    TTL expiry    → LifecycleState = decayed
    quarantine    → LifecycleState = quarantined
    confidence override · salience decay
    → PolicyDecisionLog + AuditStore
```

**Background maintenance path — async (generic, driven by AlgorithmDispatchWorker):**

```
Scheduler trigger
  ↓
AlgorithmDispatchWorker [decay | compress | summarize]
    algo.Decay(nowTS)       → MemoryAlgorithmState · SuggestedLifecycleState honoured verbatim
    algo.Compress(memories) → derived Memory objects stored verbatim
    algo.Summarize(memories)→ summary Memory objects stored verbatim
    AuditRecord emitted for each state update
```

**Retrieval path — read-time (generic):**

```
QueryRequest
  ↓
AlgorithmDispatchWorker [recall]
    algo.Recall(query, candidates) → ScoredRefs in algorithm order
  ↓
MemoryViewBuilder
    1. scope filter  — AccessGraphSnapshot.VisibleScopes
    2. policy filter — quarantined / hidden / logically-deleted excluded
    3. algorithm rerank — AlgorithmScorer func (pluggable)
    4. MemoryView assembled
  ↓
MemoryView{RequestID, ResolvedScope, VisibleMemoryRefs, Payloads,
           AlgorithmNotes, ConstructionTrace}
  ↓
Query Worker / Planner / Reasoner  (consumes MemoryView only)
```

**Algorithm plugin contract:**

- The `MemoryManagementAlgorithm` interface (`schemas/memory_management.go`) defines: `Ingest · Update · Recall · Compress · Decay · Summarize · ExportState · LoadState`.
- Lifecycle transitions are driven **exclusively** by `MemoryAlgorithmState.SuggestedLifecycleState` — the dispatcher applies no thresholds or heuristics of its own.
- Algorithm state is persisted in `MemoryAlgorithmStateStore` keyed by `(memory_id, algorithm_id)`, leaving the canonical `Memory` schema unchanged.
- Each algorithm is self-contained under `worker/cognitive/<algo>/` and registers its own pipeline workers; other algorithms (e.g. MemoryBank) plug in by implementing this interface without affecting existing deployments.

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

#### 🟢 Collaboration Chain — multi-agent coordination with governed sharing

Memory sharing in a multi-agent system is **not** copying a record to a shared namespace.  It is a **controlled projection** — the original Memory retains its provenance and owner; the target agent receives a scope-filtered, policy-conditioned view.

```
Agent A writes Memory
  ↓
ConflictMergeWorker          (last-writer-wins · causal merge · conflict_resolved edge)
  ↓
ShareContract evaluation     (read_acl · write_acl · derive_acl
                               ttl_policy · consistency_level · merge_policy
                               quarantine_policy · audit_policy)
  ↓
AccessGraphSnapshot resolved (user → agent call-graph · agent → resource access-graph
                               → VisibleScopes for requesting agent at this moment)
  ↓
CommunicationWorker          (projection, not copy:
                               raw Memory keeps original owner + provenance
                               target agent receives scope-bound MemoryView)
  ↓
AuditRecord written          (record_id · target_memory_id · operation_type=share
                               actor_id · policy_snapshot_id · decision · timestamp)
  ↓
Target agent reads via MemoryViewBuilder
    scope filter  → AccessGraphSnapshot.VisibleScopes
    policy filter → quarantine / hidden / logically-deleted excluded
    algorithm rerank → pluggable AlgorithmScorer
    → MemoryView delivered to target Query Worker
```

**Key design principles:**

- **Sharing is projection, not copy** — provenance, owner, and base payload remain with the original object; what the target sees is a governance-conditioned view.
- **Access boundaries are dynamic** — `AccessGraphSnapshot` resolves visible scopes at request time, not as a static ACL field on the memory record.
- **Every share and projection is audited** — `AuditStore` records each share, read, algorithm-update, and policy-change action.
- **`ShareContract` is the protocol unit** — it encodes `read_acl`, `write_acl`, `derive_acl`, `ttl_policy`, `consistency_level`, `merge_policy`, `quarantine_policy`, and `audit_policy` as a first-class object rather than scattered metadata fields.

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

### Full stack via Docker 

Root [`docker-compose.yml`](docker-compose.yml) starts **MinIO** (S3 API on port 9000), creates bucket `andb-integration`, and runs the Go server with **`ANDB_STORAGE=disk`**, **`ANDB_DATA_DIR=/data`**, and **`S3_*` pointing at MinIO** (cold tier uses real S3). Server listens on **`0.0.0.0:8080`** inside the container and is published as **http://127.0.0.1:8080**.

```bash
docker compose up -d
# optional: fixture-driven JSON captures (stdlib HTTP only; no SDK install required)
python scripts/e2e/member_a_capture.py --out-dir ./out/member_a
make integration-test   # still expects a server at ANDB_BASE_URL (same URL)
```

Fixture sets and manifest: [`integration_tests/fixtures/member_a/`](integration_tests/fixtures/member_a/). Capture script: [`scripts/e2e/member_a_capture.py`](scripts/e2e/member_a_capture.py). Convenience targets: `make docker-up`, `make docker-down`, `make member-a-capture`.

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

## Team Member Responsibilities

### Member A — Full System E2E Testing & Integration

**Scope:** End-to-end integration testing across the entire stack, including S3 cold path, Docker environment, and all four execution chains.

**Deliverables:**
- Working Docker compose / environment that brings up the full server with S3 cold storage
- Test data sets (ingest events) covering: normal query, cold-tier recall, graph expansion, collaboration sharing
- Query / retrieve results for each test case, with full intermediate traces:
  - `ProofTrace` output from `QueryChain.Run`
  - Applied governance filters
  - Graph edge expansion (`Edges`, `Nodes`)
  - Evidence fragment cache hit/miss log
- Integration test script in `scripts/` or `integration_tests/python/`

**Entry point:** [`src/internal/app/bootstrap.go`](src/internal/app/bootstrap.go) wires all components; start from `BuildServer()` and trace through each chain.

**Key env vars to exercise:**
```
ANDB_STORAGE=disk
ANDB_DATA_DIR=/path/to/data
S3_ENDPOINT, S3_ACCESS_KEY, S3_SECRET_KEY, S3_BUCKET
ANDB_EMBEDDER=tfidf|openai|zhipuai|cohere
```

**Expected output format per test case:**
```json
{
  "test_name": "...",
  "query": { "raw": "...", "request": {} },
  "response": { "objects": [], "edges": [], "proof_trace": [], "applied_filters": [] },
  "chain_traces": { "main": [], "memory_pipeline": [], "query": [], "collaboration": [] }
  ...
}
```

---

### Member B — Embedding Model Layer (`src/internal/dataplane/embedding/`)

**Scope:** Extend the HTTP embedding module to support all major LLM embedding providers, plus local/CPU runtimes (ONNX, TensorRT, llama.cpp / GGUF).

**Current status:** `src/internal/dataplane/embedding/` provides:
- `TfidfEmbedder` — pure-Go, no network, already working
- `HTTPEmbedder` — OpenAI v1 schema (OpenAI, ZhipuAI, Azure, Ollama)
- `CohereEmbedder` — Cohere `/v2/embed`
- Client pool with connection reuse

**Required extensions:**

| Provider | File to add/modify | Notes |
|---|---|---|
| Ollama (local) | Already covered by `HTTPEmbedder` | Set `ANDB_EMBEDDER_BASE_URL=http://localhost:11434/v1` |
| Local ONNX model | `embedding/onnx.go` | `OnnxEmbedder`: load `.onnx` via `github.com/owulveryck/onnx-go`, run inference on CPU |
| TensorRT | `embedding/tensorrt.go` | `TensorRTEmbedder`: CUDA context, engine cache, batch inference |
| GGUF (llama.cpp) | `embedding/gguf.go` | `GGUFEmbedder`: load `.gguf` via `github.com/ggerganov/llama.cpp`, CPU/GPU hybrid |
| Vertex AI | `embedding/vertexai.go` | Google Cloud Vertex AI Embeddings API |
| HuggingFace Inference API | `embedding/huggingface.go` | `https://api-inference.huggingface.co/` |

**Interface contract** — all embedders must satisfy:
```go
type Generator interface {
    dataplane.EmbeddingGenerator  // Generate(text) ([]float32, error); Dim() int; Reset()
    Close() error
    Provider() string             // e.g. "onnx", "tensorrt", "gguf", "vertexai"
}
```

**Algorithmic requirements:**
- Batch inference: accept `[]string` input, emit `[][]float32` — must be used by `TieredDataPlane.Ingest` for bulk indexing
- Dimension probing: validate output dimension on startup; return error on mismatch
- Error propagation: `ErrProviderUnavailable` must wrap all network/runtime errors
- Pooled resources: ONNX/TensorRT sessions should be pooled, not recreated per call

**Env var convention:**
```
ANDB_EMBEDDER=onnx|tensorrt|gguf|vertexai|huggingface
ANDB_EMBEDDER_MODEL_PATH=/path/to/model.onnx   # ONNX/TensorRT/GGUF local path
ANDB_EMBEDDER_MAX_BATCH_SIZE=32                # inference batch size
ANDB_EMBEDDER_DEVICE=cpu|cuda                  # execution provider
```

**Module layout target:**
```
src/internal/dataplane/embedding/
├── embedding.go       # Generator interface, TfidfEmbedder, OpenAIEmbedder, CohereEmbedder, OpenAIConfig
├── pool.go           # HTTP client pool
├── onnx.go           # ONNX Runtime embedder
├── tensorrt.go       # NVIDIA TensorRT embedder
├── gguf.go           # llama.cpp / GGUF embedder
├── vertexai.go       # Google Vertex AI embedder
├── huggingface.go    # HuggingFace Inference API embedder
└── embedding_test.go # Shared tests + provider-specific tests
```

---

### Member C — DFS Retrieval Integration + Proof Chain + S3 Object Binding

**Scope:** Deep integration of Dense Fragment Search (DFS) into the query pipeline, verification of proof chain semantics, and binding retrieval to canonical S3 objects (not metadata).

**1. DFS integration into search** (`src/internal/dataplane/` and `src/internal/schemas/`)

All tunable DFS parameters must be externalized into `schemas.AlgorithmConfig` (currently defined in [`src/internal/schemas/constants.go`](src/internal/schemas/constants.go)). Audit the full codebase for hardcoded magic numbers and add to `AlgorithmConfig`:

| Current hardcoded | Suggested field | Default |
|---|---|---|
| `10000` evidence cache size | `EvidenceCacheSize` | ✅ done |
| `10` token count threshold | `TokenCountThreshold` | ✅ done |
| `0.1` token bonus | `TokenBonus` | ✅ done |
| `0.1` causal ref bonus | `CausalRefBonus` | ✅ done |
| `0.2` global visibility bonus | `GlobalVisibilityBonus` | ✅ done |
| `1.0` salience cap | `SalienceCap` | ✅ done |
| `0.5` hot tier threshold | `HotTierSalienceThreshold` | ✅ done |
| `8` max proof depth | `MaxProofDepth` | ✅ done |
| `256` default embedding dim | `EmbeddingDim` | **add** |
| `60` RRF k constant | `RRFK` | **add** |
| `16` HNSW M | `HNSWM` | **add** |
| `256` HNSW efConstruction | `HNSEfConstruction` | **add** |
| `64` HNSW efSearch | `HNSEfSearch` | **add** |
| cold search scoring weights | `ColdSearchWeights` | **add** |
| DFS relevance threshold | `DFSRelevanceThreshold` | **add** |
...
**DFS search path to implement:**
```
QueryRequest
  ↓
TieredDataPlane.Search (include_cold=true)
  ↓
ColdObjectStore.ColdSearch  ← lexical substring match (current, in-memory sim)
  ↓
[NEW] DFS scorer: dense vector similarity over cold-tier embeddings
  ↓
RRF fusion: cold_dfs_score + warm_lexical_score + warm_vector_score
  ↓
TopK → Assembler.Build → QueryResponse
```

**2. Verify proof chain connection semantics**

Audit [`src/internal/worker/chain/chain.go`](src/internal/worker/chain/chain.go) and [`src/internal/worker/coordination/proof_trace.go`](src/internal/worker/coordination/proof_trace.go):

- Every `ProofTrace` step must include the `EdgeID` and `EdgeType` it traversed — not just a string description
- Derivation log entries must be queryable by `ObjectID` (for audit/replay)
- The BFS proof trace must support cycle detection and depth cap (`MaxProofDepth` from `AlgorithmConfig`)
- `DerivationLog.Append` must be called by `ObjectMaterializationWorker` for all non-trivial transformations (extraction, consolidation, summarization) — currently not wired in bootstrap

**3. S3 object retrieval (not metadata)**

Current cold tier (`src/internal/storage/`) writes canonical `Memory` objects to S3. Verify and fix:

- `ColdSearch` must score by **canonical object content** (`.Content` field), not by metadata labels
- `ArchiveColdRecord` in `TieredObjectStore` must persist a full `schemas.Memory` object to S3 (or reconstruct one from the archive record)
- `GetMemoryActivated` cold path must **rehydrate the full Memory object** from S3, not return a placeholder
- S3 object key convention: `memories/{tenant_id}/{workspace_id}/{memory_id}.json`
- Verify `TieredObjectStore.ArchiveMemory` → `ColdObjectStore.PutMemory` → S3 round-trip: `GetMemory` retrieves identical object

**Audit checklist:**
```
□ ColdObjectStore.ColdSearch uses m.Content, not metadata labels
□ ArchiveColdRecord stores full Memory JSON in S3
□ GetMemoryActivated cold path rehydrates full Memory from S3
□ S3 object key = memories/{tenant_id}/{workspace_id}/{memory_id}.json
□ ColdSearch scores by Content similarity, not metadata
□ DerivationLog entries have ObjectID index for audit queries
□ ProofTrace steps carry EdgeID + EdgeType (not just string)
□ MaxProofDepth enforced by BFS in ProofTraceWorker
□ DFS relevance threshold configurable via AlgorithmConfig
□ All DFS/HNSW params externalized to AlgorithmConfig
```

---
