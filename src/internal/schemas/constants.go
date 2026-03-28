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
	WorkerKindAlgorithmDispatch     WorkerKind = "algorithm_dispatch_worker"
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

// MemoryRelation EdgeType constants (section 4.3 of memory management design).
// These extend the existing EdgeType set with MAS-specific relation semantics.
const (
	EdgeTypeOwnedBy            EdgeType = "owned_by"
	EdgeTypeCreatedBy          EdgeType = "created_by"
	EdgeTypeObservedBy         EdgeType = "observed_by"
	EdgeTypeAccessibleBy       EdgeType = "accessible_by"
	EdgeTypeGroundedOnResource EdgeType = "grounded_on_resource"
	EdgeTypeProjectedFrom      EdgeType = "projected_from"
	EdgeTypeUsedInResponse     EdgeType = "used_in_response"
	EdgeTypeUpdatedByAlgorithm EdgeType = "updated_by_algorithm"
)

// MemoryLifecycle enumerates the management lifecycle states of a Memory object.
// Provides finer-grained control than the binary IsActive field.
type MemoryLifecycle string

const (
	MemoryLifecycleActive           MemoryLifecycle = "active"
	MemoryLifecycleCompressed       MemoryLifecycle = "compressed"
	MemoryLifecycleDecayed          MemoryLifecycle = "decayed"
	MemoryLifecycleArchived         MemoryLifecycle = "archived"
	MemoryLifecycleQuarantined      MemoryLifecycle = "quarantined"
	MemoryLifecycleHidden           MemoryLifecycle = "hidden"
	MemoryLifecycleDeletedLogically MemoryLifecycle = "deleted_logically"
)

// MemoryScope defines the circulation boundary of a memory object.
// Scope determines flow boundaries, not final visibility (which also depends on policy).
type MemoryScope string

const (
	MemoryScopePrivateUser      MemoryScope = "private_user"
	MemoryScopePrivateAgent     MemoryScope = "private_agent"
	MemoryScopeSessionLocal     MemoryScope = "session_local"
	MemoryScopeWorkspaceShared  MemoryScope = "workspace_shared"
	MemoryScopeTeamShared       MemoryScope = "team_shared"
	MemoryScopeGlobalShared     MemoryScope = "global_shared"
	MemoryScopeRestrictedShared MemoryScope = "restricted_shared"
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
	DefaultConfidence        float64 = 0.85
	DefaultEdgeWeight        float64 = 1.0
	DefaultCausalWeight      float64 = 0.8
	DefaultBatchSize                 = 32
	DefaultMaxProofDepth             = 8
	DefaultEvidenceCacheSize         = 10000 // evidence.Cache default when size <= 0
)

// ColdSearchWeights controls how cold-tier lexical / dense / recency signals
// are combined before fusion into the final ranked list.
type ColdSearchWeights struct {
	Lexical float64 // lexical/content match weight
	Dense   float64 // DFS / embedding similarity weight
	Recency float64 // recency bonus weight
}

// AlgorithmConfig holds all tunable algorithm parameters.
// All fields have sensible defaults via DefaultAlgorithmConfig().
// Pass a customized instance to service constructors that accept it
// (e.g. NewPreComputeServiceWithConfig) to override defaults.
// ColdSearchWeights controls how cold-tier lexical / dense / recency signals
// are combined before fusion into the final ranked list.

// AlgorithmConfig holds all tunable algorithm parameters.
// All fields have sensible defaults via DefaultAlgorithmConfig().
// Pass a customized instance to service constructors that accept it
// (e.g. NewPreComputeServiceWithConfig) to override defaults.
type AlgorithmConfig struct {
	// ProofTrace
	MaxProofDepth int // BFS depth cap in proof trace (default 8)

	// EvidenceCache
	EvidenceCacheSize int // max entries in evidence fragment cache (default 10000)

	// PreComputeService — salience scoring
	TokenCountThreshold   int     // token count required for bonus (default 10)
	TokenBonus            float64 // salience bonus when len(tokens) > TokenCountThreshold (default 0.1)
	CausalRefBonus        float64 // salience bonus when CausalRefs non-empty (default 0.1)
	GlobalVisibilityBonus float64 // salience bonus for global-visibility objects (default 0.2)
	SalienceCap           float64 // upper bound on salience score (default 1.0)
	DefaultImportance     float64 // base salience when event.Importance == 0 (default 0.5)

	// TieredObjectStore
	HotTierSalienceThreshold float64 // minimum salience to promote to hot cache (default 0.5)

	// Embedding / hybrid retrieval
	EmbeddingDim int // default embedding dimension (default 256)

	// RRF fusion
	RRFK int // reciprocal-rank-fusion k constant (default 60)

	// HNSW / vector retrieval
	HNSWM             int // HNSW M parameter (default 16)
	HNSEfConstruction int // HNSW efConstruction (default 256)
	HNSEfSearch       int // HNSW efSearch (default 64)

	// DFS / cold-tier retrieval
	DFSRelevanceThreshold float64           // minimum dense relevance to keep a cold-tier DFS hit (default 0.2)
	ColdSearchWeights     ColdSearchWeights // weighting of lexical/dense/recency signals in cold tier
}

// DefaultAlgorithmConfig returns a AlgorithmConfig populated with all defaults.
// Use this as the baseline; override specific fields before passing to constructors.
func DefaultAlgorithmConfig() AlgorithmConfig {
	return AlgorithmConfig{
		MaxProofDepth:            8,
		EvidenceCacheSize:        10000,
		TokenCountThreshold:      10,
		TokenBonus:               0.1,
		CausalRefBonus:           0.1,
		GlobalVisibilityBonus:    0.2,
		SalienceCap:              1.0,
		DefaultImportance:        0.5,
		HotTierSalienceThreshold: 0.5,

		EmbeddingDim: 256,
		RRFK:         60,

		HNSWM:             16,
		HNSEfConstruction: 256,
		HNSEfSearch:       64,

		DFSRelevanceThreshold: 0.2,
		ColdSearchWeights: ColdSearchWeights{
			Lexical: 0.5,
			Dense:   0.4,
			Recency: 0.1,
		},
	}
}

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
