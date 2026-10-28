package cognitive

import (
	"encoding/json"
	"sync"
	"testing"

	"plasmod/src/internal/schemas"
	"plasmod/src/internal/storage"
	"plasmod/src/internal/worker/cognitive/memorybank"
)

func TestMemoryBankDurableRecallAndRepeatedIngest(t *testing.T) {
	t.Setenv("PLASMOD_STORAGE", "disk")
	t.Setenv("PLASMOD_DATA_DIR", t.TempDir())
	open := func() (*storage.RuntimeBundle, *InMemoryAlgorithmDispatchWorker) {
		t.Helper()
		store, err := storage.BuildRuntimeFromEnv()
		if err != nil {
			t.Fatal(err)
		}
		return store, CreateAlgorithmDispatchWorker("bank", memorybank.New("memorybank_v1", memorybank.DefaultConfig()), store.Objects(), store.AlgorithmStates(), store.Audits())
	}
	store, w := open()
	memory := schemas.Memory{MemoryID: "mem_durable", TenantID: "tenant", WorkspaceID: "workspace", AgentID: "agent", Content: "I prefer concise replies", IsActive: true, MemoryType: "semantic", Importance: 1, Confidence: 1}
	store.Objects().PutMemory(memory)
	dispatch := func(operation string) schemas.AlgorithmDispatchOutput {
		t.Helper()
		out, err := w.Dispatch(operation, []string{memory.MemoryID}, "replies", "", memory.AgentID, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	state := func() schemas.MemoryAlgorithmState {
		t.Helper()
		raw, ok := store.Objects().GetState("governance:memorybank_v1:" + memory.MemoryID)
		if !ok {
			t.Fatal("missing durable governance")
		}
		if raw.TenantID != memory.TenantID || raw.WorkspaceID != memory.WorkspaceID || raw.AgentID != memory.AgentID {
			t.Fatalf("lost scope: %+v", raw)
		}
		var got schemas.MemoryAlgorithmState
		if err := json.Unmarshal([]byte(raw.StateValue), &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	dispatch("ingest")
	initial := state()
	stored, _ := store.Objects().GetMemory(memory.MemoryID)
	if stored.LifecycleState != initial.SuggestedLifecycleState || stored.AlgorithmStateRef != "memorybank_v1" {
		t.Fatalf("ingest overwrote lifecycle: %+v", stored)
	}
	dispatch("recall")
	before := state()
	if before.RecallCount != 1 {
		t.Fatalf("recall_count=%d", before.RecallCount)
	}
	dispatch("ingest")
	if got := state(); got.RecallCount != 1 || got.Strength != before.Strength {
		t.Fatalf("repeated ingest reset state: %+v", got)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, w = open()
	defer store.Close()
	if got := state(); got.RecallCount != 1 {
		t.Fatalf("restart lost state: %+v", got)
	}
	dispatch("recall")
	if got := state(); got.RecallCount != 2 || got.Strength <= before.Strength {
		t.Fatalf("restart did not continue reinforcement: %+v", got)
	}
	// Concurrent calls must not lose read/modify/write updates.
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := w.Dispatch("recall", []string{memory.MemoryID}, "replies", "", memory.AgentID, "", nil); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if got := state(); got.RecallCount != 10 {
		t.Fatalf("concurrent recall_count=%d", got.RecallCount)
	}
}
