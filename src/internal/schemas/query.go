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
// Main / memory_pipeline / collaboration run on ingest and are typically empty
// on a standalone query unless future versions attach session-scoped replay.
type ChainTraceSlots struct {
	Main             []string `json:"main"`
	MemoryPipeline   []string `json:"memory_pipeline"`
	Query            []string `json:"query"`
	Collaboration    []string `json:"collaboration"`
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
}

type QueryResponse struct {
	Objects        []string        `json:"objects"`
	Nodes          []GraphNode     `json:"nodes,omitempty"`
	Edges          []Edge          `json:"edges"`
	Provenance     []string        `json:"provenance"`
	Versions       []ObjectVersion `json:"versions"`
	AppliedFilters []string        `json:"applied_filters"`
	ProofTrace     []string        `json:"proof_trace"`
	ChainTraces    ChainTraceSlots `json:"chain_traces"`
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
