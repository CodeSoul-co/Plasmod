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

## Not Yet Implemented

- `GET /readyz`
- `GET /v1/admin/config`
