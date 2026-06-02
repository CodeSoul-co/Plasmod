<div align="center">
  <img src="assets/plasmod.png" alt="Plasmod Logo" width="480"/>
</div>

<div class="column" align="middle">
  <a href="https://github.com/CodeSoul-co/Plasmod/blob/main/LICENSE"><img height="20" src="https://img.shields.io/github/license/CodeSoul-co/Plasmod" alt="license"/></a>
  <a href="https://hub.docker.com/r/oneflybird/plasmod"><img src="https://img.shields.io/docker/pulls/oneflybird/plasmod" alt="docker-pull-count"/></a>
  <a href="https://pypi.org/project/pyplasmod/"><img src="https://img.shields.io/pypi/v/pyplasmod" alt="pypi-version"/></a>
  <a href="https://github.com/CodeSoul-co/Plasmod"><img src="https://img.shields.io/github/stars/CodeSoul-co/Plasmod" alt="github-stars"/></a>
</div>

<div align="center">

[English](README.md) · [中文](README.zh-CN.md)

</div>

# Plasmod

## What is Plasmod?

[Plasmod](https://github.com/CodeSoul-co/Plasmod) is an open-source **agent-native database** designed for AI applications and multi-agent systems. It provides the memory and retrieval infrastructure that AI agents need to maintain context, track state evolution, and retrieve structured evidence across long-running workflows.

Unlike traditional vector databases that focus primarily on embedding storage and similarity search, Plasmod treats **memory**, **state**, **event**, **artifact**, and **relation** as first-class database objects. This means your AI agents can store not just vectors, but complete cognitive context — including what happened (events), what the agent knows (memory), what the agent decided (state), what the agent produced (artifacts), and how these objects relate to each other (edges).

Plasmod is built for scenarios where agents need more than top-k similarity matches. It supports **event-driven state evolution**, **provenance-aware retrieval**, and **structured evidence assembly** — returning query results that include proof traces, source attribution, and graph context. This makes Plasmod ideal for RAG systems, autonomous agents, long-term memory applications, and multi-agent collaboration workflows.

> **Core thesis:** agent memory, state, event, artifact, and relation should be modeled as first-class database objects, and (batch) query results should return structured evidence rather than only top-k text fragments.

Written in Go with a Python SDK (`pyplasmod`), Plasmod can be deployed via Docker for local development or scaled for production workloads. It integrates with LangChain and other AI frameworks, making it easy to add agent-native memory to your existing AI applications.

## Quickstart

### 1. Install the Python SDK

```bash
pip install -U pyplasmod
```

This installs `pyplasmod`, the Python SDK for Plasmod. Import `EasyPlasmod` to create a client:

```python
from pyplasmod import EasyPlasmod
```

### 2. Start Plasmod with Docker

Pull and run the Plasmod server using Docker:

```bash
docker pull oneflybird/plasmod
docker run -d --name plasmod -p 19530:19530 oneflybird/plasmod
```

The server will be available at `http://127.0.0.1:19530`.

### 3. Connect to Plasmod

```python
from pyplasmod import EasyPlasmod

# Connect to local Plasmod server
with EasyPlasmod(base_url="http://127.0.0.1:19530") as client:
    # Check server health
    print(client.health())  # {"status": "ok"}
```

### 4. Ingest Data

Plasmod uses an event-driven ingestion model. You can ingest events that will be materialized into memory objects:

```python
from pyplasmod import EasyPlasmod

with EasyPlasmod(base_url="http://127.0.0.1:19530") as client:
    # Ingest an observation event
    client.ingest_event({
        "event_type": "observation",
        "workspace_id": "my-agent",
        "payload": {
            "text": "The user prefers dark mode and uses Python for development.",
            "source": "user_preferences"
        }
    })
```

Or use the HTTP API directly:

```bash
curl -X POST http://127.0.0.1:19530/v1/ingest/events \
  -H "Content-Type: application/json" \
  -d '{
    "event_type": "observation",
    "workspace_id": "my-agent",
    "payload": {
      "text": "The user prefers dark mode and uses Python for development.",
      "source": "user_preferences"
    }
  }'
```

### 5. Query Data

Retrieve relevant memories with structured evidence:

```python
from pyplasmod import EasyPlasmod

with EasyPlasmod(base_url="http://127.0.0.1:19530") as client:
    # Search for relevant memories
    results = client.search(
        query_text="What are the user's preferences?",
        workspace_id="my-agent",
        top_k=5
    )
    print(results)
```

Or use the HTTP API:

```bash
curl -X POST http://127.0.0.1:19530/v1/query \
  -H "Content-Type: application/json" \
  -d '{
    "query_text": "What are the user preferences?",
    "workspace_id": "my-agent",
    "top_k": 5
  }'
```

Query responses include not just matching content, but also provenance information, proof traces, and related graph edges.

## Why Plasmod?

Plasmod is designed to solve the memory problem for AI agents. Here's why developers choose Plasmod over traditional approaches:

<details>
  <summary><b>Agent-Native Data Model</b></summary>

Most vector databases treat everything as embeddings with metadata. Plasmod provides a purpose-built data model for agent memory:
- **Events** — Immutable records of what happened (observations, actions, decisions)
- **Memory** — Materialized knowledge derived from events
- **State** — Current agent state with version history
- **Artifacts** — Produced outputs (code, documents, plans)
- **Edges** — Typed relationships between objects
</details>

<details>
  <summary><b>Event-Driven State Evolution</b></summary>

Instead of direct overwrites, all state changes in Plasmod flow through an append-only WAL (Write-Ahead Log). This enables:
- Full audit trail and replay capability
- Point-in-time queries ("What did the agent know at time T?")
- Conflict detection and resolution for multi-agent scenarios
</details>

<details>
  <summary><b>Structured Evidence Retrieval</b></summary>

Traditional vector search returns top-k similar chunks. Plasmod returns **evidence packages** that include:
- Matching memory objects with relevance scores
- Provenance information (where did this knowledge come from?)
- Proof traces (how was this conclusion reached?)
- 1-hop graph expansion (what's related to these results?)
</details>

<details>
  <summary><b>Workspace and Session Isolation</b></summary>

Built-in multi-tenancy with `workspace_id` and `session_id` scoping. Each agent or user session can have isolated memory without complex application-level partitioning.
</details>

<details>
  <summary><b>Tiered Storage Architecture</b></summary>

Plasmod implements a hot/warm/cold data plane:
- **Hot tier** — In-memory LRU for frequently accessed data
- **Warm tier** — Segment-based index for recent data
- **Cold tier** — S3-compatible storage for archival
</details>

## Core Concepts

| Concept | Description |
|---------|-------------|
| **Event** | Immutable record of something that happened (observation, action, decision). Events are the source of truth. |
| **Memory** | Materialized knowledge derived from events. Memories have lifecycle states (active → compressed → archived → deleted). |
| **State** | Current state of an agent or entity, with full version history. |
| **Artifact** | Produced output such as code, documents, or plans. |
| **Edge** | Typed relationship between any two objects (e.g., "derived_from", "references", "contradicts"). |
| **Workspace** | Isolation boundary for a tenant, project, or agent. |
| **Session** | Isolation boundary within a workspace for a conversation or task. |
| **Provenance** | Metadata tracking the origin and transformation history of data. |

## Key Features

| Feature | Description |
|---------|-------------|
| **Agent-native object model** | Event, Memory, State, Artifact, Edge as first-class database types |
| **Event-driven architecture** | Append-only WAL with replay and audit capability |
| **Structured evidence retrieval** | Query responses include provenance, proof traces, and graph context |
| **Workspace/session isolation** | Built-in multi-tenancy without application-level partitioning |
| **Tiered data plane** | Hot (memory) → Warm (segment index) → Cold (S3) storage |
| **HTTP API** | RESTful API for ingest, query, admin, and CRUD operations |
| **Python SDK** | `pyplasmod` with sync client, batch helpers, and embedding utilities |
| **LangChain integration** | `PlasmodVectorStore` adapter for LangChain applications |
| **Docker deployment** | Single-command local deployment with `docker run` |
| **Multiple embedding providers** | TF-IDF (default), OpenAI, Cohere, ONNX, and more |

## Use Cases

Plasmod is designed for AI applications that need more than simple vector search:

| Use Case | How Plasmod Helps |
|----------|-------------------|
| **RAG with provenance** | Return not just relevant chunks, but also source attribution and confidence scores |
| **Autonomous agents** | Maintain long-term memory across sessions with event-driven state evolution |
| **Multi-agent systems** | Workspace isolation and conflict detection for collaborative agents |
| **Conversational AI** | Session-scoped memory with automatic summarization and compression |
| **Knowledge management** | Track how knowledge evolves over time with full audit trail |
| **Decision support** | Structured evidence assembly with proof traces for explainable AI |

## Ecosystem and Integration

Plasmod integrates with popular AI development tools:

| Integration | Description |
|-------------|-------------|
| **LangChain** | `PlasmodVectorStore` adapter for seamless integration with LangChain applications |
| **Embedding providers** | Support for OpenAI, Cohere, HuggingFace, ONNX, and local TF-IDF |
| **Docker** | Official image at `oneflybird/plasmod` for easy deployment |
| **Python SDK** | `pyplasmod` available on PyPI with full API coverage |

### LangChain Integration

```bash
pip install "pyplasmod[langchain]"
```

```python
from pyplasmod.langchain import PlasmodVectorStore
from langchain_openai import OpenAIEmbeddings

# Create a Plasmod-backed vector store
vectorstore = PlasmodVectorStore(
    base_url="http://127.0.0.1:19530",
    workspace_id="my-agent",
    embedding=OpenAIEmbeddings()
)

# Use with LangChain as usual
vectorstore.add_texts(["Hello world", "Plasmod is great"])
results = vectorstore.similarity_search("greeting")
```

## Deployment Options

| Option | Description |
|--------|-------------|
| **Docker (recommended)** | `docker run -d -p 19530:19530 oneflybird/plasmod` |
| **Docker Compose** | Full stack with MinIO for cold storage |
| **From source** | Build and run the Go server directly |

### Docker Compose (with MinIO cold storage)

```bash
git clone https://github.com/CodeSoul-co/Plasmod.git
cd Plasmod
docker compose up -d
```

This starts Plasmod with MinIO for S3-compatible cold storage.

## HTTP API Overview

| Group | Endpoints |
|-------|-----------|
| **Health** | `GET /healthz` |
| **Ingest** | `POST /v1/ingest/events` · `POST /v1/ingest/document` |
| **Query** | `POST /v1/query` · `POST /v1/query/batch` |
| **Canonical CRUD** | `/v1/memory` · `/v1/agents` · `/v1/sessions` · `/v1/states` · `/v1/artifacts` · `/v1/edges` |
| **Admin** | `/v1/admin/topology` · `/v1/admin/dataset/delete` · `/v1/admin/dataset/purge` |
| **Traces** | `GET /v1/traces/{object_id}` |

## Documentation

| Resource | Description |
|----------|-------------|
| [Python SDK (pyplasmod)](https://github.com/CodeSoul-co/pyplasmod) | SDK documentation and examples |
| [GitHub Issues](https://github.com/CodeSoul-co/Plasmod/issues) | Bug reports and feature requests |
| [GitHub Discussions](https://github.com/CodeSoul-co/Plasmod/discussions) | Questions and community discussion |

## Contributing

<<<<<<< HEAD
The Plasmod open-source project welcomes contributions. See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines on submitting patches and the development workflow.

### Build Plasmod from Source

Requirements:

- **Go:** >= 1.21
- **Python:** >= 3.9 (for SDK development)
- **Docker:** For running dependencies

Clone and build:

```bash
# Clone the repository
git clone https://github.com/CodeSoul-co/Plasmod.git
cd Plasmod

# Install dependencies
make deps

# Build the server
make build

# Run the server
./bin/plasmod
```

For development with hot reload:

```bash
make dev
```

The server listens on `127.0.0.1:19530` by default. Override with `PLASMOD_HTTP_ADDR` environment variable.

## License

Plasmod is licensed under the [MIT License](LICENSE).

---

<div align="center">

**[GitHub](https://github.com/CodeSoul-co/Plasmod)** · **[Python SDK](https://github.com/CodeSoul-co/pyplasmod)** · **[Docker Hub](https://hub.docker.com/r/oneflybird/plasmod)** · **[Issues](https://github.com/CodeSoul-co/Plasmod/issues)**

</div>
=======
See [`docs/contributing.md`](docs/contributing.md) for contribution guidelines, module ownership, and interface contracts.

## TBD
Please contact us for more details.
>>>>>>> dev
