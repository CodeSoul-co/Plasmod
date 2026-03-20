package schemas

import (
	"encoding/json"
	"testing"
)

func TestQueryRequest_JSONContract(t *testing.T) {
	req := QueryRequest{}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal query request: %v", err)
	}
	s := string(data)

	expected := []string{
		`"query_text"`,
		`"query_scope"`,
		`"session_id"`,
		`"agent_id"`,
		`"top_k"`,
		`"time_window"`,
		`"relation_constraints"`,
		`"response_mode"`,
	}
	for _, key := range expected {
		if !containsSubstring(s, key) {
			t.Errorf("expected request JSON to contain %s, got %s", key, s)
		}
	}
}

func TestResponseModeConstants(t *testing.T) {
	if ResponseModeStructuredEvidence == "" || ResponseModeObjectsOnly == "" {
		t.Fatalf("response mode constants must not be empty")
	}
}
