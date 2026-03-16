package nodes

import "andb/src/internal/dataplane"

type NodeType string

const (
	NodeTypeData  NodeType = "data_node"
	NodeTypeIndex NodeType = "index_node"
	NodeTypeQuery NodeType = "query_node"
)

type NodeState string

const (
	NodeStateReady NodeState = "ready"
)

type NodeInfo struct {
	ID           string    `json:"id"`
	Type         NodeType  `json:"type"`
	State        NodeState `json:"state"`
	Capabilities []string  `json:"capabilities"`
}

type DataNode interface {
	Info() NodeInfo
	HandleIngest(record dataplane.IngestRecord)
}

type IndexNode interface {
	Info() NodeInfo
	BuildIndex(record dataplane.IngestRecord)
}

type QueryNode interface {
	Info() NodeInfo
	Search(input dataplane.SearchInput) dataplane.SearchOutput
}
