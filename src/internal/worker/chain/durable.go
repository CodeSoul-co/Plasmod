package chain

import (
	"context"
	"fmt"

	"plasmod/src/internal/schemas"
)

// Submit keeps validation before WAL acceptance. The submit callback is the
// consistency controller, so bounded/eventual writes retain their read barrier.
func (c *MainChain) Submit(ctx context.Context, event schemas.Event, submit func(context.Context, schemas.Event) (map[string]any, error)) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := c.mgr.DispatchIngestValidation(event); err != nil {
		return nil, err
	}
	ack, err := submit(ctx, event)
	if ack != nil {
		ack["chain_name"] = "main_chain"
	}
	return ack, err
}

// Project is used for both live writes and WAL recovery. Visibility can advance
// only after canonical materialization, memory governance, and index projection.
func (c *MainChain) Project(ctx context.Context, canonical, memory, retrieval func() error) error {
	for i, stage := range []func() error{canonical, memory, retrieval} {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := stage(); err != nil {
			return fmt.Errorf("main_chain stage %d: %w", i, err)
		}
	}
	return nil
}

// RunAlgorithm is the durable MemoryPipelineChain entry. Persistence of derived
// objects happens before the algorithm's source lifecycle changes are committed.
func (c *MemoryPipelineChain) RunAlgorithm(in schemas.AlgorithmDispatchInput, persist func(string, []schemas.Memory, []schemas.Memory) error) (schemas.AlgorithmDispatchOutput, ChainResult) {
	out, err := c.mgr.DispatchAlgorithmChecked(in, persist)
	if err != nil {
		return out, fail("memory_pipeline_chain", err.Error())
	}
	return out, ok("memory_pipeline_chain", map[string]any{"operation": in.Operation, "updated_count": out.UpdatedCount, "produced_ids": out.ProducedIDs})
}

// Share uses the Runtime's policy check and WAL-derived write, never the legacy
// direct-copy worker. A failed write cannot be reported as a shared object.
func (c *CollaborationChain) Share(from, to, id, contract string, share func(string, string, string, string) (string, error)) (string, error) {
	return share(from, to, id, contract)
}

func (c *CollaborationChain) Resolve(left, right string) string {
	return c.mgr.DispatchConflictMergeWithWinner(left, right, "memory")
}

func (c *MemoryPipelineChain) Mutate(apply func() error) ChainResult {
	if err := apply(); err != nil {
		return fail("memory_pipeline_chain", err.Error())
	}
	return ok("memory_pipeline_chain", nil)
}

func (c *MainChain) MaterializeState(event schemas.Event) {
	c.mgr.DispatchStateMaterialization(event)
	if event.EventInfo.EventType == string(schemas.EventTypeCheckpoint) {
		c.mgr.DispatchStateCheckpoint(event.Actor.AgentID, event.Actor.SessionID)
	}
}
func (c *MainChain) CaptureToolTrace(event schemas.Event) { c.mgr.DispatchToolTrace(event) }
func (c *MemoryPipelineChain) Reflect(memoryID string) {
	c.mgr.DispatchReflectionPolicy(memoryID, "memory")
}
func (c *MemoryPipelineChain) Consolidate(agentID, sessionID string) {
	c.mgr.DispatchMemoryConsolidation(agentID, sessionID)
}

func (c *CollaborationChain) Decide(apply func() error) ChainResult {
	if err := apply(); err != nil {
		return fail("collaboration_chain", err.Error())
	}
	return ok("collaboration_chain", nil)
}

// Ingest owns semantic extraction, admission, then derived-profile persistence.
func (c *MemoryPipelineChain) Ingest(extract func() error, in schemas.AlgorithmDispatchInput, profile func() error) ChainResult {
	if err := extract(); err != nil {
		return fail("memory_pipeline_chain", err.Error())
	}
	_, result := c.RunAlgorithm(in, nil)
	if !result.OK {
		return result
	}
	if err := profile(); err != nil {
		return fail("memory_pipeline_chain", err.Error())
	}
	return result
}
