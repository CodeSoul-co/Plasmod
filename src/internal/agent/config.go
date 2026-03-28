package agent

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"
)

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

	// HTTPClient is an optional custom http.Client. When nil a default client
	// with a 30-second request timeout is used. Useful for custom transport,
	// proxy, or testing scenarios.
	HTTPClientTimeout time.Duration
}

// LoadFromEnv populates Config from environment variables.
// Zero-value fields remain zero (caller may set them directly as overrides).
//
// Environment variables read:
//
//	ANDB_AGENT_ENDPOINT    CogDBEndpoint (default "http://127.0.0.1:8080")
//	ANDB_AGENT_ID          AgentID (required)
//	ANDB_TENANT_ID         TenantID (required)
//	ANDB_WORKSPACE_ID      WorkspaceID (required)
//	ANDB_AGENT_HTTP_PORT   HTTP listen port (default ":9090")
//	ANDB_AGENT_HTTP_TIMEOUT request timeout in seconds (default 30)
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
