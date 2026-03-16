# Module Contracts (v1 Freeze)

## Ingest -> Materialization
- Input: `schemas.Event`
- Output: canonical object mutations, object versions, and edges

## Materialization -> Data Plane
- Input: canonical objects
- Output: retrieval projections (dense/sparse/filter attributes/graph refs)

## Retrieval -> Graph/Tensor Assembly
- Input: candidate object IDs
- Output: constrained expansion seeds and evidence paths

## Graph -> Response
- Input: expanded nodes, edges, provenance, version boundaries
- Output: `schemas.QueryResponse`

## Event Backbone Contract
- Every mutation enters WAL first and gets a logical sequence.
- Workers consume from WAL as subscribers; no bypass writes.

## Policy Contract
- Policy updates must emit auditable decision logs.
- Query path must apply minimum scope/visibility/TTL/quarantine filters.
