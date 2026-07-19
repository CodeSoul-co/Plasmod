# Type Index

| Domain | Main types | Source |
|---|---|---|
| Event | Event, EventIdentity, EventActor, EventAccess, EventMaterialization, EventRetrieval | `schemas/dynamic_event.go` |
| Canonical | Agent, Session, Memory, State/AgentState, Artifact, Edge, ObjectVersion | `schemas/canonical.go` |
| Governance | Policy, PolicyRecord, ShareContract | `schemas/canonical.go` |
| Retrieval | RetrievalSegment, WarmVectorsIngestRequest, VectorWarmBatchQueryRequest | `schemas/*retrieval*`, `vector_batch_query.go` |
| Query | QueryRequest, QueryResponse, GraphExpandRequest/Response | `schemas/query.go` |
| Evidence | GraphNode, ProofStep, EvidenceSubgraph | `schemas/canonical.go`, evidence package |
| Runtime | MaterializationResult, IngestRecord, consistency status/checkpoint | materialization/dataplane/worker packages |

精确字段以 struct 和 JSON tag 为准。
