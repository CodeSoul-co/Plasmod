package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSharedAlgorithmConfig_BaselineYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "algorithm_baseline.yaml")

	yamlContent := `
baseline:
  retrieval:
    rrf_k: 77

  cold_search:
    batch_size: 256
    max_candidates: 2048
    buffer_factor: 5
    early_stop_score: 0.88
    no_improve_pages: 4
    dfs_relevance_threshold: 0.35
    weights:
      lexical: 0.6
      dense: 0.3
      recency: 0.1

  hnsw:
    m: 24
    ef_construction: 320
    ef_search: 96
`

	if err := os.WriteFile(path, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	// 只走 baseline 配置路径
	t.Setenv("ANDB_ALGORITHM_BASELINE_CONFIG", path)
	t.Setenv("ANDB_ALGORITHM_MEMORYBANK_CONFIG", "")

	cfg, err := LoadSharedAlgorithmConfig()
	if err != nil {
		t.Fatalf("LoadSharedAlgorithmConfig returned error: %v", err)
	}

	if cfg.RRFK != 77 {
		t.Fatalf("RRFK: want 77, got %d", cfg.RRFK)
	}
	if cfg.ColdBatchSize != 256 {
		t.Fatalf("ColdBatchSize: want 256, got %d", cfg.ColdBatchSize)
	}
	if cfg.ColdMaxCandidates != 2048 {
		t.Fatalf("ColdMaxCandidates: want 2048, got %d", cfg.ColdMaxCandidates)
	}
	if cfg.ColdBufferFactor != 5 {
		t.Fatalf("ColdBufferFactor: want 5, got %d", cfg.ColdBufferFactor)
	}
	if cfg.ColdEarlyStopScore != 0.88 {
		t.Fatalf("ColdEarlyStopScore: want 0.88, got %v", cfg.ColdEarlyStopScore)
	}
	if cfg.ColdNoImprovePages != 4 {
		t.Fatalf("ColdNoImprovePages: want 4, got %d", cfg.ColdNoImprovePages)
	}
	if cfg.DFSRelevanceThreshold != 0.35 {
		t.Fatalf("DFSRelevanceThreshold: want 0.35, got %v", cfg.DFSRelevanceThreshold)
	}

	if cfg.ColdSearchWeights.Lexical != 0.6 {
		t.Fatalf("ColdSearchWeights.Lexical: want 0.6, got %v", cfg.ColdSearchWeights.Lexical)
	}
	if cfg.ColdSearchWeights.Dense != 0.3 {
		t.Fatalf("ColdSearchWeights.Dense: want 0.3, got %v", cfg.ColdSearchWeights.Dense)
	}
	if cfg.ColdSearchWeights.Recency != 0.1 {
		t.Fatalf("ColdSearchWeights.Recency: want 0.1, got %v", cfg.ColdSearchWeights.Recency)
	}

	if cfg.HNSWM != 24 {
		t.Fatalf("HNSWM: want 24, got %d", cfg.HNSWM)
	}
	if cfg.HNSEfConstruction != 320 {
		t.Fatalf("HNSEfConstruction: want 320, got %d", cfg.HNSEfConstruction)
	}
	if cfg.HNSEfSearch != 96 {
		t.Fatalf("HNSEfSearch: want 96, got %d", cfg.HNSEfSearch)
	}
}
