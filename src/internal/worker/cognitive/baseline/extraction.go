package baseline

import (
	"fmt"

	"plasmod/src/internal/eventbackbone"
	"plasmod/src/internal/schemas"
	"plasmod/src/internal/storage"
	"plasmod/src/internal/worker/nodes"
)

// InMemoryMemoryExtractionWorker derives a level-0 episodic Memory from an
// event payload.  This is the baseline algorithm's extraction pipeline step;
// other algorithms may implement this pattern differently.
type InMemoryMemoryExtractionWorker struct {
	id       string
	store    storage.ObjectStore
	derivLog eventbackbone.DerivationLogger
}

func CreateInMemoryMemoryExtractionWorker(id string, store storage.ObjectStore, derivLog eventbackbone.DerivationLogger) *InMemoryMemoryExtractionWorker {
	return &InMemoryMemoryExtractionWorker{id: id, store: store, derivLog: derivLog}
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
	memID := schemas.IDPrefixMemory + eventID
	w.store.PutMemory(schemas.Memory{
		MemoryID:       memID,
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
	if w.derivLog != nil {
		w.derivLog.Append(eventID, "event", memID, "memory", "extraction")
	}
	return nil
}
