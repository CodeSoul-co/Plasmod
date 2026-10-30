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
