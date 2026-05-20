package app

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Plasmod standard listen and object-store ports.
//
//   - Dev (unified): PortDevUnified — single HTTP listener for admin + SDK routes.
//   - Split deploy: PortMgmt (health/metrics/admin), PortAPI (SDK REST + internal rpc).
//   - Object store: PortObjectStore / PortObjectStoreConsole on the host map into MinIO.
//
// Split numeric values use a fixed PortBaselineOffset (+10) from common industry defaults
// so operators can reuse familiar port planning; gRPC is not exposed yet.
const (
	PortBaselineOffset = 10

	PortDevUnified = 8080

	PortMgmt              = 9101
	PortAPI               = 19540
	PortObjectStore       = 9010
	PortObjectStoreConsole = 9011
)

const (
	ListenModeUnified = "unified"
	ListenModeSplit   = "split"
)

// ListenConfig describes which addresses the HTTP server(s) bind to.
type ListenConfig struct {
	Mode        string
	UnifiedAddr string
	MgmtAddr    string
	APIAddr     string
}

func defaultMgmtAddr() string {
	return fmt.Sprintf("0.0.0.0:%d", PortMgmt)
}

func defaultAPIAddr() string {
	return fmt.Sprintf("0.0.0.0:%d", PortAPI)
}

func defaultUnifiedAddr() string {
	return fmt.Sprintf("127.0.0.1:%d", PortDevUnified)
}

// ResolveListenConfig reads PLASMOD_LISTEN_MODE and address overrides.
//   - unified (default): single listener on PLASMOD_HTTP_ADDR or 127.0.0.1:8080
//   - split: mgmt on PLASMOD_MGMT_ADDR (9101), API/SDK on PLASMOD_API_ADDR (19540)
func ResolveListenConfig() ListenConfig {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("PLASMOD_LISTEN_MODE")))
	if mode == "" {
		mode = ListenModeUnified
	}
	cfg := ListenConfig{Mode: mode}
	switch mode {
	case ListenModeSplit:
		cfg.MgmtAddr = strings.TrimSpace(os.Getenv("PLASMOD_MGMT_ADDR"))
		if cfg.MgmtAddr == "" {
			cfg.MgmtAddr = defaultMgmtAddr()
		}
		cfg.APIAddr = strings.TrimSpace(os.Getenv("PLASMOD_API_ADDR"))
		if cfg.APIAddr == "" {
			cfg.APIAddr = defaultAPIAddr()
		}
	case ListenModeUnified:
		cfg.UnifiedAddr = strings.TrimSpace(os.Getenv("PLASMOD_HTTP_ADDR"))
		if cfg.UnifiedAddr == "" {
			cfg.UnifiedAddr = defaultUnifiedAddr()
		}
	default:
		cfg.Mode = ListenModeUnified
		cfg.UnifiedAddr = defaultUnifiedAddr()
	}
	return cfg
}

// FormatListenAddrs returns a log-friendly summary of bind addresses.
func (c ListenConfig) FormatListenAddrs() string {
	if c.Mode == ListenModeSplit {
		return fmt.Sprintf("mgmt=%s api=%s", c.MgmtAddr, c.APIAddr)
	}
	return fmt.Sprintf("unified=%s", c.UnifiedAddr)
}

// HostPortPair formats host:port for URLs and SDK helpers.
func HostPortPair(host string, port int) string {
	if strings.Contains(host, ":") {
		return fmt.Sprintf("[%s]:%d", host, port)
	}
	return fmt.Sprintf("%s:%d", host, port)
}

// ParsePort extracts the port number from a listen address like "0.0.0.0:9101".
func ParsePort(addr string) (int, error) {
	_, portStr, err := splitHostPort(addr)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(portStr)
}

func splitHostPort(addr string) (host, port string, err error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", "", fmt.Errorf("empty address")
	}
	if strings.HasPrefix(addr, "[") {
		end := strings.Index(addr, "]")
		if end < 0 {
			return "", "", fmt.Errorf("invalid address %q", addr)
		}
		host = addr[1:end]
		rest := strings.TrimPrefix(addr[end+1:], ":")
		if rest == "" {
			return "", "", fmt.Errorf("missing port in %q", addr)
		}
		return host, rest, nil
	}
	i := strings.LastIndex(addr, ":")
	if i < 0 {
		return "", "", fmt.Errorf("missing port in %q", addr)
	}
	return addr[:i], addr[i+1:], nil
}
