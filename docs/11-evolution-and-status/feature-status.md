# Feature Status

| Capability | Status | Evidence/qualification |
|---|---|---|
| Dynamic Event v0.4 ingest | Implemented | Gateway + schemas + Runtime/WAL |
| File/In-memory WAL | Implemented | eventbackbone + storage factory |
| Memory/Artifact/Edge/Version materialization | Implemented | materialization service |
| AgentState materialization | Implemented | state worker; recovery requires version/replay checks |
| strict/bounded/eventual | Implemented | consistency controller |
| Canonical CRUD | Implemented | Gateway/coordinators; not full WAL semantics |
| Structured query/evidence | Implemented | semantic/dataplane/evidence |
| Hot/Warm tiers | Implemented | cache + canonical/retrieval stores |
| S3/MinIO Cold tier | Implemented | explicit archive/query/purge |
| Native HNSW | Implemented | build-dependent |
| IVF/DiskANN | Partial | compile feature/platform dependent |
| gRPC/transport | Partial | not full HTTP API parity |
| Python SDK | Implemented | core ingest/query/vector/admin helpers |
| Node SDK | Partial | legacy naming and limited methods |
| Admin key | Implemented | admin prefix only |
| End-user/IAM authentication | Not Confirmed | deployment gateway required |
| Multi-process shared Badger HA | Not Confirmed | default is single active runtime |
| Universal idempotency protocol | Not Confirmed | no general Idempotency-Key |
