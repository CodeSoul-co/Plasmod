package coordinator

import (
	"plasmod/src/internal/schemas"
	"plasmod/src/internal/semantic"
)

// QueryCoordinator is the entry-point for query planning inside the Control
// Plane.  It wraps the semantic QueryPlanner, applies version/visibility
// constraints, and selects the correct extended operator plan type based on
// the request's ResponseMode field.
type QueryCoordinator struct {
	planner semantic.QueryPlanner
	policy  *semantic.PolicyEngine
}

func NewQueryCoordinator(planner semantic.QueryPlanner, policy *semantic.PolicyEngine) *QueryCoordinator {
	return &QueryCoordinator{planner: planner, policy: policy}
}

// Plan builds the execution plan for a query request and returns both the
// base plan and the active policy filters.
func (c *QueryCoordinator) Plan(req schemas.QueryRequest) (semantic.QueryPlan, []string) {
	plan := c.planner.Build(req)
	filters := c.policy.ApplyQueryFilters(req)
	return plan, filters
}

// PlanSubgraph builds a SubgraphQueryPlan for evidence sub-graph retrieval.
func (c *QueryCoordinator) PlanSubgraph(req schemas.QueryRequest, maxHops int, edgeTypes []string) semantic.SubgraphQueryPlan {
	base := c.planner.Build(req)
	base.ResponseMode = semantic.ResponseModeSubgraph
	return semantic.SubgraphQueryPlan{
		QueryPlan:           base,
		MaxHops:             maxHops,
		EdgeTypes:           edgeTypes,
		ConfidenceThreshold: 0.0,
	}
}

// PlanMultiHop builds a constrained multi-hop expansion plan.
func (c *QueryCoordinator) PlanMultiHop(req schemas.QueryRequest, hopLimit int, allowed []string) semantic.MultiHopQueryPlan {
	base := c.planner.Build(req)
	base.ResponseMode = semantic.ResponseModeMultiHop
	return semantic.MultiHopQueryPlan{
		QueryPlan:        base,
		HopLimit:         hopLimit,
		AllowedRelations: allowed,
	}
}

// PlanSlice builds a time-slice or version-rollback query plan.
func (c *QueryCoordinator) PlanSlice(req schemas.QueryRequest, sliceType semantic.SliceType, rollbackVersion int64) semantic.SliceQueryPlan {
	base := c.planner.Build(req)
	base.ResponseMode = semantic.ResponseModeSlice
	return semantic.SliceQueryPlan{
		QueryPlan:         base,
		SliceType:         sliceType,
		RollbackToVersion: rollbackVersion,
	}
}

// PlanAggregate builds an aggregate/consensus/conflict-compare plan.
func (c *QueryCoordinator) PlanAggregate(req schemas.QueryRequest, opType semantic.AggregateOperatorType) semantic.AggregateQueryPlan {
	base := c.planner.Build(req)
	base.ResponseMode = semantic.ResponseModeAggregate
	return semantic.AggregateQueryPlan{
		QueryPlan:    base,
		OperatorType: opType,
		GroupByScope: true,
	}
}
