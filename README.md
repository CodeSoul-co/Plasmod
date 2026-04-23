<div align="center">
  <img src="assets/plasmod.png" alt="Plasmod Logo" width="480"/>
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

# Plasmod — Agent-Native Database for Multi-Agent Systems

Plasmod is an agent-native database for multi-agent systems. Inspired by the adaptive, decentralized organization of slime mold networks, it unifies cognitive object storage, event-driven materialization, and structured evidence retrieval in a single runnable system. Plasmod integrates a tiered segment-oriented retrieval plane, an event backbone built on an append-only WAL, a canonical object materialization layer, precomputed evidence fragments, lightweight 1-hop graph expansion, and structured evidence assembly, all wired together as a single Go server for agent-native workloads.

> **Core thesis:** agent memory, state, event, artifact, and relation should be modeled as first-class database objects, and query results should return structured evidence rather than only top-k text fragments.

## What is implemented

- Go server ([`src/cmd/server/main.go`](src/cmd/server/main.go)) with **25 HTTP paths** registered in [`Gateway.RegisterRoutes`](src/internal/access/gateway.go) (see [HTTP API surface](#http-api-surface-v1)), graceful shutdown via `context.WithCancel`
- Admin dataset cleanup: `POST /v1/admin/dataset/delete` soft-deletes **Memory** records whose `Memory.Content` matches the given selectors (**AND** semantics). **`workspace_id` is required.** At least one of `file_name`, `dataset_name`, or `prefix` is required. `dry_run` only reports matches without mutating. Soft delete sets `IsActive=false` and evicts the hot-tier **cache** copy so stale rows are not served; **cold-tier embeddings are kept** until hard delete (`purge`) so metadata and vectors stay consistent. Query paths filter inactive memories.
  - Matching rules (**AND**): prefer structured fields on `Memory` when ingest provided them — `dataset` → `Memory.dataset_name`, `file_name` → `Memory.source_file_name` (from `Event.Payload`). Otherwise selectors fall back to **token-safe** parsing of `Memory.Content` (exact file token after `dataset=`, exact `dataset_name:` label without matching a longer label prefix, prefix on the file token).
  - Example bodies: `{"file_name":"deep1B.ibin","workspace_id":"w_member_a_dataset","dry_run":true}` · `{"file_name":"base.10M.fbin","dataset_name":"deep1B","workspace_id":"w_demo","dry_run":false}`
  - Response fields include `matched`, `deleted`, and `memory_ids` (all memory IDs that matched the selectors; in `dry_run`, `deleted` stays `0` while `memory_ids` still lists matches).
- Admin dataset **purge** (hard remove): `POST /v1/admin/dataset/purge` uses the same selectors and **`workspace_id` (required)**. When a tiered object store is wired, it physically removes matching memories from hot/warm/cold tiers, warm graph edges, cold embeddings, and cold memory blobs. If the runtime has **no** `TieredObjectStore`, purge falls back to **warm-only** removal (`purge_backend` in the JSON response is `warm_only`; cold embeddings may remain orphaned until a later cold GC or a deployment that wires tiered storage). By default `only_if_inactive` is **true** (only memories already soft-deleted / inactive are purged); set `only_if_inactive` to `false` to also purge active matches. `dry_run` reports `matched`, `skipped_active`, `purgeable`, and `purged` without deleting. Each successful purge appends an immutable `AuditRecord` with `reason_code=dataset_purge`.
- Optional request flags: `include_memory_ids` (default `true`, set `false` to omit `memory_ids`/`purged_memory_ids`), `async` (default `false`, enqueue background hard-delete task), `idempotency_key` (dedupe queued/running async tasks).
  - Async task API: `GET /v1/admin/dataset/purge/task?task_id=...` returns task state (`queued`/`running`/`completed`/`failed`/`cancelled`), processed/failed counters, and `progress_percent`.
  - Response includes operational diagnostics such as `scanned`, `workspace_scanned`, `cancelled`, `cancel_reason`, `purge_workers`, `purge_batch_size`, `purge_queue_size`, `purge_elapsed_ms`, `purge_scan_elapsed_ms`, `purge_delete_elapsed_ms`, and per-phase delete timings.
  - Runtime tuning env vars: `PLASMOD_DATASET_PURGE_WORKERS`, `PLASMOD_DATASET_PURGE_BATCH_SIZE`, `PLASMOD_DATASET_PURGE_QUEUE_SIZE`, `PLASMOD_HARD_DELETE_BATCH_WORKERS`, `PLASMOD_HARD_DELETE_QUEUE_FILE`.
- Append-only WAL with `Scan` and `LatestLSN` for replay and watermark tracking
- `MaterializeEvent` → `MaterializationResult` producing canonical `Memory`, `ObjectVersion`, and typed `Edge` records at ingest time
- Synchronous object materialization: `ObjectMaterializationWorker`, `ToolTraceWorker`, and `StateCheckpoint` called in `SubmitIngest` so State/Artifact/Version objects are immediately queryable
- Supplemental canonical retrieval in `ExecuteQuery`: State/Artifact IDs fetched from ObjectStore alongside retrieval-plane results
- Event store: `ObjectStore` supports Event CRUD; `QueryChain.Run` routes `evt_`/`art_` IDs to load Event/Artifact GraphNodes
- Three-tier data plane: **hot** (in-memory LRU) → **warm** (segment index, hybrid when embedder set) → **cold** (S3 or in-mem), behind a unified `DataPlane` interface
- **RRF fusion** across hot + warm + cold candidate lists for rank fusion
- Dual storage backends: in-memory (default) and Badger-backed persistent storage (`PLASMOD_STORAGE=disk`), with per-store hybrid mode; `GET /v1/admin/storage` reports resolved config
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
| **Admin** | `GET /v1/admin/topology` · `GET /v1/admin/storage` · `GET /v1/admin/config/effective` · `POST /v1/admin/s3/export` · `POST /v1/admin/s3/snapshot-export` · `POST /v1/admin/s3/cold-purge` · `POST /v1/admin/warm/prebuild` · `POST /v1/admin/dataset/delete` · `POST /v1/admin/dataset/purge` · `GET /v1/admin/dataset/purge/task` · `POST /v1/admin/data/wipe` · `POST /v1/admin/rollback` · `GET`/`POST /v1/admin/consistency-mode` · `POST /v1/admin/replay` |
| **Core** | `POST /v1/ingest/events` · `POST /v1/query` |
| **Canonical CRUD** | `GET` / `POST` — `/v1/agents`, `/v1/sessions`, `/v1/memory`, `/v1/states`, `/v1/artifacts`, `/v1/edges`, `/v1/policies`, `/v1/share-contracts` (list/filter via query params; POST creates or replaces per handler) |
| **Traces** | `GET /v1/traces/{object_id}` |
| **Internal (Agent SDK bridge)** | `POST` — `/v1/internal/memory/recall`, `/v1/internal/memory/ingest`, `/v1/internal/memory/compress`, `/v1/internal/memory/summarize`, `/v1/internal/memory/decay`, `/v1/internal/memory/share`, `/v1/internal/memory/conflict/resolve` |

**Operational notes:** `/v1/admin/*` is protected when `PLASMOD_ADMIN_API_KEY` is set (clients must send `X-Admin-Key: <key>` or `Authorization: Bearer <key>`). If the env var is not set, the default dev server does **not** authenticate admin routes — bind to localhost or put a reverse proxy in front for production. `POST /v1/admin/dataset/delete` and `POST /v1/admin/dataset/purge` require `workspace_id` and at least one selector (`file_name`, `dataset_name`, or `prefix`). Purge uses `HardDeleteMemory` when a tiered store is configured; otherwise it falls back to warm-only removal (`purge_backend: "warm_only"` in the JSON response). `POST /v1/admin/dataset/purge` can run in async mode (`async=true`) and be polled via `GET /v1/admin/dataset/purge/task`.

## Dataset bulk import and CLI delete / purge (E2E)

Use [`scripts/e2e/import_dataset.py`](scripts/e2e/import_dataset.py) to push vector-style files into Plasmod via `POST /v1/ingest/events`, or to call `POST /v1/admin/dataset/delete` / `POST /v1/admin/dataset/purge` in a loop over matched files (purge only removes rows that are already soft-deleted unless you pass `--purge-include-active`).

- **Ingest is not transactional:** use `--concurrency 1` with `--checkpoint PATH` for resumable imports after failures, plus `--ingest-retries` / `--retry-backoff` for transient HTTP errors (see script `--help`).
- **Supported suffixes:** `.fvecs`, `.ivecs`, `.ibin`, `.fbin`, `.arrow` (`.arrow` requires `pyarrow` from [`requirements.txt`](requirements.txt)).
- **Markers in ingested text:** each event’s `payload.text` includes `dataset=<file_basename>` and `dataset_name:<--dataset>` so you can delete either by file name, by dataset label, or both together (aligned with the admin delete API above).
- **`.ibin` dtype:** use `--ibin-dtype auto|float32|int32` when auto-detection by filename is wrong for your file.
- **Examples** (set `PLASMOD_BASE_URL` if the server is not `http://127.0.0.1:8080`):

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

Plasmod treats the database as cognitive infrastructure, not only as storage.

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

By default the server listens on `127.0.0.1:8080`. You can override it with `PLASMOD_HTTP_ADDR`.

### Build with HNSW (Knowhere) support

Plasmod supports HNSW vector search via the C++ Knowhere library. This is **optional** — without it, the server runs with a pure-Go fallback.

**Prerequisites:** macOS with Homebrew:
```bash
brew install libomp abseil googletest
```

**Step 1 — Build the C++ library:**
```bash
make cpp-with-knowhere
```

**Step 2 — Build the Go binary (Makefile auto-detects the dylib):**
```bash
make build        # automatically adds -tags retrieval when libandb_retrieval.dylib exists
```

**Step 3 — Deploy the new binary:**
```bash
cp bin/andb plasmod_server          # or wherever your start script points
```

**Verify HNSW is loaded:** after starting the server, check the log for:
```
[bootstrap] data plane: hybrid search enabled (provider=onnx dim=384)
```
and confirm the library is mapped:
```bash
lsof -p $(lsof -t -i:8080) | grep knowhere
# expected: .../cpp/build/vendor/libknowhere.dylib
```

**Common mistake:** Running `go build` or `go run` directly (without `make build`) will **not** include the `retrieval` build tag, so `bridge.go` is skipped and `bridge_stub.go` (pure-Go stub) is used instead. Always use `make build` or `make dev` to build with HNSW support.

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

> Full integration test suite (Docker + MinIO + fixture-driven captures) lives in the `dev` branch under `integration_tests/` and `scripts/e2e/`. See the `dev` branch README for setup instructions.

### Full stack via Docker 

```bash
docker compose up -d
```

### APP_MODE — Visibility Control

To support both QA validation and production rollout from a single codebase, Plasmod uses one environment switch: `APP_MODE`.

#### 1) Mode matrix

| Mode | Primary user | API/UI visibility | Debug endpoints |
|---|---|---|---|
| `APP_MODE=test` | Testers, developers | Transparent diagnostics (request/response metadata, timing, debug payload) | Enabled (for example `/v1/debug/echo`) |
| `APP_MODE=prod` | End users | Sanitized business-only output (debug/raw/internal fields removed) | Disabled (not registered; returns 404) |

#### 2) How testers use the test entry point

Use this mode when validating end-to-end behavior, capturing diagnostics, or reproducing defects.

```bash
# Local dev entry (tester)
export APP_MODE=test
make dev
```

```bash
# Docker entry (tester)
APP_MODE=test docker compose up -d --build
```

Validation checks for testers:

```bash
curl -sS http://127.0.0.1:8080/v1/system/mode
# expected: {"app_mode":"test","debug_enabled":true}

curl -sS http://127.0.0.1:8080/v1/debug/echo \
  -H 'Content-Type: application/json' \
  -d '{"hello":"world"}'
# expected: 200 OK in test mode
```

#### 3) How production users use the production entry point

Use this mode for real user traffic. The server only exposes business-safe fields and blocks debug routes.

```bash
# Local dev entry (production profile)
export APP_MODE=prod
make dev
```

```bash
# Docker entry (production profile)
APP_MODE=prod docker compose up -d --build
```

Validation checks for production profile:

```bash
curl -sS http://127.0.0.1:8080/v1/system/mode
# expected: {"app_mode":"prod","debug_enabled":false}

curl -i -sS http://127.0.0.1:8080/v1/debug/echo \
  -H 'Content-Type: application/json' \
  -d '{"hello":"world"}'
# expected: 404 Not Found in prod mode
```

#### 4) Implementation binding (single codebase, no hardcoded branch copies)

- Mode resolution: `src/internal/access/visibility.go` via `CurrentAppMode()` (default `prod`).
- Visibility middleware: `WrapVisibility(...)`
  - `test`: appends `_debug` metadata on JSON object responses.
  - `prod`: recursively removes debug/internal fields (`_debug`, `debug`, `raw_*`, `chain_traces`, `intermediate`, etc.).
- Server wiring: `src/internal/app/bootstrap.go`
  - `handler := access.WrapVisibility(access.WrapAdminAuth(mux))`
- Runtime probe endpoint: `GET /v1/system/mode`

#### 5) Production safety gate (automation)

Pre-release safety script: `scripts/check_prod_visibility.sh`  
Make target: `make prod-safety-check`

The check verifies:

1. Access-layer tests under `APP_MODE=prod` (sanitization + route gating)
2. Static guard that debug routes remain mode-gated
3. Static scan for known debug leakage symbols in SDK-facing code

```bash
make prod-safety-check
```

If any check fails, the script exits non-zero and should block CI/CD promotion.

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

## Contributing

See [`docs/contributing.md`](docs/contributing.md) for contribution guidelines, module ownership, and interface contracts.
