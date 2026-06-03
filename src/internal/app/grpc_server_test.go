package app

import "testing"

func TestResolveGRPCConfigDefaults(t *testing.T) {
	t.Setenv("PLASMOD_GRPC_ENABLED", "")
	t.Setenv("PLASMOD_GRPC_ADDR", "")
	t.Setenv("PLASMOD_GRPC_MAX_MESSAGE_BYTES", "")

	cfg := ResolveGRPCConfig()
	if !cfg.Enabled {
		t.Fatal("expected gRPC enabled by default")
	}
	if cfg.Addr != "0.0.0.0:19531" {
		t.Fatalf("addr=%q want default", cfg.Addr)
	}
	if cfg.MaxMessageBytes != defaultGRPCMaxMessageBytes {
		t.Fatalf("max message bytes=%d want %d", cfg.MaxMessageBytes, defaultGRPCMaxMessageBytes)
	}
}

func TestResolveGRPCConfigMaxMessageOverride(t *testing.T) {
	t.Setenv("PLASMOD_GRPC_ENABLED", "")
	t.Setenv("PLASMOD_GRPC_MAX_MESSAGE_BYTES", "134217728")

	cfg := ResolveGRPCConfig()
	if cfg.MaxMessageBytes != 134217728 {
		t.Fatalf("max message bytes=%d want override", cfg.MaxMessageBytes)
	}
}
