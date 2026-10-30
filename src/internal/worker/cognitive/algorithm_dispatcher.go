package cognitive

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"plasmod/src/internal/schemas"
	"plasmod/src/internal/storage"
	"plasmod/src/internal/worker/nodes"
)

// InMemoryAlgorithmDispatchWorker is a pure routing and persistence layer that
// bridges a MemoryManagementAlgorithm plugin into the cognitive worker pipeline.
//
// Design contract — the dispatcher NEVER makes content-level decisions:
//   - It does not decide when a memory should be decayed, compressed, or archived.
//   - It does not apply any threshold-based business logic.
//   - Every lifecycle transition is driven exclusively by
//     MemoryAlgorithmState.SuggestedLifecycleState returned by the algorithm.
//   - Every Memory object produced by Compress/Summarize is stored verbatim
//     (all fields including LifecycleState are set by the algorithm).
//
// Supported operations (via Run / Dispatch):
//
//	"ingest"    — algo.Ingest(); persists returned MemoryAlgorithmState records;
//	              marks AlgorithmStateRef on each processed memory.
//	"decay"     — algo.Decay(nowTS); persists states; applies SuggestedLifecycleState
//	              from each state if non-empty.
//	"recall"    — algo.Recall(query, candidates); returns ScoredRefs in algorithm order.
//	"compress"  — algo.Compress(); stores derived memories verbatim; persists any
//	              MemoryAlgorithmState records the algorithm returns for source memories.
//	"summarize" — algo.Summarize(); stores summary memories verbatim.
//	"update"    — algo.Update(memories, signals); persists returned state updates.
type InMemoryAlgorithmDispatchWorker struct {
	mu           sync.Mutex
	id           string
	algo         schemas.MemoryManagementAlgorithm
	objStore     storage.ObjectStore
	algoStore    storage.MemoryAlgorithmStateStore
	auditStore   storage.AuditStore
	runtimeStore storage.RuntimeStorage
}

func CreateAlgorithmDispatchWorker(
	id string,
	algo schemas.MemoryManagementAlgorithm,
	objStore storage.ObjectStore,
	algoStore storage.MemoryAlgorithmStateStore,
	auditStore storage.AuditStore,
	runtimeStores ...storage.RuntimeStorage,
) *InMemoryAlgorithmDispatchWorker {
	w := &InMemoryAlgorithmDispatchWorker{
		id:         id,
		algo:       algo,
		objStore:   objStore,
		algoStore:  algoStore,
		auditStore: auditStore,
	}
	if len(runtimeStores) > 0 {
		w.runtimeStore = runtimeStores[0]
	}
	return w
}

func (w *InMemoryAlgorithmDispatchWorker) Info() nodes.NodeInfo {
	return nodes.NodeInfo{
		ID:           w.id,
		Type:         nodes.NodeTypeAlgorithmDispatch,
		State:        nodes.NodeStateReady,
		Capabilities: []string{"algo_ingest", "algo_decay", "algo_recall", "algo_compress", "algo_summarize", "algo_update"},
	}
}

// Run implements nodes.Runnable for typed-dispatch.
func (w *InMemoryAlgorithmDispatchWorker) Run(input schemas.WorkerInput) (schemas.WorkerOutput, error) {
	in, ok := input.(schemas.AlgorithmDispatchInput)
	if !ok {
		return schemas.AlgorithmDispatchOutput{}, fmt.Errorf("algorithm_dispatch: unexpected input type %T", input)
	}
	return w.Dispatch(in.Operation, in.MemoryIDs, in.Query, in.NowTS, in.AgentID, in.SessionID, in.Signals)
}

// Dispatch implements nodes.AlgorithmDispatchWorker.
func (w *InMemoryAlgorithmDispatchWorker) Dispatch(
	operation string,
	memoryIDs []string,
	query, nowTS, agentID, sessionID string,
	signals map[string]float64,
) (schemas.AlgorithmDispatchOutput, error) {
	return w.DispatchDurable(schemas.AlgorithmDispatchInput{Operation: operation, MemoryIDs: memoryIDs, Query: query, NowTS: nowTS, AgentID: agentID, SessionID: sessionID, Signals: signals}, nil)
}

func (w *InMemoryAlgorithmDispatchWorker) DispatchDurable(in schemas.AlgorithmDispatchInput, persist func(string, []schemas.Memory, []schemas.Memory) error) (schemas.AlgorithmDispatchOutput, error) {
	operation, memoryIDs, query, nowTS, agentID, sessionID, signals := in.Operation, in.MemoryIDs, in.Query, in.NowTS, in.AgentID, in.SessionID, in.Signals
	w.mu.Lock()
	defer w.mu.Unlock()
	memories := w.fetchMemories(memoryIDs)
	// Restore per-memory governance from the durable canonical store, including after restart.
	for _, m := range memories {
		if stored, ok := w.objStore.GetState("governance:" + w.algo.AlgorithmID() + ":" + m.MemoryID); ok {
			var state schemas.MemoryAlgorithmState
			if err := json.Unmarshal([]byte(stored.StateValue), &state); err != nil {
				return schemas.AlgorithmDispatchOutput{}, err
			}
			w.algo.LoadState(state)
		}
	}
	if restorer, ok := w.algo.(interface{ RestoreMemories([]schemas.Memory) }); ok {
		restorer.RestoreMemories(w.objStore.ListMemories(agentID, ""))
	}
	ctx := schemas.AlgorithmContext{
		AgentID:   agentID,
		SessionID: sessionID,
		Timestamp: nowTS,
	}
	if ctx.Timestamp == "" {
		ctx.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}

	switch operation {
	case "ingest":
		return w.runIngest(memories, ctx)
	case "decay":
		ts := nowTS
		if ts == "" {
			ts = ctx.Timestamp
		}
		return w.runDecay(memories, ts)
	case "recall":
		return w.runRecall(query, memories, ctx)
	case "compress", "summarize":
		if len(memories) == 0 {
			return schemas.AlgorithmDispatchOutput{Operation: operation}, nil
		}
		var derived []schemas.Memory
		if operation == "compress" {
			derived = w.algo.Compress(memories, ctx)
		} else {
			derived = w.algo.Summarize(memories, ctx)
		}
		if persist != nil {
			if err := persist(operation, derived, memories); err != nil {
				return schemas.AlgorithmDispatchOutput{Operation: operation}, err
			}
		} else {
			for _, m := range derived {
				w.objStore.PutMemory(m)
			}
		}
		var states []schemas.MemoryAlgorithmState
		var ids []string
		for _, m := range derived {
			ids = append(ids, m.MemoryID)
		}
		for _, m := range memories {
			if st, ok := w.algo.ExportState(m.MemoryID); ok {
				states = append(states, st)
			}
		}
		if err := w.persistStatesAndApply(states); err != nil {
			return schemas.AlgorithmDispatchOutput{Operation: operation}, err
		}
		return schemas.AlgorithmDispatchOutput{Operation: operation, ProducedIDs: ids, UpdatedCount: len(states)}, nil
	case "update":
		return w.runUpdate(memories, signals)
	default:
		return schemas.AlgorithmDispatchOutput{}, fmt.Errorf("algorithm_dispatch: unknown operation %q", operation)
	}
}

// ─── internal helpers ─────────────────────────────────────────────────────────

func (w *InMemoryAlgorithmDispatchWorker) fetchMemories(ids []string) []schemas.Memory {
	out := make([]schemas.Memory, 0, len(ids))
	for _, id := range ids {
		if m, ok := w.objStore.GetMemory(id); ok && m.IsActive {
			out = append(out, m)
		}
	}
	return out
}

// persistStatesAndApply saves each MemoryAlgorithmState returned by the algorithm
// and honours SuggestedLifecycleState if the algorithm has set one.
// The dispatcher applies no logic of its own — it is a pure persistence relay.
func (w *InMemoryAlgorithmDispatchWorker) persistStatesAndApply(states []schemas.MemoryAlgorithmState) error {
	now := time.Now().UTC().Format(time.RFC3339)
	for i := range states {
		if states[i].UpdatedAt == "" {
			states[i].UpdatedAt = now
		}
		encoded, err := json.Marshal(states[i])
		if err != nil {
			return err
		}
		if memory, ok := w.objStore.GetMemory(states[i].MemoryID); ok && memory.IsActive {
			memory.AlgorithmStateRef = w.algo.AlgorithmID()
			if states[i].SuggestedLifecycleState != "" {
				memory.LifecycleState = states[i].SuggestedLifecycleState
			}
			state := schemas.State{StateID: "governance:" + w.algo.AlgorithmID() + ":" + memory.MemoryID, TenantID: memory.TenantID, WorkspaceID: memory.WorkspaceID, AgentID: memory.AgentID, SessionID: memory.SessionID, StateType: "memory_governance", StateValue: string(encoded), Access: memory.Access, Version: 1}
			if w.runtimeStore != nil {
				if err := w.runtimeStore.ApplyCanonicalProjection(storage.CanonicalProjection{Memory: &memory, State: &state}); err != nil {
					return err
				}
			} else {
				w.objStore.PutMemory(memory)
				w.objStore.PutState(state)
			}
			w.algoStore.PutAlgorithmState(states[i])
		}
		w.emitAudit(states[i].MemoryID)
	}
	return nil
}

func (w *InMemoryAlgorithmDispatchWorker) emitAudit(memoryID string) {
	w.auditStore.AppendAudit(schemas.AuditRecord{
		RecordID:       fmt.Sprintf("audit_algo_%s_%d", memoryID, time.Now().UnixNano()),
		TargetMemoryID: memoryID,
		OperationType:  string(schemas.AuditOpAlgorithmUpdate),
		ActorType:      "system",
		ActorID:        w.algo.AlgorithmID(),
		Decision:       "allow",
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
	})
}

// runIngest calls algo.Ingest, persists state, and records which algorithm
// processed each memory via AlgorithmStateRef.
func (w *InMemoryAlgorithmDispatchWorker) runIngest(memories []schemas.Memory, ctx schemas.AlgorithmContext) (schemas.AlgorithmDispatchOutput, error) {
	fresh := make([]schemas.Memory, 0, len(memories))
	for _, m := range memories {
		if _, exists := w.algo.ExportState(m.MemoryID); !exists {
			fresh = append(fresh, m)
		}
	}
	states := w.algo.Ingest(fresh, ctx)
	// Reevaluate existing candidates when independent evidence arrives; identical
	// content is the conservative fallback until semantic extraction normalizes it.
	var candidates []schemas.Memory
	for _, m := range w.objStore.ListMemories(ctx.AgentID, "") {
		if !m.IsActive || m.LifecycleState != "candidate" {
			continue
		}
		for _, added := range fresh {
			if m.TenantID == added.TenantID && m.WorkspaceID == added.WorkspaceID && m.AgentID == added.AgentID && strings.EqualFold(strings.TrimSpace(m.Content), strings.TrimSpace(added.Content)) {
				candidates = append(candidates, m)
				break
			}
		}
	}
	if len(candidates) > 0 {
		states = append(states, w.algo.Update(candidates, nil)...)
	}
	if err := w.persistStatesAndApply(states); err != nil {
		return schemas.AlgorithmDispatchOutput{}, err
	}
	return schemas.AlgorithmDispatchOutput{
		Operation:    "ingest",
		UpdatedCount: len(states),
	}, nil
}

// runDecay calls algo.Decay and persists the returned states.
// Any lifecycle transition is driven solely by SuggestedLifecycleState in each
// returned MemoryAlgorithmState — the dispatcher applies no threshold of its own.
func (w *InMemoryAlgorithmDispatchWorker) runDecay(memories []schemas.Memory, nowTS string) (schemas.AlgorithmDispatchOutput, error) {
	states := w.algo.Decay(memories, nowTS)
	if err := w.persistStatesAndApply(states); err != nil {
		return schemas.AlgorithmDispatchOutput{}, err
	}
	return schemas.AlgorithmDispatchOutput{
		Operation:    "decay",
		UpdatedCount: len(states),
	}, nil
}

// runRecall calls algo.Recall and returns the scored memory IDs in the order
// the algorithm produced them, persisting any reinforcement state.
func (w *InMemoryAlgorithmDispatchWorker) runRecall(query string, candidates []schemas.Memory, ctx schemas.AlgorithmContext) (schemas.AlgorithmDispatchOutput, error) {
	scored := w.algo.Recall(query, candidates, ctx)
	states := make([]schemas.MemoryAlgorithmState, 0, len(candidates))
	for _, m := range candidates {
		if state, ok := w.algo.ExportState(m.MemoryID); ok {
			states = append(states, state)
		}
	}
	if err := w.persistStatesAndApply(states); err != nil {
		return schemas.AlgorithmDispatchOutput{}, err
	}
	refs := make([]string, 0, len(scored))
	for _, s := range scored {
		refs = append(refs, s.Memory.MemoryID)
	}
	return schemas.AlgorithmDispatchOutput{
		Operation:  "recall",
		ScoredRefs: refs,
	}, nil
}

// runUpdate calls algo.Update with explicit signal map and persists states.
func (w *InMemoryAlgorithmDispatchWorker) runUpdate(memories []schemas.Memory, signals map[string]float64) (schemas.AlgorithmDispatchOutput, error) {
	states := w.algo.Update(memories, signals)
	if err := w.persistStatesAndApply(states); err != nil {
		return schemas.AlgorithmDispatchOutput{}, err
	}
	return schemas.AlgorithmDispatchOutput{
		Operation:    "update",
		UpdatedCount: len(states),
	}, nil
}
