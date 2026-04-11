package cognitive

import (
	"fmt"
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
	id         string
	algo       schemas.MemoryManagementAlgorithm
	objStore   storage.ObjectStore
	algoStore  storage.MemoryAlgorithmStateStore
	auditStore storage.AuditStore
}

func CreateAlgorithmDispatchWorker(
	id string,
	algo schemas.MemoryManagementAlgorithm,
	objStore storage.ObjectStore,
	algoStore storage.MemoryAlgorithmStateStore,
	auditStore storage.AuditStore,
) *InMemoryAlgorithmDispatchWorker {
	return &InMemoryAlgorithmDispatchWorker{
		id:         id,
		algo:       algo,
		objStore:   objStore,
		algoStore:  algoStore,
		auditStore: auditStore,
	}
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
	memories := w.fetchMemories(memoryIDs)
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
	case "compress":
		return w.runCompress(memories, ctx)
	case "summarize":
		return w.runSummarize(memories, ctx)
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
		if m, ok := w.objStore.GetMemory(id); ok {
			out = append(out, m)
		}
	}
	return out
}

// persistStatesAndApply saves each MemoryAlgorithmState returned by the algorithm
// and honours SuggestedLifecycleState if the algorithm has set one.
// The dispatcher applies no logic of its own — it is a pure persistence relay.
func (w *InMemoryAlgorithmDispatchWorker) persistStatesAndApply(states []schemas.MemoryAlgorithmState) {
	now := time.Now().UTC().Format(time.RFC3339)
	for i := range states {
		if states[i].UpdatedAt == "" {
			states[i].UpdatedAt = now
		}
		w.algoStore.PutAlgorithmState(states[i])
		// Apply lifecycle suggestion verbatim — the algorithm decides; we just relay.
		if states[i].SuggestedLifecycleState != "" {
			if m, ok := w.objStore.GetMemory(states[i].MemoryID); ok {
				m.LifecycleState = states[i].SuggestedLifecycleState
				w.objStore.PutMemory(m)
			}
		}
		w.emitAudit(states[i].MemoryID)
	}
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
	states := w.algo.Ingest(memories, ctx)
	w.persistStatesAndApply(states)
	for _, m := range memories {
		m.AlgorithmStateRef = w.algo.AlgorithmID()
		w.objStore.PutMemory(m)
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
	w.persistStatesAndApply(states)
	return schemas.AlgorithmDispatchOutput{
		Operation:    "decay",
		UpdatedCount: len(states),
	}, nil
}

// runRecall calls algo.Recall and returns the scored memory IDs in the order
// the algorithm produced them.  No state is persisted for a recall pass.
func (w *InMemoryAlgorithmDispatchWorker) runRecall(query string, candidates []schemas.Memory, ctx schemas.AlgorithmContext) (schemas.AlgorithmDispatchOutput, error) {
	scored := w.algo.Recall(query, candidates, ctx)
	refs := make([]string, 0, len(scored))
	for _, s := range scored {
		refs = append(refs, s.Memory.MemoryID)
	}
	return schemas.AlgorithmDispatchOutput{
		Operation:  "recall",
		ScoredRefs: refs,
	}, nil
}

// runCompress calls algo.Compress and stores every derived Memory verbatim.
// The algorithm owns all field values on derived memories including LifecycleState.
// If the algorithm also returns MemoryAlgorithmState records (e.g. to mark sources),
// those are handled via a secondary Ingest/Update call by the orchestrator.
func (w *InMemoryAlgorithmDispatchWorker) runCompress(memories []schemas.Memory, ctx schemas.AlgorithmContext) (schemas.AlgorithmDispatchOutput, error) {
	derived := w.algo.Compress(memories, ctx)
	ids := make([]string, 0, len(derived))
	for _, m := range derived {
		w.objStore.PutMemory(m)
		ids = append(ids, m.MemoryID)
	}
	return schemas.AlgorithmDispatchOutput{
		Operation:   "compress",
		ProducedIDs: ids,
	}, nil
}

// runSummarize calls algo.Summarize and stores every summary Memory verbatim.
// The algorithm owns all field values on summary memories.
func (w *InMemoryAlgorithmDispatchWorker) runSummarize(memories []schemas.Memory, ctx schemas.AlgorithmContext) (schemas.AlgorithmDispatchOutput, error) {
	summaries := w.algo.Summarize(memories, ctx)
	ids := make([]string, 0, len(summaries))
	for _, m := range summaries {
		w.objStore.PutMemory(m)
		ids = append(ids, m.MemoryID)
	}
	return schemas.AlgorithmDispatchOutput{
		Operation:   "summarize",
		ProducedIDs: ids,
	}, nil
}

// runUpdate calls algo.Update with explicit signal map and persists states.
func (w *InMemoryAlgorithmDispatchWorker) runUpdate(memories []schemas.Memory, signals map[string]float64) (schemas.AlgorithmDispatchOutput, error) {
	states := w.algo.Update(memories, signals)
	w.persistStatesAndApply(states)
	return schemas.AlgorithmDispatchOutput{
		Operation:    "update",
		UpdatedCount: len(states),
	}, nil
}
