# API Overview

## Purpose

This document gives a compact overview of the public HTTP surface of the ANDB v1 prototype.

Detailed endpoint behavior is documented in:

- `docs/api/admin.md`
- `docs/api/ingest.md`
- `docs/api/query.md`

## Current Base Address

By default the server listens on:

`http://127.0.0.1:8080`

This can be overridden with the `ANDB_HTTP_ADDR` environment variable.

## Public Endpoints

- `GET /healthz`
- `POST /v1/ingest/events`
- `POST /v1/query`

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

It is not yet a full production API with authentication, pagination, rate limiting, or comprehensive admin surfaces.
