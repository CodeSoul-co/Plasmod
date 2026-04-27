// Package agent provides the agent-facing SDK for CogDB.
//
// Two deployment modes:
//
//  1. Go module: import "plasmod/internal/agent", create an AgentSession
//     directly, and call methods on it (AgentSession proxies to CogDB via HTTP).
//
//  2. HTTP server: use AgentGateway to expose a REST API; agents connect via
//     HTTP.  Both modes share the same core logic.
//
// # Agent identity
//
// Every agent must identify itself before the session begins.  The SDK requires
// three immutable identifiers that never change for the lifetime of the agent
// process: AgentID, TenantID, and WorkspaceID.  These are supplied at
// construction and are attached to every request sent to CogDB.
//
// SessionID is a mutable identifier that changes each time the agent starts a
// new conversation or task.  Call StartSession to obtain one.
//
// # Chain integration
//
// The SDK integrates all four CogDB execution chains:
//
//	MainChain            SubmitUserMessage/Thought/ToolCall/etc. → Ingest → Materialize → Index
//	MemoryPipelineChain  SubmitReflection → MemoryManager.Compress/Summarize
//	QueryChain           Query() → Search → ProofTrace → MemoryView
//	CollaborationChain   ShareMemory/ResolveConflict → CommunicationWorker
package agent

import (
	"errors"
	"fmt"
)

// ─── Sentinel errors ─────────────────────────────────────────────────────────

// ErrNotConnected is returned when an operation requires a CogDB endpoint
// but none was configured.
var ErrNotConnected = errors.New("agent: CogDB endpoint not configured")

// ErrSessionNotStarted is returned when an operation requires an active session
// but none has been started via StartSession.
var ErrSessionNotStarted = errors.New("agent: session not started; call StartSession first")

// ErrSessionAlreadyStarted is returned when StartSession is called while a
// session is already active.
var ErrSessionAlreadyStarted = errors.New("agent: session already started; call EndSession first")

// ErrCogDBUnavailable is returned when CogDB returns a non-2xx response or the
// connection fails.
var ErrCogDBUnavailable = errors.New("agent: CogDB unavailable")

// ErrInvalidResponse is returned when CogDB returns a response that cannot be
// decoded as the expected type.
var ErrInvalidResponse = errors.New("agent: invalid response from CogDB")

// ErrSessionNotFound is returned when GetSession finds no active session.
var ErrSessionNotFound = errors.New("agent: session not found")

// ErrAlreadyClosed is returned when Close is called on a session that was
// already closed.
var ErrAlreadyClosed = errors.New("agent: session already closed")

// ─── SDKError wrapper ─────────────────────────────────────────────────────────

// SDKError wraps a lower-level error with operation context and optional detail.
// It preserves the underlying error so errors.Is / errors.As still work.
type SDKError struct {
	Op     string // operation name, e.g. "StartSession", "SubmitUserMessage"
	Err    error  // underlying error
	Detail string // additional context, e.g. "session_id=sess_xyz not found"
}

// Error implements error.
func (e *SDKError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("agent.%s: %v — %s", e.Op, e.Err, e.Detail)
	}
	if e.Err != nil {
		return fmt.Sprintf("agent.%s: %v", e.Op, e.Err)
	}
	return "agent." + e.Op
}

// Unwrap returns the wrapped error so errors.Is / errors.As work.
func (e *SDKError) Unwrap() error { return e.Err }

// newError is the constructor for SDKError.
//
//	 op     — operation name (caller's method name, lowercase)
//	 err    — underlying error; use sentinel above or standard library errors
//	 detail — optional human-readable context
func newError(op string, err error, detail string) error {
	if err == nil {
		return nil
	}
	return &SDKError{Op: op, Err: err, Detail: detail}
}

// wrapError is like newError but wraps a string message instead of an error.
// Use this when the underlying cause is a string rather than a typed error.
func wrapError(op, msg string) error {
	return &SDKError{Op: op, Err: errors.New(msg)}
}
