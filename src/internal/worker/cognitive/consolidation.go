package cognitive

import (
	"fmt"

	"andb/src/internal/schemas"
	"andb/src/internal/storage"
	"andb/src/internal/worker/nodes"
)

// InMemoryMemoryConsolidationWorker reads level-0 memories for an
// agent/session and produces a level-1 summary record.
type InMemoryMemoryConsolidationWorker struct {
	id    string
	store storage.ObjectStore
}

func CreateInMemoryMemoryConsolidationWorker(id string, store storage.ObjectStore) *InMemoryMemoryConsolidationWorker {
	return &InMemoryMemoryConsolidationWorker{id: id, store: store}
}

func (w *InMemoryMemoryConsolidationWorker) Run(input schemas.WorkerInput) (schemas.WorkerOutput, error) {
	in, ok := input.(schemas.MemoryConsolidationInput)
	if !ok {
		return schemas.MemoryConsolidationOutput{}, fmt.Errorf("consolidation: unexpected input type %T", input)
	}
	// capture source count before consolidation
	allBefore := w.store.ListMemories(in.AgentID, in.SessionID)
	sourceCount := 0
	for _, m := range allBefore {
		if m.Level == 0 && m.IsActive {
			sourceCount++
		}
	}
	err := w.Consolidate(in.AgentID, in.SessionID)
	if err != nil {
		return schemas.MemoryConsolidationOutput{}, err
	}
	summaryID := schemas.IDPrefixSummary + in.AgentID + "_" + in.SessionID
	if _, ok := w.store.GetMemory(summaryID); !ok {
		return schemas.MemoryConsolidationOutput{SourceCount: sourceCount}, nil
	}
	return schemas.MemoryConsolidationOutput{SummaryID: summaryID, SourceCount: sourceCount}, nil
}

func (w *InMemoryMemoryConsolidationWorker) Info() nodes.NodeInfo {
	return nodes.NodeInfo{
		ID:           w.id,
		Type:         nodes.NodeTypeMemoryConsolidation,
		State:        nodes.NodeStateReady,
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
	w.store.PutMemory(schemas.Memory{
		MemoryID:       schemas.IDPrefixSummary + agentID + "_" + sessionID,
		MemoryType:     string(schemas.MemoryTypeSemantic),
		AgentID:        agentID,
		SessionID:      sessionID,
		SourceEventIDs: sourceIDs,
		Content:        combined,
		Summary:        fmt.Sprintf("Consolidated from %d level-0 memories", len(sourceIDs)),
		Level:          1,
		IsActive:       true,
		Version:        1,
	})
	return nil
}
