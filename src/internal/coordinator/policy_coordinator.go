package coordinator

import "andb/src/internal/semantic"

type PolicyCoordinator struct {
	engine *semantic.PolicyEngine
}

func NewPolicyCoordinator(engine *semantic.PolicyEngine) *PolicyCoordinator {
	return &PolicyCoordinator{engine: engine}
}
