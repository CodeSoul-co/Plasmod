package nodes

import "andb/src/internal/dataplane"

type NodeType string

const (
	NodeTypeData  NodeType = "data_node"
	NodeTypeIndex NodeType = "index_node"
	NodeTypeQuery NodeType = "query_node"

	// Ingestion & Materialization group (spec section 16.4)
	NodeTypeIngest               NodeType = "ingest_worker"
	NodeTypeObjectMaterialization NodeType = "object_materialization_worker"
	NodeTypeMemoryExtraction     NodeType = "memory_extraction_worker"
	NodeTypeStateMaterialization NodeType = "state_materialization_worker"
	NodeTypeToolTrace            NodeType = "tool_trace_worker"

	// Memory & Governance group
	NodeTypeMemoryConsolidation NodeType = "memory_consolidation_worker"
	NodeTypeReflectionPolicy    NodeType = "reflection_policy_worker"
	NodeTypeCommunication       NodeType = "communication_worker"
	NodeTypeConflictMerge       NodeType = "conflict_merge_worker"

	// Retrieval & Reasoning group
	NodeTypeIndexBuild          NodeType = "index_build_worker"
	NodeTypeGraphRelation       NodeType = "graph_relation_worker"
	NodeTypeProofTrace          NodeType = "proof_trace_worker"
	NodeTypeMicroBatch          NodeType = "micro_batch_scheduler"
)

type NodeState string

const (
	NodeStateReady NodeState = "ready"
	NodeStateBusy  NodeState = "busy"
)

type NodeInfo struct {
	ID           string    `json:"id"`
	Type         NodeType  `json:"type"`
	State        NodeState `json:"state"`
	Capabilities []string  `json:"capabilities"`
}

// ─── Data-plane node interfaces ───────────────────────────────────────────────

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

// ─── Memory & Governance worker interfaces ────────────────────────────────────

// MemoryExtractionWorker derives Memory objects from raw Event records.
type MemoryExtractionWorker interface {
	Info() NodeInfo
	Extract(eventID, agentID, sessionID, content string) error
}

// MemoryConsolidationWorker merges or distils lower-level memories into
// higher-level summaries (level 0 → 1 → 2 distillation chain).
type MemoryConsolidationWorker interface {
	Info() NodeInfo
	Consolidate(agentID, sessionID string) error
}

// ReflectionPolicyWorker applies policy rules (TTL decay, quarantine) to
// existing memory objects based on policy decision log entries.
type ReflectionPolicyWorker interface {
	Info() NodeInfo
	Reflect(objectID, objectType string) error
}

// ConflictMergeWorker detects and resolves fact / plan / state conflicts
// between concurrent agent writes.
type ConflictMergeWorker interface {
	Info() NodeInfo
	Merge(leftID, rightID, objectType string) error
}

// ─── Retrieval & Reasoning worker interfaces ──────────────────────────────────

// GraphRelationWorker maintains the graph/edge index from derivation and
// causal references embedded in Event and Memory objects.
type GraphRelationWorker interface {
	Info() NodeInfo
	IndexEdge(srcID, srcType, dstID, dstType, edgeType string, weight float64) error
}

// ProofTraceWorker assembles explainable proof traces from the derivation log
// and graph index for a given query result set.
type ProofTraceWorker interface {
	Info() NodeInfo
	AssembleTrace(objectIDs []string) []string
}
