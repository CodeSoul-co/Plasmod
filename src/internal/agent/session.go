package agent

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"plasmod/src/internal/schemas"
)

// ─── Session lifecycle ─────────────────────────────────────────────────────────

// StartSession registers a new session with CogDB and returns its identifier.
// If a session is already active, StartSession returns ErrSessionAlreadyStarted.
//
//	goal         — natural-language description of the agent's objective
//	taskType     — classification: "investigation", "reasoning", "chat", etc.
//	budgetToken  — token budget for this session (0 = unlimited)
//	budgetTimeMS — time budget in milliseconds (0 = unlimited)
func (s *AgentSession) StartSession(ctx context.Context, goal, taskType string, budgetToken, budgetTimeMS int64) (string, error) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()

	if s.sessionID != "" {
		return "", newError("start_session", ErrSessionAlreadyStarted, "session_id="+s.sessionID)
	}
	if s.closed {
		return "", newError("start_session", ErrAlreadyClosed, "")
	}

	sessionID := "sess_" + s.agentID + "_" + time.Now().Format("20060102T150405")
	obj := schemas.Session{
		SessionID:    sessionID,
		AgentID:      s.agentID,
		ParentSessionID: "",
		TaskType:     taskType,
		Goal:         goal,
		ContextRef:   "",
		StartTS:      time.Now().UTC().Format(time.RFC3339),
		EndTS:        "",
		Status:       string(schemas.SessionStatusRunning),
		BudgetToken:  budgetToken,
		BudgetTimeMS: budgetTimeMS,
	}

	var result map[string]string
	if err := s.doPostUnlocked(ctx, s.sessionURL(), obj, &result); err != nil {
		return "", newError("start_session", err, "")
	}

	s.sessionID = sessionID
	return sessionID, nil
}

// EndSession marks the current session as completed (or failed) and clears it.
// Calling EndSession with no active session is a no-op that returns nil.
func (s *AgentSession) EndSession(ctx context.Context, finalStatus string) error {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()

	if s.sessionID == "" {
		return nil // no-op
	}
	s.sessionID = ""
	return s.endSessionLocked(ctx)
}

// PauseSession updates the current session status to "paused".
func (s *AgentSession) PauseSession(ctx context.Context) error {
	if err := s.requireSession(); err != nil {
		return err
	}
	return s.patchSessionStatus(ctx, schemas.SessionStatusPaused)
}

// ResumeSession reactivates a previously paused session. The sessionID must
// refer to a paused session in CogDB.
func (s *AgentSession) ResumeSession(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return newError("resume_session", ErrSessionNotFound, "sessionID is empty")
	}

	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()

	if err := s.patchSessionStatusUnlocked(ctx, schemas.SessionStatusRunning); err != nil {
		return newError("resume_session", err, "session_id="+sessionID)
	}
	s.sessionID = sessionID
	return nil
}

// GetSession retrieves the current session object from CogDB.
// Returns ErrSessionNotFound if no session is active.
func (s *AgentSession) GetSession(ctx context.Context) (*schemas.Session, error) {
	s.sessionMu.RLock()
	sessionID := s.sessionID
	s.sessionMu.RUnlock()

	if sessionID == "" {
		return nil, newError("get_session", ErrSessionNotFound, "")
	}

	u := s.sessionURL()
	q := u.Query()
	q.Set("agent_id", s.agentID)
	u.RawQuery = q.Encode()

	var sessions []schemas.Session
	if err := s.doGet(ctx, u, &sessions); err != nil {
		return nil, newError("get_session", err, "")
	}

	for i := range sessions {
		if sessions[i].SessionID == sessionID {
			return &sessions[i], nil
		}
	}
	return nil, newError("get_session", ErrSessionNotFound, "session_id="+sessionID)
}

// doPostUnlocked is like doPost but skips the requireConnected check.
// Caller must hold the lock.
func (s *AgentSession) doPostUnlocked(ctx context.Context, u *url.URL, body, dest any) error {
	var bodyReader any
	if body != nil {
		bodyReader = body
	}
	return httpDo(ctx, s.httpClient, http.MethodPost, u, bodyReader, dest)
}

// patchSessionStatus updates the current session's status field via PATCH.
// This is a best-effort operation; errors do not propagate if the session
// is no longer reachable.
func (s *AgentSession) patchSessionStatus(ctx context.Context, status schemas.SessionStatus) error {
	s.sessionMu.RLock()
	defer s.sessionMu.RUnlock()
	return s.patchSessionStatusUnlocked(ctx, status)
}

func (s *AgentSession) patchSessionStatusUnlocked(ctx context.Context, status schemas.SessionStatus) error {
	sess, err := s.getSessionLocked(ctx)
	if err != nil {
		return err
	}
	sess.Status = string(status)
	// PUT back to update
	var result map[string]string
	return httpDo(ctx, s.httpClient, http.MethodPut, s.sessionURL(), sess, &result)
}

// getSessionLocked retrieves the current session. Caller must hold sessionMu read lock.
func (s *AgentSession) getSessionLocked(ctx context.Context) (*schemas.Session, error) {
	u := s.sessionURL()
	q := u.Query()
	q.Set("agent_id", s.agentID)
	u.RawQuery = q.Encode()

	var sessions []schemas.Session
	if err := httpDo(ctx, s.httpClient, http.MethodGet, u, nil, &sessions); err != nil {
		return nil, err
	}
	for i := range sessions {
		if sessions[i].SessionID == s.sessionID {
			return &sessions[i], nil
		}
	}
	return nil, newError("get_session", ErrSessionNotFound, "session_id="+s.sessionID)
}
