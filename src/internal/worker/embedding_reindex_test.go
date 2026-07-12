package worker

import (
	"testing"

	"plasmod/src/internal/dataplane"
	"plasmod/src/internal/schemas"
	"plasmod/src/internal/storage"
)

func TestRuntimeReindexEmbeddingsRewritesSegmentSpec(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	tiered := storage.NewTieredObjectStore(store.HotCache(), store.Objects(), store.Edges(), storage.NewInMemoryColdStore())
	plane, err := dataplane.NewTieredDataPlaneWithEmbedder(tiered, dataplane.NewTfidfEmbedder(32))
	if err != nil {
		t.Fatalf("new tiered plane: %v", err)
	}
	runtime := &Runtime{plane: plane, storage: store, tieredObjects: tiered}
	if err := runtime.ConfigureEmbeddingSpec(storage.EmbeddingSpec{Family: "tfidf", Dim: 32}); err != nil {
		t.Fatalf("configure embedding spec: %v", err)
	}
	store.Objects().PutMemory(schemas.Memory{MemoryID: "mem_1", Content: "reindex canonical content", IsActive: true})
	store.Segments().Upsert(storage.SegmentRecord{
		SegmentID:       "mem_1",
		StorageRef:      "mem_1",
		Namespace:       "ws",
		EmbeddingFamily: "old-model",
		EmbeddingDim:    768,
		RowCount:        1,
	})

	count, err := runtime.ReindexEmbeddings()
	if err != nil {
		t.Fatalf("reindex embeddings: %v", err)
	}
	if count != 1 {
		t.Fatalf("reindexed records = %d, want 1", count)
	}
	segments := store.Segments().List("ws")
	if len(segments) != 1 {
		t.Fatalf("segment count = %d, want 1", len(segments))
	}
	if got := segments[0]; got.EmbeddingFamily != "tfidf" || got.EmbeddingDim != 32 {
		t.Fatalf("segment spec = %s/%d, want tfidf/32", got.EmbeddingFamily, got.EmbeddingDim)
	}
}

func TestRuntimeReindexEmbeddingsRewritesColdVectors(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	cold := storage.NewInMemoryColdStore()
	tiered := storage.NewTieredObjectStore(store.HotCache(), store.Objects(), store.Edges(), cold)
	plane, err := dataplane.NewTieredDataPlaneWithEmbedder(tiered, dataplane.NewTfidfEmbedder(24))
	if err != nil {
		t.Fatalf("new tiered plane: %v", err)
	}
	runtime := &Runtime{plane: plane, storage: store, tieredObjects: tiered}
	if err := runtime.ConfigureEmbeddingSpec(storage.EmbeddingSpec{Family: "tfidf", Dim: 24}); err != nil {
		t.Fatalf("configure embedding spec: %v", err)
	}
	cold.PutMemory(schemas.Memory{MemoryID: "cold_1", Content: "archived canonical content"})
	if err := cold.PutMemoryEmbedding("cold_1", []float32{1, 2}); err != nil {
		t.Fatalf("seed cold embedding: %v", err)
	}
	store.Segments().Upsert(storage.SegmentRecord{SegmentID: "cold_1", StorageRef: "cold_1", EmbeddingFamily: "old", EmbeddingDim: 2})

	if _, err := runtime.ReindexEmbeddings(); err != nil {
		t.Fatalf("reindex embeddings: %v", err)
	}
	vec, ok, err := cold.GetMemoryEmbedding("cold_1")
	if err != nil || !ok {
		t.Fatalf("reindexed cold embedding unavailable: ok=%t err=%v", ok, err)
	}
	if len(vec) != 24 {
		t.Fatalf("cold embedding dim = %d, want 24", len(vec))
	}
	segments := store.Segments().List("")
	if len(segments) != 1 || segments[0].EmbeddingFamily != "tfidf" || segments[0].EmbeddingDim != 24 {
		t.Fatalf("cold segment spec was not updated: %+v", segments)
	}
}

func TestRuntimeReindexEmbeddingsRejectsMissingCanonicalMemory(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	tiered := storage.NewTieredObjectStore(store.HotCache(), store.Objects(), store.Edges(), storage.NewInMemoryColdStore())
	plane, err := dataplane.NewTieredDataPlaneWithEmbedder(tiered, dataplane.NewTfidfEmbedder(16))
	if err != nil {
		t.Fatalf("new tiered plane: %v", err)
	}
	runtime := &Runtime{plane: plane, storage: store, tieredObjects: tiered}
	if err := runtime.ConfigureEmbeddingSpec(storage.EmbeddingSpec{Family: "tfidf", Dim: 16}); err != nil {
		t.Fatalf("configure embedding spec: %v", err)
	}
	store.Segments().Upsert(storage.SegmentRecord{SegmentID: "missing", StorageRef: "missing", EmbeddingFamily: "old", EmbeddingDim: 1})
	if _, err := runtime.ReindexEmbeddings(); err == nil {
		t.Fatal("expected missing canonical memory to fail reindex")
	}
}
