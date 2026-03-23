package schemas

// ─── AuditRecord ─────────────────────────────────────────────────────────────

// AuditRecord captures a single governance/access event for a memory object.
// All memory operations that change visibility, sharing, or lifecycle should
// emit one.  Records are append-only and keyed by TargetMemoryID.
type AuditRecord struct {
	RecordID            string `json:"record_id"`
	TargetMemoryID      string `json:"target_memory_id"`
	OperationType       string `json:"operation_type"` // see AuditOperationType
	ActorType           string `json:"actor_type"`     // "user" | "agent" | "system"
	ActorID             string `json:"actor_id"`
	PolicySnapshotID    string `json:"policy_snapshot_id,omitempty"`
	Decision            string `json:"decision"` // "allow" | "deny" | "redact"
	ReasonCode          string `json:"reason_code,omitempty"`
	Timestamp           string `json:"timestamp"`
	DownstreamRequestID string `json:"downstream_request_id,omitempty"`
}

// AuditOperationType enumerates the recordable memory governance actions.
type AuditOperationType string

const (
	AuditOpRead            AuditOperationType = "read"
	AuditOpWrite           AuditOperationType = "write"
	AuditOpShare           AuditOperationType = "share"
	AuditOpProject         AuditOperationType = "project"
	AuditOpArchive         AuditOperationType = "archive"
	AuditOpAlgorithmUpdate AuditOperationType = "algorithm_update"
	AuditOpPolicyChange    AuditOperationType = "policy_change"
	AuditOpDelete          AuditOperationType = "delete"
)

// ─── MemoryAlgorithmState ─────────────────────────────────────────────────────

// MemoryAlgorithmState holds algorithm-specific metadata for a single memory
// object.  Keyed by (MemoryID, AlgorithmID) so multiple algorithms can
// co-exist on the same memory without polluting the canonical Memory schema.
//
// SuggestedLifecycleState is the primary mechanism by which an algorithm
// requests a lifecycle transition on the target memory.  The dispatcher
// honours it verbatim — it never applies any transition of its own.
// Leave empty to make no lifecycle suggestion.
type MemoryAlgorithmState struct {
	MemoryID                string   `json:"memory_id"`
	AlgorithmID             string   `json:"algorithm_id"`
	Strength                float64  `json:"strength,omitempty"` // recall-based reinforcement weight
	LastRecalledAt          string   `json:"last_recalled_at,omitempty"`
	RecallCount             int      `json:"recall_count,omitempty"`
	RetentionScore          float64  `json:"retention_score,omitempty"`           // combined retention signal
	PortraitState           string   `json:"portrait_state,omitempty"`            // opaque serialised blob (profile/cluster)
	SummaryRefs             []string `json:"summary_refs,omitempty"`              // derived summary memory IDs
	SuggestedLifecycleState string   `json:"suggested_lifecycle_state,omitempty"` // non-empty → dispatcher applies this to the memory
	UpdatedAt               string   `json:"updated_at"`
}

// ─── MemoryView ───────────────────────────────────────────────────────────────

// MemoryView is the policy-conditioned, algorithm-processed output of the
// retrieval layer.  It is the only data structure upper-layer agents should
// consume; they must not read raw Memory objects or the underlying index directly.
type MemoryView struct {
	RequestID         string   `json:"request_id"`
	RequesterID       string   `json:"requester_id"`
	AgentID           string   `json:"agent_id"`
	ResolvedScope     string   `json:"resolved_scope"`
	VisibleMemoryRefs []string `json:"visible_memory_refs"`
	Payloads          []Memory `json:"payloads"`
	PolicyNotes       []string `json:"policy_notes,omitempty"`
	ProvenanceNotes   []string `json:"provenance_notes,omitempty"`
	AlgorithmNotes    []string `json:"algorithm_notes,omitempty"`
	ConstructionTrace []string `json:"construction_trace,omitempty"`
}

// ─── AccessGraphSnapshot ──────────────────────────────────────────────────────

// AccessGraphSnapshot is a point-in-time snapshot of the access graph for a
// given agent/session.  Used by the retrieval layer to determine memory
// visibility without re-querying the full relation store on every request.
type AccessGraphSnapshot struct {
	SnapshotID       string              `json:"snapshot_id"`
	AgentID          string              `json:"agent_id"`
	SessionID        string              `json:"session_id"`
	Timestamp        string              `json:"timestamp"`
	UserAgentACL     map[string][]string `json:"user_agent_acl,omitempty"`     // userID → []agentID
	AgentResourceACL map[string][]string `json:"agent_resource_acl,omitempty"` // agentID → []resourceID
	VisibleScopes    []string            `json:"visible_scopes,omitempty"`
}

// ─── Algorithm plugin supporting types ───────────────────────────────────────

// AlgorithmContext carries the execution context passed to a
// MemoryManagementAlgorithm on every call.
type AlgorithmContext struct {
	AgentID   string         `json:"agent_id"`
	SessionID string         `json:"session_id"`
	Timestamp string         `json:"timestamp"`
	Extra     map[string]any `json:"extra,omitempty"`
}

// ScoredMemory pairs a Memory with an algorithm-assigned relevance score.
type ScoredMemory struct {
	Memory Memory  `json:"memory"`
	Score  float64 `json:"score"`
	Signal string  `json:"signal,omitempty"` // e.g. "strength", "freshness", "recency"
}

// ─── MemoryManagementAlgorithm interface ──────────────────────────────────────

// MemoryManagementAlgorithm is the plugin interface for memory entity management
// algorithms (section 6.2 of memory management design).
//
// Each method is independently callable; a stub implementation that returns nil
// slices is valid.  Implementations should be stateless across calls and persist
// all algorithm-specific data via MemoryAlgorithmState.
//
// Example algorithm instances: MemoryBank (reinforcement + summary + profile),
// decay-only schedulers, graph-structure builders, multi-modal classifiers.
type MemoryManagementAlgorithm interface {
	// AlgorithmID returns a stable unique identifier for this algorithm.
	AlgorithmID() string

	// Ingest processes newly materialised memories and returns initial state records.
	Ingest(memories []Memory, ctx AlgorithmContext) []MemoryAlgorithmState

	// Update applies external signals (e.g. explicit recall feedback) to existing memories.
	Update(memories []Memory, signals map[string]float64) []MemoryAlgorithmState

	// Recall reranks or rescores candidate memories for a given query string.
	Recall(query string, candidates []Memory, ctx AlgorithmContext) []ScoredMemory

	// Compress merges or condenses a set of memories, returning derived objects.
	Compress(memories []Memory, ctx AlgorithmContext) []Memory

	// Decay applies time-based forgetting, returning updated algorithm states.
	Decay(memories []Memory, nowTS string) []MemoryAlgorithmState

	// Summarize produces summary Memory objects from a set of source memories.
	Summarize(memories []Memory, ctx AlgorithmContext) []Memory

	// ExportState returns the current algorithm state for the given memory ID.
	ExportState(memoryID string) (MemoryAlgorithmState, bool)

	// LoadState restores a previously exported state into the algorithm's internal store.
	LoadState(state MemoryAlgorithmState)
}
