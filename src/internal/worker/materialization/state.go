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
	stateKey, _ := ev.Payload["state_key"].(string)
	stateVal, _ := ev.Payload["state_value"].(string)
	if stateKey == "" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	lookupKey := ev.AgentID + ":" + ev.SessionID + ":" + stateKey

	w.mu.Lock()
	existingID, exists := w.stateKeys[lookupKey]
	stateID := fmt.Sprintf("state_%s_%s", ev.AgentID, stateKey)
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
