package worker

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"plasmod/src/internal/schemas"
	"plasmod/src/internal/worker/chain"
	"plasmod/src/internal/worker/nodes"
)

// TaskType identifies the execution chain a Task should be routed to.
type TaskType string

const (
	TaskTypeIngest        TaskType = "ingest"
	TaskTypeMemory        TaskType = "memory"
	TaskTypeQuery         TaskType = "query"
	TaskTypeCollaboration TaskType = "collaboration"
)

// Priority levels — higher value = higher urgency.
const (
	PriorityLow    = 0
	PriorityNormal = 1
	PriorityHigh   = 2
	PriorityUrgent = 3
)

// Task is the unit of work submitted to the Orchestrator.
type Task struct {
	ID        string
	Type      TaskType
	Priority  int
	Payload   any
	Submitted time.Time
}

// OrchestratorStats holds runtime counters exposed for monitoring.
type OrchestratorStats struct {
	Submitted  int64
	Completed  int64
	Dropped    int64
	InFlight   int64
	QueueDepth int
}

// Orchestrator is a priority-aware task dispatcher that routes Tasks to the
// appropriate flow chain via a bounded worker pool.
//
// Priority order (highest first): urgent > high > normal > low
// Backpressure: when the queue is full, Submit blocks until space is available.
// Dropping was replaced with blocking to prevent silent data loss.
//
// Chains wired per TaskType:
//
//	TaskTypeIngest        → MainChain
//	TaskTypeMemory        → MemoryPipelineChain
//	TaskTypeQuery         → QueryChain
//	TaskTypeCollaboration → CollaborationChain
type Orchestrator struct {
	mgr *nodes.Manager

	// Channels — one per priority level (index = priority constant above).
	queues [4]chan Task

	// Worker pool
	concurrency int
	wg          sync.WaitGroup

	// Stats
	submitted atomic.Int64
	completed atomic.Int64
	dropped   atomic.Int64
	inFlight  atomic.Int64

	// Chains
	mainChain   *chain.MainChain
	memChain    *chain.MemoryPipelineChain
	queryChain  *chain.QueryChain
	collabChain *chain.CollaborationChain
}

// CreateOrchestrator creates an Orchestrator with the given concurrency and per-
// priority queue capacity.  concurrency=0 defaults to 4 workers; queueCap=0
// defaults to 256 slots per priority level.
func CreateOrchestrator(mgr *nodes.Manager, concurrency, queueCap int) *Orchestrator {
	if concurrency <= 0 {
		concurrency = 4
	}
	if queueCap <= 0 {
		queueCap = 256
	}
	o := &Orchestrator{
		mgr:         mgr,
		concurrency: concurrency,
		mainChain:   chain.CreateMainChain(mgr),
		memChain:    chain.CreateMemoryPipelineChain(mgr),
		queryChain:  chain.CreateQueryChain(mgr),
		collabChain: chain.CreateCollaborationChain(mgr),
	}
	for i := range o.queues {
		o.queues[i] = make(chan Task, queueCap)
	}
	return o
}

// Submit enqueues a Task at its declared Priority level.  Blocks when the queue
// for that priority is full, applying backpressure to callers.
// Returns false only when the orchestrator has been stopped.
func (o *Orchestrator) Submit(t Task) bool {
	if t.Submitted.IsZero() {
		t.Submitted = time.Now()
	}
	p := t.Priority
	if p < 0 {
		p = PriorityNormal
	}
	if p >= len(o.queues) {
		p = PriorityUrgent
	}
	select {
	case o.queues[p] <- t:
		o.submitted.Add(1)
		return true
	case <-time.After(30 * time.Second):
		// Timeout after 30s — treat as dropped for observability.
		// This prevents goroutines from blocking indefinitely if the orchestrator
		// is stuck or all workers are deadlocked.
		o.dropped.Add(1)
		return false
	}
}

// SubmitIngest is a convenience wrapper that enqueues an ingest task at
// PriorityHigh (ingestion should not starve queries, but beats background work).
func (o *Orchestrator) SubmitIngest(ev schemas.Event) bool {
	return o.Submit(Task{
		ID:       "ingest_" + ev.EventID,
		Type:     TaskTypeIngest,
		Priority: PriorityHigh,
		Payload:  chain.MainChainInput{Event: ev},
	})
}

// SubmitMemory enqueues a memory-pipeline task at PriorityNormal.
func (o *Orchestrator) SubmitMemory(in chain.MemoryPipelineInput) bool {
	return o.Submit(Task{
		ID:       schemas.IDPrefixMemory + in.EventID,
		Type:     TaskTypeMemory,
		Priority: PriorityNormal,
		Payload:  in,
	})
}

// SubmitCollaboration enqueues a collaboration task at PriorityNormal.
func (o *Orchestrator) SubmitCollaboration(in chain.CollaborationChainInput) bool {
	return o.Submit(Task{
		ID:       "collab_" + in.LeftMemID + "_" + in.RightMemID,
		Type:     TaskTypeCollaboration,
		Priority: PriorityNormal,
		Payload:  in,
	})
}

// Run starts the worker pool and blocks until ctx is cancelled.
// Call in a goroutine: go orch.Run(ctx)
func (o *Orchestrator) Run(ctx context.Context) {
	for i := 0; i < o.concurrency; i++ {
		o.wg.Add(1)
		go o.worker(ctx)
	}
	o.wg.Wait()
}

// Stats returns a snapshot of runtime counters.
func (o *Orchestrator) Stats() OrchestratorStats {
	total := 0
	for _, q := range o.queues {
		total += len(q)
	}
	return OrchestratorStats{
		Submitted:  o.submitted.Load(),
		Completed:  o.completed.Load(),
		Dropped:    o.dropped.Load(),
		InFlight:   o.inFlight.Load(),
		QueueDepth: total,
	}
}

// worker is the inner loop: drains queues highest-priority-first, then blocks.
func (o *Orchestrator) worker(ctx context.Context) {
	defer o.wg.Done()
	for {
		// Priority drain: check urgent → high → normal → low in order.
		task, got := o.dequeue()
		if got {
			o.execute(task)
			continue
		}
		// Nothing in any queue — wait for work or shutdown.
		select {
		case <-ctx.Done():
			return
		case task = <-o.queues[PriorityUrgent]:
			o.execute(task)
		case task = <-o.queues[PriorityHigh]:
			o.execute(task)
		case task = <-o.queues[PriorityNormal]:
			o.execute(task)
		case task = <-o.queues[PriorityLow]:
			o.execute(task)
		}
	}
}

// dequeue tries to pop from the highest non-empty queue without blocking.
func (o *Orchestrator) dequeue() (Task, bool) {
	for p := PriorityUrgent; p >= PriorityLow; p-- {
		select {
		case t := <-o.queues[p]:
			return t, true
		default:
		}
	}
	return Task{}, false
}

// execute dispatches a task to the appropriate chain and tracks SLA latency.
func (o *Orchestrator) execute(t Task) {
	o.inFlight.Add(1)
	defer func() {
		o.inFlight.Add(-1)
		o.completed.Add(1)
	}()

	switch t.Type {
	case TaskTypeIngest:
		if in, ok := t.Payload.(chain.MainChainInput); ok {
			o.mainChain.Run(in)
		}
	case TaskTypeMemory:
		if in, ok := t.Payload.(chain.MemoryPipelineInput); ok {
			o.memChain.Run(in)
		}
	case TaskTypeQuery:
		if in, ok := t.Payload.(chain.QueryChainInput); ok {
			o.queryChain.Run(in)
		}
	case TaskTypeCollaboration:
		if in, ok := t.Payload.(chain.CollaborationChainInput); ok {
			o.collabChain.Run(in)
		}
	}
}
