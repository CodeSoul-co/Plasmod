package config

import (
	"os"
	"strconv"
	"strings"

	"plasmod/src/internal/schemas"
)

type sharedAlgorithmSection struct {
	Retrieval struct {
		RRFK int `yaml:"rrf_k"`
	} `yaml:"retrieval"`

	ColdSearch struct {
		BatchSize             int     `yaml:"batch_size"`
		MaxCandidates         int     `yaml:"max_candidates"`
		BufferFactor          int     `yaml:"buffer_factor"`
		EarlyStopScore        float64 `yaml:"early_stop_score"`
		NoImprovePages        int     `yaml:"no_improve_pages"`
		DFSRelevanceThreshold float64 `yaml:"dfs_relevance_threshold"`
		Weights               struct {
			Lexical float64 `yaml:"lexical"`
			Dense   float64 `yaml:"dense"`
			Recency float64 `yaml:"recency"`
		} `yaml:"weights"`
	} `yaml:"cold_search"`

	HNSW struct {
		M              int `yaml:"m"`
		EfConstruction int `yaml:"ef_construction"`
		EfSearch       int `yaml:"ef_search"`
	} `yaml:"hnsw"`
}

type sharedAlgorithmConfigDoc struct {
	Baseline   sharedAlgorithmSection `yaml:"baseline"`
	MemoryBank sharedAlgorithmSection `yaml:"memorybank"`
	Zep        sharedAlgorithmSection `yaml:"zep"`
}

// LoadSharedAlgorithmConfig loads shared retrieval / cold-search / hnsw parameters
// into schemas.AlgorithmConfig.
//
// Priority:
//  1. code defaults (schemas.DefaultAlgorithmConfig())
//  2. YAML file selected by PLASMOD_ACTIVE_ALGORITHM (baseline|memorybank|zep)
//  3. caller may still apply env overrides afterwards
func LoadSharedAlgorithmConfig() (schemas.AlgorithmConfig, error) {
	cfg := schemas.DefaultAlgorithmConfig()

	path := os.Getenv("PLASMOD_ALGORITHM_BASELINE_CONFIG")
	root := "baseline"
	if path == "" {
		path = "configs/algorithm_baseline.yaml"
	}

	if mbPath := os.Getenv("PLASMOD_ALGORITHM_MEMORYBANK_CONFIG"); mbPath != "" {
		path = mbPath
		root = "memorybank"
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("PLASMOD_ACTIVE_ALGORITHM"))) {
	case "memorybank":
		root = "memorybank"
		path = "configs/algorithm_memorybank.yaml"
		}
	case "zep":
		root = "zep"
		path = "configs/algorithm_zep.yaml"
	case "baseline":
		root = "baseline"
		path = "configs/algorithm_baseline.yaml"
	}

	var doc sharedAlgorithmConfigDoc
	if err := LoadYAML(path, &doc); err != nil {
		return cfg, err
	}

	var sec sharedAlgorithmSection
	if root == "memorybank" {
		sec = doc.MemoryBank
	} else if root == "zep" {
		sec = doc.Zep
	} else {
		sec = doc.Baseline
	}

	if sec.Retrieval.RRFK > 0 {
		cfg.RRFK = sec.Retrieval.RRFK
	}

	if sec.ColdSearch.BatchSize > 0 {
		cfg.ColdBatchSize = sec.ColdSearch.BatchSize
	}
	if sec.ColdSearch.MaxCandidates > 0 {
		cfg.ColdMaxCandidates = sec.ColdSearch.MaxCandidates
	}
	if sec.ColdSearch.BufferFactor > 0 {
		cfg.ColdBufferFactor = sec.ColdSearch.BufferFactor
	}
	if sec.ColdSearch.EarlyStopScore > 0 && sec.ColdSearch.EarlyStopScore <= 1 {
		cfg.ColdEarlyStopScore = sec.ColdSearch.EarlyStopScore
	}
	if sec.ColdSearch.NoImprovePages > 0 {
		cfg.ColdNoImprovePages = sec.ColdSearch.NoImprovePages
	}
	if sec.ColdSearch.DFSRelevanceThreshold > 0 {
		cfg.DFSRelevanceThreshold = sec.ColdSearch.DFSRelevanceThreshold
	}

	if sec.ColdSearch.Weights.Lexical > 0 {
		cfg.ColdSearchWeights.Lexical = sec.ColdSearch.Weights.Lexical
	}
	if sec.ColdSearch.Weights.Dense > 0 {
		cfg.ColdSearchWeights.Dense = sec.ColdSearch.Weights.Dense
	}
	if sec.ColdSearch.Weights.Recency > 0 {
		cfg.ColdSearchWeights.Recency = sec.ColdSearch.Weights.Recency
	}

	if sec.HNSW.M > 0 {
		cfg.HNSWM = sec.HNSW.M
	}
	if sec.HNSW.EfConstruction > 0 {
		cfg.HNSEfConstruction = sec.HNSW.EfConstruction
	}
	if sec.HNSW.EfSearch > 0 {
		cfg.HNSEfSearch = sec.HNSW.EfSearch
	}

	applySharedAlgorithmEnvOverrides(&cfg)
	return cfg, nil
}

func applySharedAlgorithmEnvOverrides(cfg *schemas.AlgorithmConfig) {
	if cfg == nil {
		return
	}
	if v, ok := envInt("PLASMOD_RRF_K"); ok && v > 0 {
		cfg.RRFK = v
	}
	if v, ok := envInt("PLASMOD_COLD_BATCH_SIZE"); ok && v > 0 {
		cfg.ColdBatchSize = v
	}
	if v, ok := envInt("PLASMOD_COLD_MAX_CANDIDATES"); ok && v > 0 {
		cfg.ColdMaxCandidates = v
	}
	if v, ok := envInt("PLASMOD_COLD_BUFFER_FACTOR"); ok && v > 0 {
		cfg.ColdBufferFactor = v
	}
	if v, ok := envFloat("PLASMOD_COLD_EARLY_STOP_SCORE"); ok && v > 0 && v <= 1 {
		cfg.ColdEarlyStopScore = v
	}
	if v, ok := envInt("PLASMOD_COLD_NO_IMPROVE_PAGES"); ok && v > 0 {
		cfg.ColdNoImprovePages = v
	}
	if v, ok := envFloat("PLASMOD_DFS_RELEVANCE_THRESHOLD"); ok && v > 0 {
		cfg.DFSRelevanceThreshold = v
	}
	if v, ok := envInt("PLASMOD_HNSW_M"); ok && v > 0 {
		cfg.HNSWM = v
	}
	if v, ok := envInt("PLASMOD_HNSW_EF_CONSTRUCTION"); ok && v > 0 {
		cfg.HNSEfConstruction = v
	}
	if v, ok := envInt("PLASMOD_HNSW_EF_SEARCH"); ok && v > 0 {
		cfg.HNSEfSearch = v
	}
	if v, ok := envFloat("PLASMOD_COLD_WEIGHT_LEXICAL"); ok && v >= 0 {
		cfg.ColdSearchWeights.Lexical = v
	}
	if v, ok := envFloat("PLASMOD_COLD_WEIGHT_DENSE"); ok && v >= 0 {
		cfg.ColdSearchWeights.Dense = v
	}
	if v, ok := envFloat("PLASMOD_COLD_WEIGHT_RECENCY"); ok && v >= 0 {
		cfg.ColdSearchWeights.Recency = v
	}
}

func envInt(key string) (int, bool) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0, false
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return v, true
}

func envFloat(key string) (float64, bool) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
