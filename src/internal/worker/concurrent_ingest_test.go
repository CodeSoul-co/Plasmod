package worker

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"plasmod/src/internal/schemas"
)

// TestRuntime_ConcurrentIngest_NoRace verifies that concurrent SubmitIngest
// calls do not trigger data races and all succeed.
// Run with: go test -race ./src/internal/worker/ -run TestRuntime_ConcurrentIngest
func TestRuntime_ConcurrentIngest_NoRace(t *testing.T) {
	rt := buildTestRuntime(t)
	const goroutines = 20
	const eventsEach = 10

	var wg sync.WaitGroup
	var errCount atomic.Int64

	for g := 0; g < goroutines; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < eventsEach; i++ {
				ev := schemas.Event{
					EventID:     fmt.Sprintf("concurrent-g%d-e%d", g, i),
					AgentID:     "agent-race",
					SessionID:   "sess-race",
					WorkspaceID: "ws-race",
					Payload:     map[string]any{"text": fmt.Sprintf("goroutine %d event %d", g, i)},
				}
				if _, err := rt.SubmitIngest(ev); err != nil {
					errCount.Add(1)
					t.Logf("ingest error g=%d i=%d: %v", g, i, err)
				}
			}
		}()
	}
	wg.Wait()

	if errCount.Load() > 0 {
		t.Errorf("concurrent ingest: %d errors out of %d events", errCount.Load(), goroutines*eventsEach)
	}
}

// TestRuntime_ConcurrentIngestAndQuery_NoRace verifies mixed concurrent
// reads and writes do not race.
func TestRuntime_ConcurrentIngestAndQuery_NoRace(t *testing.T) {
	rt := buildTestRuntime(t)

	// Pre-seed some data
	for i := 0; i < 5; i++ {
		ev := schemas.Event{
			EventID:     fmt.Sprintf("seed-%d", i),
			AgentID:     "agent-mixed",
			SessionID:   "sess-mixed",
			WorkspaceID: "ws-mixed",
			Payload:     map[string]any{"text": fmt.Sprintf("seed content %d", i)},
		}
		if _, err := rt.SubmitIngest(ev); err != nil {
			t.Fatalf("seed ingest failed: %v", err)
		}
	}

	var wg sync.WaitGroup
	var ingestErrs, queryErrs atomic.Int64

	// Concurrent writers
	for g := 0; g < 10; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 5; i++ {
				ev := schemas.Event{
					EventID:     fmt.Sprintf("mixed-w-g%d-e%d", g, i),
					AgentID:     "agent-mixed",
					SessionID:   "sess-mixed",
					WorkspaceID: "ws-mixed",
					Payload:     map[string]any{"text": fmt.Sprintf("mixed write g%d e%d", g, i)},
				}
				if _, err := rt.SubmitIngest(ev); err != nil {
					ingestErrs.Add(1)
				}
			}
		}()
	}

	// Concurrent readers
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 5; i++ {
				req := schemas.QueryRequest{
					QueryText:   "mixed content",
					TopK:        3,
					WorkspaceID: "ws-mixed",
				}
				resp := rt.ExecuteQuery(req)
				if resp.QueryStatus == "" {
					queryErrs.Add(1)
				}
			}
		}()
	}

	wg.Wait()

	if ingestErrs.Load() > 0 {
		t.Errorf("concurrent mixed: %d ingest errors", ingestErrs.Load())
	}
	if queryErrs.Load() > 0 {
		t.Errorf("concurrent mixed: %d query errors (missing query_status)", queryErrs.Load())
	}
}

// TestRuntime_IngestIdempotency_SameEventID checks that duplicate EventIDs
// are handled consistently (WAL appends, objects deduplicated by ID).
func TestRuntime_IngestIdempotency_SameEventID(t *testing.T) {
	rt := buildTestRuntime(t)
	ev := schemas.Event{
		EventID:     "idem-001",
		AgentID:     "agent-idem",
		SessionID:   "sess-idem",
		WorkspaceID: "ws-idem",
		Payload:     map[string]any{"text": "idempotency test content"},
	}
	for i := 0; i < 3; i++ {
		if _, err := rt.SubmitIngest(ev); err != nil {
			t.Fatalf("ingest attempt %d failed: %v", i, err)
		}
	}
	// Query should still return results without panicking
	resp := rt.ExecuteQuery(schemas.QueryRequest{QueryText: "idempotency", TopK: 5, WorkspaceID: "ws-idem"})
	if resp.QueryStatus == "" {
		t.Error("query after duplicate ingest should still return a valid QueryStatus")
	}
}

// TestRuntime_EmptyQueryDoesNotPanic ensures ExecuteQuery with an empty
// query string never panics or returns a nil QueryStatus.
func TestRuntime_EmptyQueryDoesNotPanic(t *testing.T) {
	rt := buildTestRuntime(t)
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("ExecuteQuery panicked with empty query: %v", r)
		}
	}()
	resp := rt.ExecuteQuery(schemas.QueryRequest{QueryText: "", TopK: 5, WorkspaceID: "ws-empty"})
	if resp.QueryStatus == "" {
		t.Error("empty query: QueryStatus should not be empty")
	}
}

// TestRuntime_LargeTopK_DoesNotPanic checks that TopK larger than the
// result set doesn't cause out-of-bounds or nil panics.
func TestRuntime_LargeTopK_DoesNotPanic(t *testing.T) {
	rt := buildTestRuntime(t)
	for i := 0; i < 3; i++ {
		ev := schemas.Event{
			EventID:     fmt.Sprintf("topk-%d", i),
			AgentID:     "agent-topk",
			SessionID:   "sess-topk",
			WorkspaceID: "ws-topk",
			Payload:     map[string]any{"text": fmt.Sprintf("topk test content %d", i)},
		}
		rt.SubmitIngest(ev) //nolint
	}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("ExecuteQuery panicked with large TopK: %v", r)
		}
	}()
	resp := rt.ExecuteQuery(schemas.QueryRequest{QueryText: "topk", TopK: 10000, WorkspaceID: "ws-topk"})
	if resp.QueryStatus == "" {
		t.Error("large TopK query: QueryStatus should not be empty")
	}
}

// TestRuntime_ProvenanceAlwaysPresent verifies that every QueryResponse
// has embedding provenance entries stamped regardless of data in store.
func TestRuntime_ProvenanceAlwaysPresent(t *testing.T) {
	rt := buildTestRuntime(t)
	resp := rt.ExecuteQuery(schemas.QueryRequest{QueryText: "anything", TopK: 1, WorkspaceID: "ws-prov"})
	hasFamily, hasDim := false, false
	for _, p := range resp.Provenance {
		if len(p) > len("embedding_runtime_family=") && p[:len("embedding_runtime_family=")] == "embedding_runtime_family=" {
			hasFamily = true
		}
		if len(p) > len("embedding_runtime_dim=") && p[:len("embedding_runtime_dim=")] == "embedding_runtime_dim=" {
			hasDim = true
		}
	}
	if !hasFamily {
		t.Errorf("provenance missing embedding_runtime_family; got: %v", resp.Provenance)
	}
	if !hasDim {
		t.Errorf("provenance missing embedding_runtime_dim; got: %v", resp.Provenance)
	}
}
