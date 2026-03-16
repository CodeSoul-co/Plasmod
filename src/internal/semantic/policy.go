package semantic

import "andb/src/internal/schemas"

type PolicyEngine struct{}

func NewPolicyEngine() *PolicyEngine {
	return &PolicyEngine{}
}

func (p *PolicyEngine) ApplyQueryFilters(req schemas.QueryRequest) []string {
	filters := []string{"scope", "visibility", "time_window"}
	if req.TimeWindow.From != "" || req.TimeWindow.To != "" {
		filters = append(filters, "time_window_bound")
	}
	if len(req.RelationConstraints) > 0 {
		filters = append(filters, "relation_constraints")
	}
	return filters
}
