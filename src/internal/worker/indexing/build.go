package indexing

import (
	"fmt"
	"time"

	"plasmod/src/internal/schemas"
	"plasmod/src/internal/storage"
	"plasmod/src/internal/worker/nodes"
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

func (w *InMemoryIndexBuildWorker) Run(input schemas.WorkerInput) (schemas.WorkerOutput, error) {
	in, ok := input.(schemas.IndexBuildInput)
	if !ok {
		return schemas.IndexBuildOutput{}, fmt.Errorf("index_build: unexpected input type %T", input)
	}
	err := w.IndexObject(in.ObjectID, in.ObjectType, in.Namespace, in.Text)
	if err != nil {
		return schemas.IndexBuildOutput{}, err
	}
	segID := schemas.IDPrefixSegment + in.Namespace + "_" + time.Now().Format(schemas.TimeBucketFormat)
	count := 0
	for _, rec := range w.idxStore.List() {
		if rec.Namespace == in.Namespace {
			count = rec.Indexed
			break
		}
	}
	return schemas.IndexBuildOutput{SegmentID: segID, IndexedCount: count}, nil
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
	bucket := now.Format(schemas.TimeBucketFormat)
	w.segStore.Upsert(storage.SegmentRecord{
		SegmentID:       schemas.IDPrefixSegment + namespace + "_" + bucket,
		ObjectType:      objectType,
		Namespace:       namespace,
		TimeBucket:      bucket,
		EmbeddingFamily: storage.ResolveEmbeddingFamily(nil),
		StorageRef:      objectID,
		RowCount:        1,
		Tier:            string(schemas.TierHot),
		UpdatedAt:       now,
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
