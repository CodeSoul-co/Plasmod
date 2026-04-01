package schemas

type TimeWindow struct {
	From string `json:"from"`
	To   string `json:"to"`
}

const (
	ResponseModeStructuredEvidence = "structured_evidence"
	ResponseModeObjectsOnly        = "objects_only"
)

// ChainTraceSlots groups per-chain trace lines exposed on QueryResponse.
// On ingest, workers may attach full chain traces. On a standalone query,
// main / memory_pipeline / collaboration carry read-path summaries (the write
// chains are not re-executed); query is filled from QueryChain.
type ChainTraceSlots struct {
	Main           []string `json:"main"`
	MemoryPipeline []string `json:"memory_pipeline"`
	Query          []string `json:"query"`
	Collaboration  []string `json:"collaboration"`
}

type QueryRequest struct {
	QueryText           string     `json:"query_text"`
	QueryScope          string     `json:"query_scope"`
	SessionID           string     `json:"session_id"`
	AgentID             string     `json:"agent_id"`
	TenantID            string     `json:"tenant_id,omitempty"`
	WorkspaceID         string     `json:"workspace_id,omitempty"`
	TopK                int        `json:"top_k"`
	TimeWindow          TimeWindow `json:"time_window"`
	ObjectTypes         []string   `json:"object_types,omitempty"`
	MemoryTypes         []string   `json:"memory_types,omitempty"`
	EdgeTypes           []string   `json:"edge_types,omitempty"`
	RelationConstraints []string   `json:"relation_constraints"`
	ResponseMode        string     `json:"response_mode"`
	// IncludeCold extends retrieval to the cold/archived tier (S3 or in-memory cold store).
	IncludeCold bool `json:"include_cold,omitempty"`
	// TargetEmbeddingFamily routes query to a specific embedding family namespace.
	// When set, runtime should reject cross-family execution.
	TargetEmbeddingFamily string `json:"target_embedding_family,omitempty"`
	// TargetDim routes query to a specific embedding dimension.
	// When set (>0), runtime should reject cross-dimension execution.
	TargetDim int `json:"target_dim,omitempty"`
}

// EvidenceCacheStats summarizes pre-computed fragment lookups for the returned object IDs.
type EvidenceCacheStats struct {
	LookedUp   int `json:"looked_up"`
	Hits       int `json:"hits"`
	Misses     int `json:"misses"`
	ColdHits   int `json:"cold_hits,omitempty"`
	ColdMisses int `json:"cold_misses,omitempty"`
}

type QueryResponse struct {
	Objects        []string            `json:"objects"`
	Nodes          []GraphNode         `json:"nodes,omitempty"`
	Edges          []Edge              `json:"edges"`
	Provenance     []string            `json:"provenance"`
	Versions       []ObjectVersion     `json:"versions"`
	AppliedFilters []string            `json:"applied_filters"`
	ProofTrace     []ProofStep         `json:"proof_trace"`
	ChainTraces    ChainTraceSlots     `json:"chain_traces"`
	EvidenceCache  *EvidenceCacheStats `json:"evidence_cache,omitempty"`
	// RouteRejected indicates query was rejected by embedding family/dim routing guards.
	RouteRejected bool `json:"route_rejected,omitempty"`
	// RouteRejectReason provides machine-readable rejection reason.
	RouteRejectReason string `json:"route_reject_reason,omitempty"`
}

type GraphExpandRequest struct {
	QueryText       string     `json:"query_text,omitempty"`
	SeedObjectIDs   []string   `json:"seed_object_ids"`
	SeedObjectTypes []string   `json:"seed_object_types,omitempty"`
	SessionID       string     `json:"session_id,omitempty"`
	AgentID         string     `json:"agent_id,omitempty"`
	Hops            int        `json:"hops"`
	TimeWindow      TimeWindow `json:"time_window"`
	EdgeTypes       []string   `json:"edge_types,omitempty"`
	MaxNodes        int        `json:"max_nodes,omitempty"`
	MaxEdges        int        `json:"max_edges,omitempty"`
	IncludeProps    bool       `json:"include_props,omitempty"`
	NeedProvenance  bool       `json:"need_provenance,omitempty"`
	ResponseMode    string     `json:"response_mode,omitempty"`
}

type GraphExpandResponse struct {
	Subgraph       EvidenceSubgraph `json:"subgraph"`
	AppliedFilters []string         `json:"applied_filters,omitempty"`
}

type GraphExpander interface {
	Expand(req GraphExpandRequest) (GraphExpandResponse, error)
}
