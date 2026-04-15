package storage

import (
	"testing"

	"plasmod/src/internal/schemas"
)

func TestMemoryViewBuilder_NoSnapshot_PassesAll(t *testing.T) {
	candidates := []schemas.Memory{
		{MemoryID: "m1", Scope: string(schemas.MemoryScopeWorkspaceShared), LifecycleState: string(schemas.MemoryLifecycleActive)},
		{MemoryID: "m2", Scope: string(schemas.MemoryScopePrivateAgent), LifecycleState: string(schemas.MemoryLifecycleActive)},
	}

	view := NewMemoryViewBuilder("req1", "user1", "agent1").
		Build(candidates, "")

	if len(view.Payloads) != 2 {
		t.Errorf("want 2 payloads (no scope filter), got %d", len(view.Payloads))
	}
	if view.RequestID != "req1" {
		t.Errorf("RequestID: want req1, got %q", view.RequestID)
	}
	if view.ResolvedScope != "unrestricted" {
		t.Errorf("ResolvedScope: want unrestricted, got %q", view.ResolvedScope)
	}
}

func TestMemoryViewBuilder_ScopeFilter_ExcludesOtherScopes(t *testing.T) {
	snap := &schemas.AccessGraphSnapshot{
		AgentID:       "agent1",
		VisibleScopes: []string{string(schemas.MemoryScopeWorkspaceShared)},
	}
	candidates := []schemas.Memory{
		{MemoryID: "m1", Scope: string(schemas.MemoryScopeWorkspaceShared)},
		{MemoryID: "m2", Scope: string(schemas.MemoryScopePrivateAgent)},
		{MemoryID: "m3", Scope: string(schemas.MemoryScopeTeamShared)},
	}

	view := NewMemoryViewBuilder("req2", "user1", "agent1").
		WithSnapshot(snap).
		Build(candidates, "")

	if len(view.Payloads) != 1 {
		t.Errorf("want 1 payload after scope filter, got %d", len(view.Payloads))
	}
	if view.Payloads[0].MemoryID != "m1" {
		t.Errorf("expected m1 to survive scope filter, got %q", view.Payloads[0].MemoryID)
	}
}

func TestMemoryViewBuilder_PolicyFilter_ExcludesQuarantined(t *testing.T) {
	store := NewMemoryRuntimeStorage()
	store.Policies().AppendPolicy(schemas.PolicyRecord{
		PolicyID:       "pol1",
		ObjectID:       "m_bad",
		QuarantineFlag: true,
	})

	candidates := []schemas.Memory{
		{MemoryID: "m_good", LifecycleState: string(schemas.MemoryLifecycleActive)},
		{MemoryID: "m_bad", LifecycleState: string(schemas.MemoryLifecycleActive)},
		{MemoryID: "m_hidden", LifecycleState: string(schemas.MemoryLifecycleHidden)},
	}

	view := NewMemoryViewBuilder("req3", "user1", "agent1").
		WithPolicyStore(store.Policies()).
		Build(candidates, "")

	if len(view.Payloads) != 1 {
		t.Errorf("want 1 payload after policy filter, got %d", len(view.Payloads))
	}
	if view.Payloads[0].MemoryID != "m_good" {
		t.Errorf("expected m_good to survive, got %q", view.Payloads[0].MemoryID)
	}
}

func TestMemoryViewBuilder_AlgorithmScorer_ReordersResults(t *testing.T) {
	candidates := []schemas.Memory{
		{MemoryID: "low", Importance: 0.1},
		{MemoryID: "high", Importance: 0.9},
	}

	scorer := func(query string, mems []schemas.Memory, ctx schemas.AlgorithmContext) []schemas.ScoredMemory {
		// return high-importance first
		scored := make([]schemas.ScoredMemory, 0, len(mems))
		for _, m := range mems {
			scored = append(scored, schemas.ScoredMemory{Memory: m, Score: m.Importance})
		}
		// sort descending
		for i := 0; i < len(scored); i++ {
			for j := i + 1; j < len(scored); j++ {
				if scored[j].Score > scored[i].Score {
					scored[i], scored[j] = scored[j], scored[i]
				}
			}
		}
		return scored
	}

	view := NewMemoryViewBuilder("req4", "user1", "agent1").
		WithAlgorithmScorer(scorer).
		Build(candidates, "test")

	if len(view.Payloads) != 2 {
		t.Fatalf("want 2 payloads, got %d", len(view.Payloads))
	}
	if view.Payloads[0].MemoryID != "high" {
		t.Errorf("expected high-importance first, got %q", view.Payloads[0].MemoryID)
	}
	if len(view.AlgorithmNotes) == 0 {
		t.Error("expected AlgorithmNotes to be populated")
	}
	if len(view.ConstructionTrace) == 0 {
		t.Error("expected ConstructionTrace to be populated")
	}
}

func TestMemoryViewBuilder_VisibleMemoryRefs_Matches_Payloads(t *testing.T) {
	candidates := []schemas.Memory{
		{MemoryID: "ref1"},
		{MemoryID: "ref2"},
	}
	view := NewMemoryViewBuilder("req5", "u1", "a1").Build(candidates, "")
	if len(view.VisibleMemoryRefs) != len(view.Payloads) {
		t.Errorf("VisibleMemoryRefs len %d != Payloads len %d", len(view.VisibleMemoryRefs), len(view.Payloads))
	}
	for i, id := range view.VisibleMemoryRefs {
		if id != view.Payloads[i].MemoryID {
			t.Errorf("ref[%d]=%q != payload[%d].MemoryID=%q", i, id, i, view.Payloads[i].MemoryID)
		}
	}
}

func TestMemoryViewBuilder_SharedPrivateScopeDefinition(t *testing.T) {
	snap := &schemas.AccessGraphSnapshot{
		AgentID: "agent-scope",
		VisibleScopes: []string{
			string(schemas.MemoryScopeRestrictedShared),
			string(schemas.MemoryScopeWorkspaceShared),
		},
	}
	candidates := []schemas.Memory{
		{MemoryID: "m_private", Scope: string(schemas.MemoryScopePrivateAgent), LifecycleState: string(schemas.MemoryLifecycleActive)},
		{MemoryID: "m_restricted", Scope: string(schemas.MemoryScopeRestrictedShared), LifecycleState: string(schemas.MemoryLifecycleActive)},
		{MemoryID: "m_workspace", Scope: string(schemas.MemoryScopeWorkspaceShared), LifecycleState: string(schemas.MemoryLifecycleActive)},
	}

	view := NewMemoryViewBuilder("req_scope_matrix", "user-scope", "agent-scope").
		WithSnapshot(snap).
		Build(candidates, "")

	if len(view.Payloads) != 2 {
		t.Fatalf("expected 2 payloads after shared/private scope filter, got %d", len(view.Payloads))
	}

	got := map[string]bool{}
	for _, payload := range view.Payloads {
		got[payload.MemoryID] = true
	}
	if got["m_private"] {
		t.Fatal("expected private-agent memory to be excluded from shared scope view")
	}
	if !got["m_restricted"] || !got["m_workspace"] {
		t.Fatalf("expected restricted/workspace shared memories to remain, got %+v", got)
	}

	t.Logf("Scope definition: private_agent_visible=%t restricted_shared_visible=%t workspace_shared_visible=%t visible_refs=%d",
		got["m_private"], got["m_restricted"], got["m_workspace"], len(view.VisibleMemoryRefs))
}

func TestMemoryViewBuilder_VisibilityScopeCoverage(t *testing.T) {
	snap := &schemas.AccessGraphSnapshot{
		AgentID: "agent-coverage",
		VisibleScopes: []string{
			string(schemas.MemoryScopeSessionLocal),
			string(schemas.MemoryScopeWorkspaceShared),
		},
	}
	candidates := []schemas.Memory{
		{MemoryID: "m_session", Scope: string(schemas.MemoryScopeSessionLocal), LifecycleState: string(schemas.MemoryLifecycleActive)},
		{MemoryID: "m_workspace", Scope: string(schemas.MemoryScopeWorkspaceShared), LifecycleState: string(schemas.MemoryLifecycleActive)},
		{MemoryID: "m_private", Scope: string(schemas.MemoryScopePrivateAgent), LifecycleState: string(schemas.MemoryLifecycleActive)},
		{MemoryID: "m_team", Scope: string(schemas.MemoryScopeTeamShared), LifecycleState: string(schemas.MemoryLifecycleActive)},
	}

	view := NewMemoryViewBuilder("req_scope_coverage", "user-coverage", "agent-coverage").
		WithSnapshot(snap).
		Build(candidates, "")

	if len(view.Payloads) != 2 {
		t.Fatalf("expected 2 visible payloads, got %d", len(view.Payloads))
	}

	excluded := len(candidates) - len(view.Payloads)
	t.Logf("Visibility scope coverage: visible_refs=%d total_candidates=%d excluded_refs=%d resolved_scope=%s",
		len(view.VisibleMemoryRefs), len(candidates), excluded, view.ResolvedScope)
}
