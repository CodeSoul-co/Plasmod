// Package retrieval provides the Go retrieval engine for CogDB.
//
// This package replaces the Python retrieval service
// (src/internal/retrieval/service/) with a native Go implementation that
// integrates directly into the Go server without a separate process or pybind11.
//
// Architecture:
//
//	Retriever.Retrieve(RetrievalRequest)
//	    ├── TieredDataPlane.Search  → ranked ObjectIDs (lexical + vector via CGO)
//	    ├── ObjectStore.GetMemory   → fetch metadata for each candidate
//	    ├── SafetyFilter.Apply      → 7 governance rules (quarantine/TTL/active/…)
//	    ├── scoreAndRank            → RRF reranking × importance × freshness × confidence
//	    ├── markSeeds               → final_score ≥ seedThreshold → IsSeed=true
//	    └── CandidateList           → typed result consumed by QueryChain
package retrieval

import "time"

// ─── Request ──────────────────────────────────────────────────────────────────

// RetrievalRequest mirrors the Python RetrievalRequest and proto/retrieval.proto.
// It is the single contract between the caller (Runtime.ExecuteQuery) and the
// retrieval engine.
type RetrievalRequest struct {
	QueryID   string // Unique query ID for tracing
	QueryText string // Natural language query (used for lexical + vector embedding)

	// Isolation
	TenantID    string
	WorkspaceID string

	// Filters
	AgentID     string
	SessionID   string
	Scope       string // "private" | "session" | "workspace" | "global"
	MemoryTypes []string
	ObjectTypes []string

	// Retrieval control
	TopK          int
	MinConfidence float64
	MinImportance float64

	// Time constraints
	TimeFrom time.Time // zero = no constraint
	TimeTo   time.Time // zero = no constraint

	// Version / time-travel constraints
	AsOfTS     time.Time // zero = no constraint; only include records visible at this time
	MinVersion int64     // 0 = no constraint

	// Policy
	ExcludeQuarantined bool // default true
	ExcludeUnverified  bool

	// Search path switches
	EnableDense      bool // default true
	EnableSparse     bool // default true
	EnableFilterOnly bool // skip dense+sparse, rank by importance only

	// Graph expansion (for QueryChain / SubgraphExecutorWorker)
	ForGraph bool // when true: return TopK*2 candidates, include SourceEventIDs
}

// DefaultRetrievalRequest returns a RetrievalRequest with sensible defaults.
func DefaultRetrievalRequest(queryText string, topK int) RetrievalRequest {
	return RetrievalRequest{
		QueryText:          queryText,
		TopK:               topK,
		ExcludeQuarantined: true,
		EnableDense:        true,
		EnableSparse:       true,
	}
}

// ─── Candidate ────────────────────────────────────────────────────────────────

// Candidate is a single retrieval result enriched with metadata.
// It mirrors the Python Candidate dataclass and the C++ Candidate struct.
type Candidate struct {
	ObjectID   string
	ObjectType string // "memory" | "event" | "artifact"

	// Scores
	RRFScore     float64 // Reciprocal rank fusion score (before reranking)
	FinalScore   float64 // RRFScore × importance × freshness × confidence
	DenseScore   float64
	SparseScore  float64

	// Metadata (populated from ObjectStore)
	AgentID       string
	SessionID     string
	Scope         string
	Version       int64
	ProvenanceRef string
	Content       string
	Summary       string
	Confidence    float64
	Importance    float64
	FreshnessScore float64
	Level         int
	MemoryType    string // episodic / semantic / procedural

	// Governance fields (used by SafetyFilter)
	IsActive       bool
	QuarantineFlag bool
	TTL            int64 // Unix timestamp; 0 = no expiry
	ValidFrom      string
	ValidTo        string
	VisibleTime    string // maps to ValidFrom for time-travel queries
	LifecycleState string // active / decayed / quarantined / archived

	// Source channels
	SourceChannels []string // ["dense", "sparse", "filter"]

	// Graph expansion (for SubgraphExecutorWorker)
	IsSeed         bool
	SeedScore      float64
	SourceEventIDs []string
}

// ─── Result ───────────────────────────────────────────────────────────────────

// QueryMeta holds diagnostic metadata about a retrieval execution.
type QueryMeta struct {
	LatencyMs   int64
	DenseHits   int
	SparseHits  int
	FilterHits  int
	ChannelsUsed []string
}

// CandidateList is the result returned by Retriever.Retrieve.
// Consumers (Runtime.ExecuteQuery, QueryChain) use this instead of the raw
// SearchOutput from TieredDataPlane.
type CandidateList struct {
	Candidates  []Candidate
	TotalFound  int
	RetrievedAt time.Time
	Meta        QueryMeta

	// SeedIDs is a pre-computed slice of ObjectIDs where IsSeed=true.
	// Passed directly to QueryChainInput.ObjectIDs for graph expansion.
	SeedIDs []string
}
