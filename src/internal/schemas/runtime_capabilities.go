package schemas

// RuntimeCapabilities selects production data-path components at process
// bootstrap. Profiles are orthogonal and the defaults preserve the complete
// Plasmod runtime.
type RuntimeCapabilities struct {
	WALMode                string `json:"wal_mode"`
	RecoveryReplay         bool   `json:"recovery_replay"`
	RecoveryProjection     string `json:"recovery_projection"`
	MaterializationProfile string `json:"materialization_profile"`
	EvidenceProfile        string `json:"evidence_profile"`
	GovernanceProfile      string `json:"governance_profile"`
	TierProfile            string `json:"tier_profile"`
	HotCacheSize           int    `json:"hot_cache_size"`
}

func DefaultRuntimeCapabilities() RuntimeCapabilities {
	return RuntimeCapabilities{
		WALMode:                "auto",
		RecoveryReplay:         true,
		RecoveryProjection:     "full",
		MaterializationProfile: "full",
		EvidenceProfile:        "full",
		GovernanceProfile:      "full",
		TierProfile:            "full",
		HotCacheSize:           2000,
	}
}
