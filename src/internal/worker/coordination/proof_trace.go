package coordination

import (
	"fmt"

	"andb/src/internal/eventbackbone"
	"andb/src/internal/storage"
	"andb/src/internal/worker/nodes"
)

const defaultMaxDepth = 8

// InMemoryProofTraceWorker assembles explainable proof traces by performing a
// BFS over the GraphEdgeStore starting from the supplied objectIDs.
//
// Each hop is labelled with edge type and weight.  DerivationLog entries are
// also appended to the trace as "derivation:" steps, giving full causal
// lineage from raw events through to materialised objects.
type InMemoryProofTraceWorker struct {
	id       string
	store    storage.GraphEdgeStore
	derivLog eventbackbone.DerivationLogger
}

// CreateInMemoryProofTraceWorker creates a ProofTraceWorker.
// derivLog may be nil; when provided, derivation entries are included in the
// assembled trace.
func CreateInMemoryProofTraceWorker(
	id string,
	store storage.GraphEdgeStore,
	derivLog eventbackbone.DerivationLogger,
) *InMemoryProofTraceWorker {
	return &InMemoryProofTraceWorker{id: id, store: store, derivLog: derivLog}
}

func (w *InMemoryProofTraceWorker) Info() nodes.NodeInfo {
	return nodes.NodeInfo{
		ID:           w.id,
		Type:         nodes.NodeTypeProofTrace,
		State:        nodes.NodeStateReady,
		Capabilities: []string{"proof_trace", "multi_hop_bfs", "derivation_log"},
	}
}

// AssembleTrace performs a BFS over the graph starting from objectIDs.
// maxDepth <= 0 uses the default cap of 8.
func (w *InMemoryProofTraceWorker) AssembleTrace(objectIDs []string, maxDepth int) []string {
	if maxDepth <= 0 {
		maxDepth = defaultMaxDepth
	}

	trace := []string{}
	seen := map[string]bool{}

	// BFS frontier: (nodeID, currentDepth)
	type item struct {
		id    string
		depth int
	}
	queue := make([]item, 0, len(objectIDs))
	for _, id := range objectIDs {
		queue = append(queue, item{id, 0})
		seen[id] = true
	}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		if cur.depth >= maxDepth {
			continue
		}

		for _, e := range w.store.EdgesFrom(cur.id) {
			step := fmt.Sprintf("[d=%d] %s -[%s]-> %s (w=%.2f)",
				cur.depth+1, e.SrcObjectID, e.EdgeType, e.DstObjectID, e.Weight)
			if !seen[step] {
				trace = append(trace, step)
				seen[step] = true
			}
			if !seen[e.DstObjectID] {
				seen[e.DstObjectID] = true
				queue = append(queue, item{e.DstObjectID, cur.depth + 1})
			}
		}
	}

	// Append DerivationLog entries for the seed objects.
	if w.derivLog != nil {
		for _, id := range objectIDs {
			for _, entry := range w.derivLog.ForDerived(id) {
				step := fmt.Sprintf("derivation: %s(%s) -[%s]-> %s(%s)",
					entry.SourceID, entry.SourceType,
					entry.Operation,
					entry.DerivedID, entry.DerivedType)
				if !seen[step] {
					trace = append(trace, step)
					seen[step] = true
				}
			}
		}
	}

	return trace
}
