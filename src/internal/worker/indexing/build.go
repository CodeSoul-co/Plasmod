package indexing

import (
	"fmt"
	"time"

	"andb/src/internal/storage"
	"andb/src/internal/worker/nodes"
)

// InMemoryIndexBuildWorker submits a materialised object to the SegmentStore
// and IndexStore for keyword and attribute retrieval.
type InMemoryIndexBuildWorker struct {
	id       string
	segStore storage.SegmentStore
	idxStore storage.IndexStore
}

func CreateInMemoryIndexBuildWorker(
	id string,
	segStore storage.SegmentStore,
	idxStore storage.IndexStore,
) *InMemoryIndexBuildWorker {
	return &InMemoryIndexBuildWorker{id: id, segStore: segStore, idxStore: idxStore}
}

func (w *InMemoryIndexBuildWorker) Info() nodes.NodeInfo {
	return nodes.NodeInfo{
		ID:           w.id,
		Type:         nodes.NodeTypeIndexBuild,
		State:        nodes.NodeStateReady,
		Capabilities: []string{"segment_index", "keyword_index", "attribute_index"},
	}
}

func (w *InMemoryIndexBuildWorker) IndexObject(objectID, objectType, namespace, _ string) error {
	now := time.Now()
	bucket := now.Format("2006-01-02")
	w.segStore.Upsert(storage.SegmentRecord{
		SegmentID:  fmt.Sprintf("seg_%s_%s", namespace, bucket),
		ObjectType: objectType,
		Namespace:  namespace,
		TimeBucket: bucket,
		StorageRef: objectID,
		RowCount:   1,
		Tier:       "hot",
		UpdatedAt:  now,
	})
	count := 1
	for _, rec := range w.idxStore.List() {
		if rec.Namespace == namespace {
			count = rec.Indexed + 1
			break
		}
	}
	w.idxStore.Upsert(storage.IndexRecord{
		Namespace: namespace,
		Indexed:   count,
		UpdatedAt: now,
	})
	return nil
}
