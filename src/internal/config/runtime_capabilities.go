package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"plasmod/src/internal/schemas"
)

var capabilityValues = map[string]map[string]struct{}{
	"PLASMOD_WAL_MODE": {
		"auto": {}, "disabled": {}, "memory": {}, "file": {},
	},
	"PLASMOD_RECOVERY_PROJECTION": {
		"full": {}, "canonical_only": {},
	},
	"PLASMOD_MATERIALIZATION_PROFILE": {
		"full": {}, "none": {}, "memory_only": {}, "no_state": {},
		"no_artifact": {}, "no_edge": {}, "no_version": {},
	},
	"PLASMOD_EVIDENCE_PROFILE": {
		"full": {}, "none": {}, "no_provenance": {}, "no_edge_expansion": {},
		"one_hop": {}, "no_proof": {}, "vector_only": {},
	},
	"PLASMOD_GOVERNANCE_PROFILE": {
		"full": {}, "no_access": {}, "metadata_only": {}, "no_share_contract": {},
		"no_quarantine": {}, "no_delete_propagation": {},
	},
	"PLASMOD_TIER_PROFILE": {
		"full": {}, "no_hot": {}, "warm_only": {}, "no_cold": {}, "no_promotion": {},
	},
}

// LoadRuntimeCapabilities validates deployment capability switches. Unknown
// values fail startup instead of silently selecting an undefined data path.
func LoadRuntimeCapabilities() (schemas.RuntimeCapabilities, error) {
	cfg := schemas.DefaultRuntimeCapabilities()
	var err error
	if cfg.WALMode, err = capabilityValue("PLASMOD_WAL_MODE", cfg.WALMode); err != nil {
		return cfg, err
	}
	if cfg.RecoveryProjection, err = capabilityValue("PLASMOD_RECOVERY_PROJECTION", cfg.RecoveryProjection); err != nil {
		return cfg, err
	}
	if cfg.MaterializationProfile, err = capabilityValue("PLASMOD_MATERIALIZATION_PROFILE", cfg.MaterializationProfile); err != nil {
		return cfg, err
	}
	if cfg.EvidenceProfile, err = capabilityValue("PLASMOD_EVIDENCE_PROFILE", cfg.EvidenceProfile); err != nil {
		return cfg, err
	}
	if cfg.GovernanceProfile, err = capabilityValue("PLASMOD_GOVERNANCE_PROFILE", cfg.GovernanceProfile); err != nil {
		return cfg, err
	}
	if cfg.TierProfile, err = capabilityValue("PLASMOD_TIER_PROFILE", cfg.TierProfile); err != nil {
		return cfg, err
	}
	cfg.RecoveryReplay = envBoolWithDefault("PLASMOD_RECOVERY_REPLAY", cfg.RecoveryReplay)
	if raw := strings.TrimSpace(os.Getenv("PLASMOD_HOT_CACHE_SIZE")); raw != "" {
		n, parseErr := strconv.Atoi(raw)
		if parseErr != nil || n < 1 {
			return cfg, fmt.Errorf("PLASMOD_HOT_CACHE_SIZE must be a positive integer, got %q", raw)
		}
		cfg.HotCacheSize = n
	}
	return cfg, nil
}

func capabilityValue(key, fallback string) (string, error) {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if raw == "" {
		return fallback, nil
	}
	if _, ok := capabilityValues[key][raw]; !ok {
		return "", fmt.Errorf("unsupported %s=%q", key, raw)
	}
	return raw, nil
}

func envBoolWithDefault(key string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}
