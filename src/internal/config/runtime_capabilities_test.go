package config

import "testing"

func TestLoadRuntimeCapabilities(t *testing.T) {
	t.Setenv("PLASMOD_WAL_MODE", "memory")
	t.Setenv("PLASMOD_RECOVERY_REPLAY", "false")
	t.Setenv("PLASMOD_RECOVERY_PROJECTION", "canonical_only")
	t.Setenv("PLASMOD_MATERIALIZATION_PROFILE", "no_edge")
	t.Setenv("PLASMOD_EVIDENCE_PROFILE", "one_hop")
	t.Setenv("PLASMOD_GOVERNANCE_PROFILE", "metadata_only")
	t.Setenv("PLASMOD_TIER_PROFILE", "no_hot")
	t.Setenv("PLASMOD_HOT_CACHE_SIZE", "64")
	cfg, err := LoadRuntimeCapabilities()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WALMode != "memory" || cfg.RecoveryReplay || cfg.RecoveryProjection != "canonical_only" ||
		cfg.MaterializationProfile != "no_edge" || cfg.EvidenceProfile != "one_hop" ||
		cfg.GovernanceProfile != "metadata_only" || cfg.TierProfile != "no_hot" || cfg.HotCacheSize != 64 {
		t.Fatalf("unexpected capabilities: %+v", cfg)
	}
}

func TestLoadRuntimeCapabilitiesRejectsUnknownProfile(t *testing.T) {
	t.Setenv("PLASMOD_EVIDENCE_PROFILE", "invented")
	if _, err := LoadRuntimeCapabilities(); err == nil {
		t.Fatal("expected invalid profile error")
	}
}
