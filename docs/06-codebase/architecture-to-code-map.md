# Architecture To Code Map

本页是快速索引。逐项 constructor、method、typed I/O、状态和成熟度以 [System Design Reference](../02-concepts-and-design/system-design/README.md) 为准。

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
| Runtime coordination | `worker.Runtime`, `worker/chain` | consistency controller, NodeManager, partially wired Orchestrator |
| Memory evolution | `schemas.MemoryManagementAlgorithm`, cognitive dispatcher | algorithm state, reflection, tiering, audit |
| Governance | policy/ShareContract stores and policy engine | Runtime filters, reflection, decision log |
| Collaboration | `CollaborationChain`, agent collaboration adapter | communication/conflict/microbatch workers; partial transaction integration |
| Reconciliation | replay/reindex/reset/purge fragments | no unified active Reconciliation Manager |
| Scheduling | consistency queues, Orchestrator, NodeManager, counter scheduler | no unified resource-aware Intelligent Scheduler |

若修改某概念，必须同时检查表中 primary 和 supporting code，不能只改 schema 或 route。

进一步核对：

- [Interface Implementation Registry](../02-concepts-and-design/system-design/06-cross-reference/interface-implementation-registry.md)
- [API to Engine Matrix](../02-concepts-and-design/system-design/06-cross-reference/api-to-engine-matrix.md)
- [Claim and Test Boundary](../02-concepts-and-design/system-design/06-cross-reference/claim-and-test-boundary.md)
