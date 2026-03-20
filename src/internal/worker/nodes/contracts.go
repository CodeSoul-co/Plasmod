package nodes

import (
	"andb/src/internal/dataplane"
	"andb/src/internal/schemas"
)

type NodeType string

const (
	NodeTypeData  NodeType = "data_node"
	NodeTypeIndex NodeType = "index_node"
	NodeTypeQuery NodeType = "query_node"

	// Ingestion & Materialization group (spec section 16.4)
	NodeTypeIngest                NodeType = "ingest_worker"
	NodeTypeObjectMaterialization NodeType = "object_materialization_worker"
	NodeTypeMemoryExtraction      NodeType = "memory_extraction_worker"
	NodeTypeStateMaterialization  NodeType = "state_materialization_worker"
	NodeTypeToolTrace             NodeType = "tool_trace_worker"

	// Memory & Governance group
	NodeTypeMemoryConsolidation NodeType = "memory_consolidation_worker"
	NodeTypeReflectionPolicy    NodeType = "reflection_policy_worker"
	NodeTypeCommunication       NodeType = "communication_worker"
	NodeTypeConflictMerge       NodeType = "conflict_merge_worker"

	// Retrieval & Reasoning group
	NodeTypeIndexBuild    NodeType = "index_build_worker"
	NodeTypeGraphRelation NodeType = "graph_relation_worker"
	NodeTypeProofTrace    NodeType = "proof_trace_worker"
	NodeTypeSubgraph      NodeType = "subgraph_executor_worker"
	NodeTypeMicroBatch    NodeType = "micro_batch_scheduler"

	// Cognitive compression
	NodeTypeSummarization NodeType = "summarization_worker"
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

// SubgraphExecutorWorker expands a seed set of object IDs into an
// EvidenceSubgraph by performing one-hop (or filtered) graph expansion using
// the canonical graph_expand logic from the schemas package.
type SubgraphExecutorWorker interface {
	Info() NodeInfo
	Expand(req schemas.GraphExpandRequest, nodes []schemas.GraphNode, edges []schemas.Edge) schemas.GraphExpandResponse
}

// ProofTraceWorker assembles explainable proof traces from the derivation log
// and graph index for a given query result set.
// maxDepth controls how many hops to traverse (1 = immediate edges only,
// 0 or negative = unlimited BFS up to an internal cap of 8).
type ProofTraceWorker interface {
	Info() NodeInfo
	AssembleTrace(objectIDs []string, maxDepth int) []string
}

// ─── Ingestion & Materialization worker interfaces ────────────────────────────

// IngestWorker performs schema validation and normalisation on a raw Event
// before it enters the WAL.  It does not write to the WAL itself.
type IngestWorker interface {
	Info() NodeInfo
	Process(ev schemas.Event) error
}

// ObjectMaterializationWorker routes a raw Event to the appropriate canonical
// object store (Memory / State / Artifact) based on event_type.
type ObjectMaterializationWorker interface {
	Info() NodeInfo
	Materialize(ev schemas.Event) error
}

// StateMaterializationWorker maintains running agent State objects from events
// and creates periodic checkpoints (ObjectVersion snapshots).
type StateMaterializationWorker interface {
	Info() NodeInfo
	Apply(ev schemas.Event) error
	Checkpoint(agentID, sessionID string) error
}

// ToolTraceWorker converts tool_call / tool_result events into structured
// Artifact records for audit and retrieval.
type ToolTraceWorker interface {
	Info() NodeInfo
	TraceToolCall(ev schemas.Event) error
}

// ─── Index & Retrieval worker interfaces ─────────────────────────────────────

// IndexBuildWorker submits a materialised object to the segment + keyword
// indices for later retrieval.
type IndexBuildWorker interface {
	Info() NodeInfo
	IndexObject(objectID, objectType, namespace, text string) error
}

// ─── Multi-Agent coordination worker interfaces ───────────────────────────────

// CommunicationWorker synchronises agent-to-agent messages and distributes
// shared Memory objects to target agent memory spaces.
type CommunicationWorker interface {
	Info() NodeInfo
	Broadcast(fromAgentID, toAgentID, memoryID string) error
}

// MicroBatchScheduler accumulates pending retrieval tasks and flushes them as
// a micro-batch for cross-agent merging and GPU-friendly execution.
type MicroBatchScheduler interface {
	Info() NodeInfo
	Enqueue(queryID string, payload any)
	Flush() []any
}

// SummarizationWorker compresses long-context memory sequences into level-1
// (summary) and level-2 (abstraction) Memory objects.
type SummarizationWorker interface {
	Info() NodeInfo
	Summarize(agentID, sessionID string, maxLevel int) error
}
