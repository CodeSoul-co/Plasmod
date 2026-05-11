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
	DatasetName         string     `json:"dataset_name,omitempty"`
	SourceFileName      string     `json:"source_file_name,omitempty"`
	ImportBatchID       string     `json:"import_batch_id,omitempty"`
	LatestBatchOnly     bool       `json:"latest_batch_only,omitempty"`
	// WarmSegmentID enables direct ANN query against a prebuilt warm segment.
	WarmSegmentID       string     `json:"warm_segment_id,omitempty"`
	// IncludeCold extends retrieval to the cold/archived tier (S3 or in-memory cold store).
	IncludeCold bool `json:"include_cold,omitempty"`
	// EmbeddingVector is a precomputed search vector. When non-nil, the
	// retrieval plane uses it directly instead of calling the embedder,
	// bypassing ONNX/TF-IDF for the embedding step.
	EmbeddingVector []float32 `json:"embedding_vector,omitempty"`
}

// EvidenceCacheStats summarizes pre-computed fragment lookups for the returned object IDs.
type EvidenceCacheStats struct {
	LookedUp   int `json:"looked_up"`
	Hits       int `json:"hits"`
	Misses     int `json:"misses"`
	ColdHits   int `json:"cold_hits,omitempty"`
	ColdMisses int `json:"cold_misses,omitempty"`
}

// RetrievalSummary exposes experiment-oriented retrieval metadata so benchmark
// runners can inspect cold-tier usage without scraping logs or proof traces.
type RetrievalSummary struct {
	Tier               string `json:"tier,omitempty"`
	ColdSearchMode     string `json:"cold_search_mode,omitempty"`
	ColdCandidateCount int    `json:"cold_candidate_count,omitempty"`
	ColdTierRequested  bool   `json:"cold_tier_requested,omitempty"`
	ColdUsedFallback   bool   `json:"cold_used_fallback,omitempty"`
	RetrievalHits      int    `json:"retrieval_hits,omitempty"`
	CanonicalAdds      int    `json:"canonical_adds,omitempty"`
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
	Retrieval      *RetrievalSummary   `json:"retrieval,omitempty"`
	// QueryStatus classifies retrieval-plane seed hits (distinct from supplemental canonical IDs).
	//   ok — retrieval returned at least one candidate before canonical supplement.
	//   no_retrieval_hits — zero retrieval seeds and empty objects list.
	//   no_retrieval_hits_supplemented — zero retrieval seeds but objects came from event/state/artifact listing.
	QueryStatus string `json:"query_status,omitempty"`
	// QueryHint is a short human-readable explanation for demos and UIs (may be localized).
	QueryHint string `json:"query_hint,omitempty"`
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
