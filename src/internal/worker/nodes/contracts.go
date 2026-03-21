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
//
// # Anti-Corruption Layer Design
//
// This interface defines the contract for memory consolidation without coupling
// to any specific implementation. The current in-memory implementation uses
// simple string concatenation as a placeholder (defaultDummyImplementation).
//
// # Future LLM Integration
//
// Production implementations SHOULD inject an LLMProvider dependency to perform
// true semantic compression. The recommended integration pattern:
//
//	type LLMConsolidationWorker struct {
//	    llm       LLMProvider           // injected dependency
//	    store     storage.ObjectStore
//	    promptTpl string                // consolidation prompt template
//	}
//
// The LLMProvider interface (to be defined in pkg/llm) should expose:
//
//	type LLMProvider interface {
//	    Complete(ctx context.Context, prompt string) (string, error)
//	    Embed(ctx context.Context, text string) ([]float32, error)
//	}
//
// Implementations may integrate with MemGPT, Letta, LangMem, or custom LLM
// backends for advanced memory management strategies.
type MemoryConsolidationWorker interface {
	Info() NodeInfo
	// Consolidate merges all active level-0 episodic memories for the given
	// agent+session into a single level-1 semantic memory.
	//
	// Current implementation: simple string concatenation (placeholder).
	// Future implementation: LLM-based semantic compression.
	Consolidate(agentID, sessionID string) error
}

// ReflectionPolicyWorker applies policy rules (TTL decay, quarantine) to
// existing memory objects based on policy decision log entries.
type ReflectionPolicyWorker interface {
	Info() NodeInfo
	Reflect(objectID, objectType string) error
}

// ConflictMergeWorker detects and resolves fact / plan / state conflicts
// between concurrent agent writes. Returns the winnerID (higher version wins).
type ConflictMergeWorker interface {
	Info() NodeInfo
	Merge(leftID, rightID, objectType string) (winnerID string, err error)
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
// Enqueue returns any auto-flushed payloads when the threshold is reached.
type MicroBatchScheduler interface {
	Info() NodeInfo
	Enqueue(queryID string, payload any) (flushed []any)
	Flush() []any
	SetThreshold(size int)
}

// SummarizationWorker compresses long-context memory sequences into level-1
// (summary) and level-2 (abstraction) Memory objects.
//
// # Anti-Corruption Layer Design
//
// This interface defines the contract for multi-level memory summarization
// without coupling to any specific implementation. The current in-memory
// implementation uses simple string concatenation as a placeholder
// (defaultDummyImplementation).
//
// # Memory Hierarchy
//
//	Level 0: Episodic memories (raw event extractions)
//	Level 1: Semantic summaries (consolidated from level-0)
//	Level 2: Procedural abstractions (meta-summaries from level-1)
//
// # Future LLM Integration
//
// Production implementations SHOULD inject an LLMProvider dependency to perform
// true semantic summarization. The recommended integration pattern:
//
//	type LLMSummarizationWorker struct {
//	    llm          LLMProvider           // injected dependency
//	    store        storage.ObjectStore
//	    summaryTpl   string                // level-1 summary prompt
//	    abstractTpl  string                // level-2 abstraction prompt
//	    maxTokens    int                   // context window budget
//	}
//
// The LLMProvider interface (to be defined in pkg/llm) should expose:
//
//	type LLMProvider interface {
//	    Complete(ctx context.Context, prompt string) (string, error)
//	    Embed(ctx context.Context, text string) ([]float32, error)
//	}
//
// Implementations may integrate with MemGPT, Letta, LangMem, or custom LLM
// backends for advanced memory compression and retrieval-augmented generation.
type SummarizationWorker interface {
	Info() NodeInfo
	// Summarize compresses memories for the given agent+session up to maxLevel.
	//
	// maxLevel=1: Consolidate level-0 → level-1 semantic summaries.
	// maxLevel=2: Additionally compress level-1 → level-2 abstractions.
	//
	// Current implementation: simple string concatenation (placeholder).
	// Future implementation: LLM-based hierarchical summarization.
	Summarize(agentID, sessionID string, maxLevel int) error
}

// ─── Typed-dispatch interface ─────────────────────────────────────────────────

// Runnable is implemented by every worker that supports typed dispatch via
// schemas.WorkerInput / schemas.WorkerOutput.
//
// It is optional — all workers additionally expose concrete domain methods
// (Process, Materialize, Consolidate, …) for direct chain / manager calls.
// Run provides a uniform entry point when the caller holds a WorkerInput value
// without knowing the concrete worker type.
//
// Implementations must type-assert the input to their expected Input struct,
// delegate to the concrete method, and return the corresponding Output struct.
// An unknown input type must return a descriptive error without panicking.
type Runnable interface {
	Run(input schemas.WorkerInput) (schemas.WorkerOutput, error)
}
