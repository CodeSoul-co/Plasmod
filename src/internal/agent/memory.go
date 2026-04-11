package agent

import (
	"context"

	"plasmod/src/internal/schemas"
)

// ─── Memory operations ──────────────────────────────────────────────────────────

// Query searches for memories relevant to the query text using the active
// MemoryManager's Recall algorithm and returns a policy-conditioned MemoryView.
//
//	scope — visibility boundary: "workspace" (default), "session", "agent"
//	topK  — maximum number of results to return (default 10)
func (s *AgentSession) Query(ctx context.Context, queryText, scope string, topK int) (*schemas.MemoryView, error) {
	if scope == "" {
		scope = "workspace"
	}
	if s.mm == nil {
		return nil, newError("query", ErrNotConnected, "no MemoryManager configured")
	}
	return s.mm.Recall(ctx, queryText, scope, topK)
}

// GetConversationHistory returns the most recent memories for the active session,
// ordered by descending timestamp. limit caps the result count (0 = no limit).
func (s *AgentSession) GetConversationHistory(ctx context.Context, limit int) ([]schemas.Memory, error) {
	sessionID, err := s.getSessionID()
	if err != nil {
		return nil, err
	}

	u := *s.baseURL
	u.Path = "/v1/memory"
	q := u.Query()
	q.Set("agent_id", s.agentID)
	q.Set("session_id", sessionID)
	u.RawQuery = q.Encode()

	var mems []schemas.Memory
	if err := s.doGet(ctx, &u, &mems); err != nil {
		return nil, newError("get_conversation_history", err, "")
	}

	// Apply client-side limit if specified.
	if limit > 0 && len(mems) > limit {
		mems = mems[len(mems)-limit:]
	}
	return mems, nil
}

// Compress triggers memory consolidation (level-0 → level-1) for the active
// session via the active MemoryManager's Compress algorithm.
func (s *AgentSession) Compress(ctx context.Context) error {
	sessionID, err := s.getSessionID()
	if err != nil {
		return err
	}
	if s.mm == nil {
		return newError("compress", ErrNotConnected, "no MemoryManager configured")
	}
	return s.mm.Compress(ctx, s.agentID, sessionID)
}

// Summarize triggers memory summarization for the active session via the active
// MemoryManager's Summarize algorithm. maxLevel is clamped to [1, 2].
// Returns the newly created summary Memory objects.
func (s *AgentSession) Summarize(ctx context.Context, maxLevel int) ([]schemas.Memory, error) {
	sessionID, err := s.getSessionID()
	if err != nil {
		return nil, err
	}
	if s.mm == nil {
		return nil, newError("summarize", ErrNotConnected, "no MemoryManager configured")
	}
	return s.mm.Summarize(ctx, s.agentID, sessionID, maxLevel)
}

// Decay applies forgetting decay to all memories of the active session via the
// active MemoryManager's Decay algorithm.
func (s *AgentSession) Decay(ctx context.Context) error {
	sessionID, err := s.getSessionID()
	if err != nil {
		return err
	}
	if s.mm == nil {
		return newError("decay", ErrNotConnected, "no MemoryManager configured")
	}
	return s.mm.Decay(ctx, s.agentID, sessionID)
}
