package semantic

import (
	"testing"

	"andb/src/internal/schemas"
)

func TestObjectModelRegistry_DefaultTypes(t *testing.T) {
	r := NewObjectModelRegistry()

	required := []schemas.ObjectType{
		schemas.ObjectTypeAgent,
		schemas.ObjectTypeSession,
		schemas.ObjectTypeEvent,
		schemas.ObjectTypeMemory,
		schemas.ObjectTypeState,
		schemas.ObjectTypeArtifact,
		schemas.ObjectTypeEdge,
	}
	for _, ot := range required {
		if !r.IsKnown(ot) {
			t.Errorf("ObjectType %q should be registered by default", ot)
		}
	}
}

func TestObjectModelRegistry_IsIndexable(t *testing.T) {
	r := NewObjectModelRegistry()

	cases := []struct {
		ot        schemas.ObjectType
		wantIndex bool
	}{
		{schemas.ObjectTypeMemory, true},
		{schemas.ObjectTypeEvent, true},
		{schemas.ObjectTypeAgent, false},
		{schemas.ObjectTypeSession, false},
	}
	for _, tc := range cases {
		got := r.IsIndexable(tc.ot)
		if got != tc.wantIndex {
			t.Errorf("IsIndexable(%q): want %v, got %v", tc.ot, tc.wantIndex, got)
		}
	}
}

func TestPolicyEngine_ApplyQueryFilters(t *testing.T) {
	pe := NewPolicyEngine()
	req := schemas.QueryRequest{
		QueryText: "test",
		AgentID:   "agent_1",
	}
	filters := pe.ApplyQueryFilters(req)
	if len(filters) == 0 {
		t.Error("ApplyQueryFilters: expected at least one filter for a request with AgentID")
	}
}
