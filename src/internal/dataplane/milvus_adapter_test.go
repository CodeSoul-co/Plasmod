package dataplane

import (
	"os"
	"testing"
	"time"
)

// TestMilvusAdapter_Integration tests the Milvus adapter against a real Milvus instance.
// Skip if MILVUS_ADDRESS is not set.
func TestMilvusAdapter_Integration(t *testing.T) {
	addr := os.Getenv("MILVUS_ADDRESS")
	if addr == "" {
		t.Skip("MILVUS_ADDRESS not set, skipping integration test")
	}

	cfg := MilvusConfig{
		Address:        addr,
		CollectionName: "cogdb_test_" + time.Now().Format("20060102150405"),
		Dim:            128,
	}

	adapter, err := NewMilvusAdapter(cfg)
	if err != nil {
		t.Fatalf("failed to create adapter: %v", err)
	}
	defer func() {
		adapter.DropCollection()
		adapter.Close()
	}()

	// Test Ingest
	t.Run("Ingest", func(t *testing.T) {
		err := adapter.Ingest(IngestRecord{
			ObjectID:    "mem_001",
			Text:        "test document about machine learning",
			Namespace:   "default",
			EventUnixTS: time.Now().Unix(),
		})
		if err != nil {
			t.Errorf("Ingest failed: %v", err)
		}
	})

	// Flush to ensure data is searchable
	t.Run("Flush", func(t *testing.T) {
		err := adapter.Flush()
		if err != nil {
			t.Errorf("Flush failed: %v", err)
		}
	})

	// Wait for index to be ready
	time.Sleep(2 * time.Second)

	// Test Search
	t.Run("Search", func(t *testing.T) {
		result := adapter.Search(SearchInput{
			QueryText: "machine learning",
			TopK:      10,
		})

		if result.Tier == "" || result.Tier[:5] == "error" {
			t.Errorf("Search failed with tier: %s", result.Tier)
		}
	})

	// Test CollectionStats
	t.Run("CollectionStats", func(t *testing.T) {
		stats, err := adapter.CollectionStats()
		if err != nil {
			t.Errorf("CollectionStats failed: %v", err)
		}
		t.Logf("Collection stats: %v", stats)
	})
}

// TestMilvusConfig validates configuration.
func TestMilvusConfig(t *testing.T) {
	cfg := MilvusConfig{
		Address:        "localhost:19530",
		CollectionName: "test_collection",
		Dim:            128,
	}

	if cfg.Address == "" {
		t.Error("Address should not be empty")
	}
	if cfg.Dim <= 0 {
		t.Error("Dim should be positive")
	}
}
