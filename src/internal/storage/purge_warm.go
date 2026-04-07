package storage

// PurgeMemoryWarmOnly removes a memory from the hot cache, warm graph edges, and warm ObjectStore.
// It does not touch the cold tier (embeddings or cold-stored Memory blobs may remain orphaned until
// a full TieredObjectStore.HardDeleteMemory or cold GC runs). Use when Runtime has no TieredObjectStore.
func PurgeMemoryWarmOnly(rs RuntimeStorage, memoryID string) {
	if rs == nil {
		return
	}
	if hc := rs.HotCache(); hc != nil {
		hc.Evict(memoryID)
	}
	if es := rs.Edges(); es != nil {
		for _, e := range es.BulkEdges([]string{memoryID}) {
			es.DeleteEdge(e.EdgeID)
		}
	}
	if os := rs.Objects(); os != nil {
		os.DeleteMemory(memoryID)
	}
}
