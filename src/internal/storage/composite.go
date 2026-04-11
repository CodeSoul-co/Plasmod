package storage

import "plasmod/src/internal/schemas"

// compositeRuntimeStorage wires independent SegmentStore / ObjectStore / … implementations
// behind a single RuntimeStorage (memory, Badger, or mixed per sub-store).
// AuditStore and MemoryAlgorithmStateStore are always in-memory (no Badger-backed implementation yet).
type compositeRuntimeStorage struct {
	seg SegmentStore
	idx IndexStore
	obj ObjectStore
	edg GraphEdgeStore
	ver SnapshotVersionStore
	pol PolicyStore
	ctr ShareContractStore
	hot *HotObjectCache
	// In-memory governance stores (not persisted via Badger in this implementation).
	audits AuditStore
	algo   MemoryAlgorithmStateStore
}

// NewCompositeRuntimeStorage returns a RuntimeStorage composed of the given stores.
// If hot is nil, a default HotObjectCache(2000) is created.
// AuditStore and MemoryAlgorithmStateStore always use in-memory backing.
func NewCompositeRuntimeStorage(
	seg SegmentStore,
	idx IndexStore,
	obj ObjectStore,
	edg GraphEdgeStore,
	ver SnapshotVersionStore,
	pol PolicyStore,
	ctr ShareContractStore,
	hot *HotObjectCache,
) RuntimeStorage {
	if hot == nil {
		hot = NewHotObjectCache(2000)
	}
	return &compositeRuntimeStorage{
		seg:    seg,
		idx:    idx,
		obj:    obj,
		edg:    edg,
		ver:    ver,
		pol:    pol,
		ctr:    ctr,
		hot:    hot,
		audits: newInMemoryAuditStore(),
		algo:   newInMemoryAlgorithmStateStore(),
	}
}

func (c *compositeRuntimeStorage) Segments() SegmentStore                     { return c.seg }
func (c *compositeRuntimeStorage) Indexes() IndexStore                        { return c.idx }
func (c *compositeRuntimeStorage) Objects() ObjectStore                       { return c.obj }
func (c *compositeRuntimeStorage) Edges() GraphEdgeStore                      { return c.edg }
func (c *compositeRuntimeStorage) Versions() SnapshotVersionStore             { return c.ver }
func (c *compositeRuntimeStorage) Policies() PolicyStore                      { return c.pol }
func (c *compositeRuntimeStorage) Contracts() ShareContractStore              { return c.ctr }
func (c *compositeRuntimeStorage) Audits() AuditStore                         { return c.audits }
func (c *compositeRuntimeStorage) AlgorithmStates() MemoryAlgorithmStateStore { return c.algo }
func (c *compositeRuntimeStorage) HotCache() *HotObjectCache                  { return c.hot }

func (c *compositeRuntimeStorage) PutMemoryWithBaseEdges(obj schemas.Memory) {
	c.obj.PutMemory(obj)
	for _, e := range schemas.BuildMemoryBaseEdges(obj) {
		c.edg.PutEdge(e)
	}
}

func (c *compositeRuntimeStorage) PutArtifactWithBaseEdges(obj schemas.Artifact) {
	c.obj.PutArtifact(obj)
	for _, e := range schemas.BuildArtifactBaseEdges(obj) {
		c.edg.PutEdge(e)
	}
}

func (c *compositeRuntimeStorage) PutEventWithBaseEdges(obj schemas.Event) {
	c.obj.PutEvent(obj)
	for _, e := range schemas.BuildEventBaseEdges(obj) {
		c.edg.PutEdge(e)
	}
}
