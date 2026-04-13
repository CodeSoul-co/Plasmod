package coordination

import (
	"fmt"

	"plasmod/src/internal/eventbackbone"
	"plasmod/src/internal/schemas"
	"plasmod/src/internal/storage"
	"plasmod/src/internal/worker/nodes"
)

// defaultMaxDepth is an alias for the canonical default kept for internal use.
const defaultMaxDepth = schemas.DefaultMaxProofDepth

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

func (w *InMemoryProofTraceWorker) Run(input schemas.WorkerInput) (schemas.WorkerOutput, error) {
	in, ok := input.(schemas.ProofTraceInput)
	if !ok {
		return schemas.ProofTraceOutput{}, fmt.Errorf("proof_trace: unexpected input type %T", input)
	}
	steps := w.AssembleTrace(in.ObjectIDs, in.MaxDepth)
	depth := in.MaxDepth
	if depth <= 0 {
		depth = defaultMaxDepth
	}
	return schemas.ProofTraceOutput{Steps: steps, HopCount: depth}, nil
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
func (w *InMemoryProofTraceWorker) AssembleTrace(objectIDs []string, maxDepth int) []schemas.ProofStep {
	if maxDepth <= 0 {
		maxDepth = defaultMaxDepth
	}

	trace := []schemas.ProofStep{}
	seenNode := map[string]bool{}
	seenStep := map[string]bool{}

	// BFS frontier: (nodeID, currentDepth)
	type item struct {
		id    string
		depth int
	}
	queue := make([]item, 0, len(objectIDs))
	for _, id := range objectIDs {
		queue = append(queue, item{id, 0})
		seenNode[id] = true
	}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		if cur.depth >= maxDepth {
			continue
		}

		for _, e := range w.store.EdgesFrom(cur.id) {
			stepKey := e.EdgeID
			if stepKey == "" {
				stepKey = e.SrcObjectID + "|" + e.EdgeType + "|" + e.DstObjectID
			}
			if !seenStep[stepKey] {
				trace = append(trace, schemas.ProofStep{
					StepType:    "edge",
					Depth:       cur.depth + 1,
					SourceID:    e.SrcObjectID,
					EdgeID:      e.EdgeID,
					EdgeType:    e.EdgeType,
					TargetID:    e.DstObjectID,
					Weight:      e.Weight,
					Description: fmt.Sprintf("[d=%d] %s -[%s]-> %s (w=%.2f)", cur.depth+1, e.SrcObjectID, e.EdgeType, e.DstObjectID, e.Weight),
				})
				seenStep[stepKey] = true
			}
			if !seenNode[e.DstObjectID] {
				seenNode[e.DstObjectID] = true
				queue = append(queue, item{e.DstObjectID, cur.depth + 1})
			}
		}
	}

	// Append DerivationLog entries for the seed objects.
	if w.derivLog != nil {
		for _, id := range objectIDs {
			for _, entry := range w.derivLog.ForDerived(id) {
				stepKey := "derivation|" + entry.SourceID + "|" + entry.Operation + "|" + entry.DerivedID
				if !seenStep[stepKey] {
					trace = append(trace, schemas.ProofStep{
						StepType:    "derivation",
						SourceID:    entry.SourceID,
						SourceType:  entry.SourceType,
						EdgeType:    entry.Operation,
						TargetID:    entry.DerivedID,
						TargetType:  entry.DerivedType,
						Operation:   entry.Operation,
						Description: fmt.Sprintf("derivation: %s(%s) -[%s]-> %s(%s)", entry.SourceID, entry.SourceType, entry.Operation, entry.DerivedID, entry.DerivedType),
					})
					seenStep[stepKey] = true
				}
			}
		}
	}

	return trace
}
