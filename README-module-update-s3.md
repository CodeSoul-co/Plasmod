# S3 Module Update (Branch Record)

## 1. Background and Goal

This branch focuses on the S3 delivery target for local development:

- make runtime data export write to S3-compatible storage (MinIO)
- make snapshot/segment-style objects write to S3 and read back successfully
- provide one-click scripts so reviewers can reproduce the full flow quickly

Required env contract from task:

- `S3_ENDPOINT`
- `S3_ACCESS_KEY`
- `S3_SECRET_KEY`
- `S3_BUCKET`

Optional:

- `S3_SECURE`
- `S3_REGION`
- `S3_PREFIX`

## 2. What Was Implemented

### 2.1 Admin API Endpoints

Implemented in `src/internal/access/gateway.go`:

- `POST /v1/admin/s3/export`
  - runs sample runtime ingest + query
  - exports one capture object to S3
  - performs immediate GET round-trip verification

- `POST /v1/admin/s3/snapshot-export`
  - builds snapshot/segment-style key layout under `S3_PREFIX`
  - writes metadata JSON, manifest AVRO bytes, and segment data JSON
  - performs immediate GET round-trip verification for each object

### 2.2 S3 Utility Layer

Implemented in `src/internal/s3util/s3util.go`:

- env loading (`LoadFromEnv`)
- bucket ensure (`EnsureBucket`)
- stdlib-only SigV4 signing
- `PutBytesAndVerify` / `PutJSONAndVerify` for PUT+GET validation

### 2.3 Local One-Click Scripts

Added in `scripts/dev/`:

- `ensure-docker.ps1`  
  ensures Docker availability on local Windows environment

- `start-minio.ps1`  
  starts MinIO container for local S3-compatible storage

- `run-s3-runtime-export.ps1`  
  one-click flow for runtime capture export endpoint

- `run-s3-snapshot-export.ps1`  
  one-click flow for snapshot/segment export endpoint

All run artifacts are stored under:

- `scripts/dev/artifacts/...`

### 2.4 Config and Tooling

- `.env.example` updated with S3 local examples
- `Makefile` updated with `integration-test-s3`
- `.gitignore` updated for local logs/artifacts/minio data
- `go.mod` / `go.sum` updated for AVRO dependency

### 2.5 Documentation

- `docs/api/admin.md` updated with new admin S3 endpoints
- `docs/architecture/s3-local-runtime-runbook.md` added as runbook

## 3. Current Runtime Behavior

### 3.1 Runtime Export Path

Entry:

- `POST /v1/admin/s3/export`

Behavior:

1. submit sample runtime ingest
2. execute sample query
3. serialize capture payload
4. PUT to S3
5. GET from same key and compare bytes

### 3.2 Snapshot/Segment Export Path

Entry:

- `POST /v1/admin/s3/snapshot-export`

Key layout example:

- `S3_PREFIX/snapshots/<collection_id>/metadata/<snapshot_id>.json`
- `S3_PREFIX/snapshots/<collection_id>/manifests/<snapshot_id>/<segment_id>.avro`
- `S3_PREFIX/segments/<collection_id>/<segment_id>/segment_data.json`

Validation:

- `roundtrip_ok.metadata == true`
- `roundtrip_ok.manifest == true`
- `roundtrip_ok.segment_data == true`

## 4. How To Reproduce (Reviewer)

From repository root:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File "scripts/dev/run-s3-snapshot-export.ps1"
```

Expected response includes:

- `"status": "ok"`
- all `roundtrip_ok` flags are `true`

Run record output path:

- `scripts/dev/artifacts/s3-snapshot-export/<timestamp>/record.md`

## 5. Known Scope and Limitation

This branch provides a practical local-delivery S3 write/read flow and key-layout validation.

It is **not yet** the full production-grade Milvus-migration object-storage path in all modules.
There are still upstream migration dependencies and `FIXME`-related areas under extended modules that need follow-up for full parity.

## 6. Next Step (Recommended)

1. continue `S3_* -> minio.*` unified config mapping for broader runtime modules
2. progressively replace dev-only snapshot export pieces with full production writer path where module dependencies are ready
3. keep one-click scripts and record artifacts for pre-push verification

