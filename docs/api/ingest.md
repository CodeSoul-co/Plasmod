# Ingest API

## Purpose

This document defines the `POST /v1/ingest/events` contract for the ANDB v1 prototype.

The endpoint accepts an event payload, appends it to WAL, materializes canonical objects, and projects retrieval records.

**Implementation:** the synchronous pipeline lives in `worker.PipelineIngestWorker` (`src/internal/worker/ingest_worker.go`), invoked by `worker.Runtime.SubmitIngest`. The coordinator module registry exposes it as `ingest_worker` after `app.BuildServer` wiring.

## Endpoint

- Method: `POST`
- Path: `/v1/ingest/events`
- Content-Type: `application/json`

## Request schema

The server decodes the request into `schemas.Event` from `src/internal/schemas/canonical.go`.

Practical required fields:

- `event_id`
- `tenant_id`
- `workspace_id`
- `agent_id`
- `session_id`
- `event_type`
- `payload`

Strongly recommended fields:

- `event_time`
- `ingest_time`
- `visible_time`
- `source`
- `version`

### Payload notes

- `payload.text` is used as the primary retrieval text for materialization.
- Optional artifact creation is enabled when payload includes a URI field:
  - `artifact_uri`, or
  - nested `artifact.uri`, or
  - top-level `uri` when `event_type=artifact_attached`

## Example request

```json
{
  "event_id": "evt_demo_001",
  "tenant_id": "t_demo",
  "workspace_id": "w_demo",
  "agent_id": "agent_a",
  "session_id": "sess_a",
  "event_type": "user_message",
  "event_time": "2026-03-16T08:00:00Z",
  "ingest_time": "2026-03-16T08:00:00Z",
  "visible_time": "2026-03-16T08:00:00Z",
  "logical_ts": 0,
  "parent_event_id": "",
  "causal_refs": [],
  "payload": {
    "text": "hello plasmod",
    "artifact_uri": "s3://bucket/demo.bin"
  },
  "source": "api",
  "importance": 0.5,
  "visibility": "private",
  "version": 1
}
```

## Success response

On success, the runtime returns an acknowledgment object.

| Field | Type | Description |
|-------|------|-------------|
| `status` | string | `"accepted"` |
| `lsn` | number | WAL logical sequence number |
| `event_id` | string | Echo of ingested event id |
| `memory_id` | string | Canonical memory object id (`mem_<event_id>`) |
| `edges` | number | Count of derived edges |
| `state_id` | string | *(optional)* Present when a state checkpoint row is materialized |
| `artifact_id` | string | *(optional)* Present when artifact URI fields are provided |

### Example response

```json
{
  "status": "accepted",
  "lsn": 12,
  "event_id": "evt_demo_001",
  "memory_id": "mem_evt_demo_001",
  "edges": 2,
  "state_id": "state_sess_a_evt_demo_001",
  "artifact_id": "art_evt_demo_001"
}
```

## Error behavior

- Malformed JSON: HTTP `400`
- Unsupported method: HTTP `405`
- Runtime validation errors: HTTP `400`

Current explicit runtime validation is intentionally small; at minimum, `event_id` must be non-empty.

## Runtime behavior (ingest-only)

The processing order for this endpoint is:

`HTTP gateway -> Runtime.SubmitIngest -> WAL.Append -> materialization -> canonical persistence -> retrieval projection`

Primary entry points:

- `src/internal/access/gateway.go` (`handleIngest`)
- `src/internal/worker/runtime.go` (`SubmitIngest`)
- `src/internal/materialization/service.go` (`MaterializeEvent`)
