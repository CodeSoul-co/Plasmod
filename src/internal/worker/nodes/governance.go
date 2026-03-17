package nodes

import (
	"fmt"

	"andb/src/internal/schemas"
	"andb/src/internal/storage"
)

// ─── InMemoryMemoryExtractionWorker ──────────────────────────────────────────

// InMemoryMemoryExtractionWorker derives a level-0 Memory object from an
// event payload and persists it via the ObjectStore.
type InMemoryMemoryExtractionWorker struct {
	id    string
	store storage.ObjectStore
}

func NewInMemoryMemoryExtractionWorker(id string, store storage.ObjectStore) *InMemoryMemoryExtractionWorker {
	return &InMemoryMemoryExtractionWorker{id: id, store: store}
}

func (w *InMemoryMemoryExtractionWorker) Info() NodeInfo {
	return NodeInfo{
		ID:           w.id,
		Type:         NodeTypeMemoryExtraction,
		State:        NodeStateReady,
		Capabilities: []string{"memory_extract", "level0_record"},
	}
}

func (w *InMemoryMemoryExtractionWorker) Extract(eventID, agentID, sessionID, content string) error {
	mem := schemas.Memory{
		MemoryID:       fmt.Sprintf("mem_%s", eventID),
		MemoryType:     string(schemas.MemoryTypeEpisodic),
		AgentID:        agentID,
		SessionID:      sessionID,
		SourceEventIDs: []string{eventID},
		Content:        content,
		Level:          0,
		IsActive:       true,
		Version:        1,
	}
	w.store.PutMemory(mem)
	return nil
}

// ─── InMemoryMemoryConsolidationWorker ───────────────────────────────────────

// InMemoryMemoryConsolidationWorker reads level-0 memories for an
// agent/session and produces a level-1 summary record.
type InMemoryMemoryConsolidationWorker struct {
	id    string
	store storage.ObjectStore
}

func NewInMemoryMemoryConsolidationWorker(id string, store storage.ObjectStore) *InMemoryMemoryConsolidationWorker {
	return &InMemoryMemoryConsolidationWorker{id: id, store: store}
}

func (w *InMemoryMemoryConsolidationWorker) Info() NodeInfo {
	return NodeInfo{
		ID:           w.id,
		Type:         NodeTypeMemoryConsolidation,
		State:        NodeStateReady,
		Capabilities: []string{"memory_consolidate", "level1_summary"},
	}
}

func (w *InMemoryMemoryConsolidationWorker) Consolidate(agentID, sessionID string) error {
	memories := w.store.ListMemories(agentID, sessionID)
	if len(memories) == 0 {
		return nil
	}
	combined := ""
	sourceIDs := []string{}
	for _, m := range memories {
		if m.Level == 0 && m.IsActive {
			combined += m.Content + " "
			sourceIDs = append(sourceIDs, m.MemoryID)
		}
	}
	if len(sourceIDs) == 0 {
		return nil
	}
	summary := schemas.Memory{
		MemoryID:       fmt.Sprintf("summary_%s_%s", agentID, sessionID),
		MemoryType:     string(schemas.MemoryTypeSemantic),
		AgentID:        agentID,
		SessionID:      sessionID,
		SourceEventIDs: sourceIDs,
		Content:        combined,
		Summary:        fmt.Sprintf("Consolidated from %d level-0 memories", len(sourceIDs)),
		Level:          1,
		IsActive:       true,
		Version:        1,
	}
	w.store.PutMemory(summary)
	return nil
}

// ─── InMemoryGraphRelationWorker ─────────────────────────────────────────────

// InMemoryGraphRelationWorker maintains the graph/edge index.
type InMemoryGraphRelationWorker struct {
	id    string
	store storage.GraphEdgeStore
}

func NewInMemoryGraphRelationWorker(id string, store storage.GraphEdgeStore) *InMemoryGraphRelationWorker {
	return &InMemoryGraphRelationWorker{id: id, store: store}
}

func (w *InMemoryGraphRelationWorker) Info() NodeInfo {
	return NodeInfo{
		ID:           w.id,
		Type:         NodeTypeGraphRelation,
		State:        NodeStateReady,
		Capabilities: []string{"graph_index", "edge_write"},
	}
}

func (w *InMemoryGraphRelationWorker) IndexEdge(srcID, srcType, dstID, dstType, edgeType string, weight float64) error {
	edge := schemas.Edge{
		EdgeID:      fmt.Sprintf("edge_%s_%s_%s", srcID, edgeType, dstID),
		SrcObjectID: srcID,
		SrcType:     srcType,
		EdgeType:    edgeType,
		DstObjectID: dstID,
		DstType:     dstType,
		Weight:      weight,
	}
	w.store.PutEdge(edge)
	return nil
}

// ─── InMemoryProofTraceWorker ─────────────────────────────────────────────────

// InMemoryProofTraceWorker assembles a proof trace by looking up edges for
// each result object ID.
type InMemoryProofTraceWorker struct {
	id    string
	store storage.GraphEdgeStore
}

func NewInMemoryProofTraceWorker(id string, store storage.GraphEdgeStore) *InMemoryProofTraceWorker {
	return &InMemoryProofTraceWorker{id: id, store: store}
}

func (w *InMemoryProofTraceWorker) Info() NodeInfo {
	return NodeInfo{
		ID:           w.id,
		Type:         NodeTypeProofTrace,
		State:        NodeStateReady,
		Capabilities: []string{"proof_trace", "graph_traversal"},
	}
}

func (w *InMemoryProofTraceWorker) AssembleTrace(objectIDs []string) []string {
	trace := []string{}
	seen := map[string]bool{}
	for _, id := range objectIDs {
		edges := w.store.EdgesFrom(id)
		for _, e := range edges {
			step := fmt.Sprintf("%s -[%s]-> %s (w=%.2f)", e.SrcObjectID, e.EdgeType, e.DstObjectID, e.Weight)
			if !seen[step] {
				trace = append(trace, step)
				seen[step] = true
			}
		}
	}
	return trace
}
