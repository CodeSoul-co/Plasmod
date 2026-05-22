# API Overview

## Purpose

This document gives a compact overview of the HTTP surface of the ANDB v1 prototype. The **authoritative route list** is `Gateway.RegisterRoutes` in [`src/internal/access/gateway.go`](../../src/internal/access/gateway.go) (25 path registrations). A tabular summary also lives in the root [`README.md`](../../README.md#http-api-surface-v1).

Detailed endpoint behavior is documented in:

- `docs/api/admin.md`
- `docs/api/ingest.md`
- `docs/api/query.md`

## Plasmod ports (gRPC deferred)

Constants: `src/internal/app/ports.go` (`PortDevUnified`, `PortMgmt`, `PortAPI`, `PortObjectStore`, …).

| Role | Port | Env / notes |
|------|------|-------------|
| Dev unified (SDK + admin) | **8080** | `PLASMOD_LISTEN_MODE=unified`, `PLASMOD_HTTP_ADDR` (default `127.0.0.1:8080`) |
| Mgmt / health / metrics | **9101** | `PLASMOD_LISTEN_MODE=split`, `PLASMOD_MGMT_ADDR` |
| SDK REST API | **19540** | `PLASMOD_LISTEN_MODE=split`, `PLASMOD_API_ADDR`; pyplasmod `PLASMOD_URI` |
| Object store S3 API (host) | **9010** | `docker compose` maps `9010:9000`; in-cluster `S3_ENDPOINT=minio:9000` |
| Object store console (host) | **9011** | maps `9011:9001` |

`make dev` uses **unified :8080**. `docker compose` uses **split** (:9101 + :19540) and object storage on host **:9010/:9011**.

## Endpoint groups

**Health**

- `GET /healthz`

**Core (documented in ingest/query docs)**

- `POST /v1/ingest/events`
- `POST /v1/ingest/vectors` (warm segment; optional `index_type`: HNSW / IVF_FLAT / IVF_PQ / IVF_SQ8)
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
