package agent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// ─── IngestAck ────────────────────────────────────────────────────────────────

// IngestAck is the acknowledgment returned by event submission methods.
// It mirrors the structure returned by Runtime.SubmitIngest.
type IngestAck struct {
	Status   string `json:"status"`    // "accepted"
	LSN      int64  `json:"lsn"`      // WAL log sequence number
	EventID  string `json:"event_id"` // echoed from the submitted event
	MemoryID string `json:"memory_id"` // derived Memory ID (empty for non-memory events)
	EdgeCount int    `json:"edge_count"`
}

// ─── AgentSession ─────────────────────────────────────────────────────────────

// AgentSession is the agent-facing handle for interacting with CogDB.
// It is safe for concurrent use by a single agent process.
//
// Agents must be constructed via NewAgentSession. After construction, call
// StartSession before submitting events. Call Close when done.
//
// Example:
//
//	cfg := agent.LoadFromEnv()
//	sess, err := agent.NewAgentSession(cfg.AgentID, cfg.TenantID, cfg.WorkspaceID, cfg)
//	if err != nil { ... }
//	ctx := context.Background()
//	sessionID, err := sess.StartSession(ctx, "debug a memory corruption", "investigation", 0, 0)
//	ack, err := sess.SubmitUserMessage(ctx, "memory at 0x7f is corrupt", nil)
//	// ...
//	sess.Close(ctx)
type AgentSession struct {
	// Immutable identity fields — set at construction, never change.
	agentID     string
	tenantID    string
	workspaceID string

	// Mutable session state. Guarded by sessionMu; sessionID may be updated
	// by StartSession / EndSession / ResumeSession.
	sessionMu sync.RWMutex
	sessionID string
	closed    bool

	// HTTP client and CogDB URL.
	httpClient *http.Client
	baseURL   *url.URL

	// MemoryManager is pluggable. Defaults to BaselineMemoryManager on construction.
	// Set via WithMemoryManager for custom algorithms.
	mm MemoryManager
}

// NewAgentSession creates a new session for the given agent identity.
// agentID, tenantID, and workspaceID must be non-empty.
//
// The returned session has no active CogDB session. Call StartSession before
// submitting events.
//
//	cfg.CogDBEndpoint is used as the CogDB gateway URL.
//	If cfg.HTTPClientTimeout > 0 it is used as the request timeout;
//	otherwise a default 30-second client is created.
func NewAgentSession(agentID, tenantID, workspaceID string, cfg Config) (*AgentSession, error) {
	if agentID == "" {
		return nil, newError("new_agentsession", fmt.Errorf("agentID is required"), "")
	}
	if tenantID == "" {
		return nil, newError("new_agentsession", fmt.Errorf("tenantID is required"), "")
	}
	if workspaceID == "" {
		return nil, newError("new_agentsession", fmt.Errorf("workspaceID is required"), "")
	}

	baseURL, err := url.Parse(cfg.CogDBEndpoint)
	if err != nil {
		return nil, newError("new_agentsession", err, "invalid CogDBEndpoint")
	}

	var client *http.Client
	if cfg.HTTPClientTimeout > 0 {
		client = &http.Client{Timeout: cfg.HTTPClientTimeout}
	} else {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	s := &AgentSession{
		agentID:     agentID,
		tenantID:    tenantID,
		workspaceID: workspaceID,
		httpClient:  client,
		baseURL:    baseURL,
	}

	// Default to BaselineMemoryManager when a CogDB endpoint is configured.
	if cfg.CogDBEndpoint != "" {
		s.mm = NewBaselineMemoryManager(client, cfg.CogDBEndpoint, agentID, tenantID, workspaceID)
	}

	return s, nil
}

// AgentID returns the immutable agent identifier supplied at construction.
func (s *AgentSession) AgentID() string { return s.agentID }

// TenantID returns the immutable tenant identifier supplied at construction.
func (s *AgentSession) TenantID() string { return s.tenantID }

// WorkspaceID returns the immutable workspace identifier supplied at construction.
func (s *AgentSession) WorkspaceID() string { return s.workspaceID }

// SessionID returns the current CogDB session identifier, or "" if no session
// is active.
func (s *AgentSession) SessionID() string {
	s.sessionMu.RLock()
	defer s.sessionMu.RUnlock()
	return s.sessionID
}

// IsConnected reports whether a CogDB endpoint is configured and the session
// is not closed.
func (s *AgentSession) IsConnected() bool {
	s.sessionMu.RLock()
	defer s.sessionMu.RUnlock()
	return s.baseURL.Host != "" && !s.closed
}

// Close ends the active CogDB session and releases resources.
// It is idempotent: subsequent calls after the first are no-ops that return nil.
func (s *AgentSession) Close(ctx context.Context) error {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	if s.sessionID != "" {
		_ = s.endSessionLocked(ctx)
	}
	s.sessionID = ""

	if s.mm != nil {
		if c, ok := s.mm.(io.Closer); ok {
			_ = c.Close()
		}
	}
	return nil
}

// Heartbeat submits a checkpoint event with reason "heartbeat". It is used by
// long-running agents to indicate aliveness without a meaningful session state
// change.
func (s *AgentSession) Heartbeat(ctx context.Context) (*IngestAck, error) {
	if err := s.requireSession(); err != nil {
		return nil, err
	}
	return s.SubmitCheckpoint(ctx, "heartbeat")
}

// MemoryManager returns the currently configured MemoryManager.
func (s *AgentSession) MemoryManager() MemoryManager {
	return s.mm
}

// WithMemoryManager returns a copy of s with mm set to the provided MemoryManager.
// Use this to inject a custom algorithm (e.g. MemoryBank, VectorGraph).
func (s *AgentSession) WithMemoryManager(mm MemoryManager) *AgentSession {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	cp := *s
	cp.mm = mm
	return &cp
}

// ─── internal helpers ─────────────────────────────────────────────────────────

// requireSession returns ErrSessionNotStarted if no session is active.
func (s *AgentSession) requireSession() error {
	s.sessionMu.RLock()
	defer s.sessionMu.RUnlock()
	if s.sessionID == "" {
		return newError("require_session", ErrSessionNotStarted, "")
	}
	return nil
}

// requireConnected returns ErrNotConnected if no CogDB endpoint is configured.
func (s *AgentSession) requireConnected() error {
	s.sessionMu.RLock()
	defer s.sessionMu.RUnlock()
	if s.baseURL.Host == "" {
		return newError("require_connected", ErrNotConnected, "")
	}
	return nil
}

// endSessionLocked calls the session close endpoint. Caller must hold sessionMu.
func (s *AgentSession) endSessionLocked(ctx context.Context) error {
	u := s.sessionURL()
	q := u.Query()
	q.Set("agent_id", s.agentID)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u.String(), nil)
	if err != nil {
		return err
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return newError("end_session", ErrCogDBUnavailable, err.Error())
	}
	defer resp.Body.Close()
	return nil
}

// ingestURL returns the absolute URL for the ingest endpoint.
func (s *AgentSession) ingestURL() *url.URL {
	u := *s.baseURL
	u.Path = "/v1/ingest/events"
	return &u
}

// queryURL returns the absolute URL for the query endpoint.
func (s *AgentSession) queryURL() *url.URL {
	u := *s.baseURL
	u.Path = "/v1/query"
	return &u
}

// sessionURL returns the base URL for session CRUD.
func (s *AgentSession) sessionURL() *url.URL {
	u := *s.baseURL
	u.Path = "/v1/sessions"
	return &u
}

// internalMemoryURL returns the absolute URL for an internal memory action.
func (s *AgentSession) internalMemoryURL(action string) *url.URL {
	u := *s.baseURL
	u.Path = "/v1/internal/memory/" + action
	return &u
}

// doPost sends a POST request to the given URL with body, decoding the JSON
// response into dest. dest may be nil.
func (s *AgentSession) doPost(ctx context.Context, u *url.URL, body, dest any) error {
	if err := s.requireConnected(); err != nil {
		return err
	}
	return httpDo(ctx, s.httpClient, http.MethodPost, u, body, dest)
}

// doGet sends a GET request to the given URL, decoding the JSON response
// into dest. dest may be nil.
func (s *AgentSession) doGet(ctx context.Context, u *url.URL, dest any) error {
	if err := s.requireConnected(); err != nil {
		return err
	}
	return httpDo(ctx, s.httpClient, http.MethodGet, u, nil, dest)
}
