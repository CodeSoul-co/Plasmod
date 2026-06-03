package app

import "testing"

func TestPlasmodStandardPorts(t *testing.T) {
	if PortMgmt != 9091 || PortAPI != 19530 || PortObjectStore != 9000 || PortObjectStoreConsole != 9001 {
		t.Fatalf("ports: mgmt=%d api=%d object=%d console=%d",
			PortMgmt, PortAPI, PortObjectStore, PortObjectStoreConsole)
	}
	if PortDevUnified != 8080 {
		t.Fatalf("dev unified port=%d", PortDevUnified)
	}
}

func TestResolveListenConfig_defaults(t *testing.T) {
	t.Setenv("PLASMOD_LISTEN_MODE", "")
	t.Setenv("PLASMOD_HTTP_ADDR", "")
	cfg := ResolveListenConfig()
	if cfg.Mode != ListenModeUnified || cfg.UnifiedAddr != "127.0.0.1:8080" {
		t.Fatalf("unified default: %+v", cfg)
	}
}

func TestResolveListenConfig_split(t *testing.T) {
	t.Setenv("PLASMOD_LISTEN_MODE", "split")
	t.Setenv("PLASMOD_MGMT_ADDR", "")
	t.Setenv("PLASMOD_API_ADDR", "")
	cfg := ResolveListenConfig()
	if cfg.Mode != ListenModeSplit || cfg.MgmtAddr != "0.0.0.0:9091" || cfg.APIAddr != "0.0.0.0:19530" {
		t.Fatalf("split default: %+v", cfg)
	}
}
