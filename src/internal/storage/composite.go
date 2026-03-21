package storage

// compositeRuntimeStorage wires independent SegmentStore / ObjectStore / … implementations
// behind a single RuntimeStorage (memory, Badger, or mixed per sub-store).
type compositeRuntimeStorage struct {
	seg SegmentStore
	idx IndexStore
	obj ObjectStore
	edg GraphEdgeStore
	ver SnapshotVersionStore
	pol PolicyStore
	ctr ShareContractStore
	hot *HotObjectCache
}

// NewCompositeRuntimeStorage returns a RuntimeStorage composed of the given stores.
// If hot is nil, a default HotObjectCache(2000) is created.
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
		seg: seg,
		idx: idx,
		obj: obj,
		edg: edg,
		ver: ver,
		pol: pol,
		ctr: ctr,
		hot: hot,
	}
}

func (c *compositeRuntimeStorage) Segments() SegmentStore        { return c.seg }
func (c *compositeRuntimeStorage) Indexes() IndexStore         { return c.idx }
func (c *compositeRuntimeStorage) Objects() ObjectStore        { return c.obj }
func (c *compositeRuntimeStorage) Edges() GraphEdgeStore        { return c.edg }
func (c *compositeRuntimeStorage) Versions() SnapshotVersionStore { return c.ver }
func (c *compositeRuntimeStorage) Policies() PolicyStore       { return c.pol }
func (c *compositeRuntimeStorage) Contracts() ShareContractStore { return c.ctr }
func (c *compositeRuntimeStorage) HotCache() *HotObjectCache   { return c.hot }
