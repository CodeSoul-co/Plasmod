# Worker Contracts

> **Source of truth:** `src/internal/schemas/worker_params.go` (typed Input/Output structs)  
> `src/internal/worker/nodes/contracts.go` (worker interfaces)  
> **Last updated:** 2026-03-21

Every worker in the cognitive dataflow pipeline exposes:
- A **typed Input struct** implementing `schemas.WorkerInput`
- A **typed Output struct** implementing `schemas.WorkerOutput`
- An **interface** in `nodes/contracts.go` with one or more named methods
- A **constructor** following `Create*` naming convention

All input/output types are JSON-serialisable and can be used as HTTP payloads,
chain arguments, or test fixtures without additional conversion.

---

## Extensibility Pattern

```go
// Implement WorkerInput to create a custom worker input type.
type MyInput struct { ... }
func (MyInput) WorkerKind() string { return "my_worker_type" }

// Implement WorkerOutput to create a custom worker output type.
type MyOutput struct { ... }
func (o MyOutput) IsEmpty() bool { return ... }
```

Any struct implementing `WorkerInput` / `WorkerOutput` can be dispatched
through the existing `Manager` without modifying `contracts.go` — just
register a new worker type constant and implement the interface.

---

## Layer Map

| Layer | Workers |
|---|---|
| L1 Data Plane | `IndexBuildWorker` |
| L2 Event/Log | `IngestWorker` |
| L3 Object | `ObjectMaterializationWorker`, `StateMaterializationWorker`, `ToolTraceWorker` |
| L4 Cognitive | `MemoryExtractionWorker`, `MemoryConsolidationWorker`, `SummarizationWorker`, `ReflectionPolicyWorker` |
| L5 Structure | `GraphRelationWorker`, `SubgraphExecutorWorker` |
| L6 Policy | `ConflictMergeWorker` |
| L7 Query/Reasoning | `ProofTraceWorker`, `MicroBatchScheduler` |
| L8 Coordination | `CommunicationWorker` |

---

## L2 — IngestWorker

**NodeType:** `ingest_worker`  
**Package:** `worker/ingestion`  
**Constructor:** `CreateInMemoryIngestWorker(id string)`  
**Capabilities:** `schema_validation`, `field_normalisation`

### Interface

```go
type IngestWorker interface {
    Info() NodeInfo
    Process(ev schemas.Event) error
}
```

### Parameters

| Struct | Field | Type | Required | Description |
|---|---|---|---|---|
| `IngestInput` | `Event` | `schemas.Event` | ✅ | Raw event to validate |
| `IngestOutput` | `Valid` | `bool` | — | true if all mandatory fields pass |
| `IngestOutput` | `Error` | `string` | — | validation error message, omitempty |

### Validation rules

- `event_id` must be non-empty
- `agent_id` must be non-empty
- `event_type` must be non-empty

### Notes

- Does **not** write to the WAL; `Runtime.SubmitIngest` owns that step.
- Runs as step 0 in `MainChain` before any materialization.

---

## L3 — ObjectMaterializationWorker

**NodeType:** `object_materialization_worker`  
**Package:** `worker/materialization`  
**Constructor:** `CreateInMemoryObjectMaterializationWorker(id, objStore, verStore)`  
**Capabilities:** `memory_route`, `state_route`, `artifact_route`

### Interface

```go
type ObjectMaterializationWorker interface {
    Info() NodeInfo
    Materialize(ev schemas.Event) error
}
```

### Parameters

| Struct | Field | Type | Required | Description |
|---|---|---|---|---|
| `ObjectMaterializationInput` | `Event` | `schemas.Event` | ✅ | Event to route |
| `ObjectMaterializationOutput` | `ObjectID` | `string` | — | ID of produced object |
| `ObjectMaterializationOutput` | `ObjectType` | `string` | — | `"memory"` \| `"state"` \| `"artifact"` |

### Routing table

| `event_type` | Canonical object | Key fields extracted |
|---|---|---|
| `tool_call`, `tool_result` | `Artifact` | `uri`, `mime_type` from `payload` |
| `state_update`, `state_change`, `checkpoint` | `State` | `state_key`, `state_value` from `payload` |
| _(anything else)_ | `Memory` (level-0 episodic) | `text` from `payload` |

All produced objects also generate an `ObjectVersion` snapshot.

---

## L3 — StateMaterializationWorker

**NodeType:** `state_materialization_worker`  
**Package:** `worker/materialization`  
**Constructor:** `CreateInMemoryStateMaterializationWorker(id, objStore, verStore)`  
**Capabilities:** `state_apply`, `state_checkpoint`

### Interface

```go
type StateMaterializationWorker interface {
    Info() NodeInfo
    Apply(ev schemas.Event) error
    Checkpoint(agentID, sessionID string) error
}
```

### Apply parameters

| Struct | Field | Type | Required | Description |
|---|---|---|---|---|
| `StateApplyInput` | `Event` | `schemas.Event` | ✅ | State-mutating event |
| `StateApplyOutput` | `StateID` | `string` | — | ID of upserted State; empty if no `state_key` |
| `StateApplyOutput` | `Version` | `int64` | — | New version number |

### Checkpoint parameters

| Struct | Field | Type | Required | Description |
|---|---|---|---|---|
| `StateCheckpointInput` | `AgentID` | `string` | ✅ | Agent to snapshot |
| `StateCheckpointInput` | `SessionID` | `string` | ✅ | Session to snapshot |
| `StateCheckpointOutput` | `SnapshotCount` | `int` | — | Number of ObjectVersion records written |
| `StateCheckpointOutput` | `SnapshotTag` | `string` | — | e.g. `"checkpoint_2026-03-21T10:00:00Z"` |

### Notes

- Maintains an internal `map[agentID:sessionID:stateKey → stateID]` for upsert tracking.
- Version is auto-incremented from the existing State's version.

---

## L3 — ToolTraceWorker

**NodeType:** `tool_trace_worker`  
**Package:** `worker/materialization`  
**Constructor:** `CreateInMemoryToolTraceWorker(id, objStore, derivLog)`  
**Capabilities:** `tool_call_trace`, `tool_result_capture`, `derivation_log`

### Interface

```go
type ToolTraceWorker interface {
    Info() NodeInfo
    TraceToolCall(ev schemas.Event) error
}
```

### Parameters

| Struct | Field | Type | Required | Description |
|---|---|---|---|---|
| `ToolTraceInput` | `Event` | `schemas.Event` | ✅ | Must be `tool_call` or `tool_result` |
| `ToolTraceOutput` | `ArtifactID` | `string` | — | `"tool_trace_{event_id}"` |
| `ToolTraceOutput` | `DerivationLogged` | `bool` | — | true when DerivationLogger is wired |

### Notes

- `derivLog` may be `nil`; when provided, appends `event → artifact` causal edge.
- ArtifactType is always `"tool_trace"`, MimeType `"application/json"`.
- Full `payload` is copied into `Artifact.Metadata` plus `traced_event_id` and `traced_agent_id`.

---

## L4 — MemoryExtractionWorker

**NodeType:** `memory_extraction_worker`  
**Package:** `worker/cognitive`  
**Constructor:** `CreateInMemoryMemoryExtractionWorker(id, store)`  
**Capabilities:** `memory_extract`, `level0_record`

### Interface

```go
type MemoryExtractionWorker interface {
    Info() NodeInfo
    Extract(eventID, agentID, sessionID, content string) error
}
```

### Parameters

| Struct | Field | Type | Required | Description |
|---|---|---|---|---|
| `MemoryExtractionInput` | `EventID` | `string` | ✅ | Source event ID |
| `MemoryExtractionInput` | `AgentID` | `string` | ✅ | Owning agent |
| `MemoryExtractionInput` | `SessionID` | `string` | ✅ | Owning session |
| `MemoryExtractionInput` | `Content` | `string` | ✅ | Raw text content |
| `MemoryExtractionOutput` | `MemoryID` | `string` | — | `"mem_{eventID}"` |

### Notes

- Produces MemoryType `"episodic"`, Level 0, Version 1, IsActive true.

---

## L4 — MemoryConsolidationWorker

**NodeType:** `memory_consolidation_worker`  
**Package:** `worker/cognitive`  
**Constructor:** `CreateInMemoryMemoryConsolidationWorker(id, store)`  
**Capabilities:** `memory_consolidate`, `level1_summary`

### Interface

```go
type MemoryConsolidationWorker interface {
    Info() NodeInfo
    Consolidate(agentID, sessionID string) error
}
```

### Parameters

| Struct | Field | Type | Required | Description |
|---|---|---|---|---|
| `MemoryConsolidationInput` | `AgentID` | `string` | ✅ | Agent scope |
| `MemoryConsolidationInput` | `SessionID` | `string` | ✅ | Session scope |
| `MemoryConsolidationOutput` | `SummaryID` | `string` | — | `"summary_{agentID}_{sessionID}"` |
| `MemoryConsolidationOutput` | `SourceCount` | `int` | — | Number of level-0 memories consumed |

### Notes

- Concatenates all active level-0 memories; produces MemoryType `"semantic"`, Level 1.
- No-op when no active level-0 memories exist.

---

## L4 — SummarizationWorker

**NodeType:** `summarization_worker`  
**Package:** `worker/cognitive`  
**Constructor:** `CreateInMemorySummarizationWorker(id, objStore)`  
**Capabilities:** `level1_summary`, `level2_abstraction`, `context_compression`

### Interface

```go
type SummarizationWorker interface {
    Info() NodeInfo
    Summarize(agentID, sessionID string, maxLevel int) error
}
```

### Parameters

| Struct | Field | Type | Required | Description |
|---|---|---|---|---|
| `SummarizationInput` | `AgentID` | `string` | ✅ | Agent scope |
| `SummarizationInput` | `SessionID` | `string` | ✅ | Session scope |
| `SummarizationInput` | `MaxLevel` | `int` | ✅ | `1` or `2` (clamped) |
| `SummarizationOutput` | `ProducedIDs` | `[]string` | — | MemoryIDs of new summaries |

### Level semantics

| MaxLevel | Source level | Output MemoryType | Minimum source count |
|---|---|---|---|
| 1 | 0 | `"semantic"` | 2 |
| 2 | 1 | `"procedural"` | 2 |

### Notes

- Importance of output = average of source memories' Importance.
- Confidence fixed at `0.85`.

---

## L4 — ReflectionPolicyWorker

**NodeType:** `reflection_policy_worker`  
**Package:** `worker/cognitive`  
**Constructor:** `CreateInMemoryReflectionPolicyWorker(id, objStore, polStore, policyLog)`  
**Capabilities:** `ttl_decay`, `quarantine`, `confidence_override`, `salience_decay`, `policy_audit`

### Interface

```go
type ReflectionPolicyWorker interface {
    Info() NodeInfo
    Reflect(objectID, objectType string) error
}
```

### Parameters

| Struct | Field | Type | Required | Description |
|---|---|---|---|---|
| `ReflectionPolicyInput` | `ObjectID` | `string` | ✅ | Target object |
| `ReflectionPolicyInput` | `ObjectType` | `string` | ✅ | `"memory"` (only supported in v1) |
| `ReflectionPolicyOutput` | `Modified` | `bool` | — | true if any rule changed the object |
| `ReflectionPolicyOutput` | `AppliedRules` | `[]string` | — | Rule tags applied |

### Applied rule tags

| Tag | Condition |
|---|---|
| `"quarantined"` | `Policy.QuarantineFlag == true` |
| `"ttl_expired"` | `time.Since(ValidFrom) > Policy.TTL seconds` |
| `"confidence_overridden"` | `Policy.ConfidenceOverride != current` |
| `"salience_decayed"` | `0 < Policy.SalienceWeight < 1.0` |

---

## L1 — IndexBuildWorker

**NodeType:** `index_build_worker`  
**Package:** `worker/indexing`  
**Constructor:** `CreateInMemoryIndexBuildWorker(id, segStore, idxStore)`  
**Capabilities:** `segment_index`, `keyword_index`, `attribute_index`

### Interface

```go
type IndexBuildWorker interface {
    Info() NodeInfo
    IndexObject(objectID, objectType, namespace, text string) error
}
```

### Parameters

| Struct | Field | Type | Required | Description |
|---|---|---|---|---|
| `IndexBuildInput` | `ObjectID` | `string` | ✅ | Object to index |
| `IndexBuildInput` | `ObjectType` | `string` | ✅ | Canonical object type |
| `IndexBuildInput` | `Namespace` | `string` | ✅ | Segment partition key |
| `IndexBuildInput` | `Text` | `string` | — | Optional full-text content |
| `IndexBuildOutput` | `SegmentID` | `string` | — | `"seg_{namespace}_{date}"` |
| `IndexBuildOutput` | `IndexedCount` | `int` | — | Cumulative count in namespace |

### Notes

- TimeBucket is `YYYY-MM-DD` (UTC). Segments are day-partitioned.
- Segment Tier is set to `"hot"` at index time; tiering is handled by DataPlane.

---

## L5 — GraphRelationWorker

**NodeType:** `graph_relation_worker`  
**Package:** `worker/indexing`  
**Constructor:** `CreateInMemoryGraphRelationWorker(id, store)`  
**Capabilities:** `graph_index`, `edge_write`

### Interface

```go
type GraphRelationWorker interface {
    Info() NodeInfo
    IndexEdge(srcID, srcType, dstID, dstType, edgeType string, weight float64) error
}
```

### Parameters

| Struct | Field | Type | Required | Description |
|---|---|---|---|---|
| `GraphRelationInput` | `SrcID` | `string` | ✅ | Source object ID |
| `GraphRelationInput` | `SrcType` | `string` | ✅ | Source object type |
| `GraphRelationInput` | `DstID` | `string` | ✅ | Destination object ID |
| `GraphRelationInput` | `DstType` | `string` | ✅ | Destination object type |
| `GraphRelationInput` | `EdgeType` | `string` | ✅ | Semantic edge type |
| `GraphRelationInput` | `Weight` | `float64` | ✅ | Edge weight [0.0, 1.0] typical |
| `GraphRelationOutput` | `EdgeID` | `string` | — | `"edge_{srcID}_{edgeType}_{dstID}"` |

### Standard EdgeType values

| EdgeType | Meaning |
|---|---|
| `derived_from` | Memory / Artifact derived from an Event |
| `conflict_resolved` | Winner memory supersedes loser memory |
| `shared_from` | Memory copied to another agent's space |
| `tool_produces` | Tool call produced an Artifact |
| `references` | General reference between objects |

---

## L5 — SubgraphExecutorWorker

**NodeType:** `subgraph_executor_worker`  
**Package:** `worker/indexing`  
**Constructor:** `CreateInMemorySubgraphExecutorWorker(id)`  
**Capabilities:** `one_hop_expand`, `edge_type_filter`, `subgraph_assemble`

### Interface

```go
type SubgraphExecutorWorker interface {
    Info() NodeInfo
    Expand(req schemas.GraphExpandRequest,
           nodes []schemas.GraphNode,
           edges []schemas.Edge) schemas.GraphExpandResponse
}
```

### Parameters

| Struct | Field | Type | Required | Description |
|---|---|---|---|---|
| `SubgraphExpandInput` | `Req` | `GraphExpandRequest` | ✅ | Expansion request with seed IDs |
| `SubgraphExpandInput` | `Nodes` | `[]GraphNode` | ✅ | Pre-fetched graph nodes |
| `SubgraphExpandInput` | `Edges` | `[]Edge` | ✅ | Pre-fetched edges (from BulkEdges) |
| `SubgraphExpandOutput` | — | `GraphExpandResponse` | — | Expanded subgraph |

### GraphExpandRequest fields relevant to expansion

| Field | Type | Default | Description |
|---|---|---|---|
| `SeedObjectIDs` | `[]string` | — | Starting nodes for expansion |
| `SeedObjectTypes` | `[]string` | — | Optional type filter on seeds |
| `Hops` | `int` | 1 | Number of expansion hops |
| `EdgeTypes` | `[]string` | — | Filter to specific edge types (empty = all) |
| `MaxNodes` | `int` | 0 | Cap on returned nodes (0 = unlimited) |
| `MaxEdges` | `int` | 0 | Cap on returned edges (0 = unlimited) |
| `NeedProvenance` | `bool` | false | Include provenance in response |

### ⚠️ Caller responsibility

`Nodes` and `Edges` must be pre-fetched by the caller before passing to `Expand`.
The worker does not perform storage reads.

---

## L6 — ConflictMergeWorker

**NodeType:** `conflict_merge_worker`  
**Package:** `worker/coordination`  
**Constructor:** `CreateInMemoryConflictMergeWorker(id, objStore, edgeStore)`  
**Capabilities:** `conflict_detect`, `last_writer_wins`, `conflict_edge`

### Interface

```go
type ConflictMergeWorker interface {
    Info() NodeInfo
    Merge(leftID, rightID, objectType string) error
}
```

### Parameters

| Struct | Field | Type | Required | Description |
|---|---|---|---|---|
| `ConflictMergeInput` | `LeftID` | `string` | ✅ | First competing object |
| `ConflictMergeInput` | `RightID` | `string` | ✅ | Second competing object |
| `ConflictMergeInput` | `ObjectType` | `string` | ✅ | `"memory"` (only type in v1) |
| `ConflictMergeOutput` | `WinnerID` | `string` | — | Higher-version object |
| `ConflictMergeOutput` | `LoserID` | `string` | — | Lower-version object (IsActive=false) |
| `ConflictMergeOutput` | `Resolved` | `bool` | — | true if conflict was detected and resolved |

### Resolution algorithm

1. Both objects must be `IsActive=true` and share `AgentID` + `SessionID`.
2. Higher `Version` wins (last-writer-wins).
3. Loser: `IsActive = false` written back to ObjectStore.
4. A `conflict_resolved` edge is written: `winner → loser`.

---

## L7 — ProofTraceWorker

**NodeType:** `proof_trace_worker`  
**Package:** `worker/coordination`  
**Constructor:** `CreateInMemoryProofTraceWorker(id, store, derivLog)`  
**Capabilities:** `proof_trace`, `multi_hop_bfs`, `derivation_log`

### Interface

```go
type ProofTraceWorker interface {
    Info() NodeInfo
    AssembleTrace(objectIDs []string, maxDepth int) []string
}
```

### Parameters

| Struct | Field | Type | Required | Description |
|---|---|---|---|---|
| `ProofTraceInput` | `ObjectIDs` | `[]string` | ✅ | Seed object IDs for BFS |
| `ProofTraceInput` | `MaxDepth` | `int` | — | BFS depth cap; 0 → internal default 8 |
| `ProofTraceOutput` | `Steps` | `[]string` | — | Assembled trace step strings |
| `ProofTraceOutput` | `HopCount` | `int` | — | Actual BFS hops traversed |

### Step formats

```
[d=N] {srcID} -[{edgeType}]-> {dstID} (w={weight:.2f})
derivation: {srcID}({srcType}) -[{operation}]-> {derivedID}({derivedType})
```

### Notes

- `derivLog` may be `nil`; derivation steps are only included when wired.
- BFS uses a `seen` map to deduplicate both node IDs and step strings.
- Cycles are safe: BFS terminates at `maxDepth`, not at node exhaustion.

---

## L7 — MicroBatchScheduler

**NodeType:** `micro_batch_scheduler`  
**Package:** `worker/coordination`  
**Constructor:** `CreateInMemoryMicroBatchScheduler(id string, batchSize int)`  
**Capabilities:** `query_batching`, `cross_agent_merge`, `backpressure`

### Interface

```go
type MicroBatchScheduler interface {
    Info() NodeInfo
    Enqueue(queryID string, payload any)
    Flush() []any
}
```

### Parameters

| Struct | Field | Type | Required | Description |
|---|---|---|---|---|
| `MicroBatchEnqueueInput` | `QueryID` | `string` | ✅ | Informational task identifier |
| `MicroBatchEnqueueInput` | `Payload` | `any` | ✅ | Opaque task payload |
| `MicroBatchFlushOutput` | `Items` | `[]any` | — | All queued items (FIFO) |
| `MicroBatchFlushOutput` | `Count` | `int` | — | `len(Items)` |

### Constructor parameters

| Parameter | Type | Default | Description |
|---|---|---|---|
| `id` | `string` | — | Worker ID |
| `batchSize` | `int` | `32` | Soft drain threshold (≤0 → 32) |

### ⚠️ Known gap

`Flush()` is not automatically called in current code paths.  
A periodic flush goroutine or WAL-watermark hook must be added before production use.

---

## L8 — CommunicationWorker

**NodeType:** `communication_worker`  
**Package:** `worker/coordination`  
**Constructor:** `CreateInMemoryCommunicationWorker(id, objStore)`  
**Capabilities:** `memory_broadcast`, `shared_memory_distribution`

### Interface

```go
type CommunicationWorker interface {
    Info() NodeInfo
    Broadcast(fromAgentID, toAgentID, memoryID string) error
}
```

### Parameters

| Struct | Field | Type | Required | Description |
|---|---|---|---|---|
| `BroadcastInput` | `FromAgentID` | `string` | ✅ | Sending agent |
| `BroadcastInput` | `ToAgentID` | `string` | ✅ | Receiving agent |
| `BroadcastInput` | `MemoryID` | `string` | ✅ | Memory to share |
| `BroadcastOutput` | `SharedMemoryID` | `string` | — | `"shared_{memoryID}_to_{toAgentID}"` |

### Notes

- No-op when `FromAgentID == ToAgentID`.
- No-op when source Memory does not exist.
- `ProvenanceRef` on the copy is set to `"shared_from:{fromAgentID}/{memoryID}"`.

---

## Data Node Workers (Data Plane)

These workers operate at the DataPlane level and are not parameterised via
WorkerInput/WorkerOutput — they receive `dataplane.IngestRecord` /
`dataplane.SearchInput` directly.

| Worker | NodeType | Method | Description |
|---|---|---|---|
| `InMemoryDataNode` | `data_node` | `HandleIngest(IngestRecord)` | Writes an IngestRecord to the segment store |
| `InMemoryIndexNode` | `index_node` | `BuildIndex(IngestRecord)` | Builds keyword/attribute index for a record |
| `InMemoryQueryNode` | `query_node` | `Search(SearchInput) SearchOutput` | Executes a segment-level search |

---

## Adding a New Worker

1. Add a `NodeType` constant in `nodes/contracts.go`.
2. Define the interface in `nodes/contracts.go` with `Info() NodeInfo` + domain methods.
3. Add `*Input` and `*Output` structs in `schemas/worker_params.go` implementing `WorkerInput` / `WorkerOutput`.
4. Implement the worker in the appropriate subpackage (`cognitive/`, `coordination/`, `indexing/`, etc.) using the `Create*` constructor convention.
5. Register the worker in `app/bootstrap.go` via the relevant `Manager.Register*` method.
6. Wire the worker into the appropriate chain in `worker/chain/chain.go`.
7. Add the worker type to the expected set in `integration_tests/topology_test.go`.
