package coordinator

import (
	"andb/src/internal/schemas"
	"andb/src/internal/storage"
)

// IndexCoordinator manages the lifecycle of retrieval segments and index
// metadata.  It tracks which segments exist, their tiers, and exposes helpers
// for the index build workers to register newly sealed segments.
type IndexCoordinator struct {
	segStore storage.SegmentStore
	idxStore storage.IndexStore
}

func NewIndexCoordinator(segStore storage.SegmentStore, idxStore storage.IndexStore) *IndexCoordinator {
	return &IndexCoordinator{segStore: segStore, idxStore: idxStore}
}

// RegisterSegment records a newly created or sealed retrieval segment.
func (c *IndexCoordinator) RegisterSegment(seg schemas.RetrievalSegment) {
	c.segStore.Upsert(storage.SegmentRecord{
		SegmentID: seg.SegmentID,
		Namespace: seg.Namespace,
		RowCount:  seg.RowCount,
	})
}

// ListSegments returns all segments for the given namespace (empty = all).
func (c *IndexCoordinator) ListSegments(namespace string) []storage.SegmentRecord {
	return c.segStore.List(namespace)
}

// IncrementIndexed bumps the indexed-object counter for a namespace after an
// index build worker completes a batch.
func (c *IndexCoordinator) IncrementIndexed(namespace string, delta int) {
	records := c.idxStore.List()
	existing := 0
	for _, r := range records {
		if r.Namespace == namespace {
			existing = r.Indexed
			break
		}
	}
	c.idxStore.Upsert(storage.IndexRecord{Namespace: namespace, Indexed: existing + delta})
}

// IndexStats returns per-namespace index statistics.
func (c *IndexCoordinator) IndexStats() []storage.IndexRecord {
	return c.idxStore.List()
}
