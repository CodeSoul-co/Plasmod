package zep

import (
	"os"

	"plasmod/src/internal/config"
)

// AlgorithmID is the stable identifier for the Zep profile plugin.
const AlgorithmID = "zep_v1"

// Config mirrors the paper-aligned zep section in configs/algorithm_zep.yaml.
type Config struct {
	Zep struct {
		Graph struct {
			ContextWindowMessages int      `yaml:"context_window_messages"`
			EntityEmbeddingDim    int      `yaml:"entity_embedding_dim"`
			EpisodeTypes          []string `yaml:"episode_types"`
			TemporalModel         string   `yaml:"temporal_model"`
			TemporalFields        []string `yaml:"temporal_fields"`
		} `yaml:"graph"`
		Retrieval struct {
			SearchMethods []string `yaml:"search_methods"`
			RerankMethods []string `yaml:"rerank_methods"`
			TopK          int      `yaml:"top_k"`
			TopKDMR       int      `yaml:"top_k_dmr"`
			BFS           struct {
				Enabled      bool   `yaml:"enabled"`
				SeedStrategy string `yaml:"seed_strategy"`
			} `yaml:"bfs"`
			Constructor struct {
				Include          []string `yaml:"include"`
				IncludeDateRange bool     `yaml:"include_date_range"`
			} `yaml:"constructor"`
		} `yaml:"retrieval"`
		Community struct {
			Algorithm       string `yaml:"algorithm"`
			DynamicUpdate   bool   `yaml:"dynamic_update"`
			PeriodicRefresh bool   `yaml:"periodic_refresh"`
		} `yaml:"community"`
		TemporalExtraction struct {
			DateTimeFormat                string `yaml:"datetime_format"`
			UseReferenceTimestamp         bool   `yaml:"use_reference_timestamp"`
			PresentTenseUsesReferenceTime bool   `yaml:"present_tense_uses_reference_time"`
			InferRelativeTime             bool   `yaml:"infer_relative_time"`
		} `yaml:"temporal_extraction"`
	} `yaml:"zep"`
}

func DefaultConfig() Config {
	var cfg Config
	cfg.Zep.Graph.ContextWindowMessages = 4
	cfg.Zep.Graph.EntityEmbeddingDim = 1024
	cfg.Zep.Graph.EpisodeTypes = []string{"message", "text", "json"}
	cfg.Zep.Graph.TemporalModel = "bi_temporal"
	cfg.Zep.Graph.TemporalFields = []string{"valid_at", "invalid_at", "created_at_tx", "expired_at_tx"}
	cfg.Zep.Retrieval.SearchMethods = []string{"cosine", "bm25", "bfs"}
	cfg.Zep.Retrieval.RerankMethods = []string{"rrf", "mmr", "episode_mentions", "node_distance"}
	cfg.Zep.Retrieval.TopK = 20
	cfg.Zep.Retrieval.TopKDMR = 10
	cfg.Zep.Retrieval.BFS.Enabled = true
	cfg.Zep.Retrieval.BFS.SeedStrategy = "recent_episodes"
	cfg.Zep.Retrieval.Constructor.Include = []string{"facts", "entities"}
	cfg.Zep.Retrieval.Constructor.IncludeDateRange = true
	cfg.Zep.Community.Algorithm = "label_propagation"
	cfg.Zep.Community.DynamicUpdate = true
	cfg.Zep.Community.PeriodicRefresh = true
	cfg.Zep.TemporalExtraction.DateTimeFormat = "ISO8601"
	cfg.Zep.TemporalExtraction.UseReferenceTimestamp = true
	cfg.Zep.TemporalExtraction.PresentTenseUsesReferenceTime = true
	cfg.Zep.TemporalExtraction.InferRelativeTime = true
	return cfg
}

// LoadFromYAML reads configs/algorithm_zep.yaml and merges it with defaults.
func LoadFromYAML() (Config, error) {
	cfg := DefaultConfig()
	path := os.Getenv("PLASMOD_ALGORITHM_ZEP_CONFIG")
	if path == "" {
		path = "configs/algorithm_zep.yaml"
	}
	if err := config.LoadYAML(path, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}
