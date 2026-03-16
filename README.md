# Agent-Native Database for Multi-Agent Systems

ANDB is a v1 research prototype for an agent-native database aimed at multi-agent systems (MAS). The repository combines a segment-oriented retrieval plane, a Manu-inspired control and event backbone, and an agent-native semantic layer for canonical objects, reasoning-oriented retrieval, and structured evidence return.

The core thesis is simple:

**agent memory, state, event, artifact, and relation should be modeled as first-class database objects, and query results should return structured evidence rather than only top-k text fragments.**

## Project Status

This repository is in the framework-skeleton plus runnable-prototype stage.

What exists today:

- a runnable Go server in [`src/cmd/server/main.go`](src/cmd/server/main.go)
- an in-memory event backbone and runtime wiring
- a segment-oriented embedded retrieval plane
- canonical object and query schemas in Go
- ingest and query HTTP endpoints
- Python SDK bootstrap and demo scripts
- architecture, schema, and milestone documentation

What is still intentionally lightweight in v1:

- distributed runtime and persistence
- full policy/governance execution
- deep graph expansion and proof construction
- production-grade indexing and optimization

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

The repository is organized around three perspectives:

- Execution layers: access, coordinator, event backbone, worker, storage/data plane
- Semantic layers: base semantics, policy/governance, adaptation
- Representation layers: canonical records, retrieval projections, reasoning structures

Current code layout:

- [`src/internal/access`](src/internal/access): HTTP gateway and route registration
- [`src/internal/coordinator`](src/internal/coordinator): schema, object, policy, version, and worker coordination
- [`src/internal/eventbackbone`](src/internal/eventbackbone): in-memory WAL, bus, and clock primitives
- [`src/internal/worker`](src/internal/worker): ingest/query runtime wiring
- [`src/internal/dataplane`](src/internal/dataplane): embedded retrieval and segment-planning path
- [`src/internal/materialization`](src/internal/materialization): event-to-projection materialization service
- [`src/internal/evidence`](src/internal/evidence): evidence-oriented response assembly
- [`src/internal/semantic`](src/internal/semantic): object model registry and policy engine stubs
- [`src/internal/schemas`](src/internal/schemas): canonical Go contracts for v1 objects and queries
- [`sdk/python`](sdk/python): Python SDK and bootstrap scripts
- [`cpp`](cpp): C++ retrieval stub for future high-performance execution
- [`src/internal/dataplane/retrievalplane`](src/internal/dataplane/retrievalplane): imported retrieval-plane source subtree adapted under ANDB naming
- [`src/internal/coordinator/controlplane`](src/internal/coordinator/controlplane): imported control-plane source subtree adapted under ANDB naming
- [`src/internal/eventbackbone/streamplane`](src/internal/eventbackbone/streamplane): imported stream/event source subtree adapted under ANDB naming
- [`src/internal/platformpkg`](src/internal/platformpkg): imported shared platform package subtree used for compatibility

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

The current query path supports:

- structured request parsing
- dense retrieval over embedded data-plane segments
- basic scope/time/relation fields in the request contract
- lightweight response assembly

The target v1 path is:

`event ingest -> canonical object materialization -> retrieval projection -> query planning -> hybrid retrieval -> graph expansion -> structured evidence response`

The current Go contract lives in [`src/internal/schemas/query.go`](src/internal/schemas/query.go). The richer intended semantics are documented in the schema docs below.

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
- [Phase-2 Step 1](docs/architecture/phase2-step1.md)
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

The near-term milestone is a coherent v1 prototype that can demonstrate:

- event ingest through the public API
- event-to-object materialization
- retrieval over canonical-object projections
- constrained evidence assembly
- benchmark comparison against simple top-k return

## Long-Term Direction

Later versions may extend ANDB with:

- policy-aware retrieval and visibility enforcement
- stronger version/time semantics
- share contracts and governance objects
- richer graph reasoning and proof replay
- tensor memory operators
- cloud-native distributed orchestration

v1 does not aim to complete that roadmap. It aims to establish the right abstraction and the right main path.
