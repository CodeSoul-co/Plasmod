package materialization

import (
	"fmt"
	"sync"
	"time"

	"andb/src/internal/schemas"
	"andb/src/internal/storage"
	"andb/src/internal/worker/nodes"
)

// InMemoryStateMaterializationWorker maintains a live map of agent+session
// State objects and creates ObjectVersion snapshots on Checkpoint calls.
type InMemoryStateMaterializationWorker struct {
	id       string
	objStore storage.ObjectStore
	verStore storage.SnapshotVersionStore

	mu        sync.Mutex
	stateKeys map[string]string // "agentID:sessionID:stateKey" → stateID
}

func CreateInMemoryStateMaterializationWorker(
	id string,
	objStore storage.ObjectStore,
	verStore storage.SnapshotVersionStore,
) *InMemoryStateMaterializationWorker {
	return &InMemoryStateMaterializationWorker{
		id:        id,
		objStore:  objStore,
		verStore:  verStore,
		stateKeys: make(map[string]string),
	}
}

func (w *InMemoryStateMaterializationWorker) Run(input schemas.WorkerInput) (schemas.WorkerOutput, error) {
	switch in := input.(type) {
	case schemas.StateApplyInput:
		err := w.Apply(in.Event)
		if err != nil {
			return schemas.StateApplyOutput{}, err
		}
		stateKey, _ := in.Event.Payload[schemas.PayloadKeyStateKey].(string)
		stateID := schemas.IDPrefixState + in.Event.AgentID + "_" + stateKey
		var ver int64
		if s, ok := w.objStore.GetState(stateID); ok {
			ver = s.Version
		}
		return schemas.StateApplyOutput{StateID: stateID, Version: ver}, nil

	case schemas.StateCheckpointInput:
		err := w.Checkpoint(in.AgentID, in.SessionID)
		if err != nil {
			return schemas.StateCheckpointOutput{}, err
		}
		states := w.objStore.ListStates(in.AgentID, in.SessionID)
		return schemas.StateCheckpointOutput{SnapshotCount: len(states)}, nil

	default:
		return schemas.StateApplyOutput{}, fmt.Errorf("state_mat: unexpected input type %T", input)
	}
}

func (w *InMemoryStateMaterializationWorker) Info() nodes.NodeInfo {
	return nodes.NodeInfo{
		ID:           w.id,
		Type:         nodes.NodeTypeStateMaterialization,
		State:        nodes.NodeStateReady,
		Capabilities: []string{"state_apply", "state_checkpoint"},
	}
}

func (w *InMemoryStateMaterializationWorker) Apply(ev schemas.Event) error {
	if ev.Payload == nil {
		return nil
	}
	stateKey, _ := ev.Payload[schemas.PayloadKeyStateKey].(string)
	stateVal, _ := ev.Payload[schemas.PayloadKeyStateValue].(string)
	if stateKey == "" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	lookupKey := ev.AgentID + ":" + ev.SessionID + ":" + stateKey

	w.mu.Lock()
	existingID, exists := w.stateKeys[lookupKey]
	stateID := schemas.IDPrefixState + ev.AgentID + "_" + stateKey
	w.stateKeys[lookupKey] = stateID
	w.mu.Unlock()

	var version int64 = 1
	if exists {
		if prev, ok := w.objStore.GetState(existingID); ok {
			version = prev.Version + 1
		}
	}
	w.objStore.PutState(schemas.State{
		StateID:            stateID,
		AgentID:            ev.AgentID,
		SessionID:          ev.SessionID,
		StateType:          ev.EventType,
		StateKey:           stateKey,
		StateValue:         stateVal,
		DerivedFromEventID: ev.EventID,
		CheckpointTS:       now,
		Version:            version,
	})
	return nil
}

func (w *InMemoryStateMaterializationWorker) Checkpoint(agentID, sessionID string) error {
	states := w.objStore.ListStates(agentID, sessionID)
	now := time.Now().UTC().Format(time.RFC3339)
	for _, s := range states {
		w.verStore.PutVersion(schemas.ObjectVersion{
			ObjectID:    s.StateID,
			ObjectType:  "state",
			Version:     s.Version,
			ValidFrom:   now,
			SnapshotTag: fmt.Sprintf("checkpoint_%s", now),
		})
	}
	return nil
}
