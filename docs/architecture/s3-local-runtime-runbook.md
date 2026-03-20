# Local S3 Runtime Runbook

## Purpose

This runbook describes the current local S3 validation workflow used by ANDB
for delivery checks:
- runtime capture export
- snapshot/segment-style object export

It is intentionally focused on local MinIO-compatible execution.

## Prerequisites

- Docker Desktop running locally
- MinIO-compatible credentials:
  - `S3_ENDPOINT`
  - `S3_ACCESS_KEY`
  - `S3_SECRET_KEY`
  - `S3_BUCKET`
- Optional:
  - `S3_SECURE`
  - `S3_REGION`
  - `S3_PREFIX`

Defaults are already provided in `.env.example`.

## If Docker/MinIO Is Not Installed

Use the built-in helper scripts first (recommended):

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File "scripts/dev/ensure-docker.ps1"
powershell -NoProfile -ExecutionPolicy Bypass -File "scripts/dev/start-minio.ps1"
```

What happens:
- `ensure-docker.ps1` checks Docker availability, tries to start Docker Desktop,
  and attempts installation via package manager when missing.
- `start-minio.ps1` starts a local MinIO container (it calls `ensure-docker.ps1` internally).

Manual fallback (Windows + winget):

```powershell
winget install -e --id Docker.DockerDesktop
```

After installation, start Docker Desktop once, then run:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File "scripts/dev/start-minio.ps1"
```

## One-Click Scripts

### Runtime capture export

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File "scripts/dev/run-s3-runtime-export.ps1"
```

What it does:
- starts MinIO (`scripts/dev/start-minio.ps1`)
- starts ANDB server
- calls `POST /v1/admin/s3/export`
- writes evidence under `scripts/dev/artifacts/s3-runtime-export/<timestamp>/`

### Snapshot/segment export

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File "scripts/dev/run-s3-snapshot-export.ps1"
```

What it does:
- starts MinIO (`scripts/dev/start-minio.ps1`)
- starts ANDB server
- calls `POST /v1/admin/s3/snapshot-export`
- writes evidence under `scripts/dev/artifacts/s3-snapshot-export/<timestamp>/`

## API Entry Points

- Route registration: `src/internal/access/gateway.go`
  - `POST /v1/admin/s3/export`
  - `POST /v1/admin/s3/snapshot-export`

## Current Snapshot/Segment Object Layout

Keys are built from `S3_PREFIX` as root:
- `snapshots/<collection_id>/metadata/<snapshot_id>.json`
- `snapshots/<collection_id>/manifests/<snapshot_id>/<segment_id>.avro`
- `segments/<collection_id>/<segment_id>/segment_data.json`

## Verification Criteria

Treat the run as passed only when response contains:
- `status = "ok"`
- `roundtrip_ok` fields are all `true`

For snapshot export:
- `roundtrip_ok.metadata = true`
- `roundtrip_ok.manifest = true`
- `roundtrip_ok.segment_data = true`

## Important Scope Note

This workflow is a practical local delivery path for S3 write/read validation.
It is not yet the full Milvus-migration production path that depends on all
`extended`-tag storage modules being available and fully wired.
