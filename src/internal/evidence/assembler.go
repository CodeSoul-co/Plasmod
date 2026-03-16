package evidence

import (
	"andb/src/internal/dataplane"
	"andb/src/internal/schemas"
)

// Assembler converts retrieval output into the evidence-oriented response
// contract exposed by the current API.
type Assembler struct{}

func NewAssembler() *Assembler {
	return &Assembler{}
}

func (a *Assembler) Build(result dataplane.SearchOutput, filters []string) schemas.QueryResponse {
	trace := []string{"planner", "retrieval_search", "policy_filter", "response"}
	for _, partition := range result.PlannedSegments {
		trace = append(trace, "plan_partition:"+partition.ID+":"+partition.State)
	}

	return schemas.QueryResponse{
		Objects:        result.ObjectIDs,
		Edges:          []schemas.Edge{},
		Provenance:     []string{"event_projection", "retrieval_projection"},
		Versions:       []schemas.ObjectVersion{},
		AppliedFilters: filters,
		ProofTrace:     append(trace, result.ScannedSegments...),
	}
}
