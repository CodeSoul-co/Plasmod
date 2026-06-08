package semantic

import (
	"strings"
	"testing"

	"plasmod/src/internal/schemas"
)

func TestDefaultQueryPlannerCarriesHooks(t *testing.T) {
	planner := NewDefaultQueryPlanner()
	plan := planner.Build(schemas.QueryRequest{
		QueryText: "find memory",
		QueryOps:  []string{"query.base"},
		Hooks: schemas.EventHooks{
			QueryOps: []string{"query.custom"},
			Policy:   []string{"policy.custom"},
		},
	})

	if got := strings.Join(plan.QueryOps, ","); got != "query.base,query.custom" {
		t.Fatalf("query ops should merge direct and hook query ops, got %q", got)
	}
	if got := strings.Join(plan.Hooks.Policy, ","); got != "policy.custom" {
		t.Fatalf("planner should preserve hooks, got %q", got)
	}
}
