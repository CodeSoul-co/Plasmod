package nodes

import (
	"andb/src/internal/dataplane"
	"andb/src/internal/storage"
)

// InMemoryIndexNode maintains keyword and segment index statistics for a
// namespace, incrementing the indexed count on every ingest.
type InMemoryIndexNode struct {
	id    string
	store storage.IndexStore
}

func CreateInMemoryIndexNode(id string, store storage.IndexStore) *InMemoryIndexNode {
	return &InMemoryIndexNode{id: id, store: store}
}

func (n *InMemoryIndexNode) Info() NodeInfo {
	return NodeInfo{
		ID:           n.id,
		Type:         NodeTypeIndex,
		State:        NodeStateReady,
		Capabilities: []string{"index_build", "index_stats"},
	}
}

func (n *InMemoryIndexNode) BuildIndex(record dataplane.IngestRecord) {
	existing := 0
	for _, item := range n.store.List() {
		if item.Namespace == record.Namespace {
			existing = item.Indexed
			break
		}
	}
	n.store.Upsert(storage.IndexRecord{Namespace: record.Namespace, Indexed: existing + 1})
}
