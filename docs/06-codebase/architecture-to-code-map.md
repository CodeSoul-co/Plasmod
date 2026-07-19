# Architecture To Code Map

| Architecture concept | Primary code | Supporting code |
|---|---|---|
| Event source of truth | `eventbackbone.WAL` | `worker.Runtime`, consistency tracker |
| Canonical objects | `schemas/canonical.go` | `storage.ObjectStore`, coordinators |
| Materialization | `materialization.Service` | `worker/nodes` materializers |
| Retrieval projection | `dataplane` | `retrievalplane`, C++ bridge |
| Evidence | `evidence` | graph edges, versions, policies |
| Query planning | `semantic.QueryPlanner` | dataplane + evidence assembler |
| Consistency | `worker/consistency.Controller` | checkpoint, tracker, queues |
| Tiered storage | `storage/tiered.go` | Badger, S3/MinIO |
| HTTP boundary | `access.Gateway` | auth and visibility middleware |
| Process wiring | `app.BuildServer` | server lifecycle and ports |

若修改某概念，必须同时检查表中 primary 和 supporting code，不能只改 schema 或 route。
