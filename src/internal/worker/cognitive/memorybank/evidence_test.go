package memorybank

import (
	"plasmod/src/internal/schemas"
	"testing"
)

func TestExtractedFactsSharingAnUtteranceAreNotContradictions(t *testing.T) {
	a := schemas.Memory{MemoryID: "a", TenantID: "t", WorkspaceID: "w", AgentID: "agent", MemoryType: "factual", FactKey: "user.home_city", Content: "lives in Shanghai", SourceEventIDs: []string{"same-utterance"}, IsActive: true}
	b := a
	b.MemoryID = "b"
	b.Content = "works as an engineer"
	b.FactKey = "user.profession"
	if got := DetectConflicts(NewConflictRegistry(), []schemas.Memory{a}, b); len(got) != 0 {
		t.Fatal("independent extracted facts quarantined")
	}
	a.FactKey = "user.home_city"
	b.FactKey = "user.home_city"
	b.Content = "lives in Beijing"
	if got := DetectConflicts(NewConflictRegistry(), []schemas.Memory{a}, b); len(got) != 1 {
		t.Fatal("same-attribute conflict missed")
	}
	b.TenantID = "other"
	if got := DetectConflicts(NewConflictRegistry(), []schemas.Memory{a}, b); len(got) != 0 {
		t.Fatal("cross-tenant conflict")
	}
}

func TestFactIdentityConflictAndEvidenceBoundaries(t *testing.T) {
	base := schemas.Memory{MemoryID: "en", TenantID: "t", WorkspaceID: "w", AgentID: "a", SessionID: "s1", MemoryType: "factual", FactKey: "user.home_city", Content: "User lives in Shanghai.", FactIdentity: "same-city", Confidence: .95, IsActive: true, SourceEventIDs: []string{"e1"}}
	translated := base
	translated.MemoryID = "zh"
	translated.Content = "用户居住在上海。"
	translated.SessionID = "s2"
	translated.SourceEventIDs = []string{"e2"}
	if conflicts := DetectConflicts(NewConflictRegistry(), []schemas.Memory{base}, translated); len(conflicts) != 0 {
		t.Fatalf("translation is a conflict: %+v", conflicts)
	}
	third := translated
	third.MemoryID = "third"
	third.SessionID = "s3"
	third.SourceEventIDs = []string{"e3"}
	for _, tc := range []struct {
		name   string
		mutate func(*schemas.Memory)
		want   int
	}{
		{"equivalent", func(*schemas.Memory) {}, 3},
		{"other tenant", func(m *schemas.Memory) { m.TenantID = "other" }, 2},
		{"other workspace", func(m *schemas.Memory) { m.WorkspaceID = "other" }, 2},
		{"other agent", func(m *schemas.Memory) { m.AgentID = "other" }, 2},
		{"other type", func(m *schemas.Memory) { m.MemoryType = "preference_constraint" }, 2},
		{"negated", func(m *schemas.Memory) { m.Content = "User does not live in Shanghai."; m.FactIdentity = "negated" }, 2},
		{"changed value", func(m *schemas.Memory) { m.Content = "User lives in Beijing."; m.FactIdentity = "beijing" }, 2},
		{"different period", func(m *schemas.Memory) { m.FactValidFrom = "2026-11-01T00:00:00Z" }, 2},
		{"repeated event", func(m *schemas.Memory) { m.SourceEventIDs = []string{"e2"} }, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := third
			tc.mutate(&m)
			mb := New("test", DefaultConfig())
			mb.RestoreMemories([]schemas.Memory{base, translated, m})
			state := schemas.MemoryAlgorithmState{SuggestedLifecycleState: "candidate"}
			mb.evaluateAdmissionLocked(base, &state, false)
			if len(state.EvidenceEventIDs) != tc.want {
				t.Fatalf("evidence=%v want=%d", state.EvidenceEventIDs, tc.want)
			}
			if tc.want < 3 && state.SuggestedLifecycleState == "active" {
				t.Fatal("insufficient independent evidence promoted")
			}
		})
	}
	third.SessionID = "s2"
	mb := New("test", DefaultConfig())
	mb.RestoreMemories([]schemas.Memory{base, translated, third})
	state := schemas.MemoryAlgorithmState{SuggestedLifecycleState: "candidate"}
	mb.evaluateAdmissionLocked(base, &state, false)
	if state.SuggestedLifecycleState != "candidate" {
		t.Fatal("same session counted twice")
	}
	base.FactValidFrom, base.FactValidTo = "2025-01-01T00:00:00Z", "2026-01-01T00:00:00Z"
	third.Content, third.FactIdentity, third.FactValidFrom = "User lives in Beijing.", "beijing", "2026-01-01T00:00:00Z"
	if conflicts := DetectConflicts(NewConflictRegistry(), []schemas.Memory{base}, third); len(conflicts) != 0 {
		t.Fatalf("non-overlapping history conflicted: %+v", conflicts)
	}
}
