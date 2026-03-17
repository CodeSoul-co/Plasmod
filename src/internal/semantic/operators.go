package semantic

import (
	"time"

	"andb/src/internal/schemas"
)

// ─── Base query plan ─────────────────────────────────────────────────────────

// QueryPlan is the base execution descriptor produced by the query planner.
type QueryPlan struct {
	TopK           int
	Namespace      string
	Constraints    []string
	TimeFromUnixTS int64
	TimeToUnixTS   int64
	IncludeGrowing bool
	// ResponseMode drives which extended operator the retrieval layer uses.
	ResponseMode ResponseMode
}

// ResponseMode selects the retrieval execution strategy (section 11).
type ResponseMode string

const (
	ResponseModeDefault        ResponseMode = "default"
	ResponseModeSubgraph       ResponseMode = "subgraph"
	ResponseModeMultiHop       ResponseMode = "multi_hop"
	ResponseModeSlice          ResponseMode = "slice"
	ResponseModeAggregate      ResponseMode = "aggregate"
	ResponseModeProofTrace     ResponseMode = "proof_trace"
)

// QueryPlanner builds a QueryPlan from a QueryRequest.
type QueryPlanner interface {
	Build(req schemas.QueryRequest) QueryPlan
}

// DefaultQueryPlanner covers the common flat-retrieval path.
type DefaultQueryPlanner struct{}

func NewDefaultQueryPlanner() *DefaultQueryPlanner {
	return &DefaultQueryPlanner{}
}

func (p *DefaultQueryPlanner) Build(req schemas.QueryRequest) QueryPlan {
	topK := req.TopK
	if topK <= 0 {
		topK = 10
	}
	ns := req.QueryScope
	if ns == "" {
		ns = req.SessionID
	}
	fromTS, _ := parseRFC3339ToUnix(req.TimeWindow.From)
	toTS, _ := parseRFC3339ToUnix(req.TimeWindow.To)
	mode := ResponseMode(req.ResponseMode)
	if mode == "" {
		mode = ResponseModeDefault
	}
	return QueryPlan{
		TopK:           topK,
		Namespace:      ns,
		Constraints:    req.RelationConstraints,
		TimeFromUnixTS: fromTS,
		TimeToUnixTS:   toTS,
		IncludeGrowing: true,
		ResponseMode:   mode,
	}
}

// ─── Subgraph / Subtensor Retrieval (section 11.1) ───────────────────────────

// SubgraphQueryPlan extends QueryPlan for connected-evidence subgraph retrieval.
// The retrieval layer returns a connected evidence sub-graph rather than a flat
// list of object IDs.
type SubgraphQueryPlan struct {
	QueryPlan
	// MaxHops limits how far the graph traversal expands from seed nodes.
	MaxHops int
	// EdgeTypes restricts traversal to specific relation types.
	EdgeTypes []string
	// ScopeFilter restricts traversal to a single sharing scope.
	ScopeFilter string
	// ConfidenceThreshold drops edges below this confidence weight.
	ConfidenceThreshold float64
}

// ─── Constrained Multi-hop Expansion (section 11.3) ──────────────────────────

// MultiHopQueryPlan drives constrained multi-hop expansion queries.
type MultiHopQueryPlan struct {
	QueryPlan
	HopLimit            int
	AllowedRelations    []string
	ScopeConstraint     string
	TimeWindowSeconds   int64
	ConfidenceThreshold float64
}

// ─── Slice / Window / Rollback Query (section 11.4) ──────────────────────────

// SliceQueryPlan retrieves a time-slice, scope-slice, or versioned snapshot.
type SliceQueryPlan struct {
	QueryPlan
	// SliceType selects the kind of slice operation.
	SliceType SliceType
	// RollbackToVersion targets a specific object version (version rollback).
	RollbackToVersion int64
	// VisibilityAt makes the query visibility-aware at a specific logical TS.
	VisibilityAt int64
}

// SliceType enumerates slice operation kinds.
type SliceType string

const (
	SliceTypeTime       SliceType = "time"
	SliceTypeScope      SliceType = "scope"
	SliceTypeVersion    SliceType = "version_rollback"
	SliceTypeVisibility SliceType = "visibility"
)

// ─── Aggregate / Contrast / Consensus Operators (section 11.5) ───────────────

// AggregateOperatorType enumerates MAS-native aggregation modes.
type AggregateOperatorType string

const (
	AggregateConsensusReduce  AggregateOperatorType = "consensus_reduce"
	AggregateConflictCompare  AggregateOperatorType = "conflict_compare"
	AggregateScopedAggregate  AggregateOperatorType = "scoped_aggregate"
)

// AggregateQueryPlan drives multi-agent aggregation, consensus, or conflict
// comparison over a candidate set.
type AggregateQueryPlan struct {
	QueryPlan
	OperatorType AggregateOperatorType
	// GroupByScope produces per-scope aggregation buckets.
	GroupByScope bool
	// ConflictWindowSeconds defines the time range for conflict detection.
	ConflictWindowSeconds int64
}

// ─── Proof Trace (section 11.2) ───────────────────────────────────────────────

// ProofTraceQueryPlan requests an explainable proof trace alongside results.
// Every result item will carry which objects, edges, versions and policy
// filters contributed to it.
type ProofTraceQueryPlan struct {
	QueryPlan
	// IncludePolicySteps appends policy-filter steps to the trace.
	IncludePolicySteps bool
	// IncludeDerivationSteps appends derivation-log steps to the trace.
	IncludeDerivationSteps bool
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func parseRFC3339ToUnix(ts string) (int64, bool) {
	if ts == "" {
		return 0, false
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return 0, false
	}
	return t.Unix(), true
}
