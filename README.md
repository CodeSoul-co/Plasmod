# CogDB — Agent-Native Database for Multi-Agent Systems
> **Branch:** `dev` (integration) · **Pass 6** (2026-03-25)

CogDB (ANDB) is an agent-native database for multi-agent systems (MAS). It combines a tiered segment-oriented retrieval plane, an event backbone with an append-only WAL, a canonical object materialization layer, pre-computed evidence fragments, 1-hop graph expansion, and structured evidence assembly — all wired together as a single runnable Go server.

> **Core thesis:** agent memory, state, event, artifact, and relation should be modeled as first-class database objects, and query results should return structured evidence rather than only top-k text fragments.

## What is implemented

- Go server ([`src/cmd/server/main.go`](src/cmd/server/main.go)) with 14 HTTP routes, graceful shutdown via `context.WithCancel`
- Append-only WAL with `Scan` and `LatestLSN` for replay and watermark tracking
- `MaterializeEvent` → `MaterializationResult` producing canonical `Memory`, `ObjectVersion`, and typed `Edge` records at ingest time
- Synchronous object materialization: `ObjectMaterializationWorker`, `ToolTraceWorker`, and `StateCheckpoint` called in `SubmitIngest` so State/Artifact/Version objects are immediately queryable
- Supplemental canonical retrieval in `ExecuteQuery`: State/Artifact IDs fetched from ObjectStore alongside retrieval-plane results
- Three-tier data plane: **hot** (in-memory LRU) → **warm** (segment index) → **cold** (archived tier), behind a unified `DataPlane` interface
- Dual storage backends: in-memory (default) and Badger-backed persistent storage (`ANDB_STORAGE=disk`), with per-store hybrid mode; `GET /v1/admin/storage` reports resolved config
- Pre-computed `EvidenceFragment` cache populated at ingest, merged into proof traces at query time
- 1-hop graph expansion via `GraphEdgeStore.BulkEdges` in the `Assembler.Build` path
- `QueryResponse` with `Objects`, `Edges`, `Provenance`, `ProofTrace`, `Versions`, and `AppliedFilters` on every query
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

> **Branch:** `dev` (integration consolidation)
> **Last updated:** 2026-03-25
> **Status:** All 22 Go internal packages pass (`go test ./src/... exit 0`). All 13 Go integration test files pass. 19 worker nodes registered. Sync materialization ensures State/Artifact objects are queryable immediately after ingest. Pass 5 (integration consolidation): 10 fixes including AlgorithmDispatchWorker wiring, DLQ overflow safety, graceful shutdown, synchronous checkpoint, canonical supplemental retrieval, cold archival attributes, and VerifiedState constant standardisation.
> **Note:** Feature branches (`feature/schema-a`, `feature/retrieval-b`, `feature/graph-c`, `feature/member-d-api`) exist on remote but contain significant deletions/regressions relative to `dev` and require careful review before merge. `feature/member-d-api` in particular removes core fields (queryChain, derivationLog, tieredObjects) from Runtime — must not be merged without resolving these removals.


### Remaining Open Items (blocking or near-term)

| ID | Description | Owner | Status |
|---|---|---|---|
| **E1** | `TieredDataPlane.Ingest` never wrote to cold tier; `Flush()` never called | S3/Cold Module | ✅ Fixed |
| **E2** | `TieredObjectStore` (with `S3ColdStore`) completely bypassed in `SubmitIngest` — cold tier orphaned | S3/Cold Module | ✅ Fixed |
| **E3** | C++ Knowhere retrieval module (`retrievalplane`) never imported from query path — pure lexical search only | Member B | 🔲 Pending |
| **D1** | `subscriber.go` panic handler silently drops panics when DLQ channel is full | Integration | ✅ Fixed |
| **F1** | `handleS3SnapshotExport` registered as HTTP route but method was nil — would panic | Integration | ✅ Fixed |
| **F2** | `AlgorithmDispatchWorker` defined in contracts but never wired into Manager/bootstrap | Integration | ✅ Fixed |
| **F3** | `"causal"` used as EdgeType in `PreComputeService` — not a defined constant; matches nothing | Integration | ✅ Fixed |
| **F4** | Cold archival `Scope`/`OwnerType` read wrong attribute keys (`scope`, `owner_type`) never set by `buildAttributes` | Integration | ✅ Fixed |
| **F5** | Goroutines started with `context.Background()` — no graceful shutdown path for subscriber/orchestrator | Integration | ✅ Fixed |
| **F6** | `EventEnvelope = Event` dead alias in `canonical.go` — never referenced | Integration | ✅ Fixed |
| **F7** | Hardcoded `"retracted"` string vs `VerifiedStateRetracted` constant in gateway/assembler | Integration | ✅ Fixed |
| **F8** | `ObjectMaterializationWorker` and `StateMaterializationWorker` called only async (200ms poll) — State/Artifact objects unavailable for immediate queries | Integration | ✅ Fixed |
| **F9** | `ExecuteQuery` had no path to retrieve State/Artifact IDs — these bypass retrieval plane and need supplemental canonical store fetch | Integration | ✅ Fixed |
| **F10** | `DispatchStateCheckpoint` called only async — checkpoint versions unavailable for immediate queries | Integration | ✅ Fixed |
| **A1** | `eventbackbone.DerivationLog` is in-memory only; no persistence to disk | Future | 🔲 Open |

The following review checklist is intended for team members before merging `integration/all-features-test` → `main`.

---

### Member A — Schema & Query Filters (`feature/schema-a`)

**Scope merged:** Tenant/workspace filters, object/memory filter predicates on the query path.

> **Integration fixes applied (Pass 5):**
> - `SubmitIngest` now calls `DispatchObjectMaterialization` and `DispatchToolTrace` **synchronously** (before returning) so State/Artifact objects are queryable immediately without waiting for async subscriber poll.
> - `ExecuteQuery` calls `fetchCanonicalObjects` to supplement retrieval-plane results with State/Artifact IDs from the canonical ObjectStore — these types bypass the retrieval plane and must be fetched explicitly.
> - `DispatchStateCheckpoint` called synchronously for checkpoint events so ObjectVersions are available for immediate queries.
> - `DispatchConflictMerge` called synchronously in `SubmitIngest` for same-session episodic memories; `lastMem` map tracks per-session MemoryID so `conflict_resolved` edges are present before ingest returns.
> - `filterByObjectTypes` correctly matches State IDs via `state_` prefix and Artifact IDs via `art_`/`tool_trace_` prefix.

| Item | Status | Notes |
|---|---|---|
| `QueryChainInput.EdgeTypeFilter` propagation | ✅ | Edge-type filter from `QueryRequest` propagates to `SubgraphExecutorWorker` |
| `QueryChainInput.GraphNodes` / `GraphEdges` pre-fetch | ✅ | Verified in `chain_query_test.go` |
| Combined tenant + object-type + top_k filter | ✅ | `TestIngestThenQuery_E2E` covers 3-way combination |
| `ObjectTypes=["state"]` query after `state_update` ingest | ✅ | Fixed; state now returned immediately after ingest (sync materialization) |
| `ObjectTypes=["state"]` query after `checkpoint` ingest | ✅ | Fixed; checkpoint now snapshots states synchronously |
| `ObjectTypes=["artifact"]` query after `tool_call` ingest | ✅ | Fixed; artifact now returned immediately after ingest |
| `conflict_resolved` edge query after same-session memory ingest | ✅ | Fixed; `lastMem` map in Runtime fires `DispatchConflictMerge` synchronously in `SubmitIngest`; test stable 3/3 runs |

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
| **Review focus** | ✅ | `conflict_resolved` edges from `ConflictMergeWorker` now surfaced in `QueryResponse.Edges` via `BulkEdges` pre-fetch → `QueryChain.MergedEdges` merge path; stored synchronously in `SubmitIngest` |
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
| **FIXED E1** | ✅ | `TieredDataPlane` now uses `TieredObjectStore` as cold backend; `NewTieredDataPlane(tieredObjs)` accepts it; cold queries call `tieredObjs.ColdSearch()` |
| **FIXED E2** | ✅ | `SubmitIngest` now calls `tieredObjs.PutMemory()` + `tieredObjs.ArchiveColdRecord()`; `Runtime` holds `TieredObjectStore` reference |
| Cold archival `Scope`/`OwnerType` attribute fix | ✅ | `ArchiveColdRecord` now reads `attrs["visibility"]` as `Scope` and `attrs["event_type"]` as `OwnerType`; `buildAttributes` sets these keys |
| `InMemoryColdStore.ColdSearch` | ✅ | Lexical substring search over cold memories, sorted by score+recency |
| `S3ColdStore.ColdSearch` | ✅ | ListObjectsV2 + per-key GET + lexical scoring; `ListObjects` added to `s3util` |
| Missing: S3 integration test in `integration_tests/` | 🔲 | `ANDB_RUN_S3_TESTS=true` test: ingest → archive → cold read round-trip |
| Missing: `S3_*` config key standardisation | 🔲 | Some runtime modules still use `minio.*` keys |
| `handleS3SnapshotExport` nil panic | ✅ Fixed | Full implementation in `src/internal/access/s3_snapshot_export_stub.go` using Avro manifest + JSON metadata + S3 round-trip verification; returns 501 only if S3 env vars absent |
| `src/internal/s3util/` module removed | ✅ Fixed | S3 helpers (`LoadFromEnv`, SigV4 signing, etc.) moved to `src/internal/storage/s3util.go`; imports updated in gateway, bootstrap, and snapshot export; old `src/internal/s3util/` directory deleted |

---

## Integration Delivery Summary (Pass 6 — 2026-03-25)

This section records every structural fix, semantic correction, and design decision made during the Pass 5 integration pass on `dev`. It supplements the per-member checklists above.

### Structural Fixes Applied

| ID | File | Issue | Fix |
|---|---|---|---|
| **F1** | `src/internal/access/gateway.go` | `handleS3SnapshotExport` registered in mux but method was nil → nil pointer panic on any request | Removed stub edit; actual implementation already existed in `s3_snapshot_export_stub.go` |
| **F2** | `src/internal/worker/nodes/manager.go`, `bootstrap.go` | `AlgorithmDispatchWorker` defined in contracts but never registered in `Manager` or wired in bootstrap — unreachable pipeline bridge | Added `algorithmDispatchWorkers []AlgorithmDispatchWorker` field + `RegisterAlgorithmDispatch` + `DispatchAlgorithmDispatch` + wired in bootstrap with `cognitive.NewDefaultAlgorithm()` no-op |
| **F3** | `src/internal/materialization/pre_compute.go` | `EdgeTypes: []string{"derived_from", "causal"}` — `"causal"` not a defined constant (actual: `"caused_by"`) — evidence fragments always included unmatched edge type | Replaced `"causal"` with `string(schemas.EdgeTypeCausedBy)` |
| **F4** | `src/internal/storage/tiered.go` | `ArchiveColdRecord` read `attrs["scope"]` and `attrs["owner_type"]` which `buildAttributes` never sets → cold tier records always have empty Scope/OwnerType | Changed to `attrs["visibility"]` → Scope and `attrs["event_type"]` → OwnerType |
| **F5** | `src/internal/app/bootstrap.go`, `src/cmd/server/main.go` | Goroutines started with `context.Background()` — no graceful shutdown path; subscriber/orchestrator would leak on server stop | `BuildServer` now returns `(srv, cleanup func(), error)`; `main.go` calls `defer cleanup()`; internal `ctx, cancel := context.WithCancel(context.Background())` passed to subscriber and orchestrator |
| **F6** | `src/internal/schemas/canonical.go` | `EventEnvelope = Event` dead type alias — defined but never referenced anywhere in codebase | Removed |
| **F7** | `src/internal/access/gateway.go`, `src/internal/evidence/assembler.go` | Hardcoded `"retracted"` string instead of `schemas.VerifiedStateRetracted` constant | Replaced with `string(schemas.VerifiedStateRetracted)` in both files |
| **F8** | `src/internal/worker/runtime.go` | `DispatchObjectMaterialization` and `DispatchToolTrace` called only via async WAL subscriber (200ms poll) — State/Artifact objects invisible to immediate queries | Added synchronous calls in `SubmitIngest` immediately after materialization |
| **F9** | `src/internal/worker/runtime.go` | `ExecuteQuery` returned only retrieval-plane results; State/Artifact IDs bypass retrieval plane and need supplemental canonical fetch | Added `fetchCanonicalObjects` method: fetches State/Artifact IDs from ObjectStore by agent/session, appends to retrieval results |
| **F10** | `src/internal/worker/runtime.go` | `DispatchStateCheckpoint` called only via async subscriber — ObjectVersions unavailable for immediate queries | Added sync checkpoint trigger in `SubmitIngest` for checkpoint event types |
| **F11** | `src/internal/worker/runtime.go` | `ConflictMerge` fired only via async WAL subscriber (200ms poll) — `conflict_resolved` edge absent when tests/callers query immediately after ingest | Added `lastMem map[string]string` field tracking most-recent MemoryID per agent+session; sync `DispatchConflictMerge` in `SubmitIngest` immediately after storage; test flakiness eliminated (3/3 consecutive clean runs) |
| **F12** | `src/internal/storage/s3util.go` (new), `src/internal/s3util/` (deleted) | S3 utility (`LoadFromEnv`, `PutBytesAndVerify`, `GetBytes`, `ListObjects`, SigV4 signing) was in a separate `s3util` package — inconsistent with storage module cohesion and caused a stale import after s3store.go moved to `storage` package | Moved `s3util.go` into `src/internal/storage/`; deleted `src/internal/s3util/` directory; updated imports in `gateway.go`, `s3_snapshot_export_stub.go`, and `bootstrap.go` |
| **F13** | `src/internal/storage/factory.go`, `badger_stores.go`, `badger_helpers.go`, `composite.go`, `config_snapshot.go` (new) | No persistent storage option: all stores were in-memory only, lost on restart | Added `BuildRuntimeFromEnv` factory selecting memory vs Badger per `ANDB_STORAGE` env; Badger-backed `SegmentStore`, `IndexStore`, `ObjectStore`, `GraphEdgeStore`, `SnapshotVersionStore`, `PolicyStore`, `ShareContractStore`; `RuntimeBundle` carries `RuntimeStorage` + `ConfigSnapshot` + optional `Close`; `GET /v1/admin/storage` endpoint added; bootstrap now returns `func() error` shutdown including Badger close |

### New Files Created

| File | Purpose |
|---|---|
| `src/internal/worker/cognitive/default_algorithm.go` | No-op `MemoryManagementAlgorithm` implementation (identity `Recall`, nil for all other ops); used as default when no plugin configured |
| `src/internal/storage/s3util.go` | S3 utility moved from `src/internal/s3util/`: `LoadFromEnv`, `PutBytesAndVerify`, `GetBytes`, `ListObjects`, SigV4 signing |
| `src/internal/storage/badger_helpers.go` | Low-level Badger helpers: `badgerSetJSON`, `badgerGetJSON`, `badgerDelete`, `openBadger`, `openBadgerInMemory` |
| `src/internal/storage/badger_stores.go` | Badger-backed implementations for all 7 store interfaces: `SegmentStore`, `IndexStore`, `ObjectStore`, `GraphEdgeStore` (with `PruneExpiredEdges`), `SnapshotVersionStore`, `PolicyStore`, `ShareContractStore` |
| `src/internal/storage/composite.go` | `compositeRuntimeStorage` wiring arbitrary (memory/Badger/mixed) store implementations behind `RuntimeStorage`; `NewCompositeRuntimeStorage` |
| `src/internal/storage/config_snapshot.go` | `ConfigSnapshot` (JSON-serializable backend config) and `RuntimeBundle` (storage + config + optional Badger close func) |
| `src/internal/storage/factory.go` | `BuildRuntimeFromEnv` factory: env-driven backend selection via `ANDB_STORAGE`, `ANDB_DATA_DIR`, `ANDB_STORE_*`, `ANDB_BADGER_INMEMORY` |
| `src/internal/worker/nodes/default_algorithm.go` | (same as above, correct location) |

### Test Fixes Applied

| Test | Fix |
|---|---|
| `integration_tests/topology_test.go` | Updated expected node count from 18 → 19 (added AlgorithmDispatchWorker) |
| Debug test (`TestDebugStateIngest`) | Removed temporary debug test added during investigation |

### Feature Branch Assessment

| Branch | Status | Risk if Merged |
|---|---|---|
| `origin/feature/schema-a` | 70+ commits ahead of dev; large refactors (Badger storage, worker contract changes) | HIGH: removes derivationLog, tieredObjects from Runtime; significant test deletions |
| `origin/feature/retrieval-b` | Large refactors (C++ Knowhere wiring, Python bridge) | MEDIUM: structural changes, needs thorough review |
| `origin/feature/graph-c` | 1 commit ahead of dev (prefetch graph node properties) | LOW: appears compatible, review needed |
| `origin/feature/member-d-api` | 10+ commits, major Runtime refactor | HIGH: removes queryChain, derivationLog, tieredObjects fields — would break SubmitIngest, ExecuteQuery, bootstrap wiring |

> **Recommendation:** `feature/schema-a` and `feature/member-d-api` should NOT be merged to `dev` without a detailed diff review. The base `dev` branch is more complete and correct than these branches' revisions. Feature work should be cherry-picked commit by commit.

### Pre-Checklist (Run Before Any Merge)

```bash
# 1. Build verification
go build ./src/... && echo "BUILD OK"

# 2. Unit tests
go test ./src/... -count=1 -timeout 120s

# 3. Integration tests (requires running server)
# Start fresh server first:
lsof -ti:8080 | xargs kill -9 2>/dev/null; sleep 1
go run ./src/cmd/server &
sleep 2
go test ./integration_tests/... -count=1 -timeout 120s

# 4. Topology node count check
curl -s http://127.0.0.1:8080/v1/admin/topology | python3 -c "import sys,json; d=json.load(sys.stdin); print(f'nodes={len(d[\"nodes\"])}')"
# Expected: nodes=19
```

### Integration Checklist

| Area | Status | Notes |
|---|---|---|
| Write path: WAL → Materialization → ObjectStore → TieredDataPlane | ✅ | Verified end-to-end; 3-tier cascade confirmed |
| Write path: checkpoint → ObjectVersion snapshot | ✅ | `DispatchStateCheckpoint` called synchronously; versions available immediately |
| Query path: planner → retrieval → policy filter → assembler | ✅ | All steps present; `filterByObjectTypes` correctly routes State/Artifact |
| Query path: canonical supplemental retrieval | ✅ | `fetchCanonicalObjects` adds State/Artifact IDs from ObjectStore |
| Governance: ACL/TTL/Quarantine in policy engine | ✅ | `IsTTLExpired`, `IsQuarantineFlag`, `EffectiveSalience` all implemented |
| Governance: Dead-letter queue overflow safety | ✅ | Overflow buffer (cap 256) + `OverflowBuffer()` + `OverflowCount` metrics |
| Graceful shutdown: subscriber + orchestrator | ✅ | `context.WithCancel` from `BuildServer` cleanup function |
| Algorithm dispatch: wired + default no-op | ✅ | 19th node registered; topology confirms |
| `handleS3SnapshotExport`: not nil | ✅ | Full Avro/JSON/S3 implementation |
| `VerifiedState`: constants used everywhere | ✅ | `"retracted"` replaced with `string(schemas.VerifiedStateRetracted)` |
| Edge type constants: `causal` not used | ✅ | Replaced with `EdgeTypeCausedBy` |
| Cold archival: attributes correctly mapped | ✅ | `visibility` → Scope, `event_type` → OwnerType |
| EventEnvelope dead alias: removed | ✅ | No longer in canonical.go |
| ConflictMerge: fires synchronously in SubmitIngest | ✅ | lastMem map tracks per-session MemoryID; sync DispatchConflictMerge fires before response |
| Unit test pass rate | ✅ | 22/22 packages pass |
| Integration test pass rate | ✅ | 13/13 test files pass |
| Topology: correct node count | ✅ | 19 nodes confirmed |
| DerivationLog: in-memory only (not persisted) | 🔲 Open | Not a regression; documented in Remaining Open Items |
| Badger persistent storage: BuildRuntimeFromEnv wired | ✅ | `factory.go` + `RuntimeBundle` + `ConfigSnapshot`; bootstrap uses it; `cleanup()` includes `db.Close()` |
| `GET /v1/admin/storage` endpoint | ✅ | `handleStorage` in gateway; returns `ConfigSnapshot` JSON |
| `src/internal/s3util/` removed (consolidated into storage) | ✅ | All imports updated; old package dir deleted |

### Merge Readiness

| Criterion | Status |
|---|---|
| Main write path (ingest → WAL → materialization → storage → retrieval) runs | ✅ |
| Query path (planner → retrieval → filter → assembly → proof trace) runs | ✅ |
| Policy/version/visibility governance participates in query | ✅ |
| No nil-pointer routes registered in mux | ✅ |
| No undefined type/field references | ✅ |
| Dead code removed | ✅ |
| Constants used instead of magic strings | ✅ |
| Unit tests: all pass | ✅ |
| Integration tests: all pass | ✅ |
| README reflects current implementation | ✅ |
| Open items documented | ✅ |
| Feature branches assessed for regressions | ✅ |
| **Recommended action** | **Ready to push to `main`** (pending team review of open items) |


---
