package app

import (
	"fmt"
	"log"
	"os"
	"runtime/debug"
	"strings"
	"unicode/utf8"
)

const startupBannerWidth = 78

// PrintStartupBanner prints a welcome panel with logo, ports, and runtime hints.
// Shown by default inside Docker; override with PLASMOD_SHOW_BANNER=0/1.
func PrintStartupBanner(cfg ListenConfig, bundle *ServerBundle) {
	if !shouldShowStartupBanner() {
		return
	}

	for _, line := range buildStartupBannerLines(cfg, bundle) {
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
	return strings.Contains(s, "docker") ||
		strings.Contains(s, "containerd")
}

func buildStartupBannerLines(cfg ListenConfig, bundle *ServerBundle) []string {
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

		mgmtURL := fmt.Sprintf(
			"http://%s",
			HostPortPair(host, mgmtPort),
		)

		apiURL := fmt.Sprintf(
			"http://%s",
			HostPortPair(host, apiPort),
		)

		lines = append(lines,
			fmt.Sprintf(
				"  Mgmt / health: %s  (port %d)",
				mgmtURL,
				mgmtPort,
			),
			fmt.Sprintf(
				"  SDK REST API : %s  (port %d)",
				apiURL,
				apiPort,
			),
			"",
			"  Quick checks",
			fmt.Sprintf("    curl %s/healthz", mgmtURL),
			fmt.Sprintf(
				"    curl %s/v1/query -H \"Content-Type: application/json\" -d '{\"query_text\":\"hello\"}'",
				apiURL,
			),
		)

	default:
		unifiedPort, _ := ParsePort(cfg.UnifiedAddr)

		baseURL := fmt.Sprintf(
			"http://%s",
			HostPortPair(host, unifiedPort),
		)

		lines = append(lines,
			fmt.Sprintf(
				"  HTTP (unified): %s  (port %d)",
				baseURL,
				unifiedPort,
			),
			"",
			"  Quick checks",
			fmt.Sprintf("    curl %s/healthz", baseURL),
			fmt.Sprintf(
				"    curl %s/v1/query -H \"Content-Type: application/json\" -d '{\"query_text\":\"hello\"}'",
				baseURL,
			),
		)
	}

	if bundle != nil && bundle.GRPCEnabled {
		grpcPort, _ := ParsePort(bundle.GRPCAddr)
		lines = append(lines,
			fmt.Sprintf(
				"  gRPC API       : %s  (port %d, plasmod.v1.PlasmodAPIService)",
				HostPortPair(host, grpcPort),
				grpcPort,
			),
		)
	}

	lines = append(lines,
		"",
		bannerRule("─"),
		"  Runtime",
		fmt.Sprintf(
			"  APP_MODE         : %s",
			envOrDefault("APP_MODE", "dev"),
		),
		fmt.Sprintf(
			"  PLASMOD_STORAGE  : %s",
			envOrDefault("PLASMOD_STORAGE", "memory"),
		),
		fmt.Sprintf(
			"  PLASMOD_DATA_DIR : %s",
			envOrDefault("PLASMOD_DATA_DIR", ".plasmod_data"),
		),
		fmt.Sprintf(
			"  PLASMOD_EMBEDDER : %s",
			envOrDefault("PLASMOD_EMBEDDER", "tfidf"),
		),
		fmt.Sprintf(
			"  Cold store (S3)  : %s",
			s3Summary(),
		),

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
	const (
		reset = "\033[0m"

		// logo 深蓝
		blue = "\033[38;5;25m"

		// logo 金黄
		yellow = "\033[38;5;214m"

		// 淡化说明文字
		dim = "\033[2m"
	)

	return []string{
		"",
		"   " + blue +
			"██████╗ ██╗      █████╗ ███████╗" +
			yellow +
			"███╗   ███╗ ██████╗ ██████╗" +
			reset,

		"   " + blue +
			"██╔══██╗██║     ██╔══██╗██╔════╝" +
			yellow +
			"████╗ ████║██╔═══██╗██╔══██╗" +
			reset,

		"   " + blue +
			"██████╔╝██║     ███████║███████╗" +
			yellow +
			"██╔████╔██║██║   ██║██║  ██║" +
			reset,

		"   " + blue +
			"██╔═══╝ ██║     ██╔══██║╚════██║" +
			yellow +
			"██║╚██╔╝██║██║   ██║██║  ██║" +
			reset,

		"   " + blue +
			"██║     ███████╗██║  ██║███████║" +
			yellow +
			"██║ ╚═╝ ██║╚██████╔╝██████╔╝" +
			reset,

		"   " + blue +
			"╚═╝     ╚══════╝╚═╝  ╚═╝╚══════╝" +
			yellow +
			"╚═╝     ╚═╝ ╚═════╝ ╚═════╝" +
			reset,

		"",
		"           " + dim +
			"Agent-Native Database for AI Systems" +
			reset,

		"         " + dim +
			"memory · vectors · structured evidence" +
			reset,

		"",
	}
}

func bannerRule(ch string) string {
	return strings.Repeat(ch, startupBannerWidth)
}

func centerBannerLine(s string) string {
	s = strings.TrimRight(s, " ")

	w := displayWidth(s)

	if w >= startupBannerWidth {
		return s
	}

	pad := (startupBannerWidth - w) / 2
	return strings.Repeat(" ", pad) + s
}

func displayWidth(s string) int {
	return utf8.RuneCountInString(stripANSI(s))
}

func stripANSI(s string) string {
	var out strings.Builder
	inEscape := false

	for i := 0; i < len(s); i++ {
		c := s[i]

		if c == 0x1b {
			inEscape = true
			continue
		}

		if inEscape {
			if (c >= 'a' && c <= 'z') ||
				(c >= 'A' && c <= 'Z') {
				inEscape = false
			}
			continue
		}

		out.WriteByte(c)
	}

	return out.String()
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

	return fmt.Sprintf(
		"%s  bucket=%s",
		endpoint,
		bucket,
	)
}

func adminKeyLine() string {
	key := strings.TrimSpace(
		os.Getenv("PLASMOD_ADMIN_API_KEY"),
	)

	if key == "" {
		key = strings.TrimSpace(
			os.Getenv("ANDB_ADMIN_API_KEY"),
		)
	}

	if key == "" {
		return "  PLASMOD_ADMIN_API_KEY : (not set — /v1/admin/* is UNPROTECTED)"
	}

	return fmt.Sprintf(
		"  PLASMOD_ADMIN_API_KEY : %s",
		maskSecret(key),
	)
}

func maskSecret(s string) string {
	if len(s) <= 4 {
		return "****"
	}

	return s[:4] +
		strings.Repeat("*", len(s)-4)
}

func pythonSDKLine(cfg ListenConfig, host string) string {
	var baseURL string

	switch cfg.Mode {
	case ListenModeSplit:
		apiPort, err := ParsePort(cfg.APIAddr)
		if err != nil {
			apiPort = PortAPI
		}

		baseURL = fmt.Sprintf(
			"http://%s",
			HostPortPair(host, apiPort),
		)

	default:
		unifiedPort, err := ParsePort(cfg.UnifiedAddr)
		if err != nil {
			unifiedPort = PortDevUnified
		}

		baseURL = fmt.Sprintf(
			"http://%s",
			HostPortPair(host, unifiedPort),
		)
	}

	return fmt.Sprintf(
		"    from pyplasmod import EasyPlasmod\n    EasyPlasmod(base_url=%q).health()",
		baseURL,
	)
}
