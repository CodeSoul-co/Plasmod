package cognitive

import (
	"fmt"

	"andb/src/internal/schemas"
	"andb/src/internal/storage"
	"andb/src/internal/worker/nodes"
)

// InMemoryMemoryExtractionWorker derives a level-0 Memory object from an
// event payload and persists it via the ObjectStore.
type InMemoryMemoryExtractionWorker struct {
	id    string
	store storage.ObjectStore
}

func CreateInMemoryMemoryExtractionWorker(id string, store storage.ObjectStore) *InMemoryMemoryExtractionWorker {
	return &InMemoryMemoryExtractionWorker{id: id, store: store}
}

func (w *InMemoryMemoryExtractionWorker) Info() nodes.NodeInfo {
	return nodes.NodeInfo{
		ID:           w.id,
		Type:         nodes.NodeTypeMemoryExtraction,
		State:        nodes.NodeStateReady,
		Capabilities: []string{"memory_extract", "level0_record"},
	}
}

func (w *InMemoryMemoryExtractionWorker) Extract(eventID, agentID, sessionID, content string) error {
	w.store.PutMemory(schemas.Memory{
		MemoryID:       fmt.Sprintf("mem_%s", eventID),
		MemoryType:     string(schemas.MemoryTypeEpisodic),
		AgentID:        agentID,
		SessionID:      sessionID,
		SourceEventIDs: []string{eventID},
		Content:        content,
		Level:          0,
		IsActive:       true,
		Version:        1,
	})
	return nil
}
