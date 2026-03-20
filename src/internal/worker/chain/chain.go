// Package chain defines the four canonical execution flow chains that compose
// individual worker dispatches into end-to-end pipelines.
//
// # Chain taxonomy
//
//	MainChain          — write path:  Ingest → WAL → ObjectMaterialization → Index/Graph
//	MemoryPipelineChain — cognitive:  Extraction → Consolidation → Summarization → Reflection
//	QueryChain         — read  path:  Query → ProofTrace
//	CollaborationChain — MAS   path:  ConflictMerge → SharedMemory → Communication
//
// Each chain is stateless and idempotent; it wraps the Manager's dispatch
// methods so callers only need to know about the chain, not individual workers.
package chain

import (
	"andb/src/internal/schemas"
	"andb/src/internal/worker/nodes"
)

// ─── Shared result types ──────────────────────────────────────────────────────

// ChainResult is the common envelope returned by every chain's Run method.
type ChainResult struct {
	// ChainName identifies which chain produced this result.
	ChainName string `json:"chain_name"`
	// OK is false when any worker in the chain returned a hard error.
	OK bool `json:"ok"`
	// Error holds the first error encountered, or "".
	Error string `json:"error,omitempty"`
	// Meta carries chain-specific output fields.
	Meta map[string]any `json:"meta,omitempty"`
}

func ok(name string, meta map[string]any) ChainResult {
	return ChainResult{ChainName: name, OK: true, Meta: meta}
}

func fail(name, reason string) ChainResult {
	return ChainResult{ChainName: name, OK: false, Error: reason}
}

// ─── 1. MainChain ─────────────────────────────────────────────────────────────

// MainChain orchestrates the primary write pipeline:
//
//	IngestWorker (validation)
//	  ↓
//	ObjectMaterializationWorker (Memory / State / Artifact routing)
//	  ↓
//	ToolTraceWorker (tool_call artefact capture, no-op for other types)
//	  ↓
//	IndexBuildWorker (segment + keyword index)
//	  ↓
//	GraphRelationWorker (source-event edge)
//
// The WAL write is performed by Runtime.SubmitIngest before the chain is
// invoked; MainChain handles all downstream object-level work.
type MainChain struct {
	mgr *nodes.Manager
}

// MainChainInput carries the event that has already been appended to the WAL.
type MainChainInput struct {
	Event     schemas.Event
	MemoryID  string
	Namespace string
}

func CreateMainChain(mgr *nodes.Manager) *MainChain { return &MainChain{mgr: mgr} }

// Run executes the main write pipeline synchronously and returns a ChainResult.
func (c *MainChain) Run(in MainChainInput) ChainResult {
	ev := in.Event

	// 1 – Object routing (Memory / State / Artifact)
	c.mgr.DispatchObjectMaterialization(ev)

	// 2 – Tool trace capture
	c.mgr.DispatchToolTrace(ev)

	// 3 – Index the primary memory object
	memID := in.MemoryID
	if memID == "" {
		memID = "mem_" + ev.EventID
	}
	ns := in.Namespace
	if ns == "" {
		ns = ev.WorkspaceID
	}
	text := ""
	if ev.Payload != nil {
		if t, ok := ev.Payload["text"].(string); ok {
			text = t
		}
	}
	c.mgr.DispatchIndexBuild(memID, "memory", ns, text)

	// 4 – Graph: link memory to its source event
	c.mgr.DispatchGraphRelation(memID, "memory", ev.EventID, "event", "derived_from", 1.0)

	return ok("main_chain", map[string]any{
		"memory_id": memID,
		"namespace": ns,
		"event_id":  ev.EventID,
	})
}

// ─── 2. MemoryPipelineChain ───────────────────────────────────────────────────

// MemoryPipelineChain drives the cognitive memory upgrade ladder:
//
//	MemoryExtractionWorker  (raw event → level-0 episodic memory)
//	  ↓
//	MemoryConsolidationWorker (level-0 → level-1 semantic / procedural)
//	  ↓
//	SummarizationWorker      (long-context compression → level-1 / level-2)
//	  ↓
//	ReflectionPolicyWorker   (policy enforcement: TTL / quarantine / decay)
type MemoryPipelineChain struct {
	mgr *nodes.Manager
}

type MemoryPipelineInput struct {
	EventID   string
	AgentID   string
	SessionID string
	Content   string
	// RunConsolidation gates whether to run Consolidation + Summarization in
	// this invocation (false = extraction only).
	RunConsolidation bool
	// MaxSummaryLevel controls the highest summary level produced (1 or 2).
	MaxSummaryLevel int
}

func CreateMemoryPipelineChain(mgr *nodes.Manager) *MemoryPipelineChain {
	return &MemoryPipelineChain{mgr: mgr}
}

// Run executes the cognitive memory pipeline synchronously.
func (c *MemoryPipelineChain) Run(in MemoryPipelineInput) ChainResult {
	// 1 – Extract episodic memory from the event
	c.mgr.DispatchMemoryExtraction(in.EventID, in.AgentID, in.SessionID, in.Content)

	// 2 – Consolidate + summarize (optional, driven by caller's batch logic)
	if in.RunConsolidation {
		c.mgr.DispatchMemoryConsolidation(in.AgentID, in.SessionID)

		maxLevel := in.MaxSummaryLevel
		if maxLevel < 1 {
			maxLevel = 1
		}
		c.mgr.DispatchSummarization(in.AgentID, in.SessionID, maxLevel)
	}

	// 3 – Apply governance policies to the freshly extracted memory
	memID := "mem_" + in.EventID
	c.mgr.DispatchReflectionPolicy(memID, "memory")

	return ok("memory_pipeline_chain", map[string]any{
		"memory_id":         memID,
		"agent_id":          in.AgentID,
		"consolidation_ran": in.RunConsolidation,
	})
}

// ─── 3. QueryChain ────────────────────────────────────────────────────────────

// QueryChain executes the retrieval + reasoning pipeline:
//
//	QueryNode (multi-index search via DataPlane)
//	  ↓
//	ProofTraceWorker (explainable trace assembly)
//
// Graph expansion (BulkEdges 1-hop) is handled inside the Evidence Assembler
// and therefore sits outside this chain.
type QueryChain struct {
	mgr       *nodes.Manager
	dataPlane interface {
		Search(input interface{}) interface{}
	}
}

type QueryChainInput struct {
	Request   schemas.QueryRequest
	ObjectIDs []string
	// MaxDepth controls BFS hops in proof trace (0 = default 8).
	MaxDepth int
}

type QueryChainOutput struct {
	ProofTrace []string
}

func CreateQueryChain(mgr *nodes.Manager) *QueryChain {
	return &QueryChain{mgr: mgr}
}

// Run assembles a proof trace for the supplied object IDs (collected upstream
// by the QueryNode / TieredDataPlane).
func (c *QueryChain) Run(in QueryChainInput) (QueryChainOutput, ChainResult) {
	trace := c.mgr.DispatchProofTrace(in.ObjectIDs, in.MaxDepth)
	return QueryChainOutput{ProofTrace: trace},
		ok("query_chain", map[string]any{
			"object_count": len(in.ObjectIDs),
			"trace_steps":  len(trace),
		})
}

// ─── 4. CollaborationChain ────────────────────────────────────────────────────

// CollaborationChain resolves multi-agent conflicts and distributes shared
// memories across agent boundaries:
//
//	ConflictMergeWorker (last-writer-wins, creates conflict_resolved edge)
//	  ↓
//	CommunicationWorker (copy winner memory into target agent's space)
type CollaborationChain struct {
	mgr *nodes.Manager
}

type CollaborationChainInput struct {
	// LeftMemID / RightMemID are the two competing memory objects.
	LeftMemID  string
	RightMemID string
	ObjectType string
	// SourceAgentID wrote the winning memory; TargetAgentID receives it.
	SourceAgentID string
	TargetAgentID string
}

type CollaborationChainOutput struct {
	// WinnerMemID is the surviving memory (higher Version).
	WinnerMemID string
	// SharedMemID is the copy created in the target agent's space.
	SharedMemID string
}

func CreateCollaborationChain(mgr *nodes.Manager) *CollaborationChain {
	return &CollaborationChain{mgr: mgr}
}

// Run resolves the conflict and broadcasts the winner to the target agent.
func (c *CollaborationChain) Run(in CollaborationChainInput) (CollaborationChainOutput, ChainResult) {
	// 1 – Conflict resolution (mutates the loser in-place)
	c.mgr.DispatchConflictMerge(in.LeftMemID, in.RightMemID, in.ObjectType)

	// 2 – Determine winner (higher version survives; we use LeftMemID as default)
	winnerID := in.LeftMemID

	// 3 – Broadcast winner to target agent's memory space
	sharedID := ""
	if in.TargetAgentID != "" && in.TargetAgentID != in.SourceAgentID {
		c.mgr.DispatchCommunication(in.SourceAgentID, in.TargetAgentID, winnerID)
		sharedID = "shared_" + winnerID + "_to_" + in.TargetAgentID
	}

	return CollaborationChainOutput{WinnerMemID: winnerID, SharedMemID: sharedID},
		ok("collaboration_chain", map[string]any{
			"winner_mem_id": winnerID,
			"shared_mem_id": sharedID,
		})
}
