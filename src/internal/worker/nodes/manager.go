package nodes

import (
	"sync"

	"andb/src/internal/dataplane"
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
}

func NewManager() *Manager {
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
	m.mu.Lock(); defer m.mu.Unlock()
	m.memExtractionWorkers = append(m.memExtractionWorkers, w)
}

func (m *Manager) RegisterMemoryConsolidation(w MemoryConsolidationWorker) {
	m.mu.Lock(); defer m.mu.Unlock()
	m.memConsolidWorkers = append(m.memConsolidWorkers, w)
}

func (m *Manager) RegisterReflectionPolicy(w ReflectionPolicyWorker) {
	m.mu.Lock(); defer m.mu.Unlock()
	m.reflectionWorkers = append(m.reflectionWorkers, w)
}

func (m *Manager) RegisterConflictMerge(w ConflictMergeWorker) {
	m.mu.Lock(); defer m.mu.Unlock()
	m.conflictMergeWorkers = append(m.conflictMergeWorkers, w)
}

func (m *Manager) RegisterGraphRelation(w GraphRelationWorker) {
	m.mu.Lock(); defer m.mu.Unlock()
	m.graphWorkers = append(m.graphWorkers, w)
}

func (m *Manager) RegisterProofTrace(w ProofTraceWorker) {
	m.mu.Lock(); defer m.mu.Unlock()
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

// ─── Topology ─────────────────────────────────────────────────────────────────

func (m *Manager) Topology() []NodeInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []NodeInfo{}
	for _, n := range m.dataNodes            { out = append(out, n.Info()) }
	for _, n := range m.indexNodes           { out = append(out, n.Info()) }
	for _, n := range m.queryNodes           { out = append(out, n.Info()) }
	for _, n := range m.memExtractionWorkers { out = append(out, n.Info()) }
	for _, n := range m.memConsolidWorkers   { out = append(out, n.Info()) }
	for _, n := range m.reflectionWorkers    { out = append(out, n.Info()) }
	for _, n := range m.conflictMergeWorkers { out = append(out, n.Info()) }
	for _, n := range m.graphWorkers         { out = append(out, n.Info()) }
	for _, n := range m.proofTraceWorkers    { out = append(out, n.Info()) }
	return out
}
