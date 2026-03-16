# Ingest API

## Purpose

This document describes the event ingest endpoint used by the ANDB v1 prototype.

The ingest path accepts an event envelope, appends it to the event backbone, and forwards it into the runtime materialization and retrieval-projection path.

## Endpoint

- Method: `POST`
- Path: `/v1/ingest/events`
- Content-Type: `application/json`

## Request Schema

The current server decodes the request into `schemas.Event` from `src/internal/schemas/canonical.go`.

Required practical fields:

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

## Example Request

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
  "logical_ts": 1,
  "parent_event_id": "",
  "causal_refs": [],
  "payload": {
    "text": "hello andb"
  },
  "source": "api",
  "importance": 0.5,
  "visibility": "private",
  "version": 1
}
```

## Success Response

The current runtime returns a lightweight acknowledgment.

### Example

```json
{
  "status": "accepted",
  "lsn": 1,
  "event_id": "evt_demo_001"
}
```

## Error Behavior

Current server behavior:

- malformed JSON returns HTTP `400`
- unsupported methods return HTTP `405`
- runtime validation errors return HTTP `400`

Current explicit runtime validation is intentionally small. At minimum, `event_id` must be present.

## Current Runtime Flow

The current path is:

`HTTP gateway -> runtime.SubmitIngest -> WAL append -> materialization -> retrieval projection`

The implementation entry points are:

- `src/internal/access/gateway.go`
- `src/internal/worker/runtime.go`

## v1 Notes

In the current prototype, ingest immediately feeds a lightweight materialization path and then projects a retrieval-ready record.

This is intentionally simpler than the long-term target, but it already preserves the core rule:

**events are written first, and downstream state derives from events rather than direct overwrite.**
