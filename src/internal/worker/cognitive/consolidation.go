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
	})
	return nil
}
