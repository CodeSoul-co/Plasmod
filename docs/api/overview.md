# API Overview

## Purpose

This document gives a compact overview of the HTTP surface of the ANDB v1 prototype. The **authoritative route list** is `Gateway.RegisterRoutes` in [`src/internal/access/gateway.go`](../../src/internal/access/gateway.go) (25 path registrations). A tabular summary also lives in the root [`README.md`](../../README.md#http-api-surface-v1).

Detailed endpoint behavior is documented in:

- `docs/api/admin.md`
- `docs/api/ingest.md`
- `docs/api/query.md`

## Current Base Address

By default the server listens on:

`http://127.0.0.1:8080`

This can be overridden with the `ANDB_HTTP_ADDR` environment variable.

## Endpoint groups

**Health**

- `GET /healthz`

**Core (documented in ingest/query docs)**

- `POST /v1/ingest/events`
- `POST /v1/query`

**Admin (see `docs/api/admin.md`; unauthenticated in default dev — do not expose publicly)**

- `GET /v1/admin/topology`
- `GET /v1/admin/storage`
- `GET /v1/admin/config/effective`
- `POST /v1/admin/s3/export`
- `POST /v1/admin/s3/snapshot-export`
- `POST /v1/admin/dataset/delete`
- `POST /v1/admin/dataset/purge`

**Canonical object CRUD** (`GET` list / filter + `POST` create or replace per resource)

- `/v1/agents`, `/v1/sessions`, `/v1/memory`, `/v1/states`, `/v1/artifacts`, `/v1/edges`, `/v1/policies`, `/v1/share-contracts`

**Proof traces**

- `GET /v1/traces/{object_id}`

**Internal — Agent SDK algorithm bridge** (`POST` only)

- `/v1/internal/memory/recall`, `/v1/internal/memory/ingest`, `/v1/internal/memory/compress`, `/v1/internal/memory/summarize`, `/v1/internal/memory/decay`, `/v1/internal/memory/share`, `/v1/internal/memory/conflict/resolve`

## Content Type

Current JSON endpoints expect:

- request `Content-Type: application/json`
- JSON response bodies

## Versioning

The current prototype uses path-based versioning for API routes:

- `/v1/...`

Health routes are currently unversioned.

## Design Notes

The current API layer is intentionally small. Its job in v1 is to stabilize:

- ingest request shape
- query request shape
- response categories for structured evidence

It is not yet a full production API with authentication, pagination, rate limiting, or comprehensive admin surfaces. In particular, **`/v1/admin/*` has no API-key or token check** in the stock server; use network isolation or a reverse proxy for production.
