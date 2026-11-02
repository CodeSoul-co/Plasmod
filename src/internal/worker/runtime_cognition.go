package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"

	"plasmod/src/internal/schemas"
	"plasmod/src/internal/storage"
	"plasmod/src/internal/worker/cognition"
)

// ConfigureCognition is called before serving requests; nil uses conservative
// non-semantic materialization and explicitly labeled extractive summaries.
func (r *Runtime) ConfigureCognition(p cognition.Provider) { r.cognitionProvider = p }

func (r *Runtime) extractConversation(ctx context.Context, ev schemas.Event, source schemas.Memory) (resultErr error) {
	if r.cognitionProvider == nil || ev.EventInfo.EventType != string(schemas.EventTypeUserMessage) {
		return nil
	}
	// Serialize identity assignment across sessions of the same scoped agent.
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(stableID(source.TenantID, source.WorkspaceID, source.AgentID)))
	lock := &r.cognitionLocks[hash.Sum64()%uint64(len(r.cognitionLocks))]
	lock.Lock()
	defer lock.Unlock()
	receipt, _ := r.ExtractionStatus(source.TenantID, source.WorkspaceID, ev.Identity.EventID)
	receipt.EventID, receipt.SourceMemoryID, receipt.LSN = ev.Identity.EventID, source.MemoryID, ev.Time.WalLSN
	receipt.BaseProjection, receipt.Status, receipt.Stage = "completed", "running", "extract"
	receipt.Attempts++
	if err := r.saveExtractionStatus(source, &receipt, nil); err != nil {
		return err
	}
	defer func() {
		if resultErr == nil {
			receipt.Status, receipt.Stage = "completed", "admission"
		}
		resultErr = errors.Join(resultErr, r.saveExtractionStatus(source, &receipt, resultErr))
	}()
	stateID := "extraction:" + stableID(source.TenantID, source.WorkspaceID, source.MemoryID)
	var facts []cognition.Fact
	saveFacts := func(facts []cognition.Fact) error {
		data, err := json.Marshal(facts)
		if err != nil {
			return err
		}
		st := schemas.State{StateID: stateID, TenantID: source.TenantID, WorkspaceID: source.WorkspaceID, AgentID: source.AgentID, SessionID: source.SessionID, StateType: "memory_extraction", StateValue: string(data), DerivedFromEventID: ev.Identity.EventID, Access: source.Access, Version: 1}
		return r.storage.ApplyCanonicalProjection(storage.CanonicalProjection{State: &st})
	}
	if saved, ok := r.storage.Objects().GetState(stateID); ok {
		if err := json.Unmarshal([]byte(saved.StateValue), &facts); err != nil {
			return err
		}
	} else {
		var err error
		facts, err = cognition.Extract(ctx, r.cognitionProvider, source.Content)
		if err != nil {
			return err
		}
		if err := saveFacts(facts); err != nil {
			return err
		}
	}
	receipt.Stage = "normalize"
	if err := r.resolveFactIdentities(ctx, source, facts); err != nil {
		return err
	}
	if err := saveFacts(facts); err != nil {
		return err
	}

	receipt.FactCount, receipt.Stage = len(facts), "materialize"
	for i, f := range facts {
		receipt.Stage = "materialize"
		m := factMemory(source, f)
		m.MemoryID = "mem_fact_" + stableID(source.MemoryID, fmt.Sprint(i))
		m.Summary = f.Text
		m.Confidence = f.Confidence
		m.LifecycleState = "candidate"
		m.Version = 1
		m.SourceSpans = []schemas.MemorySourceSpan{{EventID: ev.Identity.EventID, Start: f.Start, End: f.End, Quote: f.Quote, ObservedAt: ev.Time.EventTime}}
		if f.ValidFrom != "" {
			m.ValidFrom = f.ValidFrom
		}
		if f.ValidTo != "" {
			m.ValidTo = f.ValidTo
		}
		if err := r.persistDerivedMemoriesContext(ctx, "extract", []schemas.Memory{m}, []schemas.Memory{source}); err != nil {
			return err
		}
		receipt.Stage = "admission"
		out := r.DispatchAlgorithm("ingest", []string{m.MemoryID}, "", "", m.AgentID, m.SessionID, nil)
		if out.Error != "" {
			return fmt.Errorf("fact admission: %s", out.Error)
		}
	}
	return nil
}

func (r *Runtime) summarizeContent(ctx context.Context, operation string, m *schemas.Memory, sources []schemas.Memory) error {
	if operation != "compress" && operation != "summarize" {
		return nil
	}
	if r.cognitionProvider == nil {
		m.SummaryMethod = "extractive"
		return nil
	}
	input := map[string]string{}
	for _, source := range sources {
		input[source.MemoryID] = source.Content
	}
	conclusions, err := cognition.Summarize(ctx, r.cognitionProvider, input)
	if err != nil {
		return err
	}
	var text []string
	for _, c := range conclusions {
		text = append(text, c.Text)
		m.Conclusions = append(m.Conclusions, schemas.MemoryConclusion{Text: c.Text, SourceIDs: c.SourceIDs, Uncertain: c.Uncertain})
	}
	m.Content = strings.Join(text, "\n")
	m.Summary = m.Content
	m.SummaryMethod = "semantic"
	return nil
}
