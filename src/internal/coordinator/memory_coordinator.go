package coordinator

import (
	"andb/src/internal/schemas"
	"andb/src/internal/storage"
)

// MemoryCoordinator manages the lifecycle of Memory objects: creation,
// consolidation, TTL-based soft-expiry, scope resolution, and the multi-level
// distillation chain (level 0 raw → level 1 summary → level 2 induction).
type MemoryCoordinator struct {
	store storage.ObjectStore
}

func NewMemoryCoordinator(store storage.ObjectStore) *MemoryCoordinator {
	return &MemoryCoordinator{store: store}
}

// Put persists or updates a memory object and marks it active.
func (c *MemoryCoordinator) Put(mem schemas.Memory) {
	mem.IsActive = true
	c.store.PutMemory(mem)
}

// Get fetches a memory by ID.
func (c *MemoryCoordinator) Get(id string) (schemas.Memory, bool) {
	return c.store.GetMemory(id)
}

// List returns active memories for the given agent/session filter.
// Pass empty strings to list across all agents or sessions.
func (c *MemoryCoordinator) List(agentID, sessionID string) []schemas.Memory {
	all := c.store.ListMemories(agentID, sessionID)
	active := make([]schemas.Memory, 0, len(all))
	for _, m := range all {
		if m.IsActive {
			active = append(active, m)
		}
	}
	return active
}

// SoftExpire marks a memory inactive without deleting the base record,
// satisfying the "default soft-forget" policy from the spec.
func (c *MemoryCoordinator) SoftExpire(id string) bool {
	mem, ok := c.store.GetMemory(id)
	if !ok {
		return false
	}
	mem.IsActive = false
	c.store.PutMemory(mem)
	return true
}

// BumpVersion stores an updated memory with an incremented version counter.
func (c *MemoryCoordinator) BumpVersion(mem schemas.Memory) {
	mem.Version++
	c.store.PutMemory(mem)
}
