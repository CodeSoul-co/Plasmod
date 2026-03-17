# Extension Points

ANDB keeps interfaces stable while allowing implementation replacement.

## Stable Interfaces
- Event backbone: `src/internal/eventbackbone/contracts.go`
- Data plane: `src/internal/dataplane/contracts.go`
- Query planner: `src/internal/semantic/operators.go`

## Replaceable Components
- `DataPlane` implementation can be replaced with a deeper extended-plane runtime (enable with `extended` build tag).
- `QueryPlanner` can be replaced with graph/tensor-aware planner.
- `PolicyEngine` can be replaced with ACL/TTL/quarantine policy engine.

## Compatibility Rule
Any replacement component must preserve:
- request/response schemas under `src/internal/schemas`
- ingest and query API payload compatibility
- proof_trace and applied_filters presence in query response
