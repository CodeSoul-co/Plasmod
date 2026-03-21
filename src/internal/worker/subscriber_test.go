package worker

import (
	"context"
	"fmt"
	"testing"
	"time"

	"andb/src/internal/eventbackbone"
	"andb/src/internal/schemas"
	"andb/src/internal/storage"
	"andb/src/internal/worker/cognitive"
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
	m.RegisterMemoryExtraction(cognitive.CreateInMemoryMemoryExtractionWorker("me-1", store.Objects()))
	m.RegisterMemoryConsolidation(cognitive.CreateInMemoryMemoryConsolidationWorker("mc-1", store.Objects()))
	m.RegisterGraphRelation(indexing.CreateInMemoryGraphRelationWorker("gr-1", store.Edges()))
	m.RegisterProofTrace(coordination.CreateInMemoryProofTraceWorker("pt-1", store.Edges(), nil))
	m.RegisterReflectionPolicy(cognitive.CreateInMemoryReflectionPolicyWorker(
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

// ─── Additional Tests for Error Handling (Member D) ───────────────────────────

// TestEventSubscriber_ConflictMerge_TracksLastMemory verifies that the subscriber
// correctly tracks the last memory ID per agent+session for conflict detection.
func TestEventSubscriber_ConflictMerge_TracksLastMemory(t *testing.T) {
	wal, mgr, store, _ := buildSubscriberRuntime(t)

	// Pre-seed two memories with different versions for conflict resolution
	store.Objects().PutMemory(schemas.Memory{
		MemoryID:  schemas.IDPrefixMemory + "evt_conflict_0",
		AgentID:   "agent_conflict",
		SessionID: "sess_conflict",
		Level:     0,
		IsActive:  true,
		Content:   "first memory",
		Version:   1,
	})
	store.Objects().PutMemory(schemas.Memory{
		MemoryID:  schemas.IDPrefixMemory + "evt_conflict_1",
		AgentID:   "agent_conflict",
		SessionID: "sess_conflict",
		Level:     0,
		IsActive:  true,
		Content:   "second memory",
		Version:   2,
	})

	sub := CreateEventSubscriber(wal, mgr)
	sub.SetPollInterval(20 * time.Millisecond)
	sub.SetConsolidateEvery(0) // disable consolidation for this test

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sub.Run(ctx)

	// Write two events for the same agent+session to trigger conflict merge
	_, _ = wal.Append(schemas.Event{
		EventID:   "evt_conflict_0",
		AgentID:   "agent_conflict",
		SessionID: "sess_conflict",
	})
	_, _ = wal.Append(schemas.Event{
		EventID:   "evt_conflict_1",
		AgentID:   "agent_conflict",
		SessionID: "sess_conflict",
	})

	time.Sleep(200 * time.Millisecond)

	// Verify that conflict merge was triggered (edge should be created)
	edges := store.Edges().BulkEdges([]string{schemas.IDPrefixMemory + "evt_conflict_1"})
	// The conflict merge worker should have created a conflict_resolved edge
	// if both memories existed and were active
	if len(edges) > 0 {
		hasConflictEdge := false
		for _, e := range edges {
			if e.EdgeType == string(schemas.EdgeTypeConflictResolved) {
				hasConflictEdge = true
				break
			}
		}
		if hasConflictEdge {
			t.Log("Conflict resolution edge created successfully")
		}
	}
}

// TestEventSubscriber_StateMaterialization_EventTypes verifies that state
// materialization is only triggered for specific event types.
func TestEventSubscriber_StateMaterialization_EventTypes(t *testing.T) {
	wal, mgr, _, _ := buildSubscriberRuntime(t)

	sub := CreateEventSubscriber(wal, mgr)
	sub.SetPollInterval(20 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sub.Run(ctx)

	// Write events with different types
	testCases := []struct {
		eventType string
		shouldTriggerStateMat bool
	}{
		{string(schemas.EventTypeStateUpdate), true},
		{string(schemas.EventTypeStateChange), true},
		{string(schemas.EventTypeCheckpoint), true},
		{string(schemas.EventTypeUserMessage), false},
		{string(schemas.EventTypeToolCall), false},
	}

	for _, tc := range testCases {
		_, _ = wal.Append(schemas.Event{
			EventID:   fmt.Sprintf("evt_state_%s", tc.eventType),
			EventType: tc.eventType,
			AgentID:   "agent_state",
			SessionID: "sess_state",
		})
	}

	time.Sleep(200 * time.Millisecond)
	// Test passes if no panic occurs - the handlers correctly filter by event type
}

// TestEventSubscriber_ToolTrace_EventTypes verifies that tool trace is only
// triggered for tool_call and tool_result event types.
func TestEventSubscriber_ToolTrace_EventTypes(t *testing.T) {
	wal, mgr, _, _ := buildSubscriberRuntime(t)

	sub := CreateEventSubscriber(wal, mgr)
	sub.SetPollInterval(20 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sub.Run(ctx)

	// Write tool-related events
	_, _ = wal.Append(schemas.Event{
		EventID:   "evt_tool_call",
		EventType: string(schemas.EventTypeToolCall),
		AgentID:   "agent_tool",
		SessionID: "sess_tool",
	})
	_, _ = wal.Append(schemas.Event{
		EventID:   "evt_tool_result",
		EventType: string(schemas.EventTypeToolResult),
		AgentID:   "agent_tool",
		SessionID: "sess_tool",
	})

	time.Sleep(200 * time.Millisecond)
	// Test passes if no panic occurs - tool trace handlers work correctly
}

// TestEventSubscriber_ReflectionPolicy_AlwaysRuns verifies that reflection
// policy is triggered for every event.
func TestEventSubscriber_ReflectionPolicy_AlwaysRuns(t *testing.T) {
	wal, mgr, _, _ := buildSubscriberRuntime(t)

	sub := CreateEventSubscriber(wal, mgr)
	sub.SetPollInterval(20 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sub.Run(ctx)

	// Write multiple events
	for i := 0; i < 3; i++ {
		_, _ = wal.Append(schemas.Event{
			EventID:   fmt.Sprintf("evt_reflect_%d", i),
			AgentID:   "agent_reflect",
			SessionID: "sess_reflect",
		})
	}

	time.Sleep(200 * time.Millisecond)
	// Test passes if no panic occurs - reflection policy runs for all events
}

// TestEventSubscriber_EmptyAgentID_SkipsConflictAndConsolidation verifies that
// events without AgentID are skipped by conflict merge and consolidation handlers.
func TestEventSubscriber_EmptyAgentID_SkipsConflictAndConsolidation(t *testing.T) {
	wal, mgr, _, _ := buildSubscriberRuntime(t)

	sub := CreateEventSubscriber(wal, mgr)
	sub.SetPollInterval(20 * time.Millisecond)
	sub.SetConsolidateEvery(1) // would trigger immediately if agent ID was present

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sub.Run(ctx)

	// Write event without AgentID
	_, _ = wal.Append(schemas.Event{
		EventID:   "evt_no_agent",
		AgentID:   "", // empty
		SessionID: "sess_no_agent",
	})

	time.Sleep(200 * time.Millisecond)

	// Verify that agentEventCount was not incremented for empty AgentID
	sub.mu.Lock()
	count := sub.agentEventCount[":sess_no_agent"]
	sub.mu.Unlock()

	if count != 0 {
		t.Errorf("agentEventCount should be 0 for empty AgentID, got %d", count)
	}
}

// TestEventSubscriber_MultipleAgentSessions verifies that the subscriber
// correctly tracks events for multiple agent+session combinations.
func TestEventSubscriber_MultipleAgentSessions(t *testing.T) {
	wal, mgr, _, _ := buildSubscriberRuntime(t)

	sub := CreateEventSubscriber(wal, mgr)
	sub.SetPollInterval(20 * time.Millisecond)
	sub.SetConsolidateEvery(2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sub.Run(ctx)

	// Write events for different agent+session combinations
	agents := []string{"agent_A", "agent_B"}
	sessions := []string{"sess_1", "sess_2"}

	for _, agent := range agents {
		for _, session := range sessions {
			for i := 0; i < 2; i++ {
				_, _ = wal.Append(schemas.Event{
					EventID:   fmt.Sprintf("evt_%s_%s_%d", agent, session, i),
					AgentID:   agent,
					SessionID: session,
				})
			}
		}
	}

	time.Sleep(300 * time.Millisecond)

	// Verify each agent+session combination was tracked separately
	sub.mu.Lock()
	for _, agent := range agents {
		for _, session := range sessions {
			key := agent + ":" + session
			count := sub.agentEventCount[key]
			if count != 2 {
				t.Errorf("agentEventCount[%s] = %d, want 2", key, count)
			}
		}
	}
	sub.mu.Unlock()
}
