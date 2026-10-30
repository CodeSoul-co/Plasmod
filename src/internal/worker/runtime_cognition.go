package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"plasmod/src/internal/schemas"
	"plasmod/src/internal/storage"
	"plasmod/src/internal/worker/cognition"
)

// ConfigureCognition is called before serving requests; nil uses conservative
// non-semantic materialization and explicitly labeled extractive summaries.
func (r *Runtime) ConfigureCognition(p cognition.Provider) { r.cognitionProvider = p }

func (r *Runtime) extractConversation(ctx context.Context, ev schemas.Event, source schemas.Memory) error {
	if r.cognitionProvider == nil || ev.EventInfo.EventType != string(schemas.EventTypeUserMessage) {
		return nil
	}
	stateID := "extraction:" + stableID(source.TenantID, source.WorkspaceID, source.MemoryID)
	var facts []cognition.Fact
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
		data, _ := json.Marshal(facts)
		st := schemas.State{StateID: stateID, TenantID: source.TenantID, WorkspaceID: source.WorkspaceID, AgentID: source.AgentID, SessionID: source.SessionID, StateType: "memory_extraction", StateValue: string(data), DerivedFromEventID: ev.Identity.EventID, Access: source.Access, Version: 1}
		if err := r.storage.ApplyCanonicalProjection(storage.CanonicalProjection{State: &st}); err != nil {
			return err
		}
	}
	for i, f := range facts {
		m := source
		m.MemoryID = "mem_fact_" + stableID(source.MemoryID, fmt.Sprint(i))
		m.MemoryType = f.Kind
		m.FactKey = f.Key
		m.Content = f.Text
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
