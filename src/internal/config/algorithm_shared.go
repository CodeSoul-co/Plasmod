package config

import (
	"os"

	"andb/src/internal/schemas"
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
}

// LoadSharedAlgorithmConfig loads shared retrieval / cold-search / hnsw parameters
// into schemas.AlgorithmConfig.
//
// Priority:
//  1. code defaults (schemas.DefaultAlgorithmConfig())
//  2. YAML file (baseline by default; memorybank if ANDB_ALGORITHM_MEMORYBANK_CONFIG is set)
//  3. caller may still apply env overrides afterwards
func LoadSharedAlgorithmConfig() (schemas.AlgorithmConfig, error) {
	cfg := schemas.DefaultAlgorithmConfig()

	path := os.Getenv("ANDB_ALGORITHM_BASELINE_CONFIG")
	root := "baseline"
	if path == "" {
		path = "configs/algorithm_baseline.yaml"
	}

	if mbPath := os.Getenv("ANDB_ALGORITHM_MEMORYBANK_CONFIG"); mbPath != "" {
		path = mbPath
		root = "memorybank"
	}

	var doc sharedAlgorithmConfigDoc
	if err := LoadYAML(path, &doc); err != nil {
		return cfg, err
	}

	var sec sharedAlgorithmSection
	if root == "memorybank" {
		sec = doc.MemoryBank
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

	return cfg, nil
}
