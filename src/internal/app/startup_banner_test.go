package app

import (
	"strings"
	"testing"
)

func TestShouldShowStartupBanner_envOverride(t *testing.T) {
	t.Setenv("PLASMOD_SHOW_BANNER", "1")
	if !shouldShowStartupBanner() {
		t.Fatal("expected banner when PLASMOD_SHOW_BANNER=1")
	}
	t.Setenv("PLASMOD_SHOW_BANNER", "0")
	if shouldShowStartupBanner() {
		t.Fatal("expected no banner when PLASMOD_SHOW_BANNER=0")
	}
}

func TestBuildStartupBannerLines_splitMode(t *testing.T) {
	t.Setenv("PLASMOD_ADMIN_API_KEY", "super-secret-admin-key")
	t.Setenv("PLASMOD_STORAGE", "disk")
	t.Setenv("PLASMOD_DATA_DIR", "/data")
	t.Setenv("PLASMOD_EMBEDDER", "tfidf")
	t.Setenv("S3_ENDPOINT", "minio:9000")
	t.Setenv("S3_BUCKET", "plasmod-integration")

	cfg := ListenConfig{
		Mode:     ListenModeSplit,
		MgmtAddr: "0.0.0.0:9091",
		APIAddr:  "0.0.0.0:19530",
	}
	text := strings.Join(buildStartupBannerLines(cfg, &ServerBundle{
		GRPCEnabled: true,
		GRPCAddr:    "0.0.0.0:19531",
	}), "\n")

	for _, want := range []string{
		"Listen mode  : split",
		"http://127.0.0.1:9091/healthz",
		"http://127.0.0.1:19530",
		"gRPC API",
		"19531",
		"PLASMOD_ADMIN_API_KEY : supe",
		"PLASMOD_STORAGE  : disk",
		"minio:9000  bucket=plasmod-integration",
		"EasyPlasmod(base_url=\"http://127.0.0.1:19530\")",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("banner missing %q\n%s", want, text)
		}
	}
}

func TestMaskSecret(t *testing.T) {
	if got := maskSecret("ab"); got != "****" {
		t.Fatalf("short secret: got %q", got)
	}
	if got := maskSecret("abcdef"); got != "abcd**" {
		t.Fatalf("long secret: got %q", got)
	}
}

func TestAdminKeyLine_unset(t *testing.T) {
	t.Setenv("PLASMOD_ADMIN_API_KEY", "")
	t.Setenv("ANDB_ADMIN_API_KEY", "")
	line := adminKeyLine()
	if !strings.Contains(line, "UNPROTECTED") {
		t.Fatalf("expected unprotected warning, got %q", line)
	}
}
