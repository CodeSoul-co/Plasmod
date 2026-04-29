package config

import (
	"os"
	"strings"
)

// MemoryTieringConfig controls hot-tier scoring and eviction behavior.
// All fields are soft-coded via configs/memory_tiering.yaml.
type MemoryTieringConfig struct {
	MemoryTiering struct {
		HotCache struct {
			HighWatermarkPercent float64 `yaml:"high_watermark_percent"`
			LowWatermarkPercent  float64 `yaml:"low_watermark_percent"`
			EvictionBatchSize    int     `yaml:"eviction_batch_size"`
			Score                struct {
				Wr     float64 `yaml:"wr"`
				Wf     float64 `yaml:"wf"`
				Ws     float64 `yaml:"ws"`
				Lambda float64 `yaml:"lambda"`
			} `yaml:"score"`
			Penalty struct {
				AlphaSize     float64 `yaml:"alpha_size"`
				BetaWriteBack float64 `yaml:"beta_write_back"`
				GammaHitProb  float64 `yaml:"gamma_hit_prob"`
				DeltaReload   float64 `yaml:"delta_reload"`
			} `yaml:"penalty"`
			FrequencyNormWindow int                `yaml:"frequency_norm_window"`
			RecencyTauSeconds   float64            `yaml:"recency_tau_seconds"`
			RecencyTauByType    map[string]float64 `yaml:"recency_tau_by_type"`
			EstimatedPoolBytes  float64            `yaml:"estimated_pool_bytes"`
			ObjectTypeWeight    map[string]float64 `yaml:"object_type_weight"`
			ReloadEaseByType    map[string]float64 `yaml:"reload_ease_by_type"`
			WriteBackByType     map[string]float64 `yaml:"write_back_by_type"`
			PointerPolicy       struct {
				Class1Types []string `yaml:"class1_types"`
				Class2Types []string `yaml:"class2_types"`
				Class3Types []string `yaml:"class3_types"`
			} `yaml:"pointer_policy"`
		} `yaml:"hot_cache"`
	} `yaml:"memory_tiering"`
}

func defaultMemoryTieringConfig() MemoryTieringConfig {
	var cfg MemoryTieringConfig
	h := &cfg.MemoryTiering.HotCache
	h.HighWatermarkPercent = 0.80
	h.LowWatermarkPercent = 0.60
	h.EvictionBatchSize = 16
	h.Score.Wr = 0.25
	h.Score.Wf = 0.20
	h.Score.Ws = 0.55
	h.Score.Lambda = 0.80
	h.Penalty.AlphaSize = 0.45
	h.Penalty.BetaWriteBack = 0.20
	h.Penalty.GammaHitProb = 0.25
	h.Penalty.DeltaReload = 0.10
	h.FrequencyNormWindow = 16
	h.RecencyTauSeconds = 120.0
	h.RecencyTauByType = map[string]float64{
		"memory":   180.0,
		"state":    120.0,
		"artifact": 90.0,
	}
	h.EstimatedPoolBytes = 256 * 1024 * 1024 // 256MiB logical budget for size normalization
	h.ObjectTypeWeight = map[string]float64{
		"memory":   0.95,
		"state":    0.85,
		"artifact": 0.70,
	}
	h.ReloadEaseByType = map[string]float64{
		"memory":   0.30,
		"state":    0.50,
		"artifact": 0.70,
	}
	h.WriteBackByType = map[string]float64{
		"memory":   0.40,
		"state":    0.60,
		"artifact": 0.50,
	}
	h.PointerPolicy.Class1Types = []string{"memory", "state"}
	h.PointerPolicy.Class2Types = []string{"artifact"}
	h.PointerPolicy.Class3Types = []string{}
	return cfg
}

// LoadMemoryTieringConfig loads memory tiering settings from:
// 1) defaults
// 2) configs/memory_tiering.yaml (or PLASMOD_MEMORY_TIERING_CONFIG override)
func LoadMemoryTieringConfig() (MemoryTieringConfig, error) {
	cfg := defaultMemoryTieringConfig()
	path := strings.TrimSpace(os.Getenv("PLASMOD_MEMORY_TIERING_CONFIG"))
	if path == "" {
		path = "configs/memory_tiering.yaml"
	}
	if err := LoadYAML(path, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}
