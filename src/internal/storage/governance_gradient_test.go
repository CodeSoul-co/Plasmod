package storage_test

import (
	"testing"
	"time"

	"plasmod/src/internal/retrieval"
	"plasmod/src/internal/schemas"
	"plasmod/src/internal/storage"
)

func countVisibleAfterScope(candidates []schemas.Memory, visibleScopes []string) int {
	snap := &schemas.AccessGraphSnapshot{
		AgentID:       "agent-gradient",
		VisibleScopes: visibleScopes,
	}
	view := storage.NewMemoryViewBuilder("req_gradient_scope", "user-gradient", "agent-gradient").
		WithSnapshot(snap).
		Build(candidates, "")
	return len(view.Payloads)
}

func countPassedBySafetyFilter(mems []schemas.Memory, req retrieval.RetrievalRequest) int {
	filter := retrieval.SafetyFilter{}
	passed := 0
	for _, mem := range mems {
		if filter.Apply(mem, req) {
			passed++
		}
	}
	return passed
}

func TestGovernanceGradient_SharingStrength(t *testing.T) {
	candidates := []schemas.Memory{
		{MemoryID: "m_private", Scope: string(schemas.MemoryScopePrivateAgent), LifecycleState: string(schemas.MemoryLifecycleActive), IsActive: true},
		{MemoryID: "m_session", Scope: string(schemas.MemoryScopeSessionLocal), LifecycleState: string(schemas.MemoryLifecycleActive), IsActive: true},
		{MemoryID: "m_workspace", Scope: string(schemas.MemoryScopeWorkspaceShared), LifecycleState: string(schemas.MemoryLifecycleActive), IsActive: true},
	}

	isolated := countVisibleAfterScope(candidates, []string{string(schemas.MemoryScopePrivateAgent)})
	partial := countVisibleAfterScope(candidates, []string{
		string(schemas.MemoryScopePrivateAgent),
		string(schemas.MemoryScopeSessionLocal),
	})
	full := countVisibleAfterScope(candidates, []string{
		string(schemas.MemoryScopePrivateAgent),
		string(schemas.MemoryScopeSessionLocal),
		string(schemas.MemoryScopeWorkspaceShared),
	})

	if isolated != 1 || partial != 2 || full != 3 {
		t.Fatalf("expected sharing strength ladder 1/2/3, got isolated=%d partial=%d full=%d", isolated, partial, full)
	}

	t.Logf("Governance gradient E1: isolated_visible=%d partial_visible=%d full_visible=%d level=sharing_strength",
		isolated, partial, full)
}

func TestGovernanceGradient_NoGovernance(t *testing.T) {
	now := time.Now().UTC()
	candidates := []schemas.Memory{
		{MemoryID: "m_active", Scope: string(schemas.MemoryScopeWorkspaceShared), LifecycleState: string(schemas.MemoryLifecycleActive), IsActive: true, TTL: now.Add(24 * time.Hour).Unix(), Version: 5},
		{MemoryID: "m_expired", Scope: string(schemas.MemoryScopeWorkspaceShared), LifecycleState: string(schemas.MemoryLifecycleActive), IsActive: true, TTL: now.Add(-1 * time.Hour).Unix(), Version: 5},
		{MemoryID: "m_quarantine", Scope: string(schemas.MemoryScopeWorkspaceShared), LifecycleState: string(schemas.MemoryLifecycleActive), IsActive: true, TTL: now.Add(24 * time.Hour).Unix(), Version: 5, PolicyTags: []string{"quarantine"}},
		{MemoryID: "m_old_version", Scope: string(schemas.MemoryScopeWorkspaceShared), LifecycleState: string(schemas.MemoryLifecycleActive), IsActive: true, TTL: now.Add(24 * time.Hour).Unix(), Version: 1},
	}

	returned := len(candidates)
	if returned != 4 {
		t.Fatalf("expected no-governance baseline to return all 4 candidates, got %d", returned)
	}

	t.Logf("Governance gradient E2: candidates=%d returned=%d level=no_governance",
		len(candidates), returned)
}

func TestGovernanceGradient_NamespaceOnly(t *testing.T) {
	candidates := []schemas.Memory{
		{MemoryID: "m_workspace_active", Scope: string(schemas.MemoryScopeWorkspaceShared), LifecycleState: string(schemas.MemoryLifecycleActive), IsActive: true},
		{MemoryID: "m_workspace_aux", Scope: string(schemas.MemoryScopeWorkspaceShared), LifecycleState: string(schemas.MemoryLifecycleActive), IsActive: true},
		{MemoryID: "m_private_hidden", Scope: string(schemas.MemoryScopePrivateAgent), LifecycleState: string(schemas.MemoryLifecycleActive), IsActive: true},
		{MemoryID: "m_team_hidden", Scope: string(schemas.MemoryScopeTeamShared), LifecycleState: string(schemas.MemoryLifecycleActive), IsActive: true},
	}
	snap := &schemas.AccessGraphSnapshot{
		AgentID:       "agent-gradient",
		VisibleScopes: []string{string(schemas.MemoryScopeWorkspaceShared)},
	}

	view := storage.NewMemoryViewBuilder("req_gradient_e3", "user-gradient", "agent-gradient").
		WithSnapshot(snap).
		Build(candidates, "")

	if len(view.Payloads) != 2 {
		t.Fatalf("expected namespace-only layer to keep 2 workspace-shared memories, got %d", len(view.Payloads))
	}

	t.Logf("Governance gradient E3: namespace_visible=%d excluded_by_namespace=%d returned=%d level=namespace_only",
		len(view.Payloads), len(candidates)-len(view.Payloads), len(view.Payloads))
}

func TestGovernanceGradient_NamespaceTTL(t *testing.T) {
	now := time.Now().UTC()
	snap := &schemas.AccessGraphSnapshot{
		AgentID:       "agent-gradient",
		VisibleScopes: []string{string(schemas.MemoryScopeWorkspaceShared)},
	}
	candidates := []schemas.Memory{
		{
			MemoryID:       "m_visible_active",
			Scope:          string(schemas.MemoryScopeWorkspaceShared),
			LifecycleState: string(schemas.MemoryLifecycleActive),
			IsActive:       true,
			TTL:            now.Add(24 * time.Hour).Unix(),
			Version:        5,
		},
		{
			MemoryID:       "m_visible_expired",
			Scope:          string(schemas.MemoryScopeWorkspaceShared),
			LifecycleState: string(schemas.MemoryLifecycleActive),
			IsActive:       true,
			TTL:            now.Add(-1 * time.Hour).Unix(),
			Version:        5,
		},
		{
			MemoryID:       "m_private_hidden",
			Scope:          string(schemas.MemoryScopePrivateAgent),
			LifecycleState: string(schemas.MemoryLifecycleActive),
			IsActive:       true,
			TTL:            now.Add(24 * time.Hour).Unix(),
			Version:        5,
		},
	}

	view := storage.NewMemoryViewBuilder("req_gradient_e4", "user-gradient", "agent-gradient").
		WithSnapshot(snap).
		Build(candidates, "")

	req := retrieval.DefaultRetrievalRequest("gradient namespace ttl", 10)
	returned := countPassedBySafetyFilter(view.Payloads, req)
	ttlExcluded := len(view.Payloads) - returned

	if len(view.Payloads) != 2 {
		t.Fatalf("expected 2 namespace-visible payloads, got %d", len(view.Payloads))
	}
	if returned != 1 || ttlExcluded != 1 {
		t.Fatalf("expected namespace+ttl to return=1 ttl_excluded=1, got return=%d ttl_excluded=%d", returned, ttlExcluded)
	}

	t.Logf("Governance gradient E4: namespace_visible=%d ttl_excluded=%d returned=%d level=namespace+ttl",
		len(view.Payloads), ttlExcluded, returned)
}

func TestGovernanceGradient_NamespaceTTLQuarantine(t *testing.T) {
	now := time.Now().UTC()
	snap := &schemas.AccessGraphSnapshot{
		AgentID:       "agent-gradient",
		VisibleScopes: []string{string(schemas.MemoryScopeWorkspaceShared)},
	}
	candidates := []schemas.Memory{
		{
			MemoryID:       "m_visible_active",
			Scope:          string(schemas.MemoryScopeWorkspaceShared),
			LifecycleState: string(schemas.MemoryLifecycleActive),
			IsActive:       true,
			TTL:            now.Add(24 * time.Hour).Unix(),
			Version:        5,
		},
		{
			MemoryID:       "m_visible_expired",
			Scope:          string(schemas.MemoryScopeWorkspaceShared),
			LifecycleState: string(schemas.MemoryLifecycleActive),
			IsActive:       true,
			TTL:            now.Add(-1 * time.Hour).Unix(),
			Version:        5,
		},
		{
			MemoryID:       "m_visible_quarantine",
			Scope:          string(schemas.MemoryScopeWorkspaceShared),
			LifecycleState: string(schemas.MemoryLifecycleActive),
			IsActive:       true,
			TTL:            now.Add(24 * time.Hour).Unix(),
			Version:        5,
			PolicyTags:     []string{"quarantine"},
		},
		{
			MemoryID:       "m_private_hidden",
			Scope:          string(schemas.MemoryScopePrivateAgent),
			LifecycleState: string(schemas.MemoryLifecycleActive),
			IsActive:       true,
			TTL:            now.Add(24 * time.Hour).Unix(),
			Version:        5,
		},
	}

	view := storage.NewMemoryViewBuilder("req_gradient_e5", "user-gradient", "agent-gradient").
		WithSnapshot(snap).
		Build(candidates, "")

	req := retrieval.DefaultRetrievalRequest("gradient namespace ttl quarantine", 10)
	returned := countPassedBySafetyFilter(view.Payloads, req)
	ttlExcluded := 0
	quarantineExcluded := 0
	filter := retrieval.SafetyFilter{}
	for _, mem := range view.Payloads {
		if filter.Apply(mem, req) {
			continue
		}
		switch {
		case mem.TTL > 0 && mem.TTL < now.Unix():
			ttlExcluded++
		case len(mem.PolicyTags) > 0 && mem.PolicyTags[0] == "quarantine":
			quarantineExcluded++
		}
	}

	if len(view.Payloads) != 3 {
		t.Fatalf("expected 3 namespace-visible payloads, got %d", len(view.Payloads))
	}
	if returned != 1 || ttlExcluded != 1 || quarantineExcluded != 1 {
		t.Fatalf("expected namespace+ttl+quarantine to return=1 ttl=1 quarantine=1, got return=%d ttl=%d quarantine=%d",
			returned, ttlExcluded, quarantineExcluded)
	}

	t.Logf("Governance gradient E5: namespace_visible=%d ttl_excluded=%d quarantine_excluded=%d returned=%d level=namespace+ttl+quarantine",
		len(view.Payloads), ttlExcluded, quarantineExcluded, returned)
}

func TestGovernanceGradient_FullPolicyLayer(t *testing.T) {
	now := time.Now().UTC()
	snap := &schemas.AccessGraphSnapshot{
		AgentID:       "agent-gradient",
		VisibleScopes: []string{string(schemas.MemoryScopeWorkspaceShared)},
	}
	candidates := []schemas.Memory{
		{
			MemoryID:       "m_visible_active",
			Scope:          string(schemas.MemoryScopeWorkspaceShared),
			LifecycleState: string(schemas.MemoryLifecycleActive),
			IsActive:       true,
			TTL:            now.Add(24 * time.Hour).Unix(),
			Version:        5,
		},
		{
			MemoryID:       "m_visible_expired",
			Scope:          string(schemas.MemoryScopeWorkspaceShared),
			LifecycleState: string(schemas.MemoryLifecycleActive),
			IsActive:       true,
			TTL:            now.Add(-1 * time.Hour).Unix(),
			Version:        5,
		},
		{
			MemoryID:       "m_visible_quarantine",
			Scope:          string(schemas.MemoryScopeWorkspaceShared),
			LifecycleState: string(schemas.MemoryLifecycleActive),
			IsActive:       true,
			TTL:            now.Add(24 * time.Hour).Unix(),
			Version:        5,
			PolicyTags:     []string{"quarantine"},
		},
		{
			MemoryID:       "m_visible_old_version",
			Scope:          string(schemas.MemoryScopeWorkspaceShared),
			LifecycleState: string(schemas.MemoryLifecycleActive),
			IsActive:       true,
			TTL:            now.Add(24 * time.Hour).Unix(),
			Version:        1,
		},
		{
			MemoryID:       "m_visible_unverified",
			Scope:          string(schemas.MemoryScopeWorkspaceShared),
			LifecycleState: "unverified",
			IsActive:       true,
			TTL:            now.Add(24 * time.Hour).Unix(),
			Version:        5,
		},
		{
			MemoryID:       "m_private_hidden",
			Scope:          string(schemas.MemoryScopePrivateAgent),
			LifecycleState: string(schemas.MemoryLifecycleActive),
			IsActive:       true,
			TTL:            now.Add(24 * time.Hour).Unix(),
			Version:        5,
		},
	}

	view := storage.NewMemoryViewBuilder("req_gradient_e6", "user-gradient", "agent-gradient").
		WithSnapshot(snap).
		Build(candidates, "")

	req := retrieval.DefaultRetrievalRequest("gradient full policy", 10)
	req.MinVersion = 3
	req.ExcludeUnverified = true

	returned := countPassedBySafetyFilter(view.Payloads, req)
	ttlExcluded := 0
	quarantineExcluded := 0
	minVersionExcluded := 0
	unverifiedExcluded := 0
	filter := retrieval.SafetyFilter{}
	for _, mem := range view.Payloads {
		if filter.Apply(mem, req) {
			continue
		}
		switch {
		case mem.TTL > 0 && mem.TTL < now.Unix():
			ttlExcluded++
		case len(mem.PolicyTags) > 0 && mem.PolicyTags[0] == "quarantine":
			quarantineExcluded++
		case req.MinVersion > 0 && mem.Version < req.MinVersion:
			minVersionExcluded++
		case mem.LifecycleState == "unverified":
			unverifiedExcluded++
		}
	}

	if len(view.Payloads) != 5 {
		t.Fatalf("expected 5 namespace-visible payloads, got %d", len(view.Payloads))
	}
	if returned != 1 || ttlExcluded != 1 || quarantineExcluded != 1 || minVersionExcluded != 1 || unverifiedExcluded != 1 {
		t.Fatalf("expected full policy layer to return=1 ttl=1 quarantine=1 min_version=1 unverified=1, got return=%d ttl=%d quarantine=%d min_version=%d unverified=%d",
			returned, ttlExcluded, quarantineExcluded, minVersionExcluded, unverifiedExcluded)
	}

	t.Logf("Governance gradient E6: namespace_visible=%d ttl_excluded=%d quarantine_excluded=%d min_version_excluded=%d unverified_excluded=%d returned=%d level=full_policy_layer",
		len(view.Payloads), ttlExcluded, quarantineExcluded, minVersionExcluded, unverifiedExcluded, returned)
}

func TestGovernanceGradient_LatencyOverhead(t *testing.T) {
	now := time.Now().UTC()
	candidates := []schemas.Memory{
		{MemoryID: "m_active", Scope: string(schemas.MemoryScopeWorkspaceShared), LifecycleState: string(schemas.MemoryLifecycleActive), IsActive: true, TTL: now.Add(24 * time.Hour).Unix(), Version: 5},
		{MemoryID: "m_expired", Scope: string(schemas.MemoryScopeWorkspaceShared), LifecycleState: string(schemas.MemoryLifecycleActive), IsActive: true, TTL: now.Add(-1 * time.Hour).Unix(), Version: 5},
		{MemoryID: "m_quarantine", Scope: string(schemas.MemoryScopeWorkspaceShared), LifecycleState: string(schemas.MemoryLifecycleActive), IsActive: true, TTL: now.Add(24 * time.Hour).Unix(), Version: 5, PolicyTags: []string{"quarantine"}},
		{MemoryID: "m_old_version", Scope: string(schemas.MemoryScopeWorkspaceShared), LifecycleState: string(schemas.MemoryLifecycleActive), IsActive: true, TTL: now.Add(24 * time.Hour).Unix(), Version: 1},
		{MemoryID: "m_unverified", Scope: string(schemas.MemoryScopeWorkspaceShared), LifecycleState: "unverified", IsActive: true, TTL: now.Add(24 * time.Hour).Unix(), Version: 5},
	}

	req := retrieval.DefaultRetrievalRequest("latency overhead baseline", 10)
	startNoGov := time.Now()
	_ = len(candidates)
	noGovElapsed := time.Since(startNoGov)

	req.MinVersion = 3
	req.ExcludeUnverified = true
	startPolicy := time.Now()
	_ = countPassedBySafetyFilter(candidates, req)
	policyElapsed := time.Since(startPolicy)

	overheadMs := float64(policyElapsed-noGovElapsed) / float64(time.Millisecond)
	if overheadMs < 0 {
		overheadMs = 0
	}

	t.Logf("Governance gradient E9: baseline_candidates=%d baseline_ms=%.3f policy_ms=%.3f overhead_ms=%.3f level=latency_overhead",
		len(candidates),
		float64(noGovElapsed)/float64(time.Millisecond),
		float64(policyElapsed)/float64(time.Millisecond),
		overheadMs)
}

func TestGovernanceMetrics_StaleMemoryUsageRate(t *testing.T) {
	now := time.Now().UTC()
	candidates := []schemas.Memory{
		{MemoryID: "m_active", Scope: string(schemas.MemoryScopeWorkspaceShared), LifecycleState: string(schemas.MemoryLifecycleActive), IsActive: true, TTL: now.Add(24 * time.Hour).Unix(), Version: 5},
		{MemoryID: "m_expired_a", Scope: string(schemas.MemoryScopeWorkspaceShared), LifecycleState: string(schemas.MemoryLifecycleActive), IsActive: true, TTL: now.Add(-1 * time.Hour).Unix(), Version: 5},
		{MemoryID: "m_expired_b", Scope: string(schemas.MemoryScopeWorkspaceShared), LifecycleState: string(schemas.MemoryLifecycleActive), IsActive: true, TTL: now.Add(-2 * time.Hour).Unix(), Version: 5},
	}

	req := retrieval.DefaultRetrievalRequest("stale-memory-rate", 10)
	returned := countPassedBySafetyFilter(candidates, req)
	staleFiltered := len(candidates) - returned
	rate := float64(staleFiltered) / float64(len(candidates))

	if staleFiltered != 2 {
		t.Fatalf("expected 2 stale memories to be filtered, got %d", staleFiltered)
	}

	t.Logf("Governance metric M2: total=%d stale_filtered=%d returned=%d stale_usage_rate=%.3f",
		len(candidates), staleFiltered, returned, rate)
}

func TestGovernanceMetrics_PolicyViolationRate(t *testing.T) {
	now := time.Now().UTC()
	candidates := []schemas.Memory{
		{MemoryID: "m_allowed", Scope: string(schemas.MemoryScopeWorkspaceShared), LifecycleState: string(schemas.MemoryLifecycleActive), IsActive: true, TTL: now.Add(24 * time.Hour).Unix(), Version: 5},
		{MemoryID: "m_quarantine", Scope: string(schemas.MemoryScopeWorkspaceShared), LifecycleState: string(schemas.MemoryLifecycleActive), IsActive: true, TTL: now.Add(24 * time.Hour).Unix(), Version: 5, PolicyTags: []string{"quarantine"}},
		{MemoryID: "m_unverified", Scope: string(schemas.MemoryScopeWorkspaceShared), LifecycleState: "unverified", IsActive: true, TTL: now.Add(24 * time.Hour).Unix(), Version: 5},
		{MemoryID: "m_old_version", Scope: string(schemas.MemoryScopeWorkspaceShared), LifecycleState: string(schemas.MemoryLifecycleActive), IsActive: true, TTL: now.Add(24 * time.Hour).Unix(), Version: 1},
	}

	req := retrieval.DefaultRetrievalRequest("policy-violation-rate", 10)
	req.MinVersion = 3
	req.ExcludeUnverified = true

	returned := countPassedBySafetyFilter(candidates, req)
	violations := len(candidates) - returned
	rate := float64(violations) / float64(len(candidates))

	if violations != 3 {
		t.Fatalf("expected 3 policy violations to be filtered, got %d", violations)
	}

	t.Logf("Governance metric M3: total=%d violations_filtered=%d returned=%d policy_violation_rate=%.3f",
		len(candidates), violations, returned, rate)
}

func TestGovernanceMetrics_LatencyOverhead(t *testing.T) {
	now := time.Now().UTC()
	candidates := []schemas.Memory{
		{MemoryID: "m_active", Scope: string(schemas.MemoryScopeWorkspaceShared), LifecycleState: string(schemas.MemoryLifecycleActive), IsActive: true, TTL: now.Add(24 * time.Hour).Unix(), Version: 5},
		{MemoryID: "m_expired", Scope: string(schemas.MemoryScopeWorkspaceShared), LifecycleState: string(schemas.MemoryLifecycleActive), IsActive: true, TTL: now.Add(-1 * time.Hour).Unix(), Version: 5},
		{MemoryID: "m_quarantine", Scope: string(schemas.MemoryScopeWorkspaceShared), LifecycleState: string(schemas.MemoryLifecycleActive), IsActive: true, TTL: now.Add(24 * time.Hour).Unix(), Version: 5, PolicyTags: []string{"quarantine"}},
		{MemoryID: "m_unverified", Scope: string(schemas.MemoryScopeWorkspaceShared), LifecycleState: "unverified", IsActive: true, TTL: now.Add(24 * time.Hour).Unix(), Version: 5},
	}

	req := retrieval.DefaultRetrievalRequest("governance-overhead", 10)
	startNoGov := time.Now()
	_ = len(candidates)
	noGovElapsed := time.Since(startNoGov)

	req.ExcludeUnverified = true
	startGov := time.Now()
	_ = countPassedBySafetyFilter(candidates, req)
	govElapsed := time.Since(startGov)

	overheadMs := float64(govElapsed-noGovElapsed) / float64(time.Millisecond)
	if overheadMs < 0 {
		overheadMs = 0
	}

	t.Logf("Governance metric M7: baseline_ms=%.3f governance_ms=%.3f overhead_ms=%.3f",
		float64(noGovElapsed)/float64(time.Millisecond),
		float64(govElapsed)/float64(time.Millisecond),
		overheadMs)
}
