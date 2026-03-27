# Admin and Health API

## Purpose

Operational and topology endpoints exposed by the ANDB v1 server.

## Implemented Endpoints

### `GET /healthz`

Returns process liveness.

Example:

```json
{
  "status": "ok"
}
```

### `GET /v1/admin/storage`

Returns the resolved **runtime storage** configuration (memory vs Badger per sub-store).

Example shape:

```json
{
  "mode": "hybrid",
  "data_dir": ".andb_data",
  "badger_enabled": true,
  "stores": {
    "segments": "memory",
    "indexes": "memory",
    "objects": "disk",
    "edges": "memory",
    "versions": "memory",
    "policies": "memory",
    "contracts": "memory"
  },
  "wal_persistence": false
}
```

When `data_dir` is `:memory:`, Badger was opened with `InMemory` (see `ANDB_BADGER_INMEMORY`).

### `GET /v1/admin/topology`

Returns runtime topology and storage snapshots.

Example shape:

```json
{
  "nodes": [],
  "segments": [],
  "indexes": []
}
```

### `POST /v1/admin/s3/export`

Dev-only endpoint to validate runtime ingest/query capture export to S3-compatible storage.

Behavior:
- runs one sample ingest (`/v1/ingest/events`) in-process
- runs one sample query (`/v1/query`) in-process
- writes one capture JSON object to S3 (SigV4)
- immediately reads it back and validates byte-level round-trip

Main response fields:
- `status`: `"ok"` on success
- `bucket`: target bucket
- `object_key`: object key written to S3
- `bytes_written`: payload size
- `roundtrip_ok`: boolean validation result

### `POST /v1/admin/s3/snapshot-export`

Dev-only endpoint to validate snapshot/segment style object layouts in S3-compatible storage.

Behavior:
- builds key paths under `S3_PREFIX`:
  - `snapshots/<collection_id>/metadata/<snapshot_id>.json`
  - `snapshots/<collection_id>/manifests/<snapshot_id>/<segment_id>.avro`
  - `segments/<collection_id>/<segment_id>/segment_data.json`
- writes metadata, Avro manifest, and segment data objects
- immediately reads each object back and validates round-trip

Main response fields:
- `metadata_path`, `manifest_path`, `segment_data_path`
- `roundtrip_ok.metadata`, `roundtrip_ok.manifest`, `roundtrip_ok.segment_data`

> Note: This endpoint is intended for local/dev validation and pre-delivery checks.

## Not Yet Implemented

- `GET /readyz`
- `GET /v1/admin/config`
