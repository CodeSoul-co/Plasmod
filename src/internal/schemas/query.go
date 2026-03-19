package schemas

type TimeWindow struct {
	From string `json:"from"`
	To   string `json:"to"`
}

const (
	ResponseModeStructuredEvidence = "structured_evidence"
	ResponseModeObjectsOnly        = "objects_only"
)

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
	RelationConstraints []string   `json:"relation_constraints"`
	ResponseMode        string     `json:"response_mode"`
}

type QueryResponse struct {
	Objects        []string        `json:"objects"`
	Edges          []Edge          `json:"edges"`
	Provenance     []string        `json:"provenance"`
	Versions       []ObjectVersion `json:"versions"`
	AppliedFilters []string        `json:"applied_filters"`
	ProofTrace     []string        `json:"proof_trace"`
}
