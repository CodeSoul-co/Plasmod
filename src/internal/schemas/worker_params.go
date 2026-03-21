package schemas

// worker_params.go — typed input / output contracts for every worker in the
// cognitive dataflow pipeline.
//
// These types live in the schemas package so any layer (worker, chain,
// coordinator, gateway) can import them without import cycles.
//
// Extensibility pattern:
//   - Each worker has exactly one Input type and one Output type.
//   - Both implement WorkerInput / WorkerOutput so custom worker implementations
//     can be dispatched through the same path without modifying contracts.go.
//   - The zero value of every Output type is a valid "no-op" result.
//   - IsEmpty() returns true when the worker produced no observable side-effects.

// ─── Generic extensibility interfaces ────────────────────────────────────────

// WorkerInput marks a struct as the typed input to a single worker operation.
// WorkerKind returns the WorkerKind constant that identifies the target worker.
// Values match the NodeType constants in worker/nodes to allow cycle-free comparison.
type WorkerInput interface {
	WorkerKind() WorkerKind
}

// WorkerOutput marks a struct as the typed result of a single worker operation.
type WorkerOutput interface {
	IsEmpty() bool
}

// ─── L2  IngestWorker ────────────────────────────────────────────────────────

// IngestInput carries a raw Event for schema validation and field normalisation.
// The worker does NOT write to the WAL; Runtime.SubmitIngest owns that step.
type IngestInput struct {
	Event Event `json:"event"`
}

func (IngestInput) WorkerKind() WorkerKind { return WorkerKindIngest }

// IngestOutput reports the validation outcome.
type IngestOutput struct {
	Valid bool   `json:"valid"`
	Error string `json:"error,omitempty"`
}

func (o IngestOutput) IsEmpty() bool { return !o.Valid && o.Error == "" }

// ─── L3  ObjectMaterializationWorker ─────────────────────────────────────────

// ObjectMaterializationInput routes a raw Event to the correct canonical store:
//
//	"tool_call"|"tool_result"                    → Artifact
//	"state_update"|"state_change"|"checkpoint"   → State
//	anything else                                → Memory (level-0 episodic)
type ObjectMaterializationInput struct {
	Event Event `json:"event"`
}

func (ObjectMaterializationInput) WorkerKind() WorkerKind { return WorkerKindObjectMaterialization }

// ObjectMaterializationOutput identifies the produced canonical object.
type ObjectMaterializationOutput struct {
	ObjectID   string `json:"object_id,omitempty"`
	ObjectType string `json:"object_type,omitempty"` // "memory" | "state" | "artifact"
}

func (o ObjectMaterializationOutput) IsEmpty() bool { return o.ObjectID == "" }

// ─── L3  StateMaterializationWorker — Apply ──────────────────────────────────

// StateApplyInput applies a state-mutating Event to the running State key map.
// state_key and state_value are extracted from ev.Payload.
type StateApplyInput struct {
	Event Event `json:"event"`
}

func (StateApplyInput) WorkerKind() WorkerKind { return WorkerKindStateMaterialization }

// StateApplyOutput returns the State identifier and its new version.
// StateID is empty when the payload carries no state_key.
type StateApplyOutput struct {
	StateID string `json:"state_id,omitempty"`
	Version int64  `json:"version"`
}

func (o StateApplyOutput) IsEmpty() bool { return o.StateID == "" }

// ─── L3  StateMaterializationWorker — Checkpoint ─────────────────────────────

// StateCheckpointInput triggers an ObjectVersion snapshot for every active
// State of the given agent+session pair.
type StateCheckpointInput struct {
	AgentID   string `json:"agent_id"`
	SessionID string `json:"session_id"`
}

func (StateCheckpointInput) WorkerKind() WorkerKind { return WorkerKindStateMaterialization }

// StateCheckpointOutput reports how many ObjectVersion snapshots were written.
type StateCheckpointOutput struct {
	SnapshotCount int    `json:"snapshot_count"`
	SnapshotTag   string `json:"snapshot_tag,omitempty"`
}

func (o StateCheckpointOutput) IsEmpty() bool { return o.SnapshotCount == 0 }

// ─── L3  ToolTraceWorker ─────────────────────────────────────────────────────

// ToolTraceInput triggers recording of a tool_call / tool_result event as a
// structured Artifact plus an optional DerivationLog entry.
// Events with other event_type values are silently ignored.
type ToolTraceInput struct {
	Event Event `json:"event"`
}

func (ToolTraceInput) WorkerKind() WorkerKind { return WorkerKindToolTrace }

// ToolTraceOutput identifies the produced Artifact and whether a derivation
// entry was appended to the log.
type ToolTraceOutput struct {
	ArtifactID       string `json:"artifact_id,omitempty"`
	DerivationLogged bool   `json:"derivation_logged"`
}

func (o ToolTraceOutput) IsEmpty() bool { return o.ArtifactID == "" }

// ─── L4  MemoryExtractionWorker ──────────────────────────────────────────────

// MemoryExtractionInput carries the minimal fields needed to derive a level-0
// episodic Memory object.
type MemoryExtractionInput struct {
	EventID   string `json:"event_id"`
	AgentID   string `json:"agent_id"`
	SessionID string `json:"session_id"`
	Content   string `json:"content"`
}

func (MemoryExtractionInput) WorkerKind() WorkerKind { return WorkerKindMemoryExtraction }

// MemoryExtractionOutput identifies the produced Memory.
type MemoryExtractionOutput struct {
	MemoryID string `json:"memory_id,omitempty"`
}

func (o MemoryExtractionOutput) IsEmpty() bool { return o.MemoryID == "" }

// ─── L4  MemoryConsolidationWorker ───────────────────────────────────────────

// MemoryConsolidationInput triggers level-0 → level-1 consolidation for the
// given agent+session pair.
type MemoryConsolidationInput struct {
	AgentID   string `json:"agent_id"`
	SessionID string `json:"session_id"`
}

func (MemoryConsolidationInput) WorkerKind() WorkerKind { return WorkerKindMemoryConsolidation }

// MemoryConsolidationOutput reports the produced summary Memory.
// SummaryID is empty when no active level-0 memories existed.
type MemoryConsolidationOutput struct {
	SummaryID   string `json:"summary_id,omitempty"`
	SourceCount int    `json:"source_count"`
}

func (o MemoryConsolidationOutput) IsEmpty() bool { return o.SummaryID == "" }

// ─── L4  SummarizationWorker ─────────────────────────────────────────────────

// SummarizationInput triggers multi-level memory compression.
//
//	MaxLevel=1: level-0 → level-1 (semantic,    MemoryType "semantic")
//	MaxLevel=2: level-1 → level-2 (abstraction, MemoryType "procedural")
//
// MaxLevel is clamped to [1,2]. Requires ≥2 source memories per level.
type SummarizationInput struct {
	AgentID   string `json:"agent_id"`
	SessionID string `json:"session_id"`
	MaxLevel  int    `json:"max_level"` // 1 or 2
}

func (SummarizationInput) WorkerKind() WorkerKind { return WorkerKindSummarization }

// SummarizationOutput lists the MemoryIDs of newly produced summary objects.
type SummarizationOutput struct {
	ProducedIDs []string `json:"produced_ids,omitempty"`
}

func (o SummarizationOutput) IsEmpty() bool { return len(o.ProducedIDs) == 0 }

// ─── L4  ReflectionPolicyWorker ──────────────────────────────────────────────

// ReflectionPolicyInput targets a single canonical object for governance
// evaluation.  Only ObjectType "memory" is handled in v1; others are ignored.
//
// Rules evaluated (in order):
//  1. Quarantine        — IsActive=false if Policy.QuarantineFlag
//  2. TTL expiry        — IsActive=false if age > Policy.TTL seconds
//  3. Confidence override — Memory.Confidence = Policy.ConfidenceOverride
//  4. Salience decay    — Memory.Importance *= Policy.SalienceWeight
type ReflectionPolicyInput struct {
	ObjectID   string `json:"object_id"`
	ObjectType string `json:"object_type"` // "memory" (only supported value in v1)
}

func (ReflectionPolicyInput) WorkerKind() WorkerKind { return WorkerKindReflectionPolicy }

// ReflectionPolicyOutput summarises the governance rules applied.
// AppliedRules entries: "quarantined", "ttl_expired",
// "confidence_overridden", "salience_decayed".
type ReflectionPolicyOutput struct {
	Modified     bool     `json:"modified"`
	AppliedRules []string `json:"applied_rules,omitempty"`
}

func (o ReflectionPolicyOutput) IsEmpty() bool { return !o.Modified }

// ─── L1  IndexBuildWorker ─────────────────────────────────────────────────────

// IndexBuildInput submits a materialised object to the SegmentStore and
// IndexStore for keyword and attribute retrieval.
// Text is optional; when provided it is used for full-text indexing.
// Namespace partitions segments; objects in different namespaces do not mix.
type IndexBuildInput struct {
	ObjectID   string `json:"object_id"`
	ObjectType string `json:"object_type"`
	Namespace  string `json:"namespace"`
	Text       string `json:"text,omitempty"`
}

func (IndexBuildInput) WorkerKind() WorkerKind { return WorkerKindIndexBuild }

// IndexBuildOutput reports the segment assigned and the cumulative indexed count.
type IndexBuildOutput struct {
	SegmentID    string `json:"segment_id,omitempty"`
	IndexedCount int    `json:"indexed_count"`
}

func (o IndexBuildOutput) IsEmpty() bool { return o.SegmentID == "" }

// ─── L5  GraphRelationWorker ──────────────────────────────────────────────────

// GraphRelationInput writes a single typed, weighted edge into GraphEdgeStore.
// Common EdgeType values: "derived_from", "conflict_resolved",
// "shared_from", "tool_produces", "references".
type GraphRelationInput struct {
	SrcID    string  `json:"src_id"`
	SrcType  string  `json:"src_type"`
	DstID    string  `json:"dst_id"`
	DstType  string  `json:"dst_type"`
	EdgeType string  `json:"edge_type"`
	Weight   float64 `json:"weight"`
}

func (GraphRelationInput) WorkerKind() WorkerKind { return WorkerKindGraphRelation }

// GraphRelationOutput identifies the produced Edge.
// EdgeID is derived as "edge_{srcID}_{edgeType}_{dstID}".
type GraphRelationOutput struct {
	EdgeID string `json:"edge_id,omitempty"`
}

func (o GraphRelationOutput) IsEmpty() bool { return o.EdgeID == "" }

// ─── L5  SubgraphExecutorWorker ───────────────────────────────────────────────

// SubgraphExpandInput bundles the graph context for SubgraphExecutorWorker.Expand.
//
// IMPORTANT: Nodes and Edges must be pre-fetched by the caller (e.g. via
// GraphEdgeStore.BulkEdges) before being supplied here.  The worker only
// applies schemas.ExpandFromRequest and does not perform storage reads.
type SubgraphExpandInput struct {
	Req   GraphExpandRequest `json:"req"`
	Nodes []GraphNode        `json:"nodes,omitempty"`
	Edges []Edge             `json:"edges,omitempty"`
}

func (SubgraphExpandInput) WorkerKind() WorkerKind { return WorkerKindSubgraphExecutor }

// SubgraphExpandOutput wraps GraphExpandResponse to satisfy WorkerOutput.
type SubgraphExpandOutput struct {
	GraphExpandResponse
}

func (o SubgraphExpandOutput) IsEmpty() bool {
	return len(o.Subgraph.Nodes) == 0 && len(o.Subgraph.Edges) == 0
}

// ─── L6  ConflictMergeWorker ──────────────────────────────────────────────────

// ConflictMergeInput submits two competing Memory objects for conflict
// resolution via last-writer-wins (higher Version survives).
//
// Preconditions enforced by the worker:
//   - ObjectType == "memory"
//   - LeftID != RightID
//   - Both objects share AgentID and SessionID
//   - Both objects are IsActive=true
type ConflictMergeInput struct {
	LeftID     string `json:"left_id"`
	RightID    string `json:"right_id"`
	ObjectType string `json:"object_type"` // "memory" in v1
}

func (ConflictMergeInput) WorkerKind() WorkerKind { return WorkerKindConflictMerge }

// ConflictMergeOutput reports the resolution outcome.
// WinnerID is empty when preconditions were not met.
type ConflictMergeOutput struct {
	WinnerID string `json:"winner_id,omitempty"`
	LoserID  string `json:"loser_id,omitempty"`
	Resolved bool   `json:"resolved"`
}

func (o ConflictMergeOutput) IsEmpty() bool { return !o.Resolved }

// ─── L7  ProofTraceWorker ─────────────────────────────────────────────────────

// ProofTraceInput specifies seed objects and BFS traversal depth.
//
//	MaxDepth <= 0  → internal default cap of 8.
//	MaxDepth = 1   → immediate edges only (legacy single-hop).
//
// DerivationLog entries for seed objects are appended after the BFS walk when
// a DerivationLogger is wired into the worker.
type ProofTraceInput struct {
	ObjectIDs []string `json:"object_ids"`
	MaxDepth  int      `json:"max_depth,omitempty"`
}

func (ProofTraceInput) WorkerKind() WorkerKind { return WorkerKindProofTrace }

// ProofTraceOutput carries assembled trace steps and the BFS hop count.
//
// Step formats:
//
//	"[d=N] {src} -[{edgeType}]-> {dst} (w={weight:.2f})"
//	"derivation: {srcID}({srcType}) -[{op}]-> {derivedID}({derivedType})"
type ProofTraceOutput struct {
	Steps    []string `json:"steps"`
	HopCount int      `json:"hop_count"`
}

func (o ProofTraceOutput) IsEmpty() bool { return len(o.Steps) == 0 }

// ─── L7  MicroBatchScheduler ─────────────────────────────────────────────────

// MicroBatchEnqueueInput enqueues a single payload for deferred batch
// processing.  QueryID is informational; Payload is opaque to the scheduler.
// The scheduler drains when Flush() is called or batchSize (default 32) is reached.
type MicroBatchEnqueueInput struct {
	QueryID string `json:"query_id"`
	Payload any    `json:"payload"`
}

func (MicroBatchEnqueueInput) WorkerKind() WorkerKind { return WorkerKindMicroBatch }

// MicroBatchFlushOutput carries all items drained from the batch queue (FIFO).
type MicroBatchFlushOutput struct {
	Items []any `json:"items"`
	Count int   `json:"count"`
}

func (o MicroBatchFlushOutput) IsEmpty() bool { return o.Count == 0 }

// ─── L8  CommunicationWorker ──────────────────────────────────────────────────

// BroadcastInput copies a Memory object into a target agent's memory space.
// SharedMemoryID is derived as "shared_{memoryID}_to_{toAgentID}".
// ProvenanceRef is set to "shared_from:{fromAgentID}/{memoryID}".
// No-op when FromAgentID == ToAgentID or the source Memory does not exist.
type BroadcastInput struct {
	FromAgentID string `json:"from_agent_id"`
	ToAgentID   string `json:"to_agent_id"`
	MemoryID    string `json:"memory_id"`
}

func (BroadcastInput) WorkerKind() WorkerKind { return WorkerKindCommunication }

// BroadcastOutput identifies the shared copy of the Memory.
// SharedMemoryID is empty when the no-op condition was hit.
type BroadcastOutput struct {
	SharedMemoryID string `json:"shared_memory_id,omitempty"`
}

func (o BroadcastOutput) IsEmpty() bool { return o.SharedMemoryID == "" }
