# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

CogDB (ANDB) is an agent-native database for multi-agent systems. It combines a tiered retrieval plane, append-only WAL, canonical object materialization, pre-computed evidence fragments, 1-hop graph expansion, and structured evidence assembly — all as a single Go server. The Go module is `andb` at the repo root.

## Common Commands

```bash
# Development server (http://127.0.0.1:8080)
make dev

# Build binary
make build

# Run unit tests (Go + pytest)
make test

# Run integration tests (requires server running: make dev)
make integration-test

# Run integration tests with S3/MinIO
ANDB_RUN_S3_TESTS=true S3_ENDPOINT=127.0.0.1:9000 \
  S3_ACCESS_KEY=minioadmin S3_SECRET_KEY=minioadmin \
  S3_BUCKET=andb-integration make integration-test

# Run Go unit tests only
go test ./src/internal/... -count=1 -timeout 30s

# Run a single test
go test ./src/internal/schemas -v -run TestCanonicalMemory

# Format Go code
make fmt

# Build C++ retrieval module
make cpp

# Install Python SDK
pip install -e ./sdk/python
```

## Architecture

### Three Execution Layers

```
HTTP API (access)
    └─ Runtime (worker)
          ├─ WAL + Bus  (eventbackbone)
          ├─ MaterializeEvent → Memory / ObjectVersion / Edges  (materialization)
          ├─ PreComputeService → EvidenceFragment cache  (materialization)
          ├─ HotCache → TieredDataPlane (hot→warm→cold)  (dataplane)
          └─ Assembler.Build → BulkEdges + EvidenceCache  (evidence)
```

### The Core End-to-End Path

```
event ingest → canonical object materialization → retrieval projection →
tiered search (hot→warm→cold) → 1-hop graph expansion →
pre-computed evidence merge → structured QueryResponse
```

**Ingest path:** `API → WAL.Append → MaterializeEvent → PutMemory + PutVersion + PutEdge → PreCompute → HotCache → TieredDataPlane.Ingest`

**Query path:** `API → TieredDataPlane.Search → Assembler.Build → EvidenceCache.GetMany + BulkEdges(1-hop) → QueryResponse`

### 8-Layer Worker Model

The worker layer is organized as:
1. **Data Plane** — `IndexBuildWorker`, `SegmentWorker`, `VectorRetrievalExecutor`
2. **Event/Log Layer** — `IngestWorker`, `LogDispatchWorker`, `TSO Worker`
3. **Object Layer** — `ObjectMaterializationWorker`, `StateMaterializationWorker`, `ToolTraceWorker`
4. **Cognitive Layer** — `MemoryExtractionWorker` (`worker/cognitive/baseline/extraction.go`),
   `MemoryConsolidationWorker` (`worker/cognitive/baseline/consolidation.go`),
   `SummarizationWorker` (`worker/cognitive/baseline/summarization.go`),
   `ReflectionPolicyWorker` (`worker/cognitive/baseline/reflection.go`)
5. **Structure Layer** — `GraphRelationWorker`, `EmbeddingBuilderWorker`, `TensorProjectionWorker`
6. **Policy Layer** — `PolicyWorker`, `ConflictMergeWorker`, `AccessControlWorker`
7. **Query/Reasoning Layer** — `QueryWorker`, `ProofTraceWorker`, `SubgraphExecutor`, `MicroBatchScheduler`
8. **Coordination Layer** — `CommunicationWorker`, `SharedMemorySyncWorker`, `ExecutionOrchestrator`

Workers implement interfaces in `src/internal/worker/nodes/contracts.go` and are managed by `ExecutionOrchestrator` (`src/internal/worker/orchestrator.go`) with priority-aware queuing (PriorityUrgent/High/Normal/Low).

### Pending member-D Work

- **Dead-letter channel** — `src/internal/worker/subscriber.go`: `safeDispatch` replaced with structured `deadLetter chan DeadLetterEntry`; expose `DeadLetterChannel()` and `DLQStats()` for downstream consumers.
- **MicroBatch persistent drain** — `src/internal/worker/coordination/microbatch.go`: flushed payloads currently cleared in-memory; need a persistent drain target (coordinator or DLQ).

### Three-Tier Retrieval Plane

| Tier | Component | Storage | Latency target |
|------|-----------|---------|----------------|
| Hot | `HotSegmentIndex` | In-memory LRU | sub-millisecond |
| Warm | `SegmentDataPlane` | Full in-memory | normal |
| Cold | `S3ColdStore` or `InMemoryColdStore` | S3 or in-mem simulation | higher |

Cold tier auto-selects: if `S3_ENDPOINT` + `S3_ACCESS_KEY` + `S3_SECRET_KEY` + `S3_BUCKET` are set, use `S3ColdStore`; otherwise use `InMemoryColdStore`.

### Canonical Objects

The v1 object set: `Agent`, `Session`, `Event`, `Memory`, `State`, `Artifact`, `Edge`, `ObjectVersion`. Authoritative Go definitions are in `src/internal/schemas/canonical.go`. These are shared contracts — changes here affect ingest, retrieval, SDKs, and tests.

### QueryResponse Structure

Every query returns: `Objects` (ranked IDs), `Edges` (1-hop neighbors), `Provenance` (pipeline stages), `Versions`, `AppliedFilters`, `ProofTrace`.

## Key Entry Points

- **Server entry:** `src/cmd/server/main.go` → `app.BuildServer()`
- **HTTP gateway:** `src/internal/access/gateway.go` — 14 routes
- **Runtime orchestration:** `src/internal/worker/runtime.go` — `SubmitIngest` and `ExecuteQuery`
- **Materialization:** `src/internal/materialization/service.go` — `MaterializeEvent`
- **Evidence assembly:** `src/internal/evidence/assembler.go` — `Assembler.Build`
- **Tiered data plane:** `src/internal/dataplane/tiered_adapter.go`
- **WAL:** `src/internal/eventbackbone/wal.go`

## Shared Contracts (Protected Files)

Changes to these require coordination across teams:
- `src/internal/schemas/canonical.go` — canonical object shapes
- `src/internal/schemas/query.go` — query request/response shapes
- `docs/schema/canonical-objects.md`
- `docs/schema/query-schema.md`
- `docs/architecture/main-flow.md`

## Branching

New branches must be prefixed with `codex/`. Recommended pattern: `codex/feature-<name>` or `codex/fix-<name>`.

## Test Requirements

Every package under `src/internal/` must have at least one `*_test.go` file. Use `package <pkg>` (white-box) by default, `package <pkg>_test` only when the package exports only interfaces.

## C++ Retrieval Module

The `cpp/` directory contains a C++ retrieval core with Knowhere HNSW/SPARSE support, built via CMake. The Go→C++ bridge is in `src/internal/dataplane/retrievalplane/fixme_external_deps.go` using CGO. To build:
```bash
cd cpp && mkdir -p build && cd build && cmake .. -DANDB_WITH_KNOWHERE=ON -DANDB_WITH_PYBIND=ON && make -j$(nproc)
```
Brute-force fallback available with `-DANDB_WITH_KNOWHERE=OFF`.

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `ANDB_HTTP_ADDR` | `127.0.0.1:8080` | Server listen address |
| `ANDB_BASE_URL` | `http://127.0.0.1:8080` | Test/client base URL |
| `ANDB_RUN_S3_TESTS` | (unset) | Set `true` to enable S3 tests |
| `S3_ENDPOINT` | — | MinIO/S3 host:port |
| `S3_ACCESS_KEY` | — | Access key |
| `S3_SECRET_KEY` | — | Secret key |
| `S3_BUCKET` | — | Bucket name |
| `S3_SECURE` | `false` | Use TLS |
