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
  "data_dir": ".plasmod_data",
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

### `GET /v1/admin/config/effective`

Returns the effective shared algorithm configuration used for retrieval and
cold-tier experiments after YAML loading and lightweight environment overrides.

Main response field:
- `algorithm_config`

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

### `GET/POST /v1/admin/consistency-mode`

Control-plane consistency mode endpoint for Layer-2 experiment orchestration.

- `GET` returns current mode and supported mode list.
- `POST` body: `{"mode":"strict_visible|bounded_staleness|eventual_visibility"}`.

> Current behavior: mode is exposed for control-plane experiments and metadata capture; runtime query execution path remains single-mode.

### `POST /v1/admin/replay`

WAL replay endpoint for recovery experiments (supports preview and apply).

Body:

```json
{
  "from_lsn": 0,
  "limit": 1000,
  "dry_run": true,
  "apply": false,
  "confirm": ""
}
```

Behavior:
- Preview mode (default): `dry_run=true` (or omit `apply`) returns scan summary only.
- Apply mode: set `apply=true` (or `dry_run=false`) and must set `confirm` to `"apply_replay"`.
- Apply mode re-submits WAL events through ingest path and mutates runtime state.

### `POST /v1/admin/rollback`

Operational rollback endpoint for memory active-state correction.

Body:

```json
{
  "memory_id": "mem_123",
  "action": "reactivate",
  "dry_run": true,
  "reason": "operator rollback"
}
```

Supported `action` values:
- `reactivate`: set `IsActive=true` and clear `valid_to`
- `deactivate`: set `IsActive=false` and set `valid_to` if empty

When `dry_run=false`, mutation is applied and an audit record is appended.

### `POST /v1/admin/s3/cold-purge`

Cold-tier purge control endpoint.

Body:

```json
{
  "confirm": "purge_cold_tier",
  "dry_run": false
}
```

- For in-memory cold tier, it clears in-process cold records.
- For S3-backed cold tier, response includes an explicit note that bucket-side lifecycle/manual cleanup is required.

### `POST /v1/admin/dataset/delete`

Soft-deletes **Memory** rows that match the request selectors (**AND** semantics), or an explicit **`memory_ids`** list. JSON body must include **`workspace_id`**, and either:

- at least one of **`file_name`**, **`dataset_name`**, **`prefix`**, or  
- a non-empty **`memory_ids`** array (trimmed, de-duplicated server-side).

Optional **`dry_run`**: if true, returns `matched` / `memory_ids` without mutating.

When **`memory_ids`** is non-empty, only those IDs are considered; each row must exist and **`Memory.Scope` must equal `workspace_id`** (other workspaces are ignored). **`file_name` / `dataset_name` / `prefix` are ignored** in that mode. The response may include **`requested_memory_ids`** (the normalized id list).

Matching prefers structured fields on `Memory` (`dataset_name`, `source_file_name` from ingest payload) and otherwise uses token-safe parsing of `Content` — see `schemas.MemoryDatasetMatch` in code and the root **`README.md`** (admin dataset cleanup).

### `POST /v1/admin/dataset/purge`

Hard-deletes memories that match the same selector keys as delete, or **`memory_ids`** as above. JSON body: **`workspace_id`**, and either at least one selector or non-empty **`memory_ids`**; optional **`dry_run`**, optional **`only_if_inactive`** (default **true** — skip active memories unless set to false). When using **`memory_ids`**, the response includes **`requested_memory_ids`** when applicable.

When a **`TieredObjectStore`** is wired, removal uses **`HardDeleteMemory`** (hot/warm/cold as applicable). If tiered storage is **not** configured, the handler falls back to **warm-only** purge (`PurgeMemoryWarmOnly`); the JSON response includes **`purge_backend`**: `"tiered"` or `"warm_only"`.

### `POST /v1/admin/memory/delete-by-source`

Soft-deletes every **Memory** in **`workspace_id`** whose **`source_event_ids`** array contains the reference string (trimmed **exact** equality to one list element).

Body: **`workspace_id`** (required), and exactly one of **`reference_id`**, **`event_id`**, or **`memory_id`** — the first non-empty field wins. These three names are aliases for the same “needle” value (usually an **`evt_...`** id ingested into `source_event_ids`). Optional **`dry_run`**.

Response: `matched`, `deleted`, `memory_ids`, `reference_id`.

### `POST /v1/admin/memory/purge-by-source`

Hard-deletes memories selected the same way as delete-by-source. Body: **`workspace_id`**, **`reference_id`** / **`event_id`** / **`memory_id`** (same precedence as above). Optional **`dry_run`**, **`only_if_inactive`** (default **true**), **`include_memory_ids`**, **`async`**, **`idempotency_key`** — behavior matches dataset purge (including async task queue and `purge_backend`).

> **Security:** these admin routes are protected when `PLASMOD_ADMIN_API_KEY` is set (legacy alias `ANDB_ADMIN_API_KEY`). Clients must send `X-Admin-Key: <key>` or `Authorization: Bearer <key>`. If neither env var is set, the default dev server does **not** authenticate `/v1/admin/*`. Always restrict by network or put a reverse proxy in front in production.

## Not Yet Implemented

- `GET /readyz`
- `GET /v1/admin/config`
