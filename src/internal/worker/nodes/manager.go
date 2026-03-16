package nodes

import (
	"sync"

	"andb/src/internal/dataplane"
)

type Manager struct {
	mu         sync.RWMutex
	dataNodes  []DataNode
	indexNodes []IndexNode
	queryNodes []QueryNode
}

func NewManager() *Manager {
	return &Manager{dataNodes: []DataNode{}, indexNodes: []IndexNode{}, queryNodes: []QueryNode{}}
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

func (m *Manager) Topology() []NodeInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]NodeInfo, 0, len(m.dataNodes)+len(m.indexNodes)+len(m.queryNodes))
	for _, n := range m.dataNodes {
		out = append(out, n.Info())
	}
	for _, n := range m.indexNodes {
		out = append(out, n.Info())
	}
	for _, n := range m.queryNodes {
		out = append(out, n.Info())
	}
	return out
}
