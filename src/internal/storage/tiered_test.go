package storage

import (
	"testing"

	"andb/src/internal/schemas"
)

type fakeMemoryEmbedder struct {
	vec []float32
	err error
}

func (f *fakeMemoryEmbedder) Generate(text string) ([]float32, error) {
	return f.vec, f.err
}

func TestTieredObjectStore_ArchiveMemory_WritesColdEmbedding(t *testing.T) {
	hot := NewHotObjectCache(100)
	warm := newMemoryObjectStore()
	warmEdges := newMemoryGraphEdgeStore()
	cold := NewInMemoryColdStore()

	embedder := &fakeMemoryEmbedder{
		vec: []float32{0.1, 0.2, 0.3},
	}

	tiered := NewTieredObjectStoreWithEmbedder(
		hot,
		warm,
		warmEdges,
		cold,
		embedder,
		schemas.DefaultAlgorithmConfig().HotTierSalienceThreshold,
	)

	mem := schemas.Memory{
		MemoryID: "mem_embed_archive_1",
		Content:  "embedding archive test content",
		IsActive: true,
	}
	warm.PutMemory(mem)

	tiered.ArchiveMemory(mem.MemoryID)

	// 1) memory should exist in cold store
	if _, ok := cold.GetMemory(mem.MemoryID); !ok {
		t.Fatal("expected archived memory to exist in cold store")
	}

	// 2) embedding should also exist in cold store
	vec, ok, err := cold.GetMemoryEmbedding(mem.MemoryID)
	if err != nil {
		t.Fatalf("GetMemoryEmbedding returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected archived memory embedding to exist in cold store")
	}
	if len(vec) != 3 {
		t.Fatalf("expected embedding length 3, got %d", len(vec))
	}
	if vec[0] != 0.1 || vec[1] != 0.2 || vec[2] != 0.3 {
		t.Fatalf("unexpected embedding values: %+v", vec)
	}
}

func TestTieredObjectStore_GetMemoryActivated_DeletesColdEmbedding(t *testing.T) {
	hot := NewHotObjectCache(100)
	warm := newMemoryObjectStore()
	warmEdges := newMemoryGraphEdgeStore()
	cold := NewInMemoryColdStore()

	tiered := NewTieredObjectStoreWithEmbedder(
		hot,
		warm,
		warmEdges,
		cold,
		nil, // embedder not needed for reactivation path
		schemas.DefaultAlgorithmConfig().HotTierSalienceThreshold,
	)

	mem := schemas.Memory{
		MemoryID: "mem_embed_reactivate_1",
		Content:  "cold reactivation test",
		IsActive: false,
	}
	cold.PutMemory(mem)
	if err := cold.PutMemoryEmbedding(mem.MemoryID, []float32{1.0, 2.0, 3.0}); err != nil {
		t.Fatalf("PutMemoryEmbedding failed: %v", err)
	}

	got, ok := tiered.GetMemoryActivated(mem.MemoryID, 0.8)
	if !ok {
		t.Fatal("expected GetMemoryActivated to reactivate memory from cold tier")
	}
	if got.MemoryID != mem.MemoryID {
		t.Fatalf("unexpected memory ID: got %q want %q", got.MemoryID, mem.MemoryID)
	}

	// 1) memory should be promoted back to warm
	if _, ok := warm.GetMemory(mem.MemoryID); !ok {
		t.Fatal("expected reactivated memory to be promoted to warm store")
	}

	// 2) embedding should be deleted from cold store
	_, exists, err := cold.GetMemoryEmbedding(mem.MemoryID)
	if err != nil {
		t.Fatalf("GetMemoryEmbedding after reactivation returned error: %v", err)
	}
	if exists {
		t.Fatal("expected cold embedding to be deleted after reactivation")
	}
}

func TestInMemoryColdStore_ColdVectorSearch(t *testing.T) {
	cold := NewInMemoryColdStore()

	cold.PutMemory(schemas.Memory{MemoryID: "m1", Version: 1})
	cold.PutMemory(schemas.Memory{MemoryID: "m2", Version: 2})
	cold.PutMemory(schemas.Memory{MemoryID: "m3", Version: 3})

	_ = cold.PutMemoryEmbedding("m1", []float32{1, 0})
	_ = cold.PutMemoryEmbedding("m2", []float32{0, 1})
	_ = cold.PutMemoryEmbedding("m3", []float32{0.5, 0.5})

	got := cold.ColdVectorSearch([]float32{1, 0}, 3)
	if len(got) == 0 {
		t.Fatal("expected at least one vector search result")
	}

	if got[0] != "m1" {
		t.Fatalf("expected m1 ranked first, got %+v", got)
	}
}
