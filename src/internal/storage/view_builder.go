package storage

import (
	"fmt"
	"time"

	"plasmod/src/internal/schemas"
)

// MemoryViewBuilder constructs a policy-conditioned, algorithm-processed
// MemoryView (section 9 of the memory management design).
//
// Construction order (mirrors design doc section 9.3):
//  1. Filter candidates to those whose Scope is in the access graph's VisibleScopes
//  2. Apply policy exclusions (quarantined / hidden / logically deleted)
//  3. Optionally apply algorithm scoring via an AlgorithmScorer func
//  4. Assemble the final MemoryView
//
// Usage:
//
//	view := NewMemoryViewBuilder(requestID, agentID).
//	    WithSnapshot(snapshot).
//	    WithPolicyStore(policyStore).
//	    WithAlgorithmScorer(scorer).
//	    Build(candidates)
type MemoryViewBuilder struct {
	requestID   string
	requesterID string
	agentID     string
	snapshot    *schemas.AccessGraphSnapshot
	policyStore PolicyStore
	scorer      AlgorithmScorer
	notes       []string
}

// AlgorithmScorer is a function that reranks candidate memories using an
// algorithm plugin.  It maps directly to MemoryManagementAlgorithm.Recall.
// Returning nil leaves the ordering unchanged.
type AlgorithmScorer func(query string, candidates []schemas.Memory, ctx schemas.AlgorithmContext) []schemas.ScoredMemory

// NewMemoryViewBuilder creates a builder for the given request and agent.
func NewMemoryViewBuilder(requestID, requesterID, agentID string) *MemoryViewBuilder {
	return &MemoryViewBuilder{
		requestID:   requestID,
		requesterID: requesterID,
		agentID:     agentID,
	}
}

// WithSnapshot attaches an AccessGraphSnapshot for scope-based filtering.
func (b *MemoryViewBuilder) WithSnapshot(s *schemas.AccessGraphSnapshot) *MemoryViewBuilder {
	b.snapshot = s
	return b
}

// WithPolicyStore attaches a PolicyStore for policy-based exclusion.
func (b *MemoryViewBuilder) WithPolicyStore(ps PolicyStore) *MemoryViewBuilder {
	b.policyStore = ps
	return b
}

// WithAlgorithmScorer attaches a scorer function for recall-aware reranking.
func (b *MemoryViewBuilder) WithAlgorithmScorer(s AlgorithmScorer) *MemoryViewBuilder {
	b.scorer = s
	return b
}

// Build constructs a MemoryView from the provided candidate memories.
// query is passed to the scorer (empty string = no scoring preference).
func (b *MemoryViewBuilder) Build(candidates []schemas.Memory, query string) schemas.MemoryView {
	trace := []string{}

	// Step 1: scope-based candidate reduction
	scoped, resolvedScope := b.filterByScope(candidates)
	trace = append(trace, fmt.Sprintf("scope_filter: %d → %d candidates (scope=%s)", len(candidates), len(scoped), resolvedScope))

	// Step 2: policy exclusion (quarantined / hidden / logically deleted)
	allowed := b.filterByPolicy(scoped)
	trace = append(trace, fmt.Sprintf("policy_filter: %d → %d candidates", len(scoped), len(allowed)))

	// Step 3: algorithm scoring / reranking
	final := allowed
	algoNotes := []string{}
	if b.scorer != nil && len(allowed) > 0 {
		ctx := schemas.AlgorithmContext{
			AgentID:   b.agentID,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
		scored := b.scorer(query, allowed, ctx)
		if len(scored) > 0 {
			final = make([]schemas.Memory, 0, len(scored))
			for _, s := range scored {
				final = append(final, s.Memory)
			}
			algoNotes = append(algoNotes, fmt.Sprintf("scored %d candidates via algorithm scorer", len(scored)))
			trace = append(trace, "algorithm_rerank: applied")
		}
	}

	// Step 4: assemble view
	refs := make([]string, 0, len(final))
	for _, m := range final {
		refs = append(refs, m.MemoryID)
	}

	return schemas.MemoryView{
		RequestID:         b.requestID,
		RequesterID:       b.requesterID,
		AgentID:           b.agentID,
		ResolvedScope:     resolvedScope,
		VisibleMemoryRefs: refs,
		Payloads:          final,
		AlgorithmNotes:    algoNotes,
		ConstructionTrace: trace,
	}
}

// filterByScope keeps only memories whose Scope is visible to the current
// agent, as determined by the AccessGraphSnapshot.  When no snapshot is
// provided all candidates pass through.
func (b *MemoryViewBuilder) filterByScope(candidates []schemas.Memory) ([]schemas.Memory, string) {
	if b.snapshot == nil || len(b.snapshot.VisibleScopes) == 0 {
		return candidates, "unrestricted"
	}
	visible := make(map[string]bool, len(b.snapshot.VisibleScopes))
	for _, s := range b.snapshot.VisibleScopes {
		visible[s] = true
	}
	out := make([]schemas.Memory, 0, len(candidates))
	for _, m := range candidates {
		scope := m.Scope
		if scope == "" {
			scope = string(schemas.MemoryScopePrivateAgent) // default: private to agent
		}
		if visible[scope] {
			out = append(out, m)
		}
	}
	return out, b.snapshot.AgentID
}

// filterByPolicy excludes memories that are quarantined, hidden, or logically
// deleted at the lifecycle level, or have an active quarantine policy record.
func (b *MemoryViewBuilder) filterByPolicy(candidates []schemas.Memory) []schemas.Memory {
	out := make([]schemas.Memory, 0, len(candidates))
	for _, m := range candidates {
		if isExcludedByLifecycle(m.LifecycleState) {
			continue
		}
		if b.policyStore != nil && hasActiveQuarantine(b.policyStore.GetPolicies(m.MemoryID)) {
			continue
		}
		out = append(out, m)
	}
	return out
}

func isExcludedByLifecycle(ls string) bool {
	switch ls {
	case string(schemas.MemoryLifecycleQuarantined),
		string(schemas.MemoryLifecycleHidden),
		string(schemas.MemoryLifecycleDeletedLogically):
		return true
	}
	return false
}

func hasActiveQuarantine(policies []schemas.PolicyRecord) bool {
	for _, p := range policies {
		if p.QuarantineFlag {
			return true
		}
	}
	return false
}
