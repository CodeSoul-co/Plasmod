# 11. Dependencies, Build, Test, and Development Workflow

> Language: [中文](../11-dependencies-build-and-development.md) | English

---

This chapter defines the supported build paths, dependency boundaries, local development workflow, and validation requirements for the Plasmod core repository. It describes the current code rather than an idealized deployment.

---

## 11.1. Dependency Architecture

Plasmod has four dependency layers.

| Layer | Primary dependencies | Responsibility | Required by default |
|---|---|---|---:|
| Go runtime | Go 1.25, Badger, gRPC, Protobuf | API, WAL, canonical state, workers, consistency, evidence | Yes |
| Native retrieval | C++17, CMake, HNSW, optional FAISS/DiskANN, OpenMP | Dense and sparse physical retrieval | No |
| Embedding providers | TF-IDF, hosted providers, ONNX, GGUF, TensorRT | Text-to-vector conversion | Provider-dependent |
| External storage | S3-compatible service or MinIO | Cold-tier object storage | No |

The architectural boundary is strict: native libraries produce physical candidates; the Go runtime owns tenant and agent scope, canonical objects, policy, provenance, lifecycle, consistency, and evidence construction.

### 11.1.1. Dependency inventory

| Dependency | Source of truth | Current role | Integration boundary |
|---|---|---|---|
| Go | `go.mod` (`go 1.25.0`) | Core toolchain | Entire `src/` tree |
| Badger | `github.com/dgraph-io/badger/v4 v4.8.0` | Persistent runtime storage | `src/internal/storage` |
| gRPC | `google.golang.org/grpc v1.72.1` | Optional public RPC transport | `src/internal/api/grpc`, `src/internal/app` |
| Protobuf | `google.golang.org/protobuf v1.36.6` | gRPC wire schema | `src/internal/api/grpc/proto` |
| Native retrieval library | `cpp/CMakeLists.txt` | Stable C ABI around ANN implementations | `cpp/include/plasmod`, `cpp/retrieval` |
| Retrieval Go module | local `replace plasmod/retrievalplane` | CGO and stub bridges | `src/internal/dataplane/retrievalplane` |
| ONNX Runtime | build/runtime installation | Local ONNX embedding | `src/internal/dataplane/embedding` |
| go-llama.cpp | `go.mod` | Optional GGUF embedding | build tag `gguf` |
| S3/MinIO | external service | Cold object store | `src/internal/storage/s3store.go` |
| Python `requests` | `sdk/python/setup.py` | HTTP SDK transport | `sdk/python` |

Exact versions must be taken from `go.mod`, `cpp/CMakeLists.txt`, Dockerfiles, and SDK package metadata. The native vendor tree does not yet provide one complete upstream revision manifest; that is a release-management gap, not evidence that the code is Plasmod-owned.

---

## 11.2. Badger Integration

`storage.BuildRuntimeFromEnv` selects the runtime backend. With no override, the storage factory uses disk mode and `.andb_data`; `PLASMOD_STORAGE=memory` explicitly selects ephemeral in-process stores.

### 11.2.1. Stored records

Badger-backed implementations persist:

- canonical Agent, Session, Event, Memory, AgentState, Artifact, and User records;
- graph Edge and ObjectVersion records;
- PolicyRecord and ShareContract records;
- segment and index metadata;
- configuration snapshots and selected algorithm or audit records.

File-backed WAL and derivation logs use files under the data directory, but they are not Badger records.

### 11.2.2. Transaction boundary

`RuntimeStorage.ApplyCanonicalProjection` can commit object, edge, and version mutations atomically when those three stores use the same Badger backend. The factory rejects a configuration that splits these stores across incompatible backends.

The following operations are outside that transaction:

- `FileWAL.Append`;
- native index mutation;
- S3 cold-tier writes;
- evidence-cache updates;
- asynchronous worker maintenance.

The runtime and consistency controller coordinate these phases. A successful Badger transaction does not by itself prove that the retrieval projection or cold tier has been updated.

### 11.2.3. Operational rules

- Allow only one compatible writer process per Badger data directory.
- Do not delete `LOCK` or edit `.sst` and value-log files to bypass a lock error.
- Coordinate writes before filesystem-level backup, or use a supported snapshot procedure.
- Monitor free space and Badger value-log growth.
- Back up and test migration before changing key prefixes or serialized schemas.
- Treat `PLASMOD_BADGER_INMEMORY=true` as a test or constrained-environment option, not durable storage.

---

## 11.3. Build and Link Models

### 11.3.1. Pure-Go build

```bash
go build -o bin/plasmod ./src/cmd/server
```

Without the `retrieval` build tag, `retrievalplane/bridge_stub.go` is compiled. Canonical, WAL, lexical, and other Go-level paths can run, but native ANN availability must not be reported.

### 11.3.2. Native retrieval build

```bash
make cpp
make build
```

`make cpp` configures and builds `libplasmod_retrieval` under `cpp/build`. `make build` detects the `.dylib` or `.so`, adds `-tags retrieval`, and supplies the library search path and rpath through `CGO_LDFLAGS`.

The native path includes these ownership layers:

| Layer | Owns |
|---|---|
| `cpp/retrieval` | index creation, build, load, search, dense/sparse primitives, batch plugin |
| `retrievalplane` CGO bridge | handle lifetime, slice conversion, error conversion |
| Go DataPlane | segment metadata, object-ID mapping, tier routing, fusion, hydration handoff |

### 11.3.3. Runtime linking

The dynamic linker must resolve `libplasmod_retrieval` and every dependency linked by that library. Inspect the final binary in the target environment:

```bash
# macOS
otool -L ./bin/plasmod

# Linux
ldd ./bin/plasmod
```

A successful compile on a developer machine does not prove that a Docker image or deployment host has a valid rpath and compatible libraries. Run a startup and query smoke test from the final artifact.

### 11.3.4. Optional accelerator builds

| Target | Build command | Platform boundary |
|---|---|---|
| FAISS-enabled native library | `make cpp` | Depends on CMake-discovered native dependencies |
| GPU retrieval | `make cpp-gpu` | Linux/CUDA-oriented; not the default macOS path |
| TensorRT embedder | `make tensorrt` | Requires CUDA and TensorRT headers/libraries |
| GGUF embedder | `make gguf` | Requires the configured go-llama.cpp build |

Build flags with the historical `ANDB_` prefix remain active in CMake and selected compatibility paths. Documentation must preserve their actual names until the code deprecates them.

---

## 11.4. Native Retrieval Stack

### 11.4.1. C ABI library

`cpp/CMakeLists.txt` builds `libplasmod_retrieval`. The principal implementation files are:

| File | Responsibility |
|---|---|
| `cpp/retrieval/retrieval.cpp` | C ABI entry points and retriever lifecycle |
| `cpp/retrieval/segment_index.cpp` | segment index operations |
| `cpp/retrieval/dense.cpp` | dense retrieval primitives |
| `cpp/retrieval/sparse.cpp` | sparse retrieval primitives |
| `cpp/retrieval/batch_optimizer.cpp` | batch reordering and parallel dispatch |

Public C declarations live under `cpp/include/plasmod`. Go code must call the C ABI rather than consume C++ implementation types.

### 11.4.2. Index availability

The Go API recognizes HNSW, IVF_FLAT, IVF_PQ, IVF_SQ8, and DISKANN variants, but runtime availability is controlled by compile-time features. Supplying an `index_type` string cannot enable a backend that was not compiled.

### 11.4.3. Batch search

The batch plugin may normalize, reorder, and dispatch query rows in parallel. It must restore output row order before returning. Logical `row_lineage`, source fan-out, and agent ownership remain Go schema and service responsibilities.

### 11.4.4. Error and resource rules

- Convert create, build, load, search, and handle errors into non-empty Go errors.
- Do not convert an unavailable native backend into an empty successful result.
- Release every native handle through its explicit destroy path.
- Do not retain Go-managed memory after a CGO call returns.
- Test empty input, dimension mismatch, repeated close, and concurrent search for every new C ABI method.

---

## 11.5. Embedding Providers

Bootstrap selects an `EmbeddingGenerator` through `PLASMOD_EMBEDDER`. Provider implementations live in `src/internal/dataplane/embedding`.

| Provider family | Typical configuration | External dependency |
|---|---|---|
| TF-IDF | `PLASMOD_EMBEDDER=tfidf` | None |
| Hosted HTTP provider | base URL, model, API key, dimension, timeout, batch size | Network service |
| ONNX | model path, vocabulary path, device, dimension | ONNX Runtime |
| GGUF | model path, device/build options | go-llama.cpp |
| TensorRT | engine path, vocabulary, CUDA device, dimension | TensorRT/CUDA |
| Precomputed vector | vector supplied by Event or vector-ingest request | None during request |

### 11.5.1. Compatibility tuple

Vectors can share an index only when their compatibility tuple agrees:

```text
embedding family + model ID + dimension + normalization + distance metric
```

Matching dimensions alone are insufficient. When any component changes, create a new segment family or run the controlled reindex path. `PLASMOD_EMBEDDING_REINDEX=1` is an explicit migration control, not a routine startup setting.

### 11.5.2. Failure semantics

An embedding error must not silently produce a zero vector. A caller or deployment may deliberately choose lexical-only behavior, skip vector indexing, or fail the required projection phase. The selected behavior must be visible in status and logs.

---

## 11.6. S3 and MinIO Integration

S3-compatible storage implements the cold-object boundary for archived canonical content and associated material.

### 11.6.1. Configuration

Required values include endpoint, bucket, credentials, region, TLS mode, and key prefix. Docker Compose exposes MinIO API port `9000` and console port `9001`; Plasmod connects to the API port.

### 11.6.2. Consistency boundary

Cold writes are not part of the Badger canonical transaction. An archive operation must:

1. identify the canonical object and target cold key;
2. write and verify the cold representation;
3. update canonical tier metadata;
4. remove or retain warm data according to policy;
5. record failure without claiming successful archival.

Purge must cover both object keys and related cold index keys.

### 11.6.3. Security

Use dedicated credentials, least-privilege bucket policies, TLS, encryption, and lifecycle rules. The default local MinIO credentials are development-only.

---

## 11.7. License and Attribution Requirements

Review these sources before distributing source, binaries, or images:

- the repository root license;
- `src/internal/platformpkg/UPSTREAM_LICENSE`;
- licenses under `cpp/vendor`;
- Go module licenses;
- distribution requirements for FAISS, DiskANN, HNSW, ONNX Runtime, Badger, and other linked components.

Release preparation must generate a dependency inventory, preserve notices, identify modified upstream code, and review static and dynamic linking obligations. This chapter records engineering checks and is not legal advice.

---

## 11.8. Build System Reference

### 11.8.1. Primary Make targets

| Target | Action | Prerequisites |
|---|---|---|
| `make setup` | Download Go modules, configure environment, install Node SDK dependencies | Go, shell, npm |
| `make dev` | Start the development server through `scripts/dev_up.sh` | Local toolchain |
| `make dev-with-s3` | Start local MinIO, then the development server | MinIO and `mc` |
| `make cpp` | Build CPU native retrieval | CMake and native dependencies |
| `make build` | Build `bin/plasmod`; enable native tag when library exists | Go; optional native library |
| `make test` | Run Go and Python tests | Go and Python test dependencies |
| `make integration-test` | Test a running service through Go and Python clients | Running server |
| `make integration-test-s3` | Add the S3 data-flow path | Running server and S3/MinIO |
| `make proto` | Regenerate gRPC Go stubs | `protoc`, Go plugins |
| `make prod-safety-check` | Verify production response sanitization | Shell and repository scripts |
| `make docker-up` | Start the default Compose stack | Docker |

### 11.8.2. Build artifacts

| Artifact | Producer | Consumer |
|---|---|---|
| `bin/plasmod` | `make build` | Local or packaged server |
| `cpp/build/libplasmod_retrieval.*` | `make cpp` | CGO retrieval build |
| generated gRPC Go code | `make proto` | gRPC server and clients |
| Python editable package | `make sdk-python` | Local SDK development |

Do not commit transient CMake output, local data directories, credentials, or platform-specific runtime libraries unless repository policy explicitly requires them.

---

## 11.9. Local Development Workflow

### 11.9.1. Initial setup

```bash
go mod download
make cpp        # optional but required for native ANN
make build
```

For a fast pure-Go iteration:

```bash
PLASMOD_STORAGE=memory go run ./src/cmd/server
```

For persistent local state:

```bash
PLASMOD_STORAGE=disk \
PLASMOD_DATA_DIR=.andb_data-dev \
go run ./src/cmd/server
```

Use a unique data directory for each concurrent service or test process.

### 11.9.2. Debug startup

```bash
APP_MODE=test \
PLASMOD_STORAGE=disk \
PLASMOD_DATA_DIR=.andb_data-debug \
PLASMOD_EMBEDDER=tfidf \
PLASMOD_GRPC_ENABLED=0 \
go run ./src/cmd/server
```

`APP_MODE=test` enables debug-only response fields and routes. Do not expose this mode on an untrusted network.

### 11.9.3. Debugging order

Trace failures in causal order:

1. process health and listener addresses;
2. effective storage, embedding, and consistency configuration;
3. WAL append and assigned LSN;
4. canonical object, edge, and version mutation;
5. retrieval projection;
6. query planning, filtering, hydration, and evidence;
7. response visibility and sanitization.

This order separates “not accepted,” “accepted but not materialized,” “materialized but not indexed,” and “retrieved but filtered.”

---

## 11.10. Coding and Architecture Conventions

### 11.10.1. Go

- Run `gofmt` on changed Go files.
- Pass `context.Context` through blocking or cancelable operations.
- Wrap errors with operation and identity context.
- Avoid package-level mutable state unless it is intentionally process-global.
- Keep interfaces in the owning package and constructors near implementations.
- Preserve typed schema fields across HTTP, runtime, worker, and storage boundaries.

### 11.10.2. C++ and CGO

- Keep the exported C ABI minimal and stable.
- Define ownership for every pointer and handle.
- Validate vector dimensions and buffer lengths before dereference.
- Translate exceptions to explicit status/error results at the C boundary.
- Test resource release under both success and failure paths.

### 11.10.3. Cross-module rules

- Business writes that require replay must enter through Event and WAL.
- Direct canonical CRUD is a management/compatibility path, not a replacement for ingest semantics.
- Canonical state is authoritative; retrieval records are rebuildable projections.
- Projection, evidence, and cold-tier maintenance must expose partial completion instead of fabricating atomicity.
- Experiment-specific behavior does not belong in this repository's runtime packages or core documentation.

---

## 11.11. Common Change Checklists

### 11.11.1. Add an Event type

Update the schema constant, normalization, payload validation, materializer mapping, deterministic IDs, edge/version derivation, query filters, trace output, replay tests, API documentation, and SDK types.

### 11.11.2. Add a canonical object

Update the canonical schema, storage interface, memory and Badger implementations, projection transaction, coordinator/handler, versioning, evidence hydration, deletion/purge, backup behavior, tests, and SDK exposure.

### 11.11.3. Add a query operator

Define a typed request field, planner semantics, candidate-stage placement, canonical enforcement, batch parity, proof annotation, SDK support, and scope-leak tests.

### 11.11.4. Add configuration

Define one canonical environment variable, parse and validate it at bootstrap, include a redacted effective value where appropriate, test defaults and invalid values, update Compose and documentation, and define any legacy alias lifetime.

### 11.11.5. Add a background worker

Define typed input/output, queue ownership, idempotency key, cancellation, retry/backoff, backpressure, state markers, shutdown drain, metrics, and recovery behavior before wiring the worker into `BuildServer` or `Runtime`.

---

## 11.12. Test Strategy

### 11.12.1. Core test command

```bash
go test ./src/...
```

### 11.12.2. Repository test command

```bash
make test
```

This target runs Go tests and Python tests. Native integration requires a built retrieval library and the appropriate build tag; the default command alone does not prove native ANN behavior.

### 11.12.3. Test layers

| Layer | Required evidence |
|---|---|
| Schema | normalization, validation, backward-compatible decoding |
| Storage | contract parity, Badger reopen, atomic canonical projection |
| WAL | append, scan, corruption handling, replay order |
| Materialization | deterministic IDs, idempotent replay, edge/version output |
| Consistency | strict/bounded/eventual behavior, timeout, retry, checkpoint |
| API | route, error, authentication, visibility, cancellation |
| Retrieval | lexical, native availability, batch lineage, cold inclusion |
| Evidence | hydration, graph expansion, version/provenance, proof steps |
| Shutdown | queue drain, WAL close, storage close, native handle release |

Any key, WAL, or canonical schema change must include a persistent compatibility fixture or an explicit migration test. Testing only a newly created empty database is insufficient.

---

## 11.13. Logging, Metrics, and Correlation

Do not log API keys, S3 secrets, full sensitive payloads, or embedding vectors. Correlate cross-module activity with Event ID, object ID, LSN, session ID, and request ID.

`GET /v1/admin/metrics` exposes runtime metrics through the admin surface. Operationally relevant groups include queue/backpressure, consistency progress and failures, materialization/projection, query tier use, purge tasks, and provider health.

Production response sanitization is separate from server-side operational logging. `APP_MODE=prod` removes debug, raw, log, and chain-trace fields from API JSON; validate that behavior with `make prod-safety-check`.

---

## 11.14. Development Troubleshooting

| Symptom | Checks | Corrective action |
|---|---|---|
| Port already in use | `lsof -nP -iTCP:8080 -sTCP:LISTEN`, split ports, Docker containers | Stop the owner or deliberately select another address |
| Badger lock | Processes sharing `PLASMOD_DATA_DIR` | Stop the existing writer; do not delete the lock |
| Build uses stub | Library filename, `cpp/build`, `-tags retrieval`, `CGO_LDFLAGS` | Rebuild native library, then rebuild Go binary |
| Native library fails at startup | `otool -L`/`ldd`, rpath, architecture | Install compatible dependencies or fix packaging |
| Query has canonical result but no native hit | vector projection, segment metadata, embedding tuple, `query_status` | Repair or reindex the projection |
| Strict ingest times out | queue, worker, embedder, projection error, tracker/checkpoint | Determine the failed phase before retrying |
| MinIO connection fails | API vs console port, TLS, credentials, bucket, container DNS | Correct S3 endpoint/configuration and retry verification |

---

## 11.15. Contribution and Review Gate

Before committing a core change:

1. Inspect the current branch and unrelated worktree changes.
2. Keep the change within the owning package and documented contract.
3. Update schemas, interfaces, implementations, wiring, tests, and docs together.
4. Run focused tests, then `go test ./src/...`.
5. Run `make build` when build tags, CGO, bootstrap, or packaging are affected.
6. Run `make prod-safety-check` for API visibility or response changes.
7. Record any partial implementation or unsupported claim in Chapter 14.

Vendored or upstream-derived code additionally requires provenance, license, revision, local-modification, and reproducible-build review.
