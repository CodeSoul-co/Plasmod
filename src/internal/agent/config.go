package agent

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// LLMConfig holds the configuration for the LLM inference backend used by the agent
// to generate responses. This is separate from the CogDB embedding provider.
type LLMConfig struct {
	// BaseURL is the OpenAI-compatible API endpoint.
	// Examples: https://api.openai.com/v1  |  http://localhost:11434/v1 (Ollama)
	// Environment: ANDB_AGENT_LLM_BASE_URL.
	BaseURL string

	// APIKey is the bearer token / secret key for the LLM provider.
	// Environment: ANDB_AGENT_LLM_API_KEY.
	APIKey string

	// Model is the model identifier, e.g. "gpt-4o", "qwen2.5:14b", "deepseek-chat".
	// Environment: ANDB_AGENT_LLM_MODEL.
	Model string

	// MaxTokens caps the number of completion tokens (0 = provider default).
	// Environment: ANDB_AGENT_LLM_MAX_TOKENS.
	MaxTokens int

	// Temperature controls response randomness: 0.0–2.0 (default 0.7).
	// Environment: ANDB_AGENT_LLM_TEMPERATURE.
	Temperature float64

	// Timeout is the per-request deadline (default 120s).
	// Environment: ANDB_AGENT_LLM_TIMEOUT.
	Timeout time.Duration
}

// MASConfig holds topology and peer information for a Multi-Agent System.
type MASConfig struct {
	// Peers is the list of sibling agent CogDB endpoints (each running their
	// own AgentGateway). Populated from comma-separated ANDB_MAS_PEERS.
	// Example: "http://agent-beta:9090,http://agent-gamma:9090"
	// Environment: ANDB_MAS_PEERS.
	Peers []string

	// Topology describes how agents interact: "mesh" | "hub" | "hierarchical".
	// Default: "mesh" (every agent can share with every peer).
	// Environment: ANDB_MAS_TOPOLOGY.
	Topology string

	// HubAgentID is the designated hub in a hub-and-spoke topology.
	// Ignored when Topology != "hub".
	// Environment: ANDB_MAS_HUB_AGENT_ID.
	HubAgentID string
}

// Config holds the configuration for an AgentSession or AgentGateway.
// Fields are populated from environment variables by LoadFromEnv, or set
// directly on the zero value.
type Config struct {
	// CogDBEndpoint is the base URL of the CogDB HTTP gateway.
	// Default: "http://127.0.0.1:8080".
	// Environment: ANDB_AGENT_ENDPOINT.
	CogDBEndpoint string

	// AgentID uniquely identifies the agent process. Required.
	// Environment: ANDB_AGENT_ID.
	AgentID string

	// SessionID is the current session identifier. Set by StartSession; not
	// read from environment.
	SessionID string

	// TenantID is the billing/organizational unit. Required.
	// Environment: ANDB_TENANT_ID.
	TenantID string

	// WorkspaceID is the agent's workspace within the tenant. Required.
	// Environment: ANDB_WORKSPACE_ID.
	WorkspaceID string

	// HTTPAddr is the listen address for AgentGateway (":9090" by default).
	// Environment: ANDB_AGENT_HTTP_PORT (value is port number, e.g. "9090",
	// not a full address).
	HTTPAddr string

	// HTTPClientTimeout is the timeout for CogDB HTTP requests (default 30s).
	// Environment: ANDB_AGENT_HTTP_TIMEOUT (seconds).
	HTTPClientTimeout time.Duration

	// LLM holds the inference backend configuration (API key, model, base URL).
	// Populated by LoadFromEnv; may also be set directly.
	LLM LLMConfig

	// MAS holds multi-agent system topology (peers, topology type).
	// Populated by LoadFromEnv; may also be set directly.
	MAS MASConfig
}

// LoadFromEnv populates Config from environment variables.
// Zero-value fields remain zero (caller may set them directly as overrides).
//
// Environment variables read:
//
//		ANDB_AGENT_ENDPOINT         CogDBEndpoint (default "http://127.0.0.1:8080")
//		ANDB_AGENT_ID               AgentID (required)
//		ANDB_TENANT_ID              TenantID (required)
//		ANDB_WORKSPACE_ID           WorkspaceID (required)
//		ANDB_AGENT_HTTP_PORT        HTTP listen port (default ":9090")
//		ANDB_AGENT_HTTP_TIMEOUT     CogDB request timeout in seconds (default 30)
//
//		ANDB_AGENT_LLM_BASE_URL     LLM API base URL (OpenAI-compatible)
//		ANDB_AGENT_LLM_API_KEY      LLM API key / bearer token
//		ANDB_AGENT_LLM_MODEL        LLM model identifier
//		ANDB_AGENT_LLM_MAX_TOKENS   Max completion tokens (default 0 = provider default)
//		ANDB_AGENT_LLM_TEMPERATURE  Sampling temperature 0.0–2.0 (default 0.7)
//		ANDB_AGENT_LLM_TIMEOUT      LLM request timeout in seconds (default 120)
//
//		ANDB_MAS_PEERS              Comma-separated peer agent gateway URLs
//		ANDB_MAS_TOPOLOGY           Topology: mesh|hub|hierarchical (default mesh)
//		ANDB_MAS_HUB_AGENT_ID       Hub agent ID (only for topology=hub)
func LoadFromEnv() Config {
	cfg := Config{
		CogDBEndpoint: getEnv("ANDB_AGENT_ENDPOINT", "http://127.0.0.1:8080"),
		AgentID:       os.Getenv("ANDB_AGENT_ID"),
		TenantID:      os.Getenv("ANDB_TENANT_ID"),
		WorkspaceID:   os.Getenv("ANDB_WORKSPACE_ID"),
		HTTPAddr:      ":" + getEnv("ANDB_AGENT_HTTP_PORT", "9090"),
	}
	if t := os.Getenv("ANDB_AGENT_HTTP_TIMEOUT"); t != "" {
		if sec, err := strconv.Atoi(t); err == nil && sec > 0 {
			cfg.HTTPClientTimeout = time.Duration(sec) * time.Second
		}
	}

	// ── LLM inference config ─────────────────────────────────────────────────
	cfg.LLM = LLMConfig{
		BaseURL:     os.Getenv("ANDB_AGENT_LLM_BASE_URL"),
		APIKey:      os.Getenv("ANDB_AGENT_LLM_API_KEY"),
		Model:       getEnv("ANDB_AGENT_LLM_MODEL", ""),
		Temperature: 0.7,
		Timeout:     120 * time.Second,
	}
	if v := os.Getenv("ANDB_AGENT_LLM_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.LLM.MaxTokens = n
		}
	}
	if v := os.Getenv("ANDB_AGENT_LLM_TEMPERATURE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.LLM.Temperature = f
		}
	}
	if v := os.Getenv("ANDB_AGENT_LLM_TIMEOUT"); v != "" {
		if sec, err := strconv.Atoi(v); err == nil && sec > 0 {
			cfg.LLM.Timeout = time.Duration(sec) * time.Second
		}
	}

	// ── MAS topology config ──────────────────────────────────────────────────
	cfg.MAS = MASConfig{
		Topology:   getEnv("ANDB_MAS_TOPOLOGY", "mesh"),
		HubAgentID: os.Getenv("ANDB_MAS_HUB_AGENT_ID"),
	}
	if peers := os.Getenv("ANDB_MAS_PEERS"); peers != "" {
		cfg.MAS.Peers = splitTrimmed(peers, ",")
	}

	return cfg
}

// Validate checks that required fields are non-empty. Returns the first validation
// error, or nil if the config is valid for establishing an AgentSession.
func (c Config) Validate() error {
	if c.CogDBEndpoint == "" {
		return fmt.Errorf("CogDBEndpoint is required")
	}
	if _, err := url.ParseRequestURI(c.CogDBEndpoint); err != nil {
		return fmt.Errorf("CogDBEndpoint is not a valid URL: %w", err)
	}
	if c.AgentID == "" {
		return fmt.Errorf("AgentID is required (set ANDB_AGENT_ID)")
	}
	if c.TenantID == "" {
		return fmt.Errorf("TenantID is required (set ANDB_TENANT_ID)")
	}
	if c.WorkspaceID == "" {
		return fmt.Errorf("WorkspaceID is required (set ANDB_WORKSPACE_ID)")
	}
	return nil
}

// getEnv is a convenience helper mirroring os.LookupEnv without the bool return.
func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

// splitTrimmed splits s by sep and trims whitespace from each element,
// omitting empty strings.
func splitTrimmed(s, sep string) []string {
	parts := strings.Split(s, sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
