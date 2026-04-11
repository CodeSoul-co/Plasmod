package nodes

import "plasmod/src/internal/dataplane"

// InMemoryQueryNode delegates search requests to the attached DataPlane,
// enabling multi-tier (hot → warm → cold) retrieval behind a single interface.
type InMemoryQueryNode struct {
	id    string
	plane dataplane.DataPlane
}

func CreateInMemoryQueryNode(id string, plane dataplane.DataPlane) *InMemoryQueryNode {
	return &InMemoryQueryNode{id: id, plane: plane}
}

func (n *InMemoryQueryNode) Info() NodeInfo {
	return NodeInfo{
		ID:           n.id,
		Type:         NodeTypeQuery,
		State:        NodeStateReady,
		Capabilities: []string{"search", "planner_dispatch"},
	}
}

func (n *InMemoryQueryNode) Search(input dataplane.SearchInput) dataplane.SearchOutput {
	return n.plane.Search(input)
}
