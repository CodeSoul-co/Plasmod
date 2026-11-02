package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"plasmod/src/internal/schemas"
	"plasmod/src/internal/storage"
	"plasmod/src/internal/worker/cognition"
	"plasmod/src/internal/worker/consistency"
)

type multilingualProvider struct {
	extracts     atomic.Int32
	matches      atomic.Int32
	invalidMatch atomic.Bool
}

func (p *multilingualProvider) Complete(_ context.Context, system, input string) (string, error) {
	if strings.HasPrefix(system, "Extract") {
		n := p.extracts.Add(1)
		text := "User likes commuting by bicycle."
		if n%2 == 0 {
			text = "用户喜欢骑自行车通勤。"
		}
		if n%3 == 0 {
			text = "User enjoys commuting by bike."
		}
		if strings.Contains(input, "不喜欢") {
			text = "User dislikes commuting by bicycle."
		}
		if strings.Contains(input, "公共汽车") {
			text = "User likes commuting by bus."
		}
		facts := []cognition.Fact{{Key: "user.commute_preference", Text: text, Kind: "preference_constraint", Quote: input, End: len([]rune(input)), Confidence: .95}}
		data, _ := json.Marshal(map[string]any{"facts": facts})
		return string(data), nil
	}
	if !strings.HasPrefix(system, "Compare") {
		return "", errors.New("unexpected provider operation")
	}
	p.matches.Add(1)
	var in struct {
		Fact       cognition.Fact            `json:"fact"`
		Candidates []cognition.FactCandidate `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", err
	}
	out := map[string]any{"relation": "distinct", "match_id": "", "confidence": 1, "source_quote": in.Fact.Quote, "candidate_quote": ""}
	for _, c := range in.Candidates {
		if c.Fact.Quote == in.Fact.Quote {
			out["relation"], out["match_id"], out["candidate_quote"] = "equivalent", c.ID, c.Fact.Quote
			if p.invalidMatch.Load() {
				out["match_id"] = "foreign-or-invented"
			}
			break
		}
	}
	data, _ := json.Marshal(out)
	return string(data), nil
}

func TestMultilingualEvidencePromotesAfterDiskRestart(t *testing.T) {
	t.Setenv(storage.EnvStorage, "disk")
	t.Setenv(storage.EnvDataDir, t.TempDir())
	t.Setenv(storage.EnvBadgerInMemory, "false")
	bundle, err := storage.BuildRuntimeFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	r := lifecycleRuntime(t, bundle.RuntimeStorage)
	p := &multilingualProvider{}
	r.ConfigureCognition(p)
	for i := 0; i < 2; i++ {
		putConversation(t, r, fmt.Sprint("bicycle-", i), fmt.Sprint("s", i), "我喜欢骑自行车通勤。", 0)
	}
	identity := ""
	for _, m := range r.storage.Objects().ListMemories("agent", "") {
		if len(m.SourceSpans) == 0 {
			continue
		}
		if m.FactIdentity == "" || (identity != "" && m.FactIdentity != identity) || m.LifecycleState != "candidate" {
			t.Fatalf("premature/fragmented admission: %+v", m)
		}
		identity = m.FactIdentity
	}
	if p.matches.Load() != 1 {
		t.Fatalf("semantic comparison calls=%d", p.matches.Load())
	}
	if err := r.ShutdownConsistency(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := bundle.Close(); err != nil {
		t.Fatal(err)
	}
	bundle, err = storage.BuildRuntimeFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.Close()
	r = lifecycleRuntime(t, bundle.RuntimeStorage)
	r.ConfigureCognition(p)
	putConversation(t, r, "bicycle-2", "s2", "我喜欢骑自行车通勤。", 0)
	facts := 0
	for _, m := range r.storage.Objects().ListMemories("agent", "") {
		if len(m.SourceSpans) == 0 {
			continue
		}
		facts++
		if m.FactIdentity != identity || m.LifecycleState != "active" || len(m.SourceSpans) != 1 || m.SourceSpans[0].Quote != "我喜欢骑自行车通勤。" {
			t.Fatalf("failed promotion: %+v", m)
		}
		state, _ := r.storage.Objects().GetState("governance:memorybank_v1:" + m.MemoryID)
		var governance schemas.MemoryAlgorithmState
		if json.Unmarshal([]byte(state.StateValue), &governance) != nil || len(governance.EvidenceEventIDs) != 3 {
			t.Fatalf("lost independent evidence: %+v", governance)
		}
	}
	if facts != 3 {
		t.Fatalf("facts=%d", facts)
	}
	// A replay reuses the saved identity and does not call extraction/comparison.
	calls := p.extracts.Load() + p.matches.Load()
	if _, err := r.AdminReplayApply(0, 0); err != nil {
		t.Fatal(err)
	}
	if p.extracts.Load()+p.matches.Load() != calls {
		t.Fatal("replay recomputed model decisions")
	}
}

func TestFactIdentityKeepsNegationValuesAndTenantsSeparate(t *testing.T) {
	r := lifecycleRuntime(t, nil)
	p := &multilingualProvider{}
	r.ConfigureCognition(p)
	for i, text := range []string{"我喜欢骑自行车通勤。", "我不喜欢骑自行车通勤。", "我喜欢乘公共汽车通勤。"} {
		putConversation(t, r, fmt.Sprint("different-", i), fmt.Sprint("s", i), text, 0)
	}
	ids := map[string]bool{}
	for _, m := range r.storage.Objects().ListMemories("agent", "") {
		if len(m.SourceSpans) == 0 {
			continue
		}
		if ids[m.FactIdentity] || m.LifecycleState != "candidate" {
			t.Fatalf("distinct values merged: %+v", m)
		}
		ids[m.FactIdentity] = true
	}
	before := p.matches.Load()
	_, err := r.SubmitIngest(schemas.Event{Identity: schemas.EventIdentity{EventID: "foreign", TenantID: "other", WorkspaceID: "workspace"}, Actor: schemas.EventActor{AgentID: "agent", SessionID: "other"}, EventInfo: schemas.EventDescriptor{EventType: "user_message"}, Payload: map[string]any{"text": "我喜欢骑自行车通勤。"}})
	if err != nil {
		t.Fatal(err)
	}
	if p.matches.Load() != before {
		t.Fatal("cross-tenant facts sent to equivalence provider")
	}
}

func TestLegacySplitFactsReconcileOnOriginalEventReplay(t *testing.T) {
	r := lifecycleRuntime(t, nil)
	defer r.ShutdownConsistency(context.Background())
	p := &multilingualProvider{}
	r.ConfigureCognition(p)
	for i := 0; i < 3; i++ {
		putConversation(t, r, fmt.Sprint("legacy-", i), fmt.Sprint("s", i), "我喜欢骑自行车通勤。", 0)
	}
	// Simulate the persisted pre-upgrade representation, retaining original sources.
	for _, m := range r.storage.Objects().ListMemories("agent", "") {
		if len(m.SourceSpans) == 0 {
			continue
		}
		m.FactIdentity = ""
		m.LifecycleState = "candidate"
		r.storage.Objects().PutMemory(m)
		st, _ := r.storage.Objects().GetState("governance:memorybank_v1:" + m.MemoryID)
		var governance schemas.MemoryAlgorithmState
		_ = json.Unmarshal([]byte(st.StateValue), &governance)
		governance.SuggestedLifecycleState = "candidate"
		data, _ := json.Marshal(governance)
		st.StateValue = string(data)
		r.storage.Objects().PutState(st)
	}
	for _, st := range r.storage.Objects().ListStates("agent", "") {
		if st.StateType != "memory_extraction" {
			continue
		}
		var facts []cognition.Fact
		if err := json.Unmarshal([]byte(st.StateValue), &facts); err != nil {
			t.Fatal(err)
		}
		for i := range facts {
			facts[i].Identity = ""
		}
		data, _ := json.Marshal(facts)
		st.StateValue = string(data)
		r.storage.Objects().PutState(st)
	}
	if _, err := r.AdminReplayApply(0, 0); err != nil {
		t.Fatal(err)
	}
	identity := ""
	count := 0
	for _, m := range r.storage.Objects().ListMemories("agent", "") {
		if len(m.SourceSpans) == 0 {
			continue
		}
		count++
		if m.FactIdentity == "" || (identity != "" && identity != m.FactIdentity) || m.LifecycleState != "active" {
			t.Fatalf("legacy evidence still split: %+v", m)
		}
		identity = m.FactIdentity
	}
	if count != 3 || p.extracts.Load() != 3 {
		t.Fatalf("replay duplicated facts/model extraction: count=%d calls=%d", count, p.extracts.Load())
	}
}

func TestNormalizationFailureRetainsValidExtractionForRetry(t *testing.T) {
	r := lifecycleRuntime(t, nil)
	defer r.ShutdownConsistency(context.Background())
	cfg := consistency.DefaultConfig()
	cfg.MaxRetries = 1
	if err := r.ConfigureConsistency(cfg, nil); err != nil {
		t.Fatal(err)
	}
	p := &multilingualProvider{}
	r.ConfigureCognition(p)
	putConversation(t, r, "match-first", "s1", "我喜欢骑自行车通勤。", 0)
	p.invalidMatch.Store(true)
	ev := schemas.Event{Identity: schemas.EventIdentity{EventID: "match-failed", TenantID: "tenant", WorkspaceID: "workspace"}, Actor: schemas.EventActor{AgentID: "agent", SessionID: "s2"}, EventInfo: schemas.EventDescriptor{EventType: "user_message"}, Payload: map[string]any{"text": "我喜欢骑自行车通勤。"}}
	if _, err := r.SubmitIngest(ev); err == nil {
		t.Fatal("invented equivalence accepted")
	}
	if _, err := r.ExecuteQueryContext(context.Background(), schemas.QueryRequest{TenantID: "tenant"}); err == nil {
		t.Fatal("missing normalization failure")
	}
	status, _ := r.ExtractionStatus("tenant", "workspace", "match-failed")
	if status.Stage != "normalize" || status.ErrorCode != "invalid_equivalence_evidence" {
		t.Fatalf("wrong classification: %+v", status)
	}
	if p.extracts.Load() != 2 {
		t.Fatal("normalization retry repeated extraction")
	}
	p.invalidMatch.Store(false)
	if _, err := r.AdminReplayApply(status.LSN, 1); err != nil {
		t.Fatal(err)
	}
	if p.extracts.Load() != 2 {
		t.Fatal("replay repeated valid extraction")
	}
	status, _ = r.ExtractionStatus("tenant", "workspace", "match-failed")
	if status.Status != "completed" {
		t.Fatalf("not recovered: %+v", status)
	}
}
