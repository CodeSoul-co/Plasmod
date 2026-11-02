package worker

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"plasmod/src/internal/schemas"
	"plasmod/src/internal/worker/consistency"
)

type recoverableExtractionProvider struct {
	valid atomic.Bool
	calls atomic.Int32
}

func (p *recoverableExtractionProvider) Complete(_ context.Context, _, input string) (string, error) {
	p.calls.Add(1)
	quote := input
	if !p.valid.Load() {
		quote = "not the source"
	}
	data, _ := json.Marshal(map[string]any{"facts": []map[string]any{{"text": "User likes tea.", "kind": "preference_constraint", "quote": quote, "start": 0, "end": len([]rune(input)), "confidence": .95}}})
	return string(data), nil
}

func TestExtractionFailureIsolatedAndReplayRecoversSameEvent(t *testing.T) {
	r := lifecycleRuntime(t, nil)
	cfg := consistency.DefaultConfig()
	cfg.MaxRetries = 1
	if err := r.ConfigureConsistency(cfg, nil); err != nil {
		t.Fatal(err)
	}
	provider := &recoverableExtractionProvider{}
	r.ConfigureCognition(provider)
	a := schemas.Event{Identity: schemas.EventIdentity{EventID: "failure-a", TenantID: "A", WorkspaceID: "w"}, Actor: schemas.EventActor{AgentID: "agent-a", SessionID: "sa"}, EventInfo: schemas.EventDescriptor{EventType: "user_message"}, Access: schemas.EventAccess{Visibility: "workspace"}, Payload: map[string]any{"text": "我喜欢茶。"}}
	if _, err := r.SubmitIngest(a); err == nil {
		t.Fatal("invalid quote accepted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := r.ExecuteQueryContext(ctx, schemas.QueryRequest{TenantID: "A"}); err == nil {
		t.Fatal("A should retain failure")
	}
	status, ok := r.ExtractionStatus("A", "w", "failure-a")
	if !ok || status.Status != "failed" || status.ErrorCode != "source_quote_mismatch" || status.BaseProjection != "completed" || status.LSN == 0 {
		t.Fatalf("failure receipt: %+v", status)
	}
	if _, ok := r.ExtractionStatus("B", "w", "failure-a"); ok {
		t.Fatal("cross-tenant receipt")
	}
	for _, memory := range r.storage.Objects().ListMemories("agent-a", "") {
		if len(memory.SourceSpans) != 0 {
			t.Fatal("invalid evidence persisted as fact")
		}
	}
	b := a
	b.Identity.EventID, b.Identity.TenantID = "success-b", "B"
	b.Actor.AgentID, b.Actor.SessionID = "agent-b", "sb"
	b.EventInfo.EventType = "memory_write_requested"
	importance, confidence := 1.0, 1.0
	b.EventInfo.Importance, b.EventInfo.Confidence = &importance, &confidence
	b.Payload = map[string]any{"text": "B's existing memory"}
	ack, err := r.SubmitIngest(b)
	if err != nil {
		t.Fatal(err)
	}
	query := schemas.QueryRequest{TenantID: "B", WorkspaceID: "w", AgentID: "agent-b", QueryScope: "workspace", ResponseMode: "objects_only", TargetObjectIDs: []string{ack["memory_id"].(string)}, TopK: 10}
	got, err := r.ExecuteQueryContext(ctx, query)
	if err != nil || !containsString(got.Objects, ack["memory_id"].(string)) {
		t.Fatalf("B not readable: %+v %v", got, err)
	}
	if r.ConsistencyStatus().VisibleWatermark >= status.LSN {
		t.Fatal("global checkpoint skipped failure")
	}
	provider.valid.Store(true)
	for i := 0; i < 2; i++ {
		if _, err := r.AdminReplayApply(status.LSN, 1); err != nil {
			t.Fatal(err)
		}
	}
	after, ok := r.ExtractionStatus("A", "w", "failure-a")
	if !ok || after.Status != "completed" || after.ErrorCode != "" || after.FactCount != 1 {
		t.Fatalf("recovery receipt: %+v", after)
	}
	facts := 0
	for _, memory := range r.storage.Objects().ListMemories("agent-a", "") {
		if len(memory.SourceSpans) == 0 {
			continue
		}
		facts++
		if len(memory.SourceEventIDs) != 1 || memory.SourceEventIDs[0] != "failure-a" || memory.SourceSpans[0].Quote != "我喜欢茶。" {
			t.Fatalf("lost provenance: %+v", memory)
		}
	}
	if facts != 1 {
		t.Fatalf("duplicate facts after replay: %d", facts)
	}
	if r.ConsistencyStatus().VisibleWatermark != r.ConsistencyStatus().LatestLSN {
		t.Fatal("recovery did not release checkpoint")
	}
}
