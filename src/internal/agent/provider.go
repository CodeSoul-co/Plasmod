package agent

// ─── SDK Provider Interfaces ──────────────────────────────────────────────────
//
// This file defines the interface contracts that the CogDB agent SDK must
// implement. The concrete implementations live in the SDK layer (outside this
// package); here we only declare the shapes so that:
//
//   1. AgentSession.WithLLM / AgentSession.WithMASPeers can be wired up
//      without importing any HTTP or model library into the core agent package.
//   2. Tests can supply lightweight stubs that satisfy the interfaces.
//   3. Future SDK developers have a clear, stable contract to target.
//
// Environment variables consumed by NewLLMProviderFromEnv / NewMASProviderFromEnv
// are documented here (and in .env.example) so the SDK knows which vars to read.
//
//   PLASMOD_AGENT_LLM_BASE_URL     OpenAI-compatible API base URL
//   PLASMOD_AGENT_LLM_API_KEY      Bearer token / API key
//   PLASMOD_AGENT_LLM_MODEL        Model identifier (gpt-4o, qwen-max, …)
//   PLASMOD_AGENT_LLM_MAX_TOKENS   Max completion tokens (0 = provider default)
//   PLASMOD_AGENT_LLM_TEMPERATURE  Sampling temperature 0.0–2.0 (default 0.7)
//   PLASMOD_AGENT_LLM_TIMEOUT      Per-request timeout seconds (default 120)
//
//   PLASMOD_MAS_PEERS              Comma-separated sibling AgentGateway URLs
//   PLASMOD_MAS_TOPOLOGY           mesh | hub | hierarchical (default mesh)
//   PLASMOD_MAS_HUB_AGENT_ID       Hub agent ID (topology=hub only)

import "context"

// ─── LLM ─────────────────────────────────────────────────────────────────────

// LLMMessage is a single entry in a chat-completion context.
type LLMMessage struct {
	Role    string // "system" | "user" | "assistant"
	Content string
}

// LLMOptions controls per-call inference parameters.
// Zero values fall back to the provider's configured defaults.
type LLMOptions struct {
	Model       string  // override the default model for this call
	MaxTokens   int     // 0 = use provider default
	Temperature float64 // 0.0 means "use provider default"
	Stream      bool    // not yet used; reserved for future streaming support
}

// LLMProvider is the interface the SDK must implement to give an AgentSession
// access to an LLM inference backend.
//
// The interface is intentionally minimal: one method for generation and one
// for introspection. The SDK may embed additional methods in its concrete type.
type LLMProvider interface {
	// Complete sends the conversation history to the LLM and returns the
	// assistant's reply as a plain string.
	Complete(ctx context.Context, messages []LLMMessage, opts LLMOptions) (string, error)

	// Provider returns a short identifier for the backend, e.g. "openai",
	// "ollama", "deepseek". Used for logging and provenance tagging.
	Provider() string
}

// WithLLM attaches an LLMProvider to the session so that future SDK helpers
// (e.g. ReflectOnMemory, SummarizeSession) can call the LLM directly.
// Pass nil to detach the current provider.
func (s *AgentSession) WithLLM(p LLMProvider) *AgentSession {
	s.llm = p
	return s
}

// LLM returns the currently attached LLMProvider, or nil if none is set.
func (s *AgentSession) LLM() LLMProvider { return s.llm }

// ─── MAS ─────────────────────────────────────────────────────────────────────

// MASTopology describes how agents in the system relate to each other.
type MASTopology string

const (
	TopologyMesh         MASTopology = "mesh"         // every agent ↔ every peer
	TopologyHub          MASTopology = "hub"           // all route through one hub
	TopologyHierarchical MASTopology = "hierarchical"  // tree-shaped delegation
)

// MASPeer represents a single sibling agent reachable via its AgentGateway.
type MASPeer struct {
	AgentID  string // the peer's PLASMOD_AGENT_ID
	Endpoint string // the peer's AgentGateway base URL, e.g. "http://agent-beta:9090"
}

// MASProvider is the interface the SDK must implement to give an AgentSession
// awareness of other agents in the system.
type MASProvider interface {
	// Peers returns the current list of reachable sibling agents.
	Peers() []MASPeer

	// Topology returns the configured interaction topology.
	Topology() MASTopology
}

// WithMAS attaches a MASProvider to the session.
// Pass nil to detach.
func (s *AgentSession) WithMAS(p MASProvider) *AgentSession {
	s.mas = p
	return s
}

// MAS returns the currently attached MASProvider, or nil if none is set.
func (s *AgentSession) MAS() MASProvider { return s.mas }
