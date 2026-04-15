package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSharedAlgorithmConfig_BaselineYAML(t *testing.T) {
	dir := mustMkdirTemp(t)
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
	t.Setenv("PLASMOD_ALGORITHM_BASELINE_CONFIG", path)
	t.Setenv("PLASMOD_ALGORITHM_MEMORYBANK_CONFIG", "")

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

func TestLoadSharedAlgorithmConfig_EnvOverrides(t *testing.T) {
	dir := mustMkdirTemp(t)
	path := filepath.Join(dir, "algorithm_baseline.yaml")
	if err := os.WriteFile(path, []byte("baseline:\n  retrieval:\n    rrf_k: 10\n"), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	t.Setenv("PLASMOD_ALGORITHM_BASELINE_CONFIG", path)
	t.Setenv("PLASMOD_ALGORITHM_MEMORYBANK_CONFIG", "")
	t.Setenv("PLASMOD_RRF_K", "91")
	t.Setenv("PLASMOD_COLD_BATCH_SIZE", "333")
	t.Setenv("PLASMOD_COLD_MAX_CANDIDATES", "4444")
	t.Setenv("PLASMOD_COLD_BUFFER_FACTOR", "6")
	t.Setenv("PLASMOD_COLD_EARLY_STOP_SCORE", "0.77")
	t.Setenv("PLASMOD_COLD_NO_IMPROVE_PAGES", "5")
	t.Setenv("PLASMOD_DFS_RELEVANCE_THRESHOLD", "0.41")
	t.Setenv("PLASMOD_HNSW_M", "29")
	t.Setenv("PLASMOD_HNSW_EF_CONSTRUCTION", "512")
	t.Setenv("PLASMOD_HNSW_EF_SEARCH", "123")
	t.Setenv("PLASMOD_COLD_WEIGHT_LEXICAL", "0.55")
	t.Setenv("PLASMOD_COLD_WEIGHT_DENSE", "0.35")
	t.Setenv("PLASMOD_COLD_WEIGHT_RECENCY", "0.10")

	cfg, err := LoadSharedAlgorithmConfig()
	if err != nil {
		t.Fatalf("LoadSharedAlgorithmConfig returned error: %v", err)
	}

	if cfg.RRFK != 91 || cfg.ColdBatchSize != 333 || cfg.ColdMaxCandidates != 4444 {
		t.Fatalf("env int overrides failed: %+v", cfg)
	}
	if cfg.ColdBufferFactor != 6 || cfg.ColdNoImprovePages != 5 {
		t.Fatalf("env cold paging overrides failed: %+v", cfg)
	}
	if cfg.ColdEarlyStopScore != 0.77 || cfg.DFSRelevanceThreshold != 0.41 {
		t.Fatalf("env float overrides failed: %+v", cfg)
	}
	if cfg.HNSWM != 29 || cfg.HNSEfConstruction != 512 || cfg.HNSEfSearch != 123 {
		t.Fatalf("env hnsw overrides failed: %+v", cfg)
	}
	if cfg.ColdSearchWeights.Lexical != 0.55 || cfg.ColdSearchWeights.Dense != 0.35 || cfg.ColdSearchWeights.Recency != 0.10 {
		t.Fatalf("env weight overrides failed: %+v", cfg.ColdSearchWeights)
	}
}

func mustMkdirTemp(t *testing.T) string {
	t.Helper()
	base := filepath.Join(".", "out", "testtmp")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("mkdir base temp dir: %v", err)
	}
	dir, err := os.MkdirTemp(base, "algorithm_shared_*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	return dir
}
