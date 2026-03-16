package nodes

import (
	"andb/src/internal/dataplane"
	"andb/src/internal/storage"
)

type InMemoryDataNode struct {
	id    string
	store storage.SegmentStore
}

func NewInMemoryDataNode(id string, store storage.SegmentStore) *InMemoryDataNode {
	return &InMemoryDataNode{id: id, store: store}
}

func (n *InMemoryDataNode) Info() NodeInfo {
	return NodeInfo{ID: n.id, Type: NodeTypeData, State: NodeStateReady, Capabilities: []string{"ingest", "segment_record"}}
}

func (n *InMemoryDataNode) HandleIngest(record dataplane.IngestRecord) {
	n.store.Upsert(storage.SegmentRecord{SegmentID: record.ObjectID, Namespace: record.Namespace, RowCount: 1})
}

type InMemoryIndexNode struct {
	id    string
	store storage.IndexStore
}

func NewInMemoryIndexNode(id string, store storage.IndexStore) *InMemoryIndexNode {
	return &InMemoryIndexNode{id: id, store: store}
}

func (n *InMemoryIndexNode) Info() NodeInfo {
	return NodeInfo{ID: n.id, Type: NodeTypeIndex, State: NodeStateReady, Capabilities: []string{"index_build", "index_stats"}}
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

type InMemoryQueryNode struct {
	id    string
	plane dataplane.DataPlane
}

func NewInMemoryQueryNode(id string, plane dataplane.DataPlane) *InMemoryQueryNode {
	return &InMemoryQueryNode{id: id, plane: plane}
}

func (n *InMemoryQueryNode) Info() NodeInfo {
	return NodeInfo{ID: n.id, Type: NodeTypeQuery, State: NodeStateReady, Capabilities: []string{"search", "planner_dispatch"}}
}

func (n *InMemoryQueryNode) Search(input dataplane.SearchInput) dataplane.SearchOutput {
	return n.plane.Search(input)
}
