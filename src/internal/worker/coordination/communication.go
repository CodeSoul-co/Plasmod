package coordination

import (
	"fmt"

	"andb/src/internal/storage"
	"andb/src/internal/worker/nodes"
)

// InMemoryCommunicationWorker broadcasts a Memory object from one agent to
// another by copying it into the target agent's memory space with a
// "shared_from" provenance reference.
type InMemoryCommunicationWorker struct {
	id       string
	objStore storage.ObjectStore
}

func CreateInMemoryCommunicationWorker(id string, objStore storage.ObjectStore) *InMemoryCommunicationWorker {
	return &InMemoryCommunicationWorker{id: id, objStore: objStore}
}

func (w *InMemoryCommunicationWorker) Info() nodes.NodeInfo {
	return nodes.NodeInfo{
		ID:           w.id,
		Type:         nodes.NodeTypeCommunication,
		State:        nodes.NodeStateReady,
		Capabilities: []string{"memory_broadcast", "shared_memory_distribution"},
	}
}

func (w *InMemoryCommunicationWorker) Broadcast(fromAgentID, toAgentID, memoryID string) error {
	if fromAgentID == toAgentID {
		return nil
	}
	mem, ok := w.objStore.GetMemory(memoryID)
	if !ok {
		return nil
	}
	shared := mem
	shared.MemoryID = fmt.Sprintf("shared_%s_to_%s", memoryID, toAgentID)
	shared.AgentID = toAgentID
	shared.ProvenanceRef = fmt.Sprintf("shared_from:%s/%s", fromAgentID, memoryID)
	shared.IsActive = true
	w.objStore.PutMemory(shared)
	return nil
}
