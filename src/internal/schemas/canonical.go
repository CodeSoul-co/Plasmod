package schemas

type Agent struct {
	AgentID             string   `json:"agent_id"`
	TenantID            string   `json:"tenant_id"`
	WorkspaceID         string   `json:"workspace_id"`
	AgentType           string   `json:"agent_type"`
	RoleProfile         string   `json:"role_profile"`
	PolicyRef           string   `json:"policy_ref"`
	CapabilitySet       []string `json:"capability_set"`
	DefaultMemoryPolicy string   `json:"default_memory_policy"`
	CreatedAt           string   `json:"created_at"`
	Status              string   `json:"status"`
}

type Session struct {
	SessionID       string `json:"session_id"`
	AgentID         string `json:"agent_id"`
	ParentSessionID string `json:"parent_session_id"`
	TaskType        string `json:"task_type"`
	Goal            string `json:"goal"`
	ContextRef      string `json:"context_ref"`
	StartTS         string `json:"start_ts"`
	EndTS           string `json:"end_ts"`
	Status          string `json:"status"`
	BudgetToken     int64  `json:"budget_token"`
	BudgetTimeMS    int64  `json:"budget_time_ms"`
}

type Event struct {
	EventID       string         `json:"event_id"`
	TenantID      string         `json:"tenant_id"`
	WorkspaceID   string         `json:"workspace_id"`
	AgentID       string         `json:"agent_id"`
	SessionID     string         `json:"session_id"`
	EventType     string         `json:"event_type"`
	EventTime     string         `json:"event_time"`
	IngestTime    string         `json:"ingest_time"`
	VisibleTime   string         `json:"visible_time"`
	LogicalTS     int64          `json:"logical_ts"`
	ParentEventID string         `json:"parent_event_id"`
	CausalRefs    []string       `json:"causal_refs"`
	Payload       map[string]any `json:"payload"`
	Source        string         `json:"source"`
	Importance    float64        `json:"importance"`
	Visibility    string         `json:"visibility"`
	Version       int64          `json:"version"`
}

// EventEnvelope keeps backward compatibility for legacy ingest package wiring.
type EventEnvelope = Event

type Memory struct {
	MemoryID       string   `json:"memory_id"`
	MemoryType     string   `json:"memory_type"`
	AgentID        string   `json:"agent_id"`
	SessionID      string   `json:"session_id"`
	OwnerType      string   `json:"owner_type"`
	Scope          string   `json:"scope"`
	Level          int      `json:"level"`
	Content        string   `json:"content"`
	Summary        string   `json:"summary"`
	SourceEventIDs []string `json:"source_event_ids"`
	Confidence     float64  `json:"confidence"`
	Importance     float64  `json:"importance"`
	FreshnessScore float64  `json:"freshness_score"`
	TTL            int64    `json:"ttl"`
	ValidFrom      string   `json:"valid_from"`
	ValidTo        string   `json:"valid_to"`
	ProvenanceRef  string   `json:"provenance_ref"`
	Version        int64    `json:"version"`
	IsActive       bool     `json:"is_active"`
}

type State struct {
	StateID            string `json:"state_id"`
	AgentID            string `json:"agent_id"`
	SessionID          string `json:"session_id"`
	StateType          string `json:"state_type"`
	StateKey           string `json:"state_key"`
	StateValue         string `json:"state_value"`
	DerivedFromEventID string `json:"derived_from_event_id"`
	CheckpointTS       string `json:"checkpoint_ts"`
	Version            int64  `json:"version"`
}

type Artifact struct {
	ArtifactID        string         `json:"artifact_id"`
	SessionID         string         `json:"session_id"`
	OwnerAgentID      string         `json:"owner_agent_id"`
	ArtifactType      string         `json:"artifact_type"`
	URI               string         `json:"uri"`
	ContentRef        string         `json:"content_ref"`
	MimeType          string         `json:"mime_type"`
	Metadata          map[string]any `json:"metadata"`
	Hash              string         `json:"hash"`
	ProducedByEventID string         `json:"produced_by_event_id"`
	Version           int64          `json:"version"`
}

type Edge struct {
	EdgeID        string  `json:"edge_id"`
	SrcObjectID   string  `json:"src_object_id"`
	SrcType       string  `json:"src_type"`
	EdgeType      string  `json:"edge_type"`
	DstObjectID   string  `json:"dst_object_id"`
	DstType       string  `json:"dst_type"`
	Weight        float64 `json:"weight"`
	ProvenanceRef string  `json:"provenance_ref"`
	CreatedTS     string  `json:"created_ts"`
}

type ObjectVersion struct {
	ObjectID        string `json:"object_id"`
	ObjectType      string `json:"object_type"`
	Version         int64  `json:"version"`
	MutationEventID string `json:"mutation_event_id"`
	ValidFrom       string `json:"valid_from"`
	ValidTo         string `json:"valid_to"`
	SnapshotTag     string `json:"snapshot_tag"`
}

// User defines a system user (model consumer, trainer, operator, etc.).
// Stored separately to allow future access-control and audit queries.
// It represents a human or service identity that can own or publish objects.
// It is intentionally minimal and may be extended as governance evolves.
type User struct {
	UserID            string `json:"user_id"`
	UserName          string `json:"user_name"`
	UserTenantID      string `json:"user_tenant_id"`
	UserWorkspaceID   string `json:"user_workspace_id"`
	DefaultVisibility string `json:"default_visibility"`
}

// Embedding stores a vector representation independently so other objects can
// reference it by vector_id without duplicating the dense representation.
// The actual vector payload may live in a vector store and be addressed by VectorRef.
type Embedding struct {
	VectorID      string `json:"vector_id"`
	VectorContext string `json:"vector_context"`
	OriginalText  string `json:"original_text"`
	EmbeddingType string `json:"embedding_type"`
	Dim           int64  `json:"dim"`
	ModelID       string `json:"model_id"`
	VectorRef     string `json:"vector_ref"`
	CreatedTS     string `json:"created_ts"`
}

// Policy is a minimal policy definition that an agent or system can reference.
// This is a canonical object-level policy descriptor (not a full decision log).
type Policy struct {
	PolicyID        string `json:"policy_id"`
	PolicyVersion   int64  `json:"policy_version"`
	PolicyStartTime string `json:"policy_start_time"`
	PolicyEndTime   string `json:"policy_end_time"`
	PublisherType   string `json:"publisher_type"`
	PublisherID     string `json:"publisher_id"`
	PolicyType      string `json:"policy_type"`
}

// PolicyRecord stores governance decisions applied to a specific object.
// All policy changes are append-only and auditable.
type PolicyRecord struct {
	PolicyID           string  `json:"policy_id"`
	PolicyVersion      int64   `json:"policy_version"`
	Context            string  `json:"context"`
	ObjectID           string  `json:"object_id"`
	ObjectType         string  `json:"object_type"`
	SalienceWeight     float64 `json:"salience_weight"`
	TTL                int64   `json:"ttl"`
	DecayFn            string  `json:"decay_fn"`
	ConfidenceOverride float64 `json:"confidence_override"`
	VerifiedState      string  `json:"verified_state"`
	QuarantineFlag     bool    `json:"quarantine_flag"`
	VisibilityPolicy   string  `json:"visibility_policy"`
	PolicyReason       string  `json:"policy_reason"`
	PolicySource       string  `json:"policy_source"`
	PolicyEventID      string  `json:"policy_event_id"`
}

// ShareContract encodes the full sharing protocol between agents or scopes,
// not just a single visibility field.
// It is used to make "shared" semantics explicit and auditable.
type ShareContract struct {
	ContractID       string `json:"contract_id"`
	Scope            string `json:"scope"`
	ReadACL          string `json:"read_acl"`
	WriteACL         string `json:"write_acl"`
	DeriveACL        string `json:"derive_acl"`
	TTLPolicy        string `json:"ttl_policy"`
	ConsistencyLevel string `json:"consistency_level"`
	MergePolicy      string `json:"merge_policy"`
	QuarantinePolicy string `json:"quarantine_policy"`
	AuditPolicy      string `json:"audit_policy"`
}

// RetrievalSegment tracks a physical segment in the materialized retrieval layer.
// Segments are the basic unit of search, scheduling, and buffering.
type RetrievalSegment struct {
	SegmentID       string `json:"segment_id"`
	ObjectType      string `json:"object_type"`
	Namespace       string `json:"namespace"`
	TimeBucket      string `json:"time_bucket"`
	EmbeddingFamily string `json:"embedding_family"`
	StorageRef      string `json:"storage_ref"`
	IndexRef        string `json:"index_ref"`
	RowCount        int    `json:"row_count"`
	MinTS           int64  `json:"min_ts"`
	MaxTS           int64  `json:"max_ts"`
	Tier            string `json:"tier"`
}
