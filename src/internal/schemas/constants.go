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
	ObjectTypeAgent          ObjectType = "agent"
	ObjectTypeSession        ObjectType = "session"
	ObjectTypeEvent          ObjectType = "event"
	ObjectTypeMemory         ObjectType = "memory"
	ObjectTypeState          ObjectType = "state"
	ObjectTypeArtifact       ObjectType = "artifact"
	ObjectTypeEdge           ObjectType = "edge"
	ObjectTypeObjectVersion  ObjectType = "object_version"
	ObjectTypeUser           ObjectType = "user"
	ObjectTypePolicyRecord   ObjectType = "policy_record"
	ObjectTypeEmbedding      ObjectType = "embedding"
	ObjectTypeShareContract  ObjectType = "share_contract"
	ObjectTypeRetrievalSeg   ObjectType = "retrieval_segment"
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
