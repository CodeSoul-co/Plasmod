package materialization

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"plasmod/src/internal/eventbackbone"
	"plasmod/src/internal/schemas"
	"plasmod/src/internal/storage"
	"plasmod/src/internal/worker/nodes"
)

// InMemoryStateMaterializationWorker maintains a live map of agent+session
// State objects and creates ObjectVersion snapshots on Checkpoint calls.
type InMemoryStateMaterializationWorker struct {
	id       string
	objStore storage.ObjectStore
	verStore storage.SnapshotVersionStore
	derivLog eventbackbone.DerivationLogger

	mu sync.Mutex
}

func CreateInMemoryStateMaterializationWorker(
	id string,
	objStore storage.ObjectStore,
	verStore storage.SnapshotVersionStore,
	derivLog eventbackbone.DerivationLogger,
) *InMemoryStateMaterializationWorker {
	return &InMemoryStateMaterializationWorker{
		id:       id,
		objStore: objStore,
		verStore: verStore,
		derivLog: derivLog,
	}
}

func (w *InMemoryStateMaterializationWorker) Run(input schemas.WorkerInput) (schemas.WorkerOutput, error) {
	switch in := input.(type) {
	case schemas.StateApplyInput:
		in.Event = in.Event.NormalizeDynamicEventV04()
		err := w.Apply(in.Event)
		if err != nil {
			return schemas.StateApplyOutput{}, err
		}
		stateKey := in.Event.StateKey()
		stateID := schemas.CanonicalStateID(
			in.Event.Identity.TenantID,
			in.Event.Identity.WorkspaceID,
			in.Event.Actor.AgentID,
			in.Event.Actor.SessionID,
			stateKey,
		)
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
	ev = ev.NormalizeDynamicEventV04()
	if ev.Payload == nil {
		return nil
	}
	stateKey := ev.StateKey()
	stateVal := ev.StateValueString()
	if stateKey == "" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	stateID := schemas.CanonicalStateID(
		ev.Identity.TenantID,
		ev.Identity.WorkspaceID,
		ev.Actor.AgentID,
		ev.Actor.SessionID,
		stateKey,
	)
	w.mu.Lock()
	defer w.mu.Unlock()
	version := int64(1)
	if prev, ok := w.objStore.GetState(stateID); ok {
		if prev.DerivedFromEventID == ev.Identity.EventID {
			return nil
		}
		version = prev.Version + 1
		if latest, found := w.verStore.LatestVersion(stateID); found {
			latest.ValidTo = now
			if len(latest.Snapshot) == 0 {
				latest.Snapshot = canonicalWorkerSnapshot(prev)
			}
			w.verStore.PutVersion(latest)
		}
	}
	state := schemas.State{
		StateID:            stateID,
		TenantID:           ev.Identity.TenantID,
		WorkspaceID:        ev.Identity.WorkspaceID,
		AgentID:            ev.Actor.AgentID,
		SessionID:          ev.Actor.SessionID,
		StateType:          ev.EventInfo.EventType,
		StateKey:           stateKey,
		StateValue:         stateVal,
		DerivedFromEventID: ev.Identity.EventID,
		CheckpointTS:       now,
		Version:            version,
		MutationLSN:        ev.Time.WalLSN,
		Access:             schemas.CanonicalAccessFromEvent(ev),
	}
	w.objStore.PutState(state)
	w.verStore.PutVersion(schemas.ObjectVersion{
		ObjectID:        stateID,
		ObjectType:      string(schemas.ObjectTypeAgentState),
		Version:         version,
		MutationEventID: ev.Identity.EventID,
		ValidFrom:       now,
		SnapshotTag:     "state_apply",
		MutationLSN:     ev.Time.WalLSN,
		Snapshot:        canonicalWorkerSnapshot(state),
		Access:          state.Access,
	})
	if w.derivLog != nil {
		w.derivLog.Append(ev.Identity.EventID, "event", stateID, string(schemas.ObjectTypeAgentState), "state_apply")
	}
	return nil
}

func (w *InMemoryStateMaterializationWorker) Checkpoint(agentID, sessionID string) error {
	states := w.objStore.ListStates(agentID, sessionID)
	now := time.Now().UTC().Format(time.RFC3339)
	for _, s := range states {
		w.verStore.PutVersion(schemas.ObjectVersion{
			ObjectID:    s.StateID,
			ObjectType:  string(schemas.ObjectTypeAgentState),
			Version:     s.Version,
			ValidFrom:   now,
			SnapshotTag: fmt.Sprintf("checkpoint_%s", now),
			MutationLSN: s.MutationLSN,
			Snapshot:    canonicalWorkerSnapshot(s),
			Access:      s.Access,
		})
	}
	return nil
}

func canonicalWorkerSnapshot(value any) map[string]any {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var snapshot map[string]any
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return nil
	}
	return snapshot
}
