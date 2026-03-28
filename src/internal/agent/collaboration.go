package agent

import (
	"context"

	"andb/src/internal/schemas"
)

// ─── Collaboration ─────────────────────────────────────────────────────────────

// ShareContract defines the access contract for a shared memory.
// Unused fields may be left empty (agent SDK applies sensible defaults).
type ShareContract struct {
	Scope            string `json:"scope"`
	ReadACL          string `json:"read_acl"`
	WriteACL         string `json:"write_acl"`
	DeriveACL        string `json:"derive_acl"`
	TTLPolicy        string `json:"ttl_policy"`
	ConsistencyLevel string `json:"consistency_level"`
	MergePolicy      string `json:"merge_policy"`
}

// shareResponse is the JSON shape returned by /v1/internal/memory/share.
type shareResponse struct {
	Status          string `json:"status"`
	SharedMemoryID  string `json:"shared_memory_id"`
	MemoryID        string `json:"memory_id"`
	ToAgentID       string `json:"to_agent_id"`
}

// ShareMemory copies a memory from the local agent to a target agent.
// The target agent can retrieve the shared copy by calling ReceiveSharedMemories.
// contract is optional; if nil a permissive default is used.
func (s *AgentSession) ShareMemory(ctx context.Context, memoryID, targetAgentID string, contract *ShareContract) (string, error) {
	if err := s.requireSession(); err != nil {
		return "", err
	}

	scope := "restricted_shared"
	if contract != nil && contract.Scope != "" {
		scope = contract.Scope
	}

	reqBody := struct {
		MemoryID       string `json:"memory_id"`
		FromAgentID    string `json:"from_agent_id"`
		ToAgentID      string `json:"to_agent_id"`
		ContractScope  string `json:"contract_scope"`
	}{
		MemoryID:      memoryID,
		FromAgentID:   s.agentID,
		ToAgentID:     targetAgentID,
		ContractScope: scope,
	}

	var resp shareResponse
	if err := s.doPost(ctx, s.internalMemoryURL("share"), reqBody, &resp); err != nil {
		return "", newError("share_memory", err, "memory_id="+memoryID+" target="+targetAgentID)
	}
	if resp.Status == "skipped" {
		return "", newError("share_memory", nil, "same_agent: no-op")
	}
	return resp.SharedMemoryID, nil
}

// ReceiveSharedMemories retrieves memories shared to this agent (OwnerType="shared")
// that are not yet consumed. It returns all matching memories from the store.
func (s *AgentSession) ReceiveSharedMemories(ctx context.Context) ([]schemas.Memory, error) {
	if err := s.requireSession(); err != nil {
		return nil, err
	}

	u := *s.baseURL
	u.Path = "/v1/memory"
	q := u.Query()
	q.Set("agent_id", s.agentID)
	u.RawQuery = q.Encode()

	var allMemories []schemas.Memory
	if err := s.doGet(ctx, &u, &allMemories); err != nil {
		return nil, newError("receive_shared_memories", err, "")
	}

	var shared []schemas.Memory
	for _, m := range allMemories {
		if m.OwnerType == "shared" {
			shared = append(shared, m)
		}
	}
	return shared, nil
}

// ResolveConflict resolves a conflict between two memory versions using last-writer-wins.
// leftID and rightID must be distinct Memory IDs. Returns the winner's MemoryID.
func (s *AgentSession) ResolveConflict(ctx context.Context, leftID, rightID string) (string, error) {
	if err := s.requireSession(); err != nil {
		return "", err
	}
	if leftID == rightID {
		return leftID, nil
	}

	u := *s.baseURL
	u.Path = "/v1/internal/memory/conflict/resolve"

	reqBody := struct {
		LeftID  string `json:"left_id"`
		RightID string `json:"right_id"`
	}{
		LeftID:  leftID,
		RightID: rightID,
	}

	var resp struct {
		Status    string `json:"status"`
		WinnerID  string `json:"winner_id"`
		LeftID    string `json:"left_id"`
		RightID   string `json:"right_id"`
	}
	if err := s.doPost(ctx, &u, reqBody, &resp); err != nil {
		return "", newError("resolve_conflict", err, "left="+leftID+" right="+rightID)
	}
	return resp.WinnerID, nil
}
