package schemas

import (
	"encoding/json"
	"testing"
)

// TestCanonicalSchemas_JSONTags ensures that the core canonical objects
// keep their JSON field tags stable for the v1 contract.
func TestCanonicalSchemas_JSONTags(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected []string
	}{
		{
			name:  "Agent",
			value: Agent{},
			expected: []string{
				`"agent_id"`, `"tenant_id"`, `"workspace_id"`,
				`"policy_ref"`, `"capability_set"`, `"default_memory_policy"`,
			},
		},
		{
			name:  "Session",
			value: Session{},
			expected: []string{
				`"session_id"`, `"agent_id"`, `"parent_session_id"`,
				`"goal"`, `"context_ref"`, `"budget_token"`, `"budget_time_ms"`,
			},
		},
		{
			name:  "Event",
			value: Event{},
			expected: []string{
				`"event_id"`, `"tenant_id"`, `"workspace_id"`,
				`"agent_id"`, `"session_id"`, `"event_type"`,
				`"payload"`, `"logical_ts"`,
			},
		},
		{
			name:  "Memory",
			value: Memory{},
			expected: []string{
				`"memory_id"`, `"memory_type"`, `"agent_id"`, `"session_id"`,
				`"owner_type"`, `"scope"`, `"level"`,
				`"content"`, `"summary"`, `"source_event_ids"`,
			},
		},
		{
			name:  "Artifact",
			value: Artifact{},
			expected: []string{
				`"artifact_id"`, `"session_id"`, `"owner_agent_id"`,
				`"artifact_type"`, `"content_ref"`, `"produced_by_event_id"`,
			},
		},
		{
			name:  "User",
			value: User{},
			expected: []string{
				`"user_id"`, `"user_name"`, `"user_tenant_id"`, `"user_workspace_id"`,
			},
		},
		{
			name:  "Embedding",
			value: Embedding{},
			expected: []string{
				`"vector_id"`, `"vector_context"`, `"original_text"`,
				`"vector_ref"`, `"model_id"`,
			},
		},
		{
			name:  "Policy",
			value: Policy{},
			expected: []string{
				`"policy_id"`, `"policy_version"`,
				`"policy_start_time"`, `"policy_end_time"`,
				`"publisher_type"`, `"publisher_id"`,
			},
		},
		{
			name:  "PolicyRecord",
			value: PolicyRecord{},
			expected: []string{
				`"policy_id"`, `"policy_version"`, `"context"`,
				`"object_id"`, `"object_type"`,
				`"ttl"`, `"quarantine_flag"`, `"policy_event_id"`,
			},
		},
		{
			name:  "ShareContract",
			value: ShareContract{},
			expected: []string{
				`"contract_id"`, `"scope"`,
				`"read_acl"`, `"write_acl"`, `"derive_acl"`,
				`"ttl_policy"`, `"consistency_level"`,
				`"merge_policy"`, `"quarantine_policy"`, `"audit_policy"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.value)
			if err != nil {
				t.Fatalf("failed to marshal %s: %v", tt.name, err)
			}
			s := string(data)
			for _, key := range tt.expected {
				if !containsSubstring(s, key) {
					t.Errorf("expected JSON for %s to contain %s, got %s", tt.name, key, s)
				}
			}
		})
	}
}

// TestMemory_ContentReferencesEmbedding documents the intended contract that
// Memory.content carries an embedding identifier rather than inline text.
func TestMemory_ContentReferencesEmbedding(t *testing.T) {
	mem := Memory{
		MemoryID: "mem_1",
		Content:  "emb_123", // embedding identifier, not raw text
	}
	data, err := json.Marshal(mem)
	if err != nil {
		t.Fatalf("failed to marshal memory: %v", err)
	}
	if !containsSubstring(string(data), `"content":"emb_123"`) {
		t.Errorf("expected content to be serialized as embedding id, got %s", string(data))
	}
}

func containsSubstring(s, sub string) bool {
	return len(sub) == 0 || (len(sub) > 0 && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	// Small helper to avoid pulling in strings just for Contains;
	// if this becomes annoying it can be replaced with strings.Contains.
outer:
	for i := 0; i+len(sub) <= len(s); i++ {
		for j := 0; j < len(sub); j++ {
			if s[i+j] != sub[j] {
				continue outer
			}
		}
		return i
	}
	return -1
}

