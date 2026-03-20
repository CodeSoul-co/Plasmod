package indexing

import (
	"andb/src/internal/schemas"
	"andb/src/internal/worker/nodes"
)

// InMemorySubgraphExecutorWorker wraps schemas.ExpandFromRequest to expose
// graph subgraph expansion as a pluggable worker in the Structure Layer (L5).
type InMemorySubgraphExecutorWorker struct {
	id string
}

func CreateInMemorySubgraphExecutorWorker(id string) *InMemorySubgraphExecutorWorker {
	return &InMemorySubgraphExecutorWorker{id: id}
}

func (w *InMemorySubgraphExecutorWorker) Info() nodes.NodeInfo {
	return nodes.NodeInfo{
		ID:           w.id,
		Type:         nodes.NodeTypeSubgraph,
		State:        nodes.NodeStateReady,
		Capabilities: []string{"one_hop_expand", "edge_type_filter", "subgraph_assemble"},
	}
}

func (w *InMemorySubgraphExecutorWorker) Expand(
	req schemas.GraphExpandRequest,
	gnodes []schemas.GraphNode,
	edges []schemas.Edge,
) schemas.GraphExpandResponse {
	return schemas.ExpandFromRequest(req, gnodes, edges)
}
