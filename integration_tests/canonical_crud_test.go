package integration_test

import (
	"fmt"
	"net/http"
	"testing"
)

func TestAgentsCRUD(t *testing.T) {
	id := fmt.Sprintf("agent_it_%s", uniqID())
	agent := map[string]any{
		"agent_id":              id,
		"tenant_id":             "t_demo",
		"workspace_id":          "w_demo",
		"agent_type":            "test",
		"role_profile":          "integration",
		"policy_ref":            "",
		"capability_set":        []string{"ingest", "query"},
		"default_memory_policy": "",
		"created_at":            "",
		"status":                "active",
	}

	t.Run("POST creates agent", func(t *testing.T) {
		status, result := doJSON(t, http.MethodPost, "/v1/agents", agent)
		if status != http.StatusOK {
			t.Fatalf("status: got %d, want 200", status)
		}
		assertKeys(t, result, "status", "agent_id")
		if result["agent_id"] != id {
			t.Errorf("agent_id: got %v, want %v", result["agent_id"], id)
		}
	})

	t.Run("GET lists agents and includes created agent", func(t *testing.T) {
		_, slice := doJSONSlice(t, http.MethodGet, "/v1/agents")
		found := false
		for _, item := range slice {
			if m, ok := item.(map[string]any); ok && m["agent_id"] == id {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("created agent %s not found in list", id)
		}
	})

	t.Run("returns 405 for DELETE", func(t *testing.T) {
		resp := doRaw(t, http.MethodDelete, "/v1/agents", "", nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("status: got %d, want 405", resp.StatusCode)
		}
	})
}

func TestSessionsCRUD(t *testing.T) {
	agentID := fmt.Sprintf("agent_sess_%s", uniqID())
	sessID := fmt.Sprintf("sess_it_%s", uniqID())

	doJSON(t, http.MethodPost, "/v1/agents", map[string]any{
		"agent_id": agentID, "tenant_id": "t_demo", "workspace_id": "w_demo",
		"agent_type": "test", "role_profile": "integration", "status": "active",
		"capability_set": []string{},
	})

	session := map[string]any{
		"session_id":        sessID,
		"agent_id":          agentID,
		"parent_session_id": "",
		"task_type":         "test",
		"goal":              "integration",
		"context_ref":       "",
		"start_ts":          "",
		"end_ts":            "",
		"status":            "active",
		"budget_token":      0,
		"budget_time_ms":    0,
	}

	t.Run("POST creates session", func(t *testing.T) {
		status, result := doJSON(t, http.MethodPost, "/v1/sessions", session)
		if status != http.StatusOK {
			t.Fatalf("status: got %d, want 200", status)
		}
		assertKeys(t, result, "status", "session_id")
	})

	t.Run("GET lists sessions filtered by agent_id", func(t *testing.T) {
		_, slice := doJSONSlice(t, http.MethodGet, fmt.Sprintf("/v1/sessions?agent_id=%s", agentID))
		found := false
		for _, item := range slice {
			if m, ok := item.(map[string]any); ok && m["session_id"] == sessID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("created session %s not found in list", sessID)
		}
	})
}

func TestMemoryCRUD(t *testing.T) {
	agentID := fmt.Sprintf("agent_mem_%s", uniqID())
	sessID := fmt.Sprintf("sess_mem_%s", uniqID())
	memID := fmt.Sprintf("mem_it_%s", uniqID())

	doJSON(t, http.MethodPost, "/v1/agents", map[string]any{
		"agent_id": agentID, "tenant_id": "t_demo", "workspace_id": "w_demo",
		"agent_type": "test", "status": "active", "capability_set": []string{},
	})
	doJSON(t, http.MethodPost, "/v1/sessions", map[string]any{
		"session_id": sessID, "agent_id": agentID, "status": "active",
	})

	memory := map[string]any{
		"memory_id":        memID,
		"memory_type":      "semantic",
		"agent_id":         agentID,
		"session_id":       sessID,
		"owner_type":       "agent",
		"scope":            "private",
		"level":            0,
		"content":          "integration memory content",
		"summary":          "integration memory summary",
		"source_event_ids": []string{},
		"confidence":       0.9,
		"importance":       0.5,
		"freshness_score":  1.0,
		"ttl":              0,
		"valid_from":       "",
		"valid_to":         "",
		"provenance_ref":   "",
		"version":          1,
		"is_active":        true,
	}

	t.Run("POST creates memory", func(t *testing.T) {
		status, result := doJSON(t, http.MethodPost, "/v1/memory", memory)
		if status != http.StatusOK {
			t.Fatalf("status: got %d, want 200", status)
		}
		assertKeys(t, result, "status", "memory_id")
	})

	t.Run("GET lists memories filtered by agent_id and session_id", func(t *testing.T) {
		path := fmt.Sprintf("/v1/memory?agent_id=%s&session_id=%s", agentID, sessID)
		_, slice := doJSONSlice(t, http.MethodGet, path)
		found := false
		for _, item := range slice {
			if m, ok := item.(map[string]any); ok && m["memory_id"] == memID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("created memory %s not found in list", memID)
		}
	})
}

func TestStatesCRUD(t *testing.T) {
	agentID := fmt.Sprintf("agent_st_%s", uniqID())
	sessID := fmt.Sprintf("sess_st_%s", uniqID())
	stateID := fmt.Sprintf("state_it_%s", uniqID())

	doJSON(t, http.MethodPost, "/v1/agents", map[string]any{
		"agent_id": agentID, "tenant_id": "t_demo", "workspace_id": "w_demo",
		"agent_type": "test", "status": "active", "capability_set": []string{},
	})
	doJSON(t, http.MethodPost, "/v1/sessions", map[string]any{
		"session_id": sessID, "agent_id": agentID, "status": "active",
	})

	state := map[string]any{
		"state_id":              stateID,
		"agent_id":              agentID,
		"session_id":            sessID,
		"state_type":            "kv",
		"state_key":             "integration_key",
		"state_value":           "integration_value",
		"derived_from_event_id": "",
		"checkpoint_ts":         "",
		"version":               1,
	}

	t.Run("POST creates state", func(t *testing.T) {
		status, result := doJSON(t, http.MethodPost, "/v1/states", state)
		if status != http.StatusOK {
			t.Fatalf("status: got %d, want 200", status)
		}
		assertKeys(t, result, "status", "state_id")
	})

	t.Run("GET lists states filtered by agent_id and session_id", func(t *testing.T) {
		path := fmt.Sprintf("/v1/states?agent_id=%s&session_id=%s", agentID, sessID)
		_, slice := doJSONSlice(t, http.MethodGet, path)
		found := false
		for _, item := range slice {
			if m, ok := item.(map[string]any); ok && m["state_id"] == stateID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("created state %s not found in list", stateID)
		}
	})
}

func TestArtifactsCRUD(t *testing.T) {
	sessID := fmt.Sprintf("sess_art_%s", uniqID())
	agentID := fmt.Sprintf("agent_art_%s", uniqID())
	artID := fmt.Sprintf("art_it_%s", uniqID())

	doJSON(t, http.MethodPost, "/v1/agents", map[string]any{
		"agent_id": agentID, "tenant_id": "t_demo", "workspace_id": "w_demo",
		"agent_type": "test", "status": "active", "capability_set": []string{},
	})
	doJSON(t, http.MethodPost, "/v1/sessions", map[string]any{
		"session_id": sessID, "agent_id": agentID, "status": "active",
	})

	artifact := map[string]any{
		"artifact_id":         artID,
		"session_id":          sessID,
		"owner_agent_id":      agentID,
		"artifact_type":       "text",
		"uri":                 "",
		"content_ref":         "",
		"mime_type":           "text/plain",
		"metadata":            map[string]any{"k": "v"},
		"hash":                "",
		"produced_by_event_id": "",
		"version":             1,
	}

	t.Run("POST creates artifact", func(t *testing.T) {
		status, result := doJSON(t, http.MethodPost, "/v1/artifacts", artifact)
		if status != http.StatusOK {
			t.Fatalf("status: got %d, want 200", status)
		}
		assertKeys(t, result, "status", "artifact_id")
	})

	t.Run("GET lists artifacts filtered by session_id", func(t *testing.T) {
		path := fmt.Sprintf("/v1/artifacts?session_id=%s", sessID)
		_, slice := doJSONSlice(t, http.MethodGet, path)
		found := false
		for _, item := range slice {
			if m, ok := item.(map[string]any); ok && m["artifact_id"] == artID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("created artifact %s not found in list", artID)
		}
	})
}

func TestEdgesCRUD(t *testing.T) {
	edgeID := fmt.Sprintf("edge_it_%s", uniqID())

	edge := map[string]any{
		"edge_id":        edgeID,
		"src_object_id":  "src_obj_test",
		"src_type":       "memory",
		"edge_type":      "refers_to",
		"dst_object_id":  "dst_obj_test",
		"dst_type":       "artifact",
		"weight":         1.0,
		"provenance_ref": "",
		"created_ts":     "",
	}

	t.Run("POST creates edge", func(t *testing.T) {
		status, result := doJSON(t, http.MethodPost, "/v1/edges", edge)
		if status != http.StatusOK {
			t.Fatalf("status: got %d, want 200", status)
		}
		assertKeys(t, result, "status", "edge_id")
	})

	t.Run("GET lists all edges", func(t *testing.T) {
		_, slice := doJSONSlice(t, http.MethodGet, "/v1/edges")
		found := false
		for _, item := range slice {
			if m, ok := item.(map[string]any); ok && m["edge_id"] == edgeID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("created edge %s not found in list", edgeID)
		}
	})
}

func TestPoliciesCRUD(t *testing.T) {
	polID := fmt.Sprintf("pol_it_%s", uniqID())
	objID := fmt.Sprintf("mem_pol_%s", uniqID())

	policy := map[string]any{
		"policy_id":           polID,
		"policy_version":      1,
		"context":             "integration",
		"object_id":           objID,
		"object_type":         "memory",
		"salience_weight":     1.0,
		"ttl":                 0,
		"decay_fn":            "",
		"confidence_override": 0.0,
		"verified_state":      "verified",
		"quarantine_flag":     false,
		"visibility_policy":   "private",
		"policy_reason":       "integration",
		"policy_source":       "integration_tests",
		"policy_event_id":     "",
	}

	t.Run("POST creates policy record", func(t *testing.T) {
		status, result := doJSON(t, http.MethodPost, "/v1/policies", policy)
		if status != http.StatusOK {
			t.Fatalf("status: got %d, want 200", status)
		}
		assertKeys(t, result, "status", "policy_id")
	})

	t.Run("GET by object_id returns created policy", func(t *testing.T) {
		path := fmt.Sprintf("/v1/policies?object_id=%s", objID)
		_, slice := doJSONSlice(t, http.MethodGet, path)
		found := false
		for _, item := range slice {
			if m, ok := item.(map[string]any); ok && m["policy_id"] == polID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("created policy %s not found for object %s", polID, objID)
		}
	})

	t.Run("GET all policies list is non-nil", func(t *testing.T) {
		status, _ := doJSONSlice(t, http.MethodGet, "/v1/policies")
		if status != http.StatusOK {
			t.Errorf("status: got %d, want 200", status)
		}
	})
}

func TestShareContractsCRUD(t *testing.T) {
	contractID := fmt.Sprintf("contract_it_%s", uniqID())
	scope := fmt.Sprintf("scope_it_%s", uniqID())

	contract := map[string]any{
		"contract_id":       contractID,
		"scope":             scope,
		"read_acl":          "*",
		"write_acl":         "*",
		"derive_acl":        "*",
		"ttl_policy":        "",
		"consistency_level": "",
		"merge_policy":      "",
		"quarantine_policy": "",
		"audit_policy":      "",
	}

	t.Run("POST creates share contract", func(t *testing.T) {
		status, result := doJSON(t, http.MethodPost, "/v1/share-contracts", contract)
		if status != http.StatusOK {
			t.Fatalf("status: got %d, want 200", status)
		}
		assertKeys(t, result, "status", "contract_id")
	})

	t.Run("GET by scope returns created contract", func(t *testing.T) {
		path := fmt.Sprintf("/v1/share-contracts?scope=%s", scope)
		_, slice := doJSONSlice(t, http.MethodGet, path)
		found := false
		for _, item := range slice {
			if m, ok := item.(map[string]any); ok && m["contract_id"] == contractID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("created contract %s not found for scope %s", contractID, scope)
		}
	})
}
