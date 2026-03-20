package coordination

import (
	"fmt"

	"andb/src/internal/storage"
	"andb/src/internal/worker/nodes"
)

// InMemoryProofTraceWorker assembles explainable proof traces from the
// derivation log and graph index for a given query result set.
type InMemoryProofTraceWorker struct {
	id    string
	store storage.GraphEdgeStore
}

func CreateInMemoryProofTraceWorker(id string, store storage.GraphEdgeStore) *InMemoryProofTraceWorker {
	return &InMemoryProofTraceWorker{id: id, store: store}
}

func (w *InMemoryProofTraceWorker) Info() nodes.NodeInfo {
	return nodes.NodeInfo{
		ID:           w.id,
		Type:         nodes.NodeTypeProofTrace,
		State:        nodes.NodeStateReady,
		Capabilities: []string{"proof_trace", "graph_traversal"},
	}
}

func (w *InMemoryProofTraceWorker) AssembleTrace(objectIDs []string) []string {
	trace := []string{}
	seen := map[string]bool{}
	for _, id := range objectIDs {
		for _, e := range w.store.EdgesFrom(id) {
			step := fmt.Sprintf("%s -[%s]-> %s (w=%.2f)", e.SrcObjectID, e.EdgeType, e.DstObjectID, e.Weight)
			if !seen[step] {
				trace = append(trace, step)
				seen[step] = true
			}
		}
	}
	return trace
}
