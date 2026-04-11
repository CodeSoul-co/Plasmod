package coordination

import (
	"fmt"

	"plasmod/src/internal/schemas"
	"plasmod/src/internal/storage"
	"plasmod/src/internal/worker/nodes"
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

func (w *InMemoryCommunicationWorker) Run(input schemas.WorkerInput) (schemas.WorkerOutput, error) {
	in, ok := input.(schemas.BroadcastInput)
	if !ok {
		return schemas.BroadcastOutput{}, fmt.Errorf("communication: unexpected input type %T", input)
	}
	err := w.Broadcast(in.FromAgentID, in.ToAgentID, in.MemoryID)
	if err != nil {
		return schemas.BroadcastOutput{}, err
	}
	if in.FromAgentID == in.ToAgentID {
		return schemas.BroadcastOutput{}, nil
	}
	sharedID := schemas.IDPrefixShared + in.MemoryID + "_to_" + in.ToAgentID
	if _, ok := w.objStore.GetMemory(sharedID); !ok {
		return schemas.BroadcastOutput{}, nil // source memory did not exist
	}
	return schemas.BroadcastOutput{SharedMemoryID: sharedID}, nil
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
	shared.MemoryID = schemas.IDPrefixShared + memoryID + "_to_" + toAgentID
	shared.AgentID = toAgentID
	shared.ProvenanceRef = fmt.Sprintf("shared_from:%s/%s", fromAgentID, memoryID)
	shared.IsActive = true
	w.objStore.PutMemory(shared)
	return nil
}
