package nodes

import (
	"sync"

	"andb/src/internal/dataplane"
	"andb/src/internal/schemas"
)

type Manager struct {
	mu                   sync.RWMutex
	dataNodes            []DataNode
	indexNodes           []IndexNode
	queryNodes           []QueryNode
	memExtractionWorkers []MemoryExtractionWorker
	memConsolidWorkers   []MemoryConsolidationWorker
	reflectionWorkers    []ReflectionPolicyWorker
	conflictMergeWorkers []ConflictMergeWorker
	graphWorkers         []GraphRelationWorker
	proofTraceWorkers    []ProofTraceWorker

	// Ingestion & Materialization
	ingestWorkers    []IngestWorker
	objMatWorkers    []ObjectMaterializationWorker
	stateMatWorkers  []StateMaterializationWorker
	toolTraceWorkers []ToolTraceWorker

	// Index & Retrieval
	indexBuildWorkers []IndexBuildWorker

	// Multi-Agent Coordination
	commWorkers       []CommunicationWorker
	microBatchWorkers []MicroBatchScheduler

	// Cognitive Compression
	summarizationWorkers []SummarizationWorker
}

func CreateManager() *Manager {
	return &Manager{
		dataNodes:            []DataNode{},
		indexNodes:           []IndexNode{},
		queryNodes:           []QueryNode{},
		memExtractionWorkers: []MemoryExtractionWorker{},
		memConsolidWorkers:   []MemoryConsolidationWorker{},
		reflectionWorkers:    []ReflectionPolicyWorker{},
		conflictMergeWorkers: []ConflictMergeWorker{},
		graphWorkers:         []GraphRelationWorker{},
		proofTraceWorkers:    []ProofTraceWorker{},
		ingestWorkers:        []IngestWorker{},
		objMatWorkers:        []ObjectMaterializationWorker{},
		stateMatWorkers:      []StateMaterializationWorker{},
		toolTraceWorkers:     []ToolTraceWorker{},
		indexBuildWorkers:    []IndexBuildWorker{},
		commWorkers:          []CommunicationWorker{},
		microBatchWorkers:    []MicroBatchScheduler{},
		summarizationWorkers: []SummarizationWorker{},
	}
}

func (m *Manager) RegisterData(node DataNode) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dataNodes = append(m.dataNodes, node)
}

func (m *Manager) RegisterIndex(node IndexNode) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.indexNodes = append(m.indexNodes, node)
}

func (m *Manager) RegisterQuery(node QueryNode) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queryNodes = append(m.queryNodes, node)
}

// ─── Governance & Reasoning worker registration ───────────────────────────────

func (m *Manager) RegisterMemoryExtraction(w MemoryExtractionWorker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memExtractionWorkers = append(m.memExtractionWorkers, w)
}

func (m *Manager) RegisterMemoryConsolidation(w MemoryConsolidationWorker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memConsolidWorkers = append(m.memConsolidWorkers, w)
}

func (m *Manager) RegisterReflectionPolicy(w ReflectionPolicyWorker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reflectionWorkers = append(m.reflectionWorkers, w)
}

func (m *Manager) RegisterConflictMerge(w ConflictMergeWorker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.conflictMergeWorkers = append(m.conflictMergeWorkers, w)
}

func (m *Manager) RegisterGraphRelation(w GraphRelationWorker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.graphWorkers = append(m.graphWorkers, w)
}

func (m *Manager) RegisterProofTrace(w ProofTraceWorker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.proofTraceWorkers = append(m.proofTraceWorkers, w)
}

// ─── Dispatch ─────────────────────────────────────────────────────────────────

func (m *Manager) DispatchIngest(record dataplane.IngestRecord) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, node := range m.dataNodes {
		node.HandleIngest(record)
	}
	for _, node := range m.indexNodes {
		node.BuildIndex(record)
	}
}

func (m *Manager) DispatchQuery(input dataplane.SearchInput, fallback dataplane.DataPlane) dataplane.SearchOutput {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.queryNodes) > 0 {
		return m.queryNodes[0].Search(input)
	}
	return fallback.Search(input)
}

// DispatchMemoryExtraction runs all registered memory extraction workers.
func (m *Manager) DispatchMemoryExtraction(eventID, agentID, sessionID, content string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, w := range m.memExtractionWorkers {
		_ = w.Extract(eventID, agentID, sessionID, content)
	}
}

// DispatchProofTrace collects trace steps from all registered proof-trace workers.
func (m *Manager) DispatchProofTrace(objectIDs []string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	trace := []string{}
	for _, w := range m.proofTraceWorkers {
		trace = append(trace, w.AssembleTrace(objectIDs)...)
	}
	return trace
}

// DispatchMemoryConsolidation runs all registered consolidation workers for the
// given agent+session pair.  Called by the async subscriber after N events.
func (m *Manager) DispatchMemoryConsolidation(agentID, sessionID string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, w := range m.memConsolidWorkers {
		_ = w.Consolidate(agentID, sessionID)
	}
}

// DispatchReflectionPolicy evaluates governance rules for objectID via all
// registered reflection-policy workers.
func (m *Manager) DispatchReflectionPolicy(objectID, objectType string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, w := range m.reflectionWorkers {
		_ = w.Reflect(objectID, objectType)
	}
}

// DispatchConflictMerge resolves a competing-write conflict between leftID and
// rightID via all registered conflict-merge workers.
func (m *Manager) DispatchConflictMerge(leftID, rightID, objectType string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, w := range m.conflictMergeWorkers {
		_ = w.Merge(leftID, rightID, objectType)
	}
}

// DispatchGraphRelation indexes a typed edge via all registered graph workers.
func (m *Manager) DispatchGraphRelation(srcID, srcType, dstID, dstType, edgeType string, weight float64) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, w := range m.graphWorkers {
		_ = w.IndexEdge(srcID, srcType, dstID, dstType, edgeType, weight)
	}
}

// ─── Ingestion & Materialization registration ────────────────────────────────────

func (m *Manager) RegisterIngest(w IngestWorker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ingestWorkers = append(m.ingestWorkers, w)
}

func (m *Manager) RegisterObjectMaterialization(w ObjectMaterializationWorker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objMatWorkers = append(m.objMatWorkers, w)
}

func (m *Manager) RegisterStateMaterialization(w StateMaterializationWorker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stateMatWorkers = append(m.stateMatWorkers, w)
}

func (m *Manager) RegisterToolTrace(w ToolTraceWorker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toolTraceWorkers = append(m.toolTraceWorkers, w)
}

// ─── Index & Retrieval registration ──────────────────────────────────────────────

func (m *Manager) RegisterIndexBuild(w IndexBuildWorker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.indexBuildWorkers = append(m.indexBuildWorkers, w)
}

// ─── Multi-Agent Coordination registration ──────────────────────────────────────

func (m *Manager) RegisterCommunication(w CommunicationWorker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.commWorkers = append(m.commWorkers, w)
}

func (m *Manager) RegisterMicroBatch(w MicroBatchScheduler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.microBatchWorkers = append(m.microBatchWorkers, w)
}

// ─── Cognitive Compression registration ─────────────────────────────────────────

func (m *Manager) RegisterSummarization(w SummarizationWorker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.summarizationWorkers = append(m.summarizationWorkers, w)
}

// ─── New Dispatch methods ────────────────────────────────────────────────────────

// DispatchIngestValidation validates ev through all registered IngestWorkers.
// Returns the first error, or nil if all pass.
func (m *Manager) DispatchIngestValidation(ev schemas.Event) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, w := range m.ingestWorkers {
		if err := w.Process(ev); err != nil {
			return err
		}
	}
	return nil
}

// DispatchObjectMaterialization routes ev through all registered
// ObjectMaterializationWorkers (Memory / State / Artifact routing).
func (m *Manager) DispatchObjectMaterialization(ev schemas.Event) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, w := range m.objMatWorkers {
		_ = w.Materialize(ev)
	}
}

// DispatchStateMaterialization applies ev to all registered
// StateMaterializationWorkers.
func (m *Manager) DispatchStateMaterialization(ev schemas.Event) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, w := range m.stateMatWorkers {
		_ = w.Apply(ev)
	}
}

// DispatchStateCheckpoint triggers a checkpoint snapshot for agentID+sessionID.
func (m *Manager) DispatchStateCheckpoint(agentID, sessionID string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, w := range m.stateMatWorkers {
		_ = w.Checkpoint(agentID, sessionID)
	}
}

// DispatchToolTrace sends a tool_call / tool_result event to all registered
// ToolTraceWorkers.
func (m *Manager) DispatchToolTrace(ev schemas.Event) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, w := range m.toolTraceWorkers {
		_ = w.TraceToolCall(ev)
	}
}

// DispatchIndexBuild submits an object to all registered IndexBuildWorkers.
func (m *Manager) DispatchIndexBuild(objectID, objectType, namespace, text string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, w := range m.indexBuildWorkers {
		_ = w.IndexObject(objectID, objectType, namespace, text)
	}
}

// DispatchCommunication broadcasts a memory from fromAgent to toAgent via all
// registered CommunicationWorkers.
func (m *Manager) DispatchCommunication(fromAgentID, toAgentID, memoryID string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, w := range m.commWorkers {
		_ = w.Broadcast(fromAgentID, toAgentID, memoryID)
	}
}

// DispatchSummarization compresses memories for agentID+sessionID up to
// maxLevel via all registered SummarizationWorkers.
func (m *Manager) DispatchSummarization(agentID, sessionID string, maxLevel int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, w := range m.summarizationWorkers {
		_ = w.Summarize(agentID, sessionID, maxLevel)
	}
}

// FlushMicroBatch collects and returns all batched payloads from every
// registered MicroBatchScheduler.
func (m *Manager) FlushMicroBatch() []any {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var all []any
	for _, w := range m.microBatchWorkers {
		all = append(all, w.Flush()...)
	}
	return all
}

// ─── Topology ─────────────────────────────────────────────────────────────────

func (m *Manager) Topology() []NodeInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []NodeInfo{}
	for _, n := range m.dataNodes {
		out = append(out, n.Info())
	}
	for _, n := range m.indexNodes {
		out = append(out, n.Info())
	}
	for _, n := range m.queryNodes {
		out = append(out, n.Info())
	}
	for _, n := range m.memExtractionWorkers {
		out = append(out, n.Info())
	}
	for _, n := range m.memConsolidWorkers {
		out = append(out, n.Info())
	}
	for _, n := range m.reflectionWorkers {
		out = append(out, n.Info())
	}
	for _, n := range m.conflictMergeWorkers {
		out = append(out, n.Info())
	}
	for _, n := range m.graphWorkers {
		out = append(out, n.Info())
	}
	for _, n := range m.proofTraceWorkers {
		out = append(out, n.Info())
	}
	for _, n := range m.ingestWorkers {
		out = append(out, n.Info())
	}
	for _, n := range m.objMatWorkers {
		out = append(out, n.Info())
	}
	for _, n := range m.stateMatWorkers {
		out = append(out, n.Info())
	}
	for _, n := range m.toolTraceWorkers {
		out = append(out, n.Info())
	}
	for _, n := range m.indexBuildWorkers {
		out = append(out, n.Info())
	}
	for _, n := range m.commWorkers {
		out = append(out, n.Info())
	}
	for _, n := range m.microBatchWorkers {
		out = append(out, n.Info())
	}
	for _, n := range m.summarizationWorkers {
		out = append(out, n.Info())
	}
	return out
}
