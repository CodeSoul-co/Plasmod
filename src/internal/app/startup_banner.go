package app

import (
	"fmt"
	"log"
	"os"
	"runtime/debug"
	"strings"
)

const startupBannerWidth = 72

// PrintStartupBanner prints a welcome panel with logo, ports, and runtime hints.
// Shown by default inside Docker; override with PLASMOD_SHOW_BANNER=0/1.
func PrintStartupBanner(cfg ListenConfig) {
	if !shouldShowStartupBanner() {
		return
	}
	for _, line := range buildStartupBannerLines(cfg) {
		log.Print(line)
	}
}

func shouldShowStartupBanner() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("PLASMOD_SHOW_BANNER"))) {
	case "0", "false", "no", "off":
		return false
	case "1", "true", "yes", "on":
		return true
	default:
		return isRunningInDocker()
	}
}

func isRunningInDocker() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	data, err := os.ReadFile("/proc/1/cgroup")
	if err != nil {
		return false
	}
	s := string(data)
	return strings.Contains(s, "docker") || strings.Contains(s, "containerd")
}

func buildStartupBannerLines(cfg ListenConfig) []string {
	host := strings.TrimSpace(os.Getenv("PLASMOD_PUBLIC_HOST"))
	if host == "" {
		host = "127.0.0.1"
	}

	lines := []string{
		"",
		bannerRule("═"),
		"",
	}
	for _, line := range plasmodASCIILogo() {
		lines = append(lines, centerBannerLine(line))
	}
	lines = append(lines,
		centerBannerLine("Agent-Native Database for AI Systems"),
		"",
		bannerRule("─"),
		"  Service ready",
		fmt.Sprintf("  Version      : %s", serverVersion()),
		fmt.Sprintf("  Listen mode  : %s", cfg.Mode),
	)

	switch cfg.Mode {
	case ListenModeSplit:
		mgmtPort, _ := ParsePort(cfg.MgmtAddr)
		apiPort, _ := ParsePort(cfg.APIAddr)
		mgmtURL := fmt.Sprintf("http://%s", HostPortPair(host, mgmtPort))
		apiURL := fmt.Sprintf("http://%s", HostPortPair(host, apiPort))
		lines = append(lines,
			fmt.Sprintf("  Mgmt / health: %s  (port %d)", mgmtURL, mgmtPort),
			fmt.Sprintf("  SDK REST API : %s  (port %d)", apiURL, apiPort),
			"",
			"  Quick checks",
			fmt.Sprintf("    curl %s/healthz", mgmtURL),
			fmt.Sprintf("    curl %s/v1/query -H \"Content-Type: application/json\" -d '{\"query_text\":\"hello\"}'", apiURL),
		)
	default:
		unifiedPort, _ := ParsePort(cfg.UnifiedAddr)
		baseURL := fmt.Sprintf("http://%s", HostPortPair(host, unifiedPort))
		lines = append(lines,
			fmt.Sprintf("  HTTP (unified): %s  (port %d)", baseURL, unifiedPort),
			"",
			"  Quick checks",
			fmt.Sprintf("    curl %s/healthz", baseURL),
			fmt.Sprintf("    curl %s/v1/query -H \"Content-Type: application/json\" -d '{\"query_text\":\"hello\"}'", baseURL),
		)
	}

	lines = append(lines,
		"",
		bannerRule("─"),
		"  Runtime",
		fmt.Sprintf("  APP_MODE         : %s", envOrDefault("APP_MODE", "dev")),
		fmt.Sprintf("  PLASMOD_STORAGE  : %s", envOrDefault("PLASMOD_STORAGE", "memory")),
		fmt.Sprintf("  PLASMOD_DATA_DIR : %s", envOrDefault("PLASMOD_DATA_DIR", ".plasmod_data")),
		fmt.Sprintf("  PLASMOD_EMBEDDER : %s", envOrDefault("PLASMOD_EMBEDDER", "tfidf")),
		fmt.Sprintf("  Cold store (S3)  : %s", s3Summary()),
		"",
		bannerRule("─"),
		"  Security",
		adminKeyLine(),
		"  Admin header     : X-Admin-Key: <key>  or  Authorization: Bearer <key>",
		"",
		bannerRule("─"),
		"  Python SDK (pyplasmod)",
		pythonSDKLine(cfg, host),
		"",
		"  Docs    : https://github.com/CodeSoul-co/Plasmod",
		"  Issues  : https://github.com/CodeSoul-co/Plasmod/issues",
		"",
		bannerRule("═"),
		"",
	)
	return lines
}

func plasmodASCIILogo() []string {
	return []string{
		" ____  _            _                 _           ",
		"|  _ \\| | __ _  ___| | __ _ ___  ___ | |__   ___ ",
		"| |_) | |/ _` |/ __| |/ _` / __|/ _ \\| '_ \\ / _ \\",
		"|  __/| | (_| | (__| | (_| \\__ \\ (_) | | | |  __/",
		"|_|   |_|\\__,_|\\___|_|\\__,_|___/\\___/|_| |_|\\___|",
	}
}

func bannerRule(ch string) string {
	return strings.Repeat(ch, startupBannerWidth)
}

func centerBannerLine(s string) string {
	s = strings.TrimRight(s, " ")
	if len(s) >= startupBannerWidth {
		return s
	}
	pad := (startupBannerWidth - len(s)) / 2
	return strings.Repeat(" ", pad) + s
}

func serverVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	version := strings.TrimSpace(info.Main.Version)
	if version == "" || version == "(devel)" {
		return "dev"
	}
	return version
}

func envOrDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func s3Summary() string {
	endpoint := strings.TrimSpace(os.Getenv("S3_ENDPOINT"))
	bucket := strings.TrimSpace(os.Getenv("S3_BUCKET"))
	if endpoint == "" && bucket == "" {
		return "not configured (in-memory cold tier)"
	}
	if bucket == "" {
		return endpoint
	}
	if endpoint == "" {
		return "bucket=" + bucket
	}
	return fmt.Sprintf("%s  bucket=%s", endpoint, bucket)
}

func adminKeyLine() string {
	key := strings.TrimSpace(os.Getenv("PLASMOD_ADMIN_API_KEY"))
	if key == "" {
		key = strings.TrimSpace(os.Getenv("ANDB_ADMIN_API_KEY"))
	}
	if key == "" {
		return "  PLASMOD_ADMIN_API_KEY : (not set — /v1/admin/* is UNPROTECTED)"
	}
	return fmt.Sprintf("  PLASMOD_ADMIN_API_KEY : %s", maskSecret(key))
}

func maskSecret(s string) string {
	if len(s) <= 4 {
		return "****"
	}
	return s[:4] + strings.Repeat("*", len(s)-4)
}

func pythonSDKLine(cfg ListenConfig, host string) string {
	var baseURL string
	switch cfg.Mode {
	case ListenModeSplit:
		apiPort, err := ParsePort(cfg.APIAddr)
		if err != nil {
			apiPort = PortAPI
		}
		baseURL = fmt.Sprintf("http://%s", HostPortPair(host, apiPort))
	default:
		unifiedPort, err := ParsePort(cfg.UnifiedAddr)
		if err != nil {
			unifiedPort = PortDevUnified
		}
		baseURL = fmt.Sprintf("http://%s", HostPortPair(host, unifiedPort))
	}
	return fmt.Sprintf("    from pyplasmod import EasyPlasmod\n    EasyPlasmod(base_url=%q).health()", baseURL)
}
