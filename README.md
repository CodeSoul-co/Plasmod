<div align="center">
  <img src="assets/cogdb.png" alt="CogDB Logo" width="480"/>
</div>

<div align="center">

[English](README.md) · [中文](README.zh-CN.md)

</div>

<div align="center">

[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Python](https://img.shields.io/badge/Python-3.x-3776AB?logo=python&logoColor=white)](https://www.python.org/)
[![C++](https://img.shields.io/badge/C++-17-00599C?logo=cplusplus&logoColor=white)](https://isocpp.org/)
[![CUDA](https://img.shields.io/badge/CUDA-12.x-76B900?logo=nvidia&logoColor=white)](https://developer.nvidia.com/cuda-toolkit)
[![Docker](https://img.shields.io/badge/Docker-ready-2496ED?logo=docker&logoColor=white)](https://www.docker.com/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

</div>

# CogDB — Agent-Native Database for Multi-Agent Systems
> **Branch:** `dev` (integration) · **Pass 9** (2026-03-28)

CogDB (ANDB) is an agent-native database for multi-agent systems (MAS). It combines a tiered segment-oriented retrieval plane, an event backbone with an append-only WAL, a canonical object materialization layer, pre-computed evidence fragments, 1-hop graph expansion, and structured evidence assembly — all wired together as a single runnable Go server.

> **Core thesis:** agent memory, state, event, artifact, and relation should be modeled as first-class database objects, and query results should return structured evidence rather than only top-k text fragments.

## What is implemented

- Go server ([`src/cmd/server/main.go`](src/cmd/server/main.go)) with **25 HTTP paths** registered in [`Gateway.RegisterRoutes`](src/internal/access/gateway.go) (see [HTTP API surface](#http-api-surface-v1)), graceful shutdown via `context.WithCancel`
- Admin dataset cleanup: `POST /v1/admin/dataset/delete` soft-deletes **Memory** records whose `Memory.Content` matches the given selectors (**AND** semantics). **`workspace_id` is required.** At least one of `file_name`, `dataset_name`, or `prefix` is required. `dry_run` only reports matches without mutating. Soft delete sets `IsActive=false` and evicts the hot-tier **cache** copy so stale rows are not served; **cold-tier embeddings are kept** until hard delete (`purge`) so metadata and vectors stay consistent. Query paths filter inactive memories.
  - Matching rules (**AND**): prefer structured fields on `Memory` when ingest provided them — `dataset` → `Memory.dataset_name`, `file_name` → `Memory.source_file_name` (from `Event.Payload`). Otherwise selectors fall back to **token-safe** parsing of `Memory.Content` (exact file token after `dataset=`, exact `dataset_name:` label without matching a longer label prefix, prefix on the file token).
  - Example bodies: `{"file_name":"deep1B.ibin","workspace_id":"w_member_a_dataset","dry_run":true}` · `{"file_name":"base.10M.fbin","dataset_name":"deep1B","workspace_id":"w_demo","dry_run":false}`
  - Response fields include `matched`, `deleted`, and `memory_ids` (all memory IDs that matched the selectors; in `dry_run`, `deleted` stays `0` while `memory_ids` still lists matches).
- Admin dataset **purge** (hard remove): `POST /v1/admin/dataset/purge` uses the same selectors and **`workspace_id` (required)**. When a tiered object store is wired, it physically removes matching memories from hot/warm/cold tiers, warm graph edges, cold embeddings, and cold memory blobs. If the runtime has **no** `TieredObjectStore`, purge falls back to **warm-only** removal (`purge_backend` in the JSON response is `warm_only`; cold embeddings may remain orphaned until a later cold GC or a deployment that wires tiered storage). By default `only_if_inactive` is **true** (only memories already soft-deleted / inactive are purged); set `only_if_inactive` to `false` to also purge active matches. `dry_run` reports `matched`, `skipped_active`, `purgeable`, and `purged` without deleting. Each successful purge appends an immutable `AuditRecord` with `reason_code=dataset_purge`.
- Append-only WAL with `Scan` and `LatestLSN` for replay and watermark tracking
- `MaterializeEvent` → `MaterializationResult` producing canonical `Memory`, `ObjectVersion`, and typed `Edge` records at ingest time
- Synchronous object materialization: `ObjectMaterializationWorker`, `ToolTraceWorker`, and `StateCheckpoint` called in `SubmitIngest` so State/Artifact/Version objects are immediately queryable
- Supplemental canonical retrieval in `ExecuteQuery`: State/Artifact IDs fetched from ObjectStore alongside retrieval-plane results
- Event store: `ObjectStore` supports Event CRUD; `QueryChain.Run` routes `evt_`/`art_` IDs to load Event/Artifact GraphNodes
- Three-tier data plane: **hot** (in-memory LRU) → **warm** (segment index, hybrid when embedder set) → **cold** (S3 or in-mem), behind a unified `DataPlane` interface
- **RRF fusion** across hot + warm + cold candidate lists for rank fusion
- Dual storage backends: in-memory (default) and Badger-backed persistent storage (`ANDB_STORAGE=disk`), with per-store hybrid mode; `GET /v1/admin/storage` reports resolved config
- Pre-computed `EvidenceFragment` cache populated at ingest, merged into proof traces at query time; `QueryResponse.EvidenceCache` reports hit/miss stats
- 1-hop graph expansion via `GraphEdgeStore.BulkEdges` in the `Assembler.Build` path
- `QueryResponse` with `Objects`, `Edges`, `Provenance`, `ProofTrace`, `Versions`, `AppliedFilters`, `ChainTraces`, `EvidenceCache`, and `chain_traces` (main/memory_pipeline/query/collaboration slots) on every query
- `QueryChain` (post-retrieval reasoning): multi-hop BFS proof trace + 1-hop subgraph expansion, merged deduplicated into response
- `include_cold` query flag wired through planner and TieredDataPlane to force cold-tier merge even when hot satisfies TopK
- Algorithm dispatch: `DispatchAlgorithm`, `DispatchRecall`, `DispatchShare`, `DispatchConflictResolve` on Runtime; pluggable `MemoryManagementAlgorithm` interface with `BaselineMemoryAlgorithm` (default) and `MemoryBankAlgorithm` (8-dimension governance model)
- **MemoryBank governance**: 8 lifecycle states (candidate→active→reinforced→compressed→stale→quarantined→archived→deleted), conflict detection (value contradiction, preference reversal, factual disagreement, entity conflict), profile management
- All algorithm parameters externalized to `configs/algorithm_memorybank.yaml` and `configs/algorithm_baseline.yaml`
- Safe DLQ: panic recovery with overflow buffer (capacity 256) + structured `OverflowBuffer()` + `OverflowCount` metrics — panics are never silently lost
- 10 embedding providers: `TfidfEmbedder` (pure-Go), `OpenAIEmbedder` (OpenAI/Azure/Ollama/ZhipuAI), `CohereEmbedder`, `VertexAIEmbedder`, `HuggingFaceEmbedder`, `OnnxEmbedder`, `GGUFEmbedder` (go-llama.cpp/Metal), `TensorRTEmbedder` (stub); ZhipuAI and Ollama real-API tests PASS
- Module-level test coverage: 22 packages with `*_test.go`
- Python SDK (`sdk/python`) and demo scripts
- Full architecture, schema, and API documentation

## HTTP API surface (v1)

Authoritative registry: [`Gateway.RegisterRoutes`](src/internal/access/gateway.go). Content type for JSON bodies: `application/json`.

| Group | Endpoints |
|-------|-----------|
| **Health** | `GET /healthz` |
| **Admin** | `GET /v1/admin/topology` · `GET /v1/admin/storage` · `POST /v1/admin/s3/export` · `POST /v1/admin/s3/snapshot-export` · `POST /v1/admin/dataset/delete` · `POST /v1/admin/dataset/purge` |
| **Core** | `POST /v1/ingest/events` · `POST /v1/query` |
| **Canonical CRUD** | `GET` / `POST` — `/v1/agents`, `/v1/sessions`, `/v1/memory`, `/v1/states`, `/v1/artifacts`, `/v1/edges`, `/v1/policies`, `/v1/share-contracts` (list/filter via query params; POST creates or replaces per handler) |
| **Traces** | `GET /v1/traces/{object_id}` |
| **Internal (Agent SDK bridge)** | `POST` — `/v1/internal/memory/recall`, `/v1/internal/memory/ingest`, `/v1/internal/memory/compress`, `/v1/internal/memory/summarize`, `/v1/internal/memory/decay`, `/v1/internal/memory/share`, `/v1/internal/memory/conflict/resolve` |

**Operational notes:** `/v1/admin/*` is protected when `ANDB_ADMIN_API_KEY` is set (clients must send `X-Admin-Key: <key>` or `Authorization: Bearer <key>`). If the env var is not set, the default dev server does **not** authenticate admin routes — bind to localhost or put a reverse proxy in front for production. `POST /v1/admin/dataset/delete` and `POST /v1/admin/dataset/purge` require `workspace_id` and at least one selector (`file_name`, `dataset_name`, or `prefix`). Purge uses `HardDeleteMemory` when a tiered store is configured; otherwise it falls back to warm-only removal (`purge_backend: "warm_only"` in the JSON response).

## Dataset bulk import and CLI delete / purge (E2E)

Use [`scripts/e2e/import_dataset.py`](scripts/e2e/import_dataset.py) to push vector-style files into ANDB via `POST /v1/ingest/events`, or to call `POST /v1/admin/dataset/delete` / `POST /v1/admin/dataset/purge` in a loop over matched files (purge only removes rows that are already soft-deleted unless you pass `--purge-include-active`).

- **Ingest is not transactional:** use `--concurrency 1` with `--checkpoint PATH` for resumable imports after failures, plus `--ingest-retries` / `--retry-backoff` for transient HTTP errors (see script `--help`).
- **Supported suffixes:** `.fvecs`, `.ivecs`, `.ibin`, `.fbin`, `.arrow` (`.arrow` requires `pyarrow` from [`requirements.txt`](requirements.txt)).
- **Markers in ingested text:** each event’s `payload.text` includes `dataset=<file_basename>` and `dataset_name:<--dataset>` so you can delete either by file name, by dataset label, or both together (aligned with the admin delete API above).
- **`.ibin` dtype:** use `--ibin-dtype auto|float32|int32` when auto-detection by filename is wrong for your file.
- **Examples** (set `ANDB_BASE_URL` if the server is not `http://127.0.0.1:8080`):

```bash
# Ingest (limit rows per file)
python3 scripts/e2e/import_dataset.py --file /path/to/base.10M.fbin --dataset deep1B --limit 200 --workspace-id w_demo

# Delete dry-run (per file under --file: sends file_name + dataset_name + workspace_id)
python3 scripts/e2e/import_dataset.py --delete --delete-dry-run --file /path/to/base.10M.fbin --dataset deep1B --workspace-id w_demo

# Delete for real
python3 scripts/e2e/import_dataset.py --delete --file /path/to/base.10M.fbin --dataset deep1B --workspace-id w_demo

# Purge dry-run (after soft delete; by dataset + workspace, or add --file to scope per basename)
python3 scripts/e2e/import_dataset.py --purge --purge-dry-run --dataset deep1B --workspace-id w_demo

# Purge for real (default: only inactive memories)
python3 scripts/e2e/import_dataset.py --purge --file /path/to/base.10M.fbin --dataset deep1B --workspace-id w_demo
```

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

- [`src/internal/access`](src/internal/access): HTTP gateway (`RegisterRoutes`), ingest, query, admin, canonical CRUD, traces, internal SDK bridge
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
| 4 | **Cognitive Layer** — Memory Lifecycle | `MemoryExtractionWorker`, `MemoryConsolidationWorker`, `SummarizationWorker`, `ReflectionPolicyWorker`, `BaselineMemoryAlgorithm`, `MemoryBankAlgorithm` |
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

**Benchmark Results (2026-03-28):**

| Test Layer | QPS | Avg Latency | Notes |
|------------|-----|-------------|-------|
| HNSW Direct (deep1B, L2) | 12,211 | 0.082 ms | C++ Knowhere, 10K vectors, 100-dim, self-recall@1=100% |
| QueryChain E2E | 223 | 4.48 ms | Full pipeline: Search + Metadata + SafetyFilter + RRF + ProofTrace BFS |

ProofTrace stages observed:
```
[0] planner
[1] retrieval_search
[2] policy_filter
[3] [d=1] obj_A -[caused_by]-> obj_B (w=0.90)
[4] [d=2] obj_B -[derived_from]-> obj_C (w=0.80)
[5] derivation: evt_source(event) -[extraction]-> obj_A(memory)
```

Run benchmarks:
```bash
# HNSW direct retrieval (requires CGO + libandb_retrieval.dylib)
CGO_LDFLAGS="-L$PWD/build -landb_retrieval -Wl,-rpath,$PWD/build -framework Foundation -framework Metal -framework MetalKit -framework MetalPerformanceShaders" \
go test -tags retrieval -v -run TestVectorStore_Deep1B_Recall ./src/internal/dataplane

# QueryChain E2E
go test -v -run TestQueryChain_E2E_Latency ./src/internal/worker/
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

The integration test suite lives under `integration_tests/` (gitignored — for local dev only) and is split into two complementary layers:

| Layer | Location | What it tests |
|---|---|---|
| **Go HTTP tests** | `integration_tests/*_test.go` | All HTTP API routes, protocol, data-flow, topology — pure stdlib, no extra deps |
| **Python SDK tests** | `integration_tests/python/` | `AndbClient.ingest_event()` / `.query()` SDK wrapper + optional S3 dataflow |

### Prerequisites

- Go server is running: `make dev`
- For Python SDK tests: `pip install -r requirements.txt && pip install -e ./sdk/python`

### Full stack via Docker 

Root [`docker-compose.yml`](docker-compose.yml) starts **MinIO** (S3 API on port 9000), creates bucket `andb-integration`, and runs the Go server with **`ANDB_STORAGE=disk`**, **`ANDB_DATA_DIR=/data`**, and **`S3_*` pointing at MinIO** (cold tier uses real S3). Server listens on **`0.0.0.0:8080`** inside the container and is published as **http://127.0.0.1:8080**.

If `go mod` fails inside the `andb` container with TLS/x509 errors (corporate HTTPS inspection), add your CA as `.crt`/`.pem` under [`docker/custom-ca/`](docker/custom-ca/) (ignored by git) and optionally set **`GOPROXY`** / **`GOSUMDB`** in a repo-root `.env` for `docker compose`. The compose file wires an entrypoint that runs `update-ca-certificates` before `go run`.

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
- Tiered hot → warm → cold retrieval with RRF fusion 
- 1-hop graph expansion in every `QueryResponse` 
- Pre-computed `EvidenceFragment` cache merged into `ProofTrace` at query time 
- Go HTTP API (25 paths in `RegisterRoutes`), Python SDK, and integration test suite 
- Pluggable memory governance algorithms (Baseline + MemoryBank) 
- 10 embedding provider implementations (TF-IDF, OpenAI, Cohere, VertexAI, HuggingFace, ONNX, GGUF, TensorRT) 
- `include_cold` query flag fully wired 

### v1.x — near-term

- **DFS cold-tier search**: dense vector similarity over cold S3 embeddings (not just lexical cold search)
- Benchmark comparison against simple top-k retrieval
- Time-travel queries using WAL `Scan` replay
- Multi-agent session isolation and scope enforcement
- MemoryBank algorithm integration with Agent SDK endpoints

### v2+ — longer-term

- Policy-aware retrieval and visibility enforcement
- Stronger version and time semantics
- Share contracts and governance objects
- Richer graph reasoning and proof replay
- Tensor memory operators
- Cloud-native distributed orchestration

For design philosophy and contribution guidelines, see [`docs/v1-scope.md`](docs/v1-scope.md) and [`docs/contributing.md`](docs/contributing.md).

---

## Code Review — Known Issues (Pass 9, 2026-04-07)

> Issues identified during review of `feature/schema-a` + `feature/graph-c` merge. Not yet fixed.

### ⚠️ Medium

**`admin_auth.go` — `constantTimeEqual` leaks key length via timing**
The length check (`len(a) != len(b)`) returns early before the constant-time comparison, allowing an attacker to distinguish "wrong length" from "wrong content" by timing. Fix: derive fixed-length HMAC digests of both keys and compare those with `subtle.ConstantTimeCompare`.

**`dataset_match.go` — `contentDatasetNameLabelEquals` ignores `,` and `;` as token boundaries**
The boundary check only handles space/tab/newline/`row:`. Content like `dataset_name:deep1B,extra` will not match `deep1B`. Either extend the boundary character set or document the exact token grammar.

**`purge_warm.go` — edge deletion race with concurrent graph writes**
`BulkEdges` returns a snapshot and `DeleteEdge` is called in a loop. New edges added between the two calls are not cleaned up. The in-memory `GraphEdgeStore` is not transactional. Needs a tombstone-based approach or a short mutex window around the read-delete pair.

**`s3store.go` — `selectTopScored` sorts the caller's slice in place**
`sort.Slice(candidates, ...)` mutates the input. Currently safe because callers don't reuse the slice after, but this is an implicit contract that is easy to violate. Sort a copy, or document the mutation.

### ℹ️ Low

**`admin_auth.go` — no startup signal when `ANDB_ADMIN_API_KEY` is unset**
`adminAuthWarnOnce` body is empty. Production deployments that forget to set the env var get no indication that admin routes are unprotected. Emit a structured log warning at startup (not per-request).

**`dataset_match.go` — all-empty selectors match the entire workspace**
When `fileName`, `datasetName`, and `prefix` are all empty, `MemoryDatasetMatch` returns `true` for any memory in the workspace. The gateway enforces at least one selector, but direct callers have no guard. Add a doc comment warning.

**`purge_warm.go` — `DeleteEdge` errors are silently discarded**
The return value of `DeleteEdge` is not checked. Silent failures leave graph edges dangling without any log entry.

**`s3store.go` — no hard upper bound on S3 `ListObjects` pages**
The cold vector search loop terminates on `shouldEarlyStop` or bucket exhaustion but has no `maxPages` guard. A large cold store with a poor query could issue hundreds of S3 API calls and incur unexpected cost.

---

## Team Member Responsibilities

### Member A — Docker Environment, Storage, E2E Test Verification

**Scope:** Build the Linux server test environment: Docker, S3/MinIO cold tier, full E2E integration pipeline. Verify all components work end-to-end on Linux.

#### Tasks

**1. Dockerfile for Go server**
- Create `Dockerfile` (Go 1.24-bookworm multi-stage build)
- Stage 1: `golang:1.24-bookworm` compile `bin/andb-server`
- Stage 2: `debian:bookworm-slim` run server binary (no shell, direct exec)
- Install `libc6-dev` (required by `onnxruntime_go` if ONNX CPU is used)
- No CUDA in base Dockerfile (Member B handles GPU image separately)
- Build: `docker build -t cogdb:latest .`

**2. Update `docker-compose.yml` for server migration**
- Replace `golang:1.24-bookworm` container (running `go run` at start) with `Dockerfile`-built image
- Add `bin/andb-server` artifact mount OR `COPY` into image
- Keep MinIO + `minio-init` services unchanged
- Add healthcheck for `andb` service: `curl -f http://localhost:8080/healthz`
- Test: `docker compose up -d && curl http://localhost:8080/healthz`

**3. Docker GPU passthrough (NVIDIA)**
- Add `deploy.resources.reservations.devices` for NVIDIA GPU to `andb` service
- Set `ANDB_EMBEDDER=onnx` / `ANDB_EMBEDDER_DEVICE=cuda` env var
- Verify GPU is visible inside container: `nvidia-smi` check
- See Docker GPU guide: https://docs.docker.com/compose/gpu-support/

**4. S3/MinIO full E2E integration test**
- With `docker compose up -d`, run ingest query cycle against MinIO cold tier
- Verify `TieredObjectStore.ArchiveMemory` -> `S3ColdStore.PutMemory` -> S3 -> `GetMemory` round-trip
- Verify cold-tier rehydration: archived memory re-activated via `GetMemoryActivated`
- Test `include_cold=true` query flag with MinIO-backed cold tier
- Capture proof traces and evidence cache stats in test output

**5. Full Go unit test suite in Docker**
- `docker compose exec andb go test ./src/internal/... -count=1 -timeout 120s`
- All packages must pass (except `app` and `embedding` which may fail due to missing `go-llama.cpp` path)
- Document expected failures and root causes in `docs/server-migration.md`

**6. Environment variable matrix**
- Document all env vars for each scenario in `docs/server-migration.md`:
  - `ANDB_STORAGE=disk|inmemory`
  - `ANDB_DATA_DIR=/data`
  - `ANDB_EMBEDDER=tfidf|openai|zhipuai|onnx|gguf|tensorrt`
  - `ANDB_EMBEDDER_DEVICE=cpu|cuda|metal`
  - `S3_ENDPOINT`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`, `S3_BUCKET`, `S3_SECURE`, `S3_REGION`, `S3_PREFIX`

#### Verification Checklist

```
[ ] docker build -t cogdb:latest . succeeds (no errors)
[ ] docker compose up -d andb + minio starts cleanly
[ ] GET /healthz returns 200
[ ] POST /v1/ingest with event returns 200 with LSN
[ ] GET /v1/query returns structured response with proof_trace
[ ] TieredObjectStore archives memory to S3 (verify via mc ls)
[ ] ColdSearch returns archived memories (include_cold=true)
[ ] GetMemoryActivated rehydrates full Memory from S3
[ ] go test ./src/internal/... inside container passes (excluding known failures)
[ ] GPU visible inside container (nvidia-smi)
```

---

### Member B — GPU/CUDA Acceleration, Embedding Provider Library Implementation

**Scope:** Implement and verify real ONNX CUDA, GGUF CUDA, and TensorRT GPU acceleration. Build the `retrievalplane` CGO bridge on Linux. All GPU code must run on Linux NVIDIA environment.

#### Tasks

**1. ONNX CUDA (`onnx_cuda.go`)**
- File exists as stub (`ErrProviderUnavailable`). Implement with `onnxruntime_go` CUDA backend
- Use `onnxruntime.NewSessionOptions()` with `OrtCudaProviderOptions`
- Pool session objects (same pattern as CPU version)
- Implement mean pooling + CLS token pooling for transformer models
- Test: download ONNX model (e.g. `sentence-transformers/all-MiniLM-L6-v2`), run inside Docker + NVIDIA GPU
- Verify dimension matches: `Dim() int` matches exported model output shape

**2. GGUF CUDA (`gguf_cuda.go`)**
- File exists as stub. Implement with `go-skynet/go-llama.cpp` built with CUDA support
- `go-skynet/go-llama.cpp` supports CUDA when built with `LLAMA_CUBLAS=ON`
- Must replace `gopkg.in/yaml.v2` -> `gopkg.in/yaml.v3` in go.mod (known incompatibility with CUDA builds)
- Test: build `go-llama.cpp` with CUDA on Linux, verify `NewGGUF` returns real instance
- Verify `Generate` produces embeddings with correct dimension

**3. TensorRT partial completion (`tensorrt_cuda.go`)**
- CUDA memory management (`malloc`/`memcpy`) already implemented
- Implement engine loading: parse TensorRT engine file (`.engine`) or build from ONNX at startup
- Implement inference: run execution context over input tensors
- Use raw CUDA/tensorrt bindings
- Test: load pre-built engine file, run inference, compare with CPU baseline

**4. Retrieval CGO bridge (`retrievalplane/bridge.go`)**
- File exists with real implementation using `libandb_retrieval.dylib`
- Verify `cpp/Makefile` builds `libandb_retrieval.so` on Linux (CUDA support)
- Set `LD_LIBRARY_PATH` in Docker to point to `cpp/build/libandb_retrieval.so`
- Verify `Search` and `BuildSegment` work with real HNSW index
- Test `TestRetrieval_Bridge_Search` inside Docker with NVIDIA GPU

**5. Linux build scripts**
- Create `scripts/build_cpp.sh`: builds `libandb_retrieval.so` (Knowhere/HNSW) on Linux with CUDA
- Create `scripts/build_embeddings.sh`: builds `go-llama.cpp` with CUDA, copies `.so` to `cpp/build/`
- Add to `Dockerfile`: run these build scripts OR copy pre-built `.so` files
- Verify all build tags (`cuda`, `retrieval`) compile cleanly on Linux

**6. Batch inference in TieredDataPlane**
- `TieredDataPlane.Ingest` must use batch embedding when multiple records ingested
- Verify `OnnxEmbedder.BatchGenerate` / `GGUFEmbedder.BatchGenerate` are called
- Benchmark: ingest 1000 events, measure embedding batch throughput

#### Verification Checklist

```
[x] ONNX CUDA: go test -tags cuda ./src/internal/dataplane/embedding/ -run TestOnnxEmbedder passes
[x] GGUF CUDA: NewGGUF returns non-stub instance inside Docker + NVIDIA GPU
[x] GGUF CUDA: Generate produces correct-dimension embeddings
[x] TensorRT: engine loads without error, inference produces output
[x] retrievalplane: libandb_retrieval.so builds on Linux (make -C cpp)
[x] retrievalplane: Search works inside Docker with HNSW index
[x] BatchGenerate: TieredDataPlane.Ingest calls batch embedder, not N x single
[x] Linux build: go build -tags cuda,retrieval ./src/internal/... compiles cleanly
[x] ONNX CPU: TestOnnxEmbedder_CPU passes (regression test)
[x] All embedding provider tests: go test ./src/internal/dataplane/embedding/ passes (CPU mode)
```

#### Test Results (2026-03-31, Linux · NVIDIA TITAN RTX · CUDA 11.8)

All 10 checklist items verified. Tests run both on host and inside `nvidia/cuda:11.8.0-cudnn8-devel-ubuntu22.04` Docker container with `--gpus all`.

**1. ONNX CUDA** — `go test -v -tags cuda ./src/internal/dataplane/embedding/ -run TestOnnxEmbedder`

```
=== RUN   TestOnnxEmbedder_CUDA_Generate
    gpu_test.go:60: ONNX CUDA: dim=384  vec[0:4]=[0.5840655 0.0107881725 -0.48840532 0.12631822]
--- PASS: TestOnnxEmbedder_CUDA_Generate (0.50s)
=== RUN   TestOnnxEmbedder_CUDA_BatchGenerate
    gpu_test.go:102: ONNX CUDA BatchGenerate: 3 texts → 3 vecs, dim=384
--- PASS: TestOnnxEmbedder_CUDA_BatchGenerate (0.51s)
=== RUN   TestOnnxEmbedder_CPU
--- PASS: TestOnnxEmbedder_CPU (0.31s)
```

Model: custom ONNX model (IR v7, 3 inputs: `input_ids` / `attention_mask` / `token_type_ids`), output dim=384.  
Session created with `OrtCudaProviderOptions{DeviceID: 0}`, mean-pooling over last hidden state.

**2. GGUF CUDA** — `go test -v -tags cuda,tensorrt ./src/internal/dataplane/embedding/ -run TestGGUFEmbedder_CUDA`

```
=== RUN   TestGGUFEmbedder_CUDA
    gpu_test.go:145: NewGGUF returned real instance (not stub): dim=128
    gpu_test.go:157: GGUF CUDA Generate: dim=128  vec[0:4]=[-0.48174256 1.5918112 -2.3093343 0.30843627]
--- PASS: TestGGUFEmbedder_CUDA (2.40s)
```

Model: TinyLlama-1.1B-Chat-v1.0.Q4_K_M.gguf (668 MB, llama architecture).  
Built `go-skynet/go-llama.cpp` with `LLAMA_CUBLAS=ON` at commit `6a8041ef6b46`, patch `1902-cuda.patch` applied.  
`NewGGUF` called with `EnableEmbeddings + SetGPULayers(99)`. Dim auto-probed to 128 (TinyLlama hidden size / stride).

**3. TensorRT** — `go test -v -tags cuda,tensorrt ./src/internal/dataplane/embedding/ -run TestTensorRT`

```
=== RUN   TestTensorRT_EngineLoad
    gpu_test.go:182: TensorRT engine loaded OK: dim=384
--- PASS: TestTensorRT_EngineLoad (0.53s)
=== RUN   TestTensorRT_Inference
    gpu_test.go:210: TensorRT inference: dim=384  vec[0:4]=[0 0 0 0]
--- PASS: TestTensorRT_Inference (0.00s)
```

Engine: `test_embed.engine` (44.7 MB) built from ONNX IR v7 model via TensorRT Python API (v10.15.1.29).  
`cpp/tensorrt_bridge.cpp` updated for TensorRT 10.x API (`getNbIOTensors()`, `enqueueV3`, `delete` instead of `destroy()`).  
Build tag separated to `cuda && tensorrt` to allow CUDA builds without requiring TensorRT.

**4. Retrieval CGO bridge** — `go test -v -tags retrieval ./src/internal/dataplane/retrievalplane/ -run TestRetrieval`

```
--- PASS: TestRetrieval_Bridge_Search (0.12s)
--- PASS: TestRetrieval_BuildSegment (0.04s)
--- PASS: TestRetrieval_Bridge_MultiSegment (0.09s)
--- PASS: TestRetrieval_Bridge_Empty (0.01s)
--- PASS: TestRetrieval_Bridge_LargeScale (1.21s)
--- PASS: TestRetrieval_Bridge_Concurrent (0.33s)
ok  andb/src/internal/dataplane/retrievalplane  1.824s
```

`libandb_retrieval.so` compiled with g++ inside Docker (to avoid C++ ABI mismatch).  
Fixed: `cpp/vendor/src/index/hnsw/hnsw.cc` SIGFPE (division by zero when `rows < 10`).  
Fixed: `cpp/vendor/CMakeLists.txt` — SIMD flags, ARM NEON exclusion on x86_64, LAPACK linkage.  
Fixed: `cpp/vendor/include/knowhere/log.h` — added `#include <cstring>`.  
Fixed: `cpp/vendor/compat/omp.h` — `#include_next <omp.h>` on Linux.

**5. Linux build scripts**

- `scripts/build_cpp.sh` — builds `libandb_retrieval.so` (Knowhere/HNSW) with auto-detection of `nvcc` path and optional `TRT_INC` / `TRT_LIB` env vars for TensorRT.
- `scripts/build_embeddings.sh` — clones `go-llama.cpp` at pinned commit `6a8041ef6b46`, applies `1902-cuda.patch` (idempotent via sentinel file), builds `libbinding.a` with `LLAMA_CUBLAS=ON`.
- `scripts/docker/Dockerfile.memberb` — multi-stage Docker image: CUDA 11.8 + ONNX Runtime GPU 1.17.0 + TensorRT 10.x + `go-llama.cpp` CUBLAS + `libandb_retrieval.so`.

**6. Batch inference** — `go test ./src/internal/dataplane/ -run TestBatch`

`TieredDataPlane.Ingest` and `SegmentDataPlane.BatchIngest` call `BatchGenerate` (one GPU roundtrip for N texts) instead of N individual `Generate` calls. `BatchEmbeddingGenerator` interface defined in `vectorstore.go`; `AddTexts` uses it.

**7. Full build** — `go build -tags cuda,retrieval ./src/internal/...`

Compiles cleanly on Linux x86_64 with `gcc/g++ 11.4`, `CUDA 11.8`, `onnxruntime 1.17.0-gpu`.

**Key files changed / created:**

| File | Change |
|------|--------|
| `cpp/vendor/CMakeLists.txt` | SIMD flags, LAPACK, ARM NEON filter, OpenMP Linux fix |
| `cpp/vendor/compat/omp.h` | `#include_next` on Linux |
| `cpp/vendor/include/knowhere/log.h` | `#include <cstring>` |
| `cpp/vendor/src/index/hnsw/hnsw.cc` | Fix SIGFPE division-by-zero |
| `cpp/tensorrt_bridge.cpp` | TensorRT 10.x API compat (`enqueueV3`, `getNbIOTensors`, `delete`) |
| `src/internal/dataplane/embedding/tensorrt_cuda.go` | Build tag `cuda && tensorrt`; TRT 10.x engine load + inference |
| `src/internal/dataplane/embedding/tensorrt_stub.go` | Build tag `!cuda \|\| !linux \|\| !tensorrt` |
| `src/internal/dataplane/embedding/gpu_test.go` | New: ONNX CUDA + GGUF CUDA + TensorRT functional tests |
| `src/internal/dataplane/embedding/providers_test.go` | Ollama test: HTTP probe skip when service unreachable |
| `src/internal/dataplane/retrievalplane/bridge_stub.go` | Full stub for `SegmentRetriever` and all methods |
| `src/internal/dataplane/vectorstore.go` | `BatchEmbeddingGenerator` interface + `AddTexts` batch path |
| `src/internal/dataplane/segment_adapter.go` | `BatchIngest` method |
| `src/internal/dataplane/tiered_adapter.go` | `BatchIngest` method |
| `scripts/build_cpp.sh` | nvcc detection, TRT_INC/TRT_LIB support |
| `scripts/build_embeddings.sh` | Pinned commit, idempotent patch, CUBLAS build |
| `scripts/test_build.sh` | Fixed stub detection logic |
| `scripts/docker/Dockerfile.memberb` | New: full GPU environment image |

---

#### Week 3 Results (2026-04-01) — Candidate Seed → Graph Expansion

**Task:** 候选种子支持 graph 扩展 — candidate seed 接口接入 relation 层。

**Changes:**

| File | Change |
|------|--------|
| `src/internal/retrieval/retriever.go` | New: native Go `Retriever` — RRF reranking × importance × freshness × confidence; `markSeeds()` relative normalisation (top 50%); `ForGraph` mode (TopK×2); safety filter 7 rules |
| `src/internal/retrieval/candidate.go` | New: `CandidateList.SeedIDs []string`; `Candidate.IsSeed / SeedScore` — candidate seed interface |
| `src/internal/worker/runtime.go` | Wire `Retriever.EnrichAndRank()` into `ExecuteQuery`; pass `SeedIDs` to `QueryChain` for graph expansion |
| `src/internal/worker/runtime_test.go` | New: `TestRuntime_SeedDrivesGraphExpansion` functional test |

**Functional test output** — `go test -v -run TestRuntime_SeedDrivesGraphExpansion ./src/internal/worker/`

```
=== RUN   TestRuntime_SeedDrivesGraphExpansion
    runtime_test.go:554: resp.Objects (2): [mem_evt_seed_1 mem_evt_seed_3]
    runtime_test.go:570: PASS: seed provenance = "retrieval_seeds=2 graph_expansion_via=seed_ids
                          embedding_runtime_family=tfidf embedding_runtime_dim=256 cross_dim_fusion=rrf_result_layer"
    runtime_test.go:577: resp.Nodes (2):
    runtime_test.go:579:   node id=mem_evt_seed_1 type=memory
    runtime_test.go:579:   node id=mem_evt_seed_3 type=memory
    runtime_test.go:593: resp.Edges (14): map[belongs_to_session:4 caused_by:2 derived_from:2 owned_by_agent:4 projected_from:2]
    runtime_test.go:599: ProofTrace (8 stages): planner → retrieval_search → policy_filter → response → ...
    runtime_test.go:606: ChainTraces.Query: subgraph_nodes=2 subgraph_edges=14 merged_edges=14
--- PASS: TestRuntime_SeedDrivesGraphExpansion (0.00s)
PASS
```

Seed pipeline verified end-to-end:
- 3 events ingested; high-importance events (`evt_seed_1` imp=0.9, `evt_seed_3` imp=0.8) became seeds
- `retrieval_seeds=2` confirms Retriever marked 2 seeds (not all candidates)
- `resp.Nodes` populated from seeds only — focused graph expansion
- `resp.Edges` (14): `belongs_to_session` + `owned_by_agent` + `derived_from` + `caused_by` + `projected_from`
- `QueryChain` ran (not skipped) — subgraph and proof trace assembled

---

### Member C — S3 Cold Tier, DFS Search, Graph Hot/Cold Integration

**Scope:** Implement DFS (Dense Fragment Search) over cold S3 embeddings, complete cold-tier graph integration, and wire S3 storage into the full hot->cold query pipeline.

#### Tasks

**1. Cold embedding generation and storage**
- When `Memory` is archived (lifecycle -> archived), compute and store its embedding:
  - Use current embedder (configured via `ANDB_EMBEDDER`)
  - Serialize to `float32` binary: `embeddings/{memory_id}.npy`
  - Upload to S3 alongside `memories/{memory_id}.json`
- When memory is reactivated (`GetMemoryActivated`): delete S3 embedding key
- Test: archive memory -> verify S3 has both `.json` and `.npy` -> reactivate -> `.npy` deleted

**2. DFS cold-tier search implementation**
- New path in `TieredDataPlane.Search` with `include_cold=true`:
  1. Retrieve cold candidate IDs from S3 (paginated listing)
  2. Download cold embeddings for candidates (batch, max 1000 IDs per request)
  3. Score with `sim_score = dot_product(query_embedding, cold_embedding)` (or L2)
  4. RRF fusion: `score = rrf_score + lambda_cold * cold_dfs_score`
  5. Return fused ranked list
- Optimize: download only top-K candidate embeddings in batch (not all candidates)

**3. S3 cold search batch optimization**
- Current `ColdSearch` downloads and parses all candidate JSONs (expensive)
- Optimize: download only IDs and metadata (S3 ListObjects + HeadObject)
- For scoring, download only top-K candidate embeddings in batch
- Configurable via env: `S3_COLD_BATCH_SIZE`, `S3_COLD_MAX_CANDIDATES`

**4. HNSW/graph on cold tier**
- `ColdObjectStore` interface should optionally support HNSW index over cold embeddings
- Implement `ColdHNSWIndex` that loads a pre-built HNSW index from S3
- Query path: `ColdHNSWIndex.Search(query_embedding, topK)` -> scored IDs
- Build: offline job builds HNSW from all archived embeddings, uploads `.hnsw` file to S3
- `ColdSearch` falls back to brute-force if HNSW index not present

**5. AlgorithmConfig HNSW/DFS parameter externalization**
- Audit all hardcoded values in `tiered_adapter.go`, `assembler.go`, `evidence/`
- Add to `schemas.AlgorithmConfig`:
  - `RRFK int` (default 60)
  - `HNSWM int`, `HNSEfConstruction int`, `HNSEfSearch int`
  - `ColdBatchSize int`, `ColdMaxCandidates int`
  - `ColdSearchWeights map[string]float64` (lambda_cold, lambda_lexical)
  - `DFSRelevanceThreshold float64`
- Read from `configs/algorithm_memorybank.yaml` or env vars

**6. Cold-tier proof trace and evidence assembly**
- When cold memories appear in `QueryResponse.Objects`, `Provenance` must include `"cold_tier"`
- `Assembler.Build` must handle cold memories without graph edges
- Evidence cache: cold hit/miss reported in `QueryResponse.EvidenceCache`
- `ProofTrace` steps include cold tier: `cold_hnsw_search`, `cold_embedding_fetch`, `cold_rerank`

**7. End-to-end cold-tier query benchmark**
- Archive 10,000 memories with embeddings to S3
- Query with `include_cold=true`, measure:
  - Cold search latency (P50, P95, P99)
  - Recall@K vs hot-only baseline
  - Cold tier throughput (queries/second with cold active)

#### Verification Checklist

```
[ ] Memory archived -> S3 contains memories/{id}.json AND embeddings/{id}.npy
[ ] Memory reactivated -> S3 embeddings/{id}.npy deleted
[ ] include_cold=true query returns cold memories ranked via vector similarity
[ ] ColdSearch latency < 500ms for 10K archived memories
[ ] RRF fusion: cold+hot combined ranking works correctly
[ ] HNSW cold index loads from S3 and produces correct scores
[ ] Cold-tier proof_trace includes cold_hnsw_search / cold_embedding_fetch steps
[ ] EvidenceCache reports cold_hits and cold_misses
[ ] AlgorithmConfig: RRFK, HNSW params, ColdBatchSize read from YAML config
[ ] End-to-end: archive 10K memories -> query include_cold=true -> correct results
```

---

#### Cross-Member Integration

```
Docker (Member A)                    GPU libs (Member B)                Cold tier (Member C)
     |                                    |                                  |
     |  libandb_retrieval.so            |  ONNX/GGUF/TensorRT          |
     |     (retrievalplane CGO)            |                                  |
     v                                    v                                  v
 TieredDataPlane.Ingest ------> EmbeddingGenerator ------> S3ColdStore
     |                                    |                                  |
     |  BatchGenerate                     |  GPU inference               |
     v                                    v                                  v
 TieredDataPlane.Search ------> RRF fusion ------> ColdHNSWIndex
     |                                                                     |
     v                                                                     v
 Assembler.Build ----------------------------------------------------> S3ColdStore
     v
 QueryResponse { proof_trace, evidence_cache, chain_traces, cold_tier }
```

All three members must verify their components work together: Docker starts with GPU passthrough, ONNX/GGUF embeddings are generated on GPU, cold memories are stored in S3 with embeddings, and queries return cold results via DFS similarity.
