package coordinator

import (
	"andb/src/internal/schemas"
	"andb/src/internal/semantic"
	"andb/src/internal/storage"
)

// PolicyCoordinator applies governance rules to objects and persists policy
// records.  It is the bridge between the semantic PolicyEngine and the durable
// PolicyStore.
type PolicyCoordinator struct {
	engine *semantic.PolicyEngine
	store  storage.PolicyStore
}

func NewPolicyCoordinator(engine *semantic.PolicyEngine, store storage.PolicyStore) *PolicyCoordinator {
	return &PolicyCoordinator{engine: engine, store: store}
}

// Append writes a new governance decision for an object.
func (c *PolicyCoordinator) Append(p schemas.PolicyRecord) {
	c.store.AppendPolicy(p)
}

// GetPolicies returns all policy records for an object (append-only log).
func (c *PolicyCoordinator) GetPolicies(objectID string) []schemas.PolicyRecord {
	return c.store.GetPolicies(objectID)
}

// IsQuarantined delegates to the semantic engine using the stored records.
func (c *PolicyCoordinator) IsQuarantined(objectID string) bool {
	policies := c.store.GetPolicies(objectID)
	return c.engine.IsQuarantined(policies)
}

// EffectiveSalience returns the governance-adjusted salience for a memory.
func (c *PolicyCoordinator) EffectiveSalience(mem schemas.Memory) float64 {
	policies := c.store.GetPolicies(mem.MemoryID)
	return c.engine.EffectiveSalience(mem, policies)
}

// IsTTLExpired delegates TTL evaluation to the semantic engine.
func (c *PolicyCoordinator) IsTTLExpired(mem schemas.Memory) bool {
	return c.engine.IsTTLExpired(mem)
}
