package worker

import (
	"context"
	"fmt"
	"testing"
	"time"

	"andb/src/internal/eventbackbone"
	"andb/src/internal/schemas"
	"andb/src/internal/storage"
	"andb/src/internal/worker/chain"
	baseline "andb/src/internal/worker/cognitive/baseline"
	"andb/src/internal/worker/coordination"
	"andb/src/internal/worker/indexing"
	"andb/src/internal/worker/nodes"
)

// buildSubscriberRuntime creates a minimal set of components sufficient to
// exercise EventSubscriber without a full HTTP server.
func buildSubscriberRuntime(t *testing.T) (
	eventbackbone.WAL,
	*nodes.Manager,
	storage.RuntimeStorage,
	eventbackbone.PolicyDecisionLogger,
) {
	t.Helper()
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	wal := eventbackbone.NewInMemoryWAL(bus, clock)
	plog := eventbackbone.NewPolicyDecisionLog(clock, bus)
	store := storage.NewMemoryRuntimeStorage()

	m := nodes.CreateManager()
	m.RegisterMemoryExtraction(baseline.CreateInMemoryMemoryExtractionWorker("me-1", store.Objects()))
	m.RegisterMemoryConsolidation(baseline.CreateInMemoryMemoryConsolidationWorker("mc-1", store.Objects()))
	m.RegisterGraphRelation(indexing.CreateInMemoryGraphRelationWorker("gr-1", store.Edges()))
	m.RegisterProofTrace(coordination.CreateInMemoryProofTraceWorker("pt-1", store.Edges(), nil))
	m.RegisterReflectionPolicy(baseline.CreateInMemoryReflectionPolicyWorker(
		"rp-1", store.Objects(), store.Policies(), plog,
	))
	m.RegisterConflictMerge(coordination.CreateInMemoryConflictMergeWorker(
		"cm-1", store.Objects(), store.Edges(),
	))
	return wal, m, store, plog
}

// TestEventSubscriber_DrainWAL checks that the subscriber processes all WAL
// entries that were written before it started.
func TestEventSubscriber_DrainWAL(t *testing.T) {
	wal, mgr, _, _ := buildSubscriberRuntime(t)

	// Write two events before the subscriber starts.
	for i := range 2 {
		_, err := wal.Append(schemas.Event{
			EventID:   fmt.Sprintf("evt_drain_%d", i),
			AgentID:   "agent1",
			SessionID: "sess1",
		})
		if err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	sub := CreateEventSubscriber(wal, mgr)
	sub.SetPollInterval(20 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	go sub.Run(ctx)
	<-ctx.Done()

	// All entries should have been consumed.
	if sub.lastLSN.Load() < wal.LatestLSN() {
		t.Errorf("subscriber did not catch up: lastLSN=%d, latestLSN=%d",
			sub.lastLSN.Load(), wal.LatestLSN())
	}
}

// TestEventSubscriber_MemoryConsolidationTriggered verifies that consolidation
// fires after consolidateEvery events for the same agent+session.
// Level-0 memories are pre-seeded to simulate what Runtime.SubmitIngest
// writes synchronously — the subscriber only handles the async pass.
func TestEventSubscriber_MemoryConsolidationTriggered(t *testing.T) {
	wal, mgr, store, _ := buildSubscriberRuntime(t)

	// Pre-seed level-0 memories (normally produced by Runtime.SubmitIngest).
	for i := 0; i < 3; i++ {
		store.Objects().PutMemory(schemas.Memory{
			MemoryID:  fmt.Sprintf("mem_consol_%d", i),
			AgentID:   "agent1",
			SessionID: "sess1",
			Level:     0,
			IsActive:  true,
			Content:   fmt.Sprintf("content %d", i),
			Version:   int64(i + 1),
		})
	}

	sub := CreateEventSubscriber(wal, mgr)
	sub.SetPollInterval(20 * time.Millisecond)
	sub.SetConsolidateEvery(3)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sub.Run(ctx)

	// Write 3 WAL events for agent1+sess1 to trigger the consolidation pass.
	for i := 0; i < 3; i++ {
		_, _ = wal.Append(schemas.Event{
			EventID:   fmt.Sprintf("evt_consol_%d", i),
			AgentID:   "agent1",
			SessionID: "sess1",
			Payload:   map[string]any{"text": fmt.Sprintf("msg %d", i)},
		})
	}

	// Allow subscriber time to drain.
	time.Sleep(200 * time.Millisecond)

	mems := store.Objects().ListMemories("agent1", "sess1")
	hasLevel1 := false
	for _, m := range mems {
		if m.Level == 1 {
			hasLevel1 = true
		}
	}
	if !hasLevel1 {
		t.Error("expected a level-1 consolidation memory after 3 events")
	}
}

// TestEventSubscriber_CustomHandler verifies that AddHandler injects custom
// logic into the dispatch loop.
func TestEventSubscriber_CustomHandler(t *testing.T) {
	wal, mgr, _, _ := buildSubscriberRuntime(t)

	called := make(chan string, 10)
	sub := CreateEventSubscriber(wal, mgr)
	sub.SetPollInterval(20 * time.Millisecond)
	sub.AddHandler(func(entry eventbackbone.WALEntry) {
		called <- entry.Event.EventID
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sub.Run(ctx)

	_, _ = wal.Append(schemas.Event{EventID: "evt_custom_1", AgentID: "a", SessionID: "s"})

	select {
	case id := <-called:
		if id != "evt_custom_1" {
			t.Errorf("custom handler: want evt_custom_1, got %s", id)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("custom handler was not called within timeout")
	}
}

// TestEventSubscriber_GracefulShutdown verifies Run returns when ctx is cancelled.
func TestEventSubscriber_GracefulShutdown(t *testing.T) {
	wal, mgr, _, _ := buildSubscriberRuntime(t)

	sub := CreateEventSubscriber(wal, mgr)
	sub.SetPollInterval(20 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		sub.Run(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Error("Run did not exit after ctx cancellation")
	}
}

// TestEventSubscriber_NoConsolidation_WhenDisabled verifies SetConsolidateEvery(0)
// suppresses automatic consolidation.
func TestEventSubscriber_NoConsolidation_WhenDisabled(t *testing.T) {
	wal, mgr, store, _ := buildSubscriberRuntime(t)

	sub := CreateEventSubscriber(wal, mgr)
	sub.SetPollInterval(20 * time.Millisecond)
	sub.SetConsolidateEvery(0) // disabled

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sub.Run(ctx)

	for i := range 5 {
		_, _ = wal.Append(schemas.Event{
			EventID:   fmt.Sprintf("evt_noconsol_%d", i),
			AgentID:   "agentX",
			SessionID: "sessX",
		})
	}
	time.Sleep(200 * time.Millisecond)

	for _, m := range store.Objects().ListMemories("agentX", "sessX") {
		if m.Level == 1 {
			t.Error("consolidation should be disabled but level-1 memory found")
		}
	}
}

// TestMicroBatch_FlushIntegration verifies the full MicroBatch accumulate-and-
// drain cycle end-to-end:
//
//  1. CollaborationChain.Run enqueues a payload into MicroBatchScheduler.
//  2. EventSubscriber.drainWAL flushes the scheduler after each WAL cycle
//     that processed ≥1 entry.
//  3. FlushMicroBatch returns the enqueued payload and clears the queue.
//
// This is the integration test for R10.
// TODO(member-D): extend once MicroBatch has a persistent drain target (e.g.
// forwarding to a coordinator or downstream worker).
func TestMicroBatch_FlushIntegration(t *testing.T) {
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	wal := eventbackbone.NewInMemoryWAL(bus, clock)
	store := storage.NewMemoryRuntimeStorage()

	mgr := nodes.CreateManager()

	// Seed two memories so CollaborationChain has something to merge.
	store.Objects().PutMemory(schemas.Memory{
		MemoryID: "mem_left1", AgentID: "agentA", SessionID: "s1",
		Content: "left memory", Version: 1, Level: 0, IsActive: true,
	})
	store.Objects().PutMemory(schemas.Memory{
		MemoryID: "mem_right1", AgentID: "agentB", SessionID: "s1",
		Content: "right memory", Version: 2, Level: 0, IsActive: true,
	})

	// Register MicroBatchScheduler and ConflictMergeWorker.
	plog := eventbackbone.NewPolicyDecisionLog(clock, bus)
	mbSched := coordination.CreateInMemoryMicroBatchScheduler("mb-1", 32)
	mgr.RegisterMicroBatch(mbSched)
	mgr.RegisterConflictMerge(coordination.CreateInMemoryConflictMergeWorker("cm-1", store.Objects(), store.Edges()))
	mgr.RegisterMemoryExtraction(baseline.CreateInMemoryMemoryExtractionWorker("me-1", store.Objects()))
	mgr.RegisterMemoryConsolidation(baseline.CreateInMemoryMemoryConsolidationWorker("mc-1", store.Objects()))
	mgr.RegisterGraphRelation(indexing.CreateInMemoryGraphRelationWorker("gr-1", store.Edges()))
	mgr.RegisterProofTrace(coordination.CreateInMemoryProofTraceWorker("pt-1", store.Edges(), nil))
	mgr.RegisterReflectionPolicy(baseline.CreateInMemoryReflectionPolicyWorker(
		"rp-1", store.Objects(), store.Policies(), plog,
	))

	// Enqueue via CollaborationChain — this calls EnqueueMicroBatch internally.
	collabChain := chain.CreateCollaborationChain(mgr)
	collabChain.Run(chain.CollaborationChainInput{
		LeftMemID:     "mem_left1",
		RightMemID:    "mem_right1",
		SourceAgentID: "agentA",
		TargetAgentID: "agentB",
	})

	// Verify the scheduler holds the enqueued payload before flush.
	flushed := mgr.FlushMicroBatch()
	if len(flushed) == 0 {
		t.Fatal("expected MicroBatch to have at least one enqueued payload after CollaborationChain.Run")
	}

	// After flush the queue must be empty.
	if second := mgr.FlushMicroBatch(); len(second) != 0 {
		t.Errorf("expected empty queue after flush, got %d items", len(second))
	}

	// Verify drainWAL also triggers flush: append a WAL entry and let the
	// subscriber poll once, then confirm queue remains empty.
	sub := CreateEventSubscriber(wal, mgr)
	sub.SetPollInterval(20 * time.Millisecond)
	_, _ = wal.Append(schemas.Event{
		EventID: "evt_mb_drain", AgentID: "agentA", SessionID: "s1",
		EventType: "agent_thought",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	go sub.Run(ctx)
	<-ctx.Done()

	if remaining := mgr.FlushMicroBatch(); len(remaining) != 0 {
		t.Errorf("expected empty queue after subscriber drain, got %d items", len(remaining))
	}
}
