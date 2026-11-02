package worker

import (
	"context"
	"fmt"
	"sort"

	"plasmod/src/internal/schemas"
	"plasmod/src/internal/worker/cognition"
)

func factMemory(source schemas.Memory, fact cognition.Fact) schemas.Memory {
	m := source
	m.Content, m.MemoryType, m.FactKey = fact.Text, fact.Kind, fact.Key
	m.FactIdentity, m.FactValidFrom, m.FactValidTo = fact.Identity, fact.ValidFrom, fact.ValidTo
	return m
}

func (r *Runtime) resolveFactIdentities(ctx context.Context, source schemas.Memory, facts []cognition.Fact) error {
	memories := r.storage.Objects().ListMemories(source.AgentID, "")
	sort.Slice(memories, func(i, j int) bool { return memories[i].MemoryID < memories[j].MemoryID })
	for i := range facts {
		fact := &facts[i]
		if fact.Identity != "" {
			continue
		}
		identity := schemas.MemoryFactIdentity(factMemory(source, *fact))
		seen := map[string]bool{}
		var candidates []cognition.FactCandidate
		for _, m := range memories {
			if m.MemoryID == "mem_fact_"+stableID(source.MemoryID, fmt.Sprint(i)) {
				continue
			}
			if !m.IsActive || m.LifecycleState == "compressed" || m.LifecycleState == "quarantined" || m.TenantID != source.TenantID || m.WorkspaceID != source.WorkspaceID || m.AgentID != source.AgentID || m.MemoryType != fact.Kind || len(m.SourceSpans) == 0 || m.FactValidFrom != fact.ValidFrom || m.FactValidTo != fact.ValidTo {
				continue
			}
			id := schemas.MemoryFactIdentity(m)
			lexical := m
			lexical.FactIdentity = ""
			if id == identity || schemas.MemoryFactIdentity(lexical) == identity {
				fact.Identity = id
				break
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			candidates = append(candidates, cognition.FactCandidate{ID: id, Fact: cognition.Fact{Key: m.FactKey, Text: m.Content, Kind: m.MemoryType, Quote: m.SourceSpans[0].Quote, Confidence: m.Confidence, ValidFrom: m.FactValidFrom, ValidTo: m.FactValidTo}})
		}
		if fact.Identity != "" {
			continue
		}
		// Bound each provider request without dropping older eligible evidence.
		for start := 0; start < len(candidates); start += 32 {
			end := start + 32
			if end > len(candidates) {
				end = len(candidates)
			}
			matched, err := cognition.MatchFact(ctx, r.cognitionProvider, *fact, candidates[start:end])
			if err != nil {
				return err
			}
			if matched != "" {
				fact.Identity = matched
				break
			}
		}
		if fact.Identity == "" {
			fact.Identity = identity
		}
	}
	return nil
}
