package baseline

import (
	"fmt"

	"andb/src/internal/schemas"
	"andb/src/internal/storage"
	"andb/src/internal/worker/nodes"
)

// InMemoryMemoryExtractionWorker derives a level-0 episodic Memory from an
// event payload.  This is the baseline algorithm's extraction pipeline step;
// other algorithms may implement this pattern differently.
type InMemoryMemoryExtractionWorker struct {
	id    string
	store storage.ObjectStore
}

func CreateInMemoryMemoryExtractionWorker(id string, store storage.ObjectStore) *InMemoryMemoryExtractionWorker {
	return &InMemoryMemoryExtractionWorker{id: id, store: store}
}

func (w *InMemoryMemoryExtractionWorker) Run(input schemas.WorkerInput) (schemas.WorkerOutput, error) {
	in, ok := input.(schemas.MemoryExtractionInput)
	if !ok {
		return schemas.MemoryExtractionOutput{}, fmt.Errorf("memory_extraction: unexpected input type %T", input)
	}
	if err := w.Extract(in.EventID, in.AgentID, in.SessionID, in.Content); err != nil {
		return schemas.MemoryExtractionOutput{}, err
	}
	return schemas.MemoryExtractionOutput{MemoryID: schemas.IDPrefixMemory + in.EventID}, nil
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
		LifecycleState: string(schemas.MemoryLifecycleActive),
		Version:        1,
	})
	return nil
}
