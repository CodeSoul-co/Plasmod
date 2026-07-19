# 12. Deployment, Operations, Recovery, and Troubleshooting

> Language: [中文](../12-deployment-operations-and-troubleshooting.md) | English

---

This chapter describes supported single-runtime deployment modes, storage operations, backup and recovery, observability, and incident handling. It does not claim that the default composition root provides a multi-node high-availability database.

---

## 12.1. Deployment Model

### 12.1.1. Unified HTTP mode

Unified mode registers management and application routes on one HTTP listener.

| Setting | Default |
|---|---|
| `PLASMOD_LISTEN_MODE` | `unified` |
| `PLASMOD_HTTP_ADDR` | `127.0.0.1:8080` |
| gRPC | `PLASMOD_GRPC_ADDR`, default `:19531` when enabled |

This mode is convenient for local development. It does not isolate management routes from application traffic.

### 12.1.2. Split HTTP mode

Split mode exposes management and application routes on separate listeners.

| Surface | Setting | Default port |
|---|---|---:|
| Health and admin | `PLASMOD_MGMT_ADDR` | `9091` |
| REST and internal HTTP transport | `PLASMOD_API_ADDR` | `19530` |
| Public gRPC | `PLASMOD_GRPC_ADDR` | `19531` |

Network policy should restrict management and internal routes even though they are separate listeners.

### 12.1.3. Supported topology boundary

The default `app.BuildServer` wiring creates one active runtime process with an in-process event bus and local runtime storage. The repository contains imported distributed-control and streaming packages, but the default server does not turn those packages into a supported shared-Badger cluster.

Do not run multiple Plasmod processes against the same Badger directory. Network filesystems and shared directory locks are not a substitute for a distributed storage design.

---

## 12.2. Docker Deployment

### 12.2.1. Split stack

The default Compose file starts Plasmod in split mode and MinIO as the cold-tier service.

```bash
docker compose up -d --build
docker compose ps
docker compose logs -f plasmod
curl -fsS http://127.0.0.1:9091/healthz
```

Published ports are `9091`, `19530`, `19531`, `9000`, and `9001`.

### 12.2.2. Unified stack

```bash
docker compose -f docker-compose.unified.yml up -d --build
curl -fsS http://127.0.0.1:8080/healthz
```

The unified Compose file exposes one HTTP port for management and application APIs plus the gRPC port.

### 12.2.3. Mandatory production overrides

The checked-in Compose files are development defaults. A production deployment must, at minimum, override:

- `PLASMOD_ADMIN_API_KEY`;
- MinIO/S3 credentials and bucket policy;
- persistent data volumes and backup policy;
- CPU, memory, and disk limits;
- restart and shutdown policy;
- `APP_MODE=prod`;
- network exposure and TLS termination;
- image tags or digests instead of an unpinned `latest` dependency.

### 12.2.4. Image contents

The Dockerfile uses a multi-stage build. The builder compiles the Go server and native retrieval library; the runtime image includes the server, retrieval shared libraries, ONNX Runtime, OpenMP, OpenBLAS, and standard C++ runtime dependencies.

Verify shared-library resolution from the built image. A local host build does not validate the container runtime.

---

## 12.3. Native Process Deployment

### 12.3.1. Build the release artifact

```bash
make cpp
make build
```

Package `bin/plasmod`, required shared libraries, configuration documentation, and license notices as one versioned artifact. Inspect dependencies with `otool -L` on macOS or `ldd` on Linux.

### 12.3.2. Service account

Run under a non-root account with only these permissions:

- read/write access to the configured data directory;
- atomic file creation and rename for WAL and checkpoints;
- access to required embedding and S3 endpoints;
- permission to bind configured non-privileged ports;
- permission to emit logs to the selected collector.

### 12.3.3. Process manager

Use `systemd`, `launchd`, Kubernetes, or an equivalent supervisor to manage environment variables, restart policy, signals, and log collection. The supervisor termination grace period must exceed the Plasmod shutdown timeout so the queue, checkpoint, WAL, storage, and native handles can close in order.

### 12.3.4. Readiness

`GET /healthz` primarily proves that the HTTP process is responsive. Before accepting traffic, also verify:

1. `/v1/admin/storage` reports the intended backend;
2. `/v1/admin/config/effective` reports the intended redacted configuration;
3. the configured memory algorithm and embedding provider are healthy;
4. one controlled write reaches its required visibility stage;
5. one scoped query returns the expected canonical object.

---

## 12.4. Storage Configuration

### 12.4.1. Persistent disk mode

```bash
PLASMOD_STORAGE=disk
PLASMOD_DATA_DIR=/var/lib/plasmod
```

The directory contains Badger data and runtime files such as `wal.log`, `consistency_checkpoint.json`, and `derivation.log`. Use a local persistent filesystem with reliable fsync behavior and adequate free space.

### 12.4.2. Ephemeral memory mode

```bash
PLASMOD_STORAGE=memory
```

Memory mode is intended for tests and short-lived development. Canonical records and WAL state are lost when the process exits.

### 12.4.3. Per-store overrides

The storage factory accepts explicit store settings for segments, indexes, objects, edges, versions, policies, and contracts. Objects, edges, and versions must resolve to the same backend so `ApplyCanonicalProjection` can preserve its atomicity guarantee.

### 12.4.4. Capacity planning

Plan capacity independently for:

- canonical payload and Badger value logs;
- WAL retention and derivation logs;
- native segment and index files;
- in-memory hot cache;
- cold-tier S3 objects and indexes;
- temporary files used during reindex, replay, and backup.

Monitor both absolute free space and growth rate. A full disk can block Badger and WAL progress at the same time.

---

## 12.5. MinIO and S3 Operations

### 12.5.1. Start local MinIO

```bash
docker compose up -d minio minio-init
docker compose logs -f minio
```

The S3 API uses port `9000`; the browser console uses `9001`. Create a dedicated bucket and least-privilege access key for Plasmod.

### 12.5.2. Configuration

The active integration uses `S3_ENDPOINT`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`, `S3_BUCKET`, `S3_SECURE`, `S3_REGION`, and `S3_PREFIX`. In Compose, the endpoint is the service DNS name (`minio:9000`), not `127.0.0.1` from inside the Plasmod container.

### 12.5.3. Verification procedure

1. Export or archive one controlled object.
2. Verify the expected bucket prefix and key with an S3 client.
3. Query with cold inclusion enabled.
4. Validate object identity, edges, versions, and provenance after rehydration.
5. Remove warm data only after cold verification succeeds.

### 12.5.4. Production controls

Enable TLS, server-side encryption, versioning where appropriate, access logging, lifecycle policy, secret rotation, and least-privilege bucket policy. Cold-tier availability is separate from canonical durability; document the behavior of explicit cold queries during an outage.

---

## 12.6. Backup and Restore

### 12.6.1. Backup scope

Capture a consistent set of:

| Data | Why it is required |
|---|---|
| Badger runtime storage | Authoritative canonical objects and metadata |
| `wal.log` | Replayable accepted events |
| consistency checkpoint | Last persisted processing progress |
| `derivation.log` | Derivation/audit history used by trace paths |
| native segment/index files | Faster recovery; otherwise rebuildable |
| S3 bucket/prefix metadata | Cold objects and archive indexes |
| effective configuration and binary version | Correct interpretation during restore |

Do not treat a native index copy as a substitute for canonical state and WAL.

### 12.6.2. Backup procedure

1. Identify the exact binary commit or release tag.
2. Stop new writes or establish a supported snapshot boundary.
3. Wait for the intended visibility/checkpoint stage.
4. Capture Badger, WAL, checkpoint, derivation log, and required native files.
5. Record the S3 bucket, prefix, and version state.
6. Generate and store checksums.
7. Resume writes only after backup completion is confirmed.

### 12.6.3. Restore procedure

1. Restore into an isolated directory and network environment.
2. Use a binary compatible with the stored schema and keys.
3. Confirm Badger opens and determine its latest canonical state.
4. Inspect WAL and checkpoint before replay.
5. Replay only the required range using an idempotent path.
6. Rebuild missing or incompatible retrieval projections.
7. Validate canonical objects, edges, versions, trace, latest-state queries, and cold reads.
8. Switch traffic only after verification succeeds.

Direct CRUD mutations that never produced an Event cannot be reconstructed from WAL. They require canonical backup.

---

## 12.7. Replay and Recovery

### 12.7.1. Failure classes

| Failure | Primary recovery source | Required validation |
|---|---|---|
| Process interruption | Badger, checkpoint, WAL | strict write and latest-state query |
| Incomplete canonical projection | WAL Event range | object/edge/version identity |
| Lost retrieval projection | canonical records and compatible embeddings | scoped retrieval and hydration |
| Cold store unavailable | Hot/Warm canonical state | explicit cold query behavior |
| Native index incompatible | canonical records plus reindex | embedding tuple and candidate IDs |
| WAL corruption | last verified backup and LSN | stop automated replay; establish loss boundary |

### 12.7.2. Recovery order

1. Block new writes.
2. Preserve the failed data directory and logs.
3. Inspect filesystem capacity, Badger, WAL, and checkpoint.
4. Restore external dependencies.
5. Open storage with a compatible binary.
6. Replay the required Event range.
7. Rebuild retrieval projections where necessary.
8. Verify a strict write, latest State, Trace, and cold query.
9. Reintroduce traffic gradually.

Do not delete the data directory or rebuild every index before preserving incident evidence.

---

## 12.8. Observability

### 12.8.1. Health and administrative state

| Endpoint | Purpose |
|---|---|
| `GET /healthz` | Process-level health |
| `GET /v1/admin/storage` | Resolved storage backend |
| `GET /v1/admin/config/effective` | Redacted effective configuration |
| `GET /v1/admin/metrics` | Point-in-time runtime metrics |
| `GET /v1/admin/memory/providers/health` | Memory algorithm provider health |

Admin routes require the configured key only when `PLASMOD_ADMIN_API_KEY` is set. The server logs a warning when they are left unprotected.

### 12.8.2. Metrics to collect

- accepted and failed ingest operations;
- write latency and query latency;
- queue depth, backpressure, retry, and projection failure;
- latest accepted, projected, and visible progress;
- query status, tier use, candidate counts, and retrieval errors;
- Badger, filesystem, and S3 errors;
- reindex, replay, delete, and purge task status;
- Go runtime process statistics and provider health.

### 12.8.3. Log correlation

Collect stdout/stderr in a parseable format and preserve Event ID, object ID, LSN, session ID, workspace/tenant scope, and request ID. Redact secrets, private payloads, and raw vectors.

### 12.8.4. Alerts

Alert on sustained queue saturation, stalled visibility progress, repeated projection failures, WAL or Badger errors, low disk space, S3 authentication failure, panic/restart loops, and an unset admin key in a non-development deployment.

---

## 12.9. Security Hardening

### 12.9.1. Required controls

1. Set `APP_MODE=prod`.
2. Set a strong `PLASMOD_ADMIN_API_KEY`.
3. Terminate TLS at a trusted proxy or service mesh.
4. Authenticate application traffic and bind tenant/workspace identity outside free-form payloads.
5. Restrict management, internal, and transport routes to trusted networks.
6. Enforce request-size, rate, concurrency, and timeout limits.
7. Run as non-root with least-privilege filesystem and network access.
8. Use least-privilege S3 credentials and rotate secrets.
9. Redact logs and production API responses.
10. Test restoration and scan dependencies regularly.

### 12.9.2. Network segmentation

- Management port: operators and control-plane services only.
- Application port: authenticated application gateways only.
- gRPC/internal transport: trusted nodes only.
- MinIO console: operator network only.
- Badger directory: local service account only; never expose it as a file share.

### 12.9.3. Known security boundary

The built-in admin key is a shared-secret gate, not a complete IAM system. It does not protect every internal route or provide tenant authorization. Deployment infrastructure must supply those controls.

---

## 12.10. Operations Runbook

### 12.10.1. Start

1. Start S3/MinIO if enabled.
2. Verify data-directory ownership, free space, and secrets.
3. Start Plasmod.
4. Check health, storage, effective configuration, and provider health.
5. Execute a controlled write, query, and trace lookup.
6. Admit production traffic.

### 12.10.2. Stop

1. Remove the instance from traffic.
2. Stop admitting new writes.
3. Wait for queues and required visibility progress.
4. Send `SIGTERM`.
5. Wait for checkpoint, WAL, storage, and native shutdown.
6. Confirm the process and listeners have exited.

### 12.10.3. Daily checks

- process health and restart count;
- disk, Badger, WAL, and index growth;
- visibility lag, queue depth, retry, and failure counts;
- S3 and provider health;
- failed or stuck admin tasks;
- age and integrity of the latest backup.

### 12.10.4. Incident evidence

Before repair, preserve the binary version, redacted effective configuration, checkpoint/LSN state, affected Event and object IDs, first error time, logs, process state, disk usage, and dependency health.

---

## 12.11. Troubleshooting Matrix

| Symptom | Diagnose | Resolution boundary |
|---|---|---|
| Service unavailable after restart | process/container state, ports, Badger lock, permissions, dynamic libraries, dependencies | Fix the first failing dependency; do not replace data preemptively |
| Write accepted but not visible | consistency metrics, queue, materializer, embedder, native projection, checkpoint | Identify the failed stage and replay/retry idempotently |
| Latest State is stale | scope, `state_key`, Event order, State version, query filter | Resolve by canonical version, not vector top-1 |
| Cold query fails | `include_cold`, endpoint, TLS, credentials, bucket, prefix, object key | Restore cold access or return an explicit partial/failure status |
| Disk usage grows | Badger value log, WAL retention, segment files, purge queue, archive status | Apply component-specific cleanup; never delete arbitrary subdirectories |
| Admin route returns `401` | key value, `X-Admin-Key` or Bearer header, split management port | Correct client configuration; do not disable authentication permanently |
| Admin routes are unprotected | startup warning and missing key | Set the key and restrict the management listener before deployment |

---

## 12.12. Upgrade and Rollback

### 12.12.1. Upgrade

1. Back up runtime storage, WAL, checkpoints, and cold metadata.
2. Build an image or binary tied to an exact commit/tag.
3. Test the new version against a copy of existing data.
4. Stop writes and shut down the old process cleanly.
5. Apply any required offline migration.
6. Start the new version and check health, storage, configuration, and providers.
7. Validate strict ingest, query, latest State, Trace, and cold-tier behavior.
8. Resume traffic gradually.

### 12.12.2. Rollback

Direct binary rollback is safe only when the previous version can read every format written by the new version. Otherwise restore the pre-upgrade backup and separately reconcile writes accepted during the upgrade window.
