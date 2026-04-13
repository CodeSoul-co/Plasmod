package nodes

import (
	"plasmod/src/internal/dataplane"
	"plasmod/src/internal/storage"
)

// InMemoryDataNode handles raw segment storage for ingested objects.
type InMemoryDataNode struct {
	id    string
	store storage.SegmentStore
}

func CreateInMemoryDataNode(id string, store storage.SegmentStore) *InMemoryDataNode {
	return &InMemoryDataNode{id: id, store: store}
}

func (n *InMemoryDataNode) Info() NodeInfo {
	return NodeInfo{
		ID:           n.id,
		Type:         NodeTypeData,
		State:        NodeStateReady,
		Capabilities: []string{"ingest", "segment_record"},
	}
}

func (n *InMemoryDataNode) HandleIngest(record dataplane.IngestRecord) {
	n.store.Upsert(storage.SegmentRecord{
		SegmentID:       record.ObjectID,
		Namespace:       record.Namespace,
		EmbeddingFamily: storage.ResolveEmbeddingFamily(record.Attributes),
		RowCount:        1,
	})
}
