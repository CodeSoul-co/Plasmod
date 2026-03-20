package indexing

import (
	"fmt"

	"andb/src/internal/schemas"
	"andb/src/internal/storage"
	"andb/src/internal/worker/nodes"
)

// InMemoryGraphRelationWorker maintains the graph/edge index from derivation
// and causal references embedded in Event and Memory objects.
type InMemoryGraphRelationWorker struct {
	id    string
	store storage.GraphEdgeStore
}

func CreateInMemoryGraphRelationWorker(id string, store storage.GraphEdgeStore) *InMemoryGraphRelationWorker {
	return &InMemoryGraphRelationWorker{id: id, store: store}
}

func (w *InMemoryGraphRelationWorker) Info() nodes.NodeInfo {
	return nodes.NodeInfo{
		ID:           w.id,
		Type:         nodes.NodeTypeGraphRelation,
		State:        nodes.NodeStateReady,
		Capabilities: []string{"graph_index", "edge_write"},
	}
}

func (w *InMemoryGraphRelationWorker) IndexEdge(srcID, srcType, dstID, dstType, edgeType string, weight float64) error {
	w.store.PutEdge(schemas.Edge{
		EdgeID:      fmt.Sprintf("edge_%s_%s_%s", srcID, edgeType, dstID),
		SrcObjectID: srcID,
		SrcType:     srcType,
		EdgeType:    edgeType,
		DstObjectID: dstID,
		DstType:     dstType,
		Weight:      weight,
	})
	return nil
}
