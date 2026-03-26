# Nodes and Storage Initialization (v1)

This document defines the current node and storage bootstrap in ANDB v1.

## Node Roles

- `DataNode`
  - receives ingest projection records
  - updates segment-side runtime records

- `IndexNode`
  - receives ingest projection records
  - updates index-side runtime counters and metadata

- `QueryNode`
  - receives search input
  - executes query via the configured data plane

Current code:
- `src/internal/worker/nodes/contracts.go`
- `src/internal/worker/nodes/manager.go`
- `src/internal/worker/nodes/inmemory.go`

## Storage Roles

- `SegmentStore`
  - stores segment records (`segment_id`, namespace, row count, update time)

- `IndexStore`
  - stores index progress records per namespace

Current code:
- `src/internal/storage/contracts.go`
- `src/internal/storage/memory.go`

## Runtime Wiring

Runtime now dispatches:
1. ingest projection -> data/index nodes
2. query input -> query node (fallback to data plane if no query node)

Current code:
- `src/internal/worker/runtime.go`
- `src/internal/app/bootstrap.go`

## Operational Endpoint

`GET /v1/admin/topology` exposes:
- active nodes
- runtime segment records
- runtime index records

Current code:
- `src/internal/access/gateway.go`
