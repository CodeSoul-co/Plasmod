package indexing

import (
	"fmt"

	"plasmod/src/internal/schemas"
	"plasmod/src/internal/storage"
	"plasmod/src/internal/worker/nodes"
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

func (w *InMemoryGraphRelationWorker) Run(input schemas.WorkerInput) (schemas.WorkerOutput, error) {
	in, ok := input.(schemas.GraphRelationInput)
	if !ok {
		return schemas.GraphRelationOutput{}, fmt.Errorf("graph_relation: unexpected input type %T", input)
	}
	err := w.IndexEdge(in.SrcID, in.SrcType, in.DstID, in.DstType, in.EdgeType, in.Weight)
	if err != nil {
		return schemas.GraphRelationOutput{}, err
	}
	return schemas.GraphRelationOutput{
		EdgeID: schemas.IDPrefixEdge + in.SrcID + "_" + in.EdgeType + "_" + in.DstID,
	}, nil
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
		EdgeID:      schemas.IDPrefixEdge + srcID + "_" + edgeType + "_" + dstID,
		SrcObjectID: srcID,
		SrcType:     srcType,
		EdgeType:    edgeType,
		DstObjectID: dstID,
		DstType:     dstType,
		Weight:      weight,
	})
	return nil
}
