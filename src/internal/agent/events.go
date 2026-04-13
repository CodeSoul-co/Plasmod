package agent

import (
	"context"
	"time"

	"plasmod/src/internal/schemas"
)

// ─── Event submission helpers ─────────────────────────────────────────────────────

// submitEvent builds a schemas.Event with all identity fields pre-filled,
// then POSTs it to the ingest endpoint. It auto-generates EventID and timestamps.
func (s *AgentSession) submitEvent(ctx context.Context, eventType schemas.EventType, payload map[string]any, causalRefs []string) (*IngestAck, error) {
	sessionID, err := s.getSessionID()
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	ev := schemas.Event{
		EventID:       "evt_" + s.agentID + "_" + now.Format("20060102T150405.000000"),
		TenantID:      s.tenantID,
		WorkspaceID:   s.workspaceID,
		AgentID:       s.agentID,
		SessionID:     sessionID,
		EventType:     string(eventType),
		EventTime:     now.Format(time.RFC3339Nano),
		IngestTime:    now.Format(time.RFC3339Nano),
		VisibleTime:   now.Format(time.RFC3339Nano),
		LogicalTS:     0, // assigned by CogDB WAL
		ParentEventID: "",
		CausalRefs:    causalRefs,
		Payload:       payload,
		Source:       "agent",
		Importance:   0.5,
		Visibility:   "private",
		Version:      1,
	}

	var ack IngestAck
	if err := s.doPost(ctx, s.ingestURL(), ev, &ack); err != nil {
		return nil, newError("submit_event", err, "event_type="+string(eventType))
	}
	return &ack, nil
}

// getSessionID returns the current session ID or ErrSessionNotStarted.
func (s *AgentSession) getSessionID() (string, error) {
	s.sessionMu.RLock()
	defer s.sessionMu.RUnlock()
	if s.sessionID == "" {
		return "", newError("require_session", ErrSessionNotStarted, "")
	}
	return s.sessionID, nil
}

// SubmitUserMessage submits a user message event.
func (s *AgentSession) SubmitUserMessage(ctx context.Context, text string, causalRefs []string) (*IngestAck, error) {
	return s.submitEvent(ctx, schemas.EventTypeUserMessage, map[string]any{"text": text}, causalRefs)
}

// SubmitAgentThought submits an agent reasoning/think event.
func (s *AgentSession) SubmitAgentThought(ctx context.Context, thought string) (*IngestAck, error) {
	return s.submitEvent(ctx, schemas.EventTypeAssistantMessage, map[string]any{"thought": thought}, nil)
}

// SubmitToolCall submits a tool-call event with the given tool name and arguments.
// args should be a JSON string representation of the tool's input.
func (s *AgentSession) SubmitToolCall(ctx context.Context, toolName, args string) (*IngestAck, error) {
	return s.submitEvent(ctx, schemas.EventTypeToolCall, map[string]any{
		"tool_name": toolName,
		"args":      args,
	}, nil)
}

// SubmitToolResult submits the result of a tool call.
// result should be a JSON string representation of the tool's output.
func (s *AgentSession) SubmitToolResult(ctx context.Context, toolName, result string) (*IngestAck, error) {
	return s.submitEvent(ctx, schemas.EventTypeToolResult, map[string]any{
		"tool_name": toolName,
		"result":    result,
	}, nil)
}

// SubmitCheckpoint submits a checkpoint event used for session heartbeats or
// explicit state snapshots. reason is a short label: "heartbeat", "pause",
// "goal_complete", etc.
func (s *AgentSession) SubmitCheckpoint(ctx context.Context, reason string) (*IngestAck, error) {
	return s.submitEvent(ctx, schemas.EventTypeCheckpoint, map[string]any{"reason": reason}, nil)
}

// SubmitReflection submits a reflection event that may trigger memory consolidation
// or summarization via the active MemoryManager algorithm.
func (s *AgentSession) SubmitReflection(ctx context.Context, content string) (*IngestAck, error) {
	return s.submitEvent(ctx, schemas.EventTypeReflection, map[string]any{"content": content}, nil)
}
