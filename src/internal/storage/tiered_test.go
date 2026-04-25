package storage

import (
	"fmt"
	"testing"
	"time"

	"plasmod/src/internal/schemas"
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
	if _, ok := warm.GetMemory(mem.MemoryID); ok {
		t.Fatal("expected archived memory to be evicted from warm store")
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
	if !hot.Contains(mem.MemoryID) {
		t.Fatal("expected reactivated memory to be promoted to hot cache")
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

func TestTieredObjectStore_SoftDeleteMemoryTierCleanup_EvictsHot(t *testing.T) {
	hot := NewHotObjectCache(100)
	warm := newMemoryObjectStore()
	tiered := NewTieredObjectStore(hot, warm, newMemoryGraphEdgeStore(), NewInMemoryColdStore())

	mem := schemas.Memory{MemoryID: "mem_soft_1", Content: "x", IsActive: true}
	tiered.PutMemory(mem, 1.0)
	if !tiered.HotCache().Contains(mem.MemoryID) {
		t.Fatal("expected memory in hot after PutMemory with high salience")
	}
	tiered.SoftDeleteMemoryTierCleanup(mem.MemoryID)
	if tiered.HotCache().Contains(mem.MemoryID) {
		t.Fatal("expected hot evicted after SoftDeleteMemoryTierCleanup")
	}
}

func TestTieredObjectStore_HardDeleteMemory_DeletesColdIncidentEdges(t *testing.T) {
	hot := NewHotObjectCache(100)
	warm := newMemoryObjectStore()
	warmEdges := newMemoryGraphEdgeStore()
	cold := NewInMemoryColdStore()
	tiered := NewTieredObjectStore(hot, warm, warmEdges, cold)

	cold.PutEdge(schemas.Edge{EdgeID: "e_inc", SrcObjectID: "mem_x", DstObjectID: "evt_1", EdgeType: "derived_from"})
	cold.PutEdge(schemas.Edge{EdgeID: "e_other", SrcObjectID: "mem_y", DstObjectID: "evt_2", EdgeType: "derived_from"})

	tiered.HardDeleteMemory("mem_x")

	if _, ok := cold.GetEdge("e_inc"); ok {
		t.Fatalf("expected incident cold edge to be deleted")
	}
	if _, ok := cold.GetEdge("e_other"); !ok {
		t.Fatalf("expected unrelated cold edge to remain")
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

func TestTieredObjectStore_ArchiveMemory_WritesS3ColdEmbedding(t *testing.T) {
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Skipf("skip S3-backed tiered archive test: %v", err)
	}
	cfg.Prefix = fmt.Sprintf("%s/test_tiered_archive_%d", cfg.Prefix, time.Now().UnixNano())

	hot := NewHotObjectCache(100)
	warm := newMemoryObjectStore()
	warmEdges := newMemoryGraphEdgeStore()
	cold := NewS3ColdStore(cfg)

	embedder := &fakeMemoryEmbedder{
		vec: []float32{0.11, 0.22, 0.33},
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
		MemoryID: fmt.Sprintf("mem_s3_archive_%d", time.Now().UnixNano()),
		Content:  "s3 tiered archive embedding test",
		IsActive: true,
		Version:  time.Now().Unix(),
	}
	warm.PutMemory(mem)

	tiered.ArchiveMemory(mem.MemoryID)

	// 1) memory should exist in S3 cold store
	gotMem, ok := cold.GetMemory(mem.MemoryID)
	if !ok {
		t.Fatal("expected archived memory to exist in S3 cold store")
	}
	if gotMem.MemoryID != mem.MemoryID {
		t.Fatalf("unexpected memory id: got %q want %q", gotMem.MemoryID, mem.MemoryID)
	}
	if _, warmOk := warm.GetMemory(mem.MemoryID); warmOk {
		t.Fatal("expected archived memory to be evicted from warm store")
	}

	// 2) embedding should also exist in S3 cold store
	vec, ok, err := cold.GetMemoryEmbedding(mem.MemoryID)
	if err != nil {
		t.Fatalf("GetMemoryEmbedding returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected archived memory embedding to exist in S3 cold store")
	}
	if len(vec) != 3 {
		t.Fatalf("expected embedding length 3, got %d", len(vec))
	}
	if vec[0] != 0.11 || vec[1] != 0.22 || vec[2] != 0.33 {
		t.Fatalf("unexpected embedding values: %+v", vec)
	}

	// cleanup (best effort)
	_ = cold.DeleteMemoryEmbedding(mem.MemoryID)
	_ = cold.DeleteMemory(mem.MemoryID)
}

func TestTieredObjectStore_GetMemoryActivated_DeletesS3ColdEmbedding(t *testing.T) {
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Skipf("skip S3-backed tiered reactivation test: %v", err)
	}
	cfg.Prefix = fmt.Sprintf("%s/test_tiered_reactivate_%d", cfg.Prefix, time.Now().UnixNano())

	hot := NewHotObjectCache(100)
	warm := newMemoryObjectStore()
	warmEdges := newMemoryGraphEdgeStore()
	cold := NewS3ColdStore(cfg)

	tiered := NewTieredObjectStoreWithEmbedder(
		hot,
		warm,
		warmEdges,
		cold,
		nil, // embedder not needed for reactivation path
		schemas.DefaultAlgorithmConfig().HotTierSalienceThreshold,
	)

	mem := schemas.Memory{
		MemoryID: fmt.Sprintf("mem_s3_reactivate_%d", time.Now().UnixNano()),
		Content:  "s3 tiered reactivation test",
		IsActive: false,
		Version:  time.Now().Unix(),
	}

	// Put memory + embedding directly into cold tier
	cold.PutMemory(mem)
	if putErr := cold.PutMemoryEmbedding(mem.MemoryID, []float32{1.1, 2.2, 3.3}); putErr != nil {
		t.Fatalf("PutMemoryEmbedding failed: %v", putErr)
	}

	// Reactivate from cold
	got, ok := tiered.GetMemoryActivated(mem.MemoryID, 0.8)
	if !ok {
		t.Fatal("expected GetMemoryActivated to reactivate memory from S3 cold tier")
	}
	if got.MemoryID != mem.MemoryID {
		t.Fatalf("unexpected memory ID: got %q want %q", got.MemoryID, mem.MemoryID)
	}

	// 1) memory should be promoted back to warm
	if _, ok := warm.GetMemory(mem.MemoryID); !ok {
		t.Fatal("expected reactivated memory to be promoted to warm store")
	}
	if !hot.Contains(mem.MemoryID) {
		t.Fatal("expected reactivated memory to be promoted to hot cache")
	}

	// 2) embedding should be deleted from cold tier
	_, exists, err := cold.GetMemoryEmbedding(mem.MemoryID)
	if err != nil {
		t.Fatalf("GetMemoryEmbedding after reactivation returned error: %v", err)
	}
	if exists {
		t.Fatal("expected cold embedding to be deleted after reactivation")
	}

	// cleanup (best effort)
	_ = cold.DeleteMemory(mem.MemoryID)
}
