package schemas

// MemoryType classifies the kind of knowledge a memory unit represents.
type MemoryType string

const (
	MemoryTypeEpisodic   MemoryType = "episodic"
	MemoryTypeSemantic   MemoryType = "semantic"
	MemoryTypeProcedural MemoryType = "procedural"
	MemoryTypeSocial     MemoryType = "social"
	MemoryTypeReflective MemoryType = "reflective"
)

// EventType enumerates the well-known event kinds produced by agents.
type EventType string

const (
	EventTypeUserMessage          EventType = "user_message"
	EventTypeAssistantMessage     EventType = "assistant_message"
	EventTypeToolCallIssued       EventType = "tool_call_issued"
	EventTypeToolResultReturned   EventType = "tool_result_returned"
	EventTypeRetrievalExecuted    EventType = "retrieval_executed"
	EventTypeMemoryWriteRequested EventType = "memory_write_requested"
	EventTypeMemoryConsolidated   EventType = "memory_consolidated"
	EventTypePlanUpdated          EventType = "plan_updated"
	EventTypeCritiqueGenerated    EventType = "critique_generated"
	EventTypeTaskFinished         EventType = "task_finished"
	EventTypeHandoffOccurred      EventType = "handoff_occurred"
)

// EdgeType enumerates the relation types used in the graph/relation index.
type EdgeType string

const (
	EdgeTypeCausedBy      EdgeType = "caused_by"
	EdgeTypeDerivedFrom   EdgeType = "derived_from"
	EdgeTypeSupports      EdgeType = "supports"
	EdgeTypeContradicts   EdgeType = "contradicts"
	EdgeTypeSummarizes    EdgeType = "summarizes"
	EdgeTypeUpdates       EdgeType = "updates"
	EdgeTypeUsesTool      EdgeType = "uses_tool"
	EdgeTypeBelongsToTask EdgeType = "belongs_to_task"
	EdgeTypeSharedWith    EdgeType = "shared_with"
)

// ObjectType enumerates the canonical first-class object kinds.
type ObjectType string

const (
	ObjectTypeAgent         ObjectType = "agent"
	ObjectTypeSession       ObjectType = "session"
	ObjectTypeEvent         ObjectType = "event"
	ObjectTypeMemory        ObjectType = "memory"
	ObjectTypeState         ObjectType = "state"
	ObjectTypeArtifact      ObjectType = "artifact"
	ObjectTypeEdge          ObjectType = "edge"
	ObjectTypeObjectVersion ObjectType = "object_version"
	ObjectTypeUser          ObjectType = "user"
	ObjectTypePolicyRecord  ObjectType = "policy_record"
	ObjectTypeEmbedding     ObjectType = "embedding"
	ObjectTypeShareContract ObjectType = "share_contract"
	ObjectTypeRetrievalSeg  ObjectType = "retrieval_segment"
)

// VerifiedState captures the epistemic status of a memory or policy record.
type VerifiedState string

const (
	VerifiedStateUnverified VerifiedState = "unverified"
	VerifiedStateVerified   VerifiedState = "verified"
	VerifiedStateRetracted  VerifiedState = "retracted"
)

// VisibilityScope controls who can read an object.
type VisibilityScope string

const (
	VisibilityPrivate   VisibilityScope = "private"
	VisibilitySession   VisibilityScope = "session"
	VisibilityWorkspace VisibilityScope = "workspace"
	VisibilityTenant    VisibilityScope = "tenant"
	VisibilityPublic    VisibilityScope = "public"
)

// Tier classifies storage hotness for retrieval segments.
type Tier string

const (
	TierHot  Tier = "hot"
	TierWarm Tier = "warm"
	TierCold Tier = "cold"
)

// AgentStatus enumerates lifecycle states of an agent.
type AgentStatus string

const (
	AgentStatusActive   AgentStatus = "active"
	AgentStatusInactive AgentStatus = "inactive"
	AgentStatusArchived AgentStatus = "archived"
)

// SessionStatus enumerates lifecycle states of a session.
type SessionStatus string

const (
	SessionStatusRunning   SessionStatus = "running"
	SessionStatusCompleted SessionStatus = "completed"
	SessionStatusFailed    SessionStatus = "failed"
	SessionStatusPaused    SessionStatus = "paused"
)

// WorkerKind identifies which worker type a WorkerInput targets.
// Values are intentionally identical to the NodeType constants in
// worker/nodes so WorkerInput.WorkerKind() can be compared to node
// registry keys without creating an import cycle.
type WorkerKind string

const (
	WorkerKindIngest                WorkerKind = "ingest_worker"
	WorkerKindObjectMaterialization WorkerKind = "object_materialization_worker"
	WorkerKindStateMaterialization  WorkerKind = "state_materialization_worker"
	WorkerKindToolTrace             WorkerKind = "tool_trace_worker"
	WorkerKindMemoryExtraction      WorkerKind = "memory_extraction_worker"
	WorkerKindMemoryConsolidation   WorkerKind = "memory_consolidation_worker"
	WorkerKindSummarization         WorkerKind = "summarization_worker"
	WorkerKindReflectionPolicy      WorkerKind = "reflection_policy_worker"
	WorkerKindIndexBuild            WorkerKind = "index_build_worker"
	WorkerKindGraphRelation         WorkerKind = "graph_relation_worker"
	WorkerKindSubgraphExecutor      WorkerKind = "subgraph_executor_worker"
	WorkerKindConflictMerge         WorkerKind = "conflict_merge_worker"
	WorkerKindCommunication         WorkerKind = "communication_worker"
	WorkerKindMicroBatch            WorkerKind = "micro_batch_scheduler"
	WorkerKindProofTrace            WorkerKind = "proof_trace_worker"
)

// Additional EventType constants used for routing in the worker and subscriber layers.
//
// Naming convention:
//   - EventTypeToolCallIssued / EventTypeToolResultReturned (defined above) are the
//     formal canonical event names used in schema documentation and the WAL.
//   - EventTypeToolCall / EventTypeToolResult (below) are the shorter runtime aliases
//     that agents and the SDK actually emit.  The subscriber and ObjectMaterialization /
//     ToolTrace workers check for these shorter values.
//   - Do NOT mix the two sets: when producing events always use the runtime aliases;
//     when matching against canonical schema use the formal names.
//   - EventTypeStateUpdate is the value emitted by agents; EventTypeStateChange is the
//     internal routing alias used by StateMaterializationWorker.
const (
	EventTypeToolCall    EventType = "tool_call"
	EventTypeToolResult  EventType = "tool_result"
	EventTypeStateUpdate EventType = "state_update"
	EventTypeStateChange EventType = "state_change"
	EventTypeCheckpoint  EventType = "checkpoint"
	EventTypeReflection  EventType = "reflection"
)

// MemoryTypeFactual classifies knowledge derived from tool outputs or retrieved facts.
const MemoryTypeFactual MemoryType = "factual"

// Additional EdgeType constants.
const (
	EdgeTypeConflictResolved EdgeType = "conflict_resolved"
	EdgeTypeToolProduces     EdgeType = "tool_produces"
	EdgeTypeBelongsToSession EdgeType = "belongs_to_session"
	EdgeTypeOwnedByAgent     EdgeType = "owned_by_agent"
)

// IDPrefix constants for deterministic canonical object ID generation.
// All generated IDs follow: IDPrefixXxx + primaryKey.
const (
	IDPrefixMemory    = "mem_"
	IDPrefixArtifact  = "art_"
	IDPrefixState     = "state_"
	IDPrefixEdge      = "edge_"
	IDPrefixSegment   = "seg_"
	IDPrefixToolTrace = "tool_trace_"
	IDPrefixSummary   = "summary_"
	IDPrefixShared    = "shared_"
)

// PayloadKey constants for well-known fields embedded in Event.Payload.
const (
	PayloadKeyText       = "text"
	PayloadKeyStateKey   = "state_key"
	PayloadKeyStateValue = "state_value"
	PayloadKeyURI        = "uri"
	PayloadKeyMimeType   = "mime_type"
)

// Numeric defaults shared across worker implementations.
const (
	DefaultConfidence    float64 = 0.85
	DefaultEdgeWeight    float64 = 1.0
	DefaultCausalWeight  float64 = 0.8
	DefaultBatchSize             = 32
	DefaultMaxProofDepth         = 8
)

// TimeBucketFormat is the Go time format string used for segment time-bucket partitioning.
const TimeBucketFormat = "2006-01-02"

// ArtifactType classifies what kind of artifact a record represents.
type ArtifactType string

const (
	ArtifactTypeToolTrace  ArtifactType = "tool_trace"
	ArtifactTypeToolCall   ArtifactType = "tool_call"
	ArtifactTypeToolResult ArtifactType = "tool_result"
)

// MimeType constants for artifact content encoding.
const (
	MimeTypeJSON      = "application/json"
	MimeTypePlainText = "text/plain"
)

// Metadata key constants embedded in Artifact.Metadata maps.
const (
	EventIDKey   = "event_id"
	AgentIDKey   = "agent_id"
	SessionIDKey = "session_id"
)
