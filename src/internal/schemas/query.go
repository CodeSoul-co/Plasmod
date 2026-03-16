package schemas

type TimeWindow struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type QueryRequest struct {
	QueryText           string     `json:"query_text"`
	QueryScope          string     `json:"query_scope"`
	SessionID           string     `json:"session_id"`
	AgentID             string     `json:"agent_id"`
	TopK                int        `json:"top_k"`
	TimeWindow          TimeWindow `json:"time_window"`
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
