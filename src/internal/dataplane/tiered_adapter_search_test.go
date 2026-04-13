package dataplane

import (
	"fmt"
	"testing"
	"time"

	"plasmod/src/internal/schemas"
	"plasmod/src/internal/storage"
)

// Regression: when the hot tier already returns TopK hits, cold must still run if IncludeCold is set.
func TestTieredDataPlane_Search_IncludeColdWhenHotSatisfiesTopK(t *testing.T) {
	cold := storage.NewInMemoryColdStore()
	cold.PutMemory(schemas.Memory{
		MemoryID: "mem_only_cold",
		Content:  "foo cold tier marker",
	})
	objs := storage.NewTieredObjectStore(nil, nil, nil, cold)
	p := NewTieredDataPlane(objs)

	p.hot.InsertObject("mem_hot_1", "foo warm one", nil, "", 1)
	p.hot.InsertObject("mem_hot_2", "foo warm two", nil, "", 2)

	// TopK=2 and two hot hits triggers the hot fast-path; cold is still merged for tier/proof trace.
	out := p.Search(SearchInput{
		QueryText:   "foo",
		TopK:        2,
		IncludeCold: true,
	})
	if out.Tier != "hot+cold" {
		t.Fatalf("tier: got %q, want hot+cold", out.Tier)
	}
	// Merged list is truncated to TopK, so cold IDs may not appear when hot already fills the page.
}

type fakeQueryEmbedder struct {
	vec []float32
	err error
}

func (f *fakeQueryEmbedder) Generate(text string) ([]float32, error) {
	return f.vec, f.err
}

func (f *fakeQueryEmbedder) Dim() int {
	return len(f.vec)
}

func (f *fakeQueryEmbedder) Reset() {}

func TestTieredDataPlane_Search_IncludeColdUsesVectorSearch(t *testing.T) {
	embedder := &fakeQueryEmbedder{
		vec: []float32{1, 0},
	}

	cold := storage.NewInMemoryColdStore()
	cold.PutMemory(schemas.Memory{MemoryID: "m1", Content: "alpha", Version: 1})
	cold.PutMemory(schemas.Memory{MemoryID: "m2", Content: "beta", Version: 2})

	if err := cold.PutMemoryEmbedding("m1", []float32{1, 0}); err != nil {
		t.Fatalf("PutMemoryEmbedding m1 failed: %v", err)
	}
	if err := cold.PutMemoryEmbedding("m2", []float32{0, 1}); err != nil {
		t.Fatalf("PutMemoryEmbedding m2 failed: %v", err)
	}

	tieredObjs := storage.NewTieredObjectStoreWithEmbedder(
		storage.NewHotObjectCache(100),
		nil,
		nil,
		cold,
		embedder,
		schemas.DefaultAlgorithmConfig().HotTierSalienceThreshold,
	)

	dp, err := NewTieredDataPlaneWithEmbedderAndConfig(
		tieredObjs,
		embedder,
		schemas.DefaultAlgorithmConfig(),
	)
	if err != nil {
		t.Fatalf("NewTieredDataPlaneWithEmbedderAndConfig failed: %v", err)
	}

	out := dp.Search(SearchInput{
		QueryText:      "q",
		TopK:           2,
		IncludeCold:    true,
		Namespace:      "",
		TimeFromUnixTS: 0,
		TimeToUnixTS:   0,
	})

	if len(out.ObjectIDs) == 0 {
		t.Fatal("expected cold vector search to return at least one result")
	}

	if out.ObjectIDs[0] != "m1" {
		t.Fatalf("expected m1 ranked first, got %+v", out.ObjectIDs)
	}
	if out.ColdSearchMode != "vector" {
		t.Fatalf("expected ColdSearchMode=vector, got %q", out.ColdSearchMode)
	}
}

func TestTieredDataPlane_Search_IncludeColdFallsBackToLexicalWhenNoEmbedder(t *testing.T) {
	cold := storage.NewInMemoryColdStore()
	cold.PutMemory(schemas.Memory{
		MemoryID: "mem_cold_lexical",
		Content:  "unique lexical marker",
		Version:  1,
	})

	objs := storage.NewTieredObjectStore(
		storage.NewHotObjectCache(100),
		nil,
		nil,
		cold,
	)

	p := NewTieredDataPlane(objs)

	out := p.Search(SearchInput{
		QueryText:   "unique lexical marker",
		TopK:        1,
		IncludeCold: true,
	})

	if len(out.ObjectIDs) == 0 {
		t.Fatal("expected lexical cold search to return at least one result")
	}
	if out.ObjectIDs[0] != "mem_cold_lexical" {
		t.Fatalf("expected mem_cold_lexical, got %+v", out.ObjectIDs)
	}
	if out.ColdSearchMode != "lexical" {
		t.Fatalf("expected ColdSearchMode=lexical, got %q", out.ColdSearchMode)
	}
}

func TestTieredDataPlane_Search_IncludeColdWhenHotSatisfiesTopK_UsesVectorSearch(t *testing.T) {
	embedder := &fakeQueryEmbedder{
		vec: []float32{1, 0},
	}

	cold := storage.NewInMemoryColdStore()
	cold.PutMemory(schemas.Memory{
		MemoryID: "mem_only_cold_vec",
		Content:  "cold vector target",
		Version:  1,
	})
	if err := cold.PutMemoryEmbedding("mem_only_cold_vec", []float32{1, 0}); err != nil {
		t.Fatalf("PutMemoryEmbedding failed: %v", err)
	}

	tieredObjs := storage.NewTieredObjectStoreWithEmbedder(
		storage.NewHotObjectCache(100),
		nil,
		nil,
		cold,
		embedder,
		schemas.DefaultAlgorithmConfig().HotTierSalienceThreshold,
	)

	dp, err := NewTieredDataPlaneWithEmbedderAndConfig(
		tieredObjs,
		embedder,
		schemas.DefaultAlgorithmConfig(),
	)
	if err != nil {
		t.Fatalf("NewTieredDataPlaneWithEmbedderAndConfig failed: %v", err)
	}

	// 让 hot tier 先满足 TopK
	dp.hot.InsertObject("mem_hot_1", "q hot one", nil, "", 1)
	dp.hot.InsertObject("mem_hot_2", "q hot two", nil, "", 2)

	out := dp.Search(SearchInput{
		QueryText:   "q",
		TopK:        2,
		IncludeCold: true,
	})

	if out.Tier != "hot+cold" {
		t.Fatalf("tier: got %q, want hot+cold", out.Tier)
	}

	if out.ColdSearchMode != "vector" {
		t.Fatalf("expected ColdSearchMode=vector, got %q", out.ColdSearchMode)
	}

	// Because mergeOutputs truncates to TopK, the cold ID may not appear in the final ObjectIDs,
	//so we don't force an assertion that the cold ID will definitely appear; we only verify that the fast-path has not been broken.
	if len(out.ObjectIDs) != 2 {
		t.Fatalf("expected merged result length 2, got %d (%+v)", len(out.ObjectIDs), out.ObjectIDs)
	}
}

// HNSW is earlier than Vector
func TestTieredDataPlane_resolveColdIDs_PrefersHNSWOverVector(t *testing.T) {
	tp := &TieredDataPlane{
		embedder: mockEmbedder{
			vec: []float32{1, 2, 3},
		},
		coldHNSWSearch: func(queryVec []float32, topK int) []string {
			return []string{"mem_hnsw"}
		},
		coldVectorSearch: func(queryVec []float32, topK int) []string {
			return []string{"mem_vector"}
		},
		coldSearch: func(query string, topK int) []string {
			return []string{"mem_lexical"}
		},
	}

	got, mode := tp.resolveColdIDs(SearchInput{
		QueryText: "hello",
		TopK:      5,
	})

	if len(got) != 1 || got[0] != "mem_hnsw" {
		t.Fatalf("expected HNSW result [mem_hnsw], got %v", got)
	}
	if mode != "hnsw" {
		t.Fatalf("expected mode hnsw, got %q", mode)
	}
}

// when HNSW no results,return Vector
func TestTieredDataPlane_resolveColdIDs_FallsBackToVectorWhenHNSWEmpty(t *testing.T) {
	tp := &TieredDataPlane{
		embedder: mockEmbedder{
			vec: []float32{1, 2, 3},
		},
		coldHNSWSearch: func(queryVec []float32, topK int) []string {
			return nil
		},
		coldVectorSearch: func(queryVec []float32, topK int) []string {
			return []string{"mem_vector"}
		},
		coldSearch: func(query string, topK int) []string {
			return []string{"mem_lexical"}
		},
	}

	got, mode := tp.resolveColdIDs(SearchInput{
		QueryText: "hello",
		TopK:      5,
	})

	if len(got) != 1 || got[0] != "mem_vector" {
		t.Fatalf("expected vector fallback [mem_vector], got %v", got)
	}
	if mode != "vector" {
		t.Fatalf("expected mode vector, got %q", mode)
	}
}

// no embedder→return lexical
func TestTieredDataPlane_resolveColdIDs_FallsBackToLexicalWithoutEmbedder(t *testing.T) {
	tp := &TieredDataPlane{
		embedder: nil,
		coldSearch: func(query string, topK int) []string {
			return []string{"mem_lexical"}
		},
	}

	got, mode := tp.resolveColdIDs(SearchInput{
		QueryText: "hello",
		TopK:      5,
	})

	if len(got) != 1 || got[0] != "mem_lexical" {
		t.Fatalf("expected lexical fallback [mem_lexical], got %v", got)
	}
	if mode != "lexical" {
		t.Fatalf("expected mode lexical, got %q", mode)
	}
}

func TestTieredDataPlane_Search_IncludeCold_10KArchivedCorrectness(t *testing.T) {
	embedder := &fakeQueryEmbedder{vec: []float32{1, 0}}

	cold := storage.NewInMemoryColdStore()
	targetIDs := map[string]struct{}{}

	for i := 0; i < 10000; i++ {
		id := fmt.Sprintf("mem_cold_10k_%05d", i)
		vec := []float32{0, 1}
		if i < 100 {
			vec = []float32{1, 0}
			targetIDs[id] = struct{}{}
		}
		cold.PutMemory(schemas.Memory{
			MemoryID: id,
			Content:  "cold archived test payload",
			Version:  int64(i + 1),
		})
		if err := cold.PutMemoryEmbedding(id, vec); err != nil {
			t.Fatalf("PutMemoryEmbedding(%s) failed: %v", id, err)
		}
	}

	tieredObjs := storage.NewTieredObjectStoreWithEmbedder(
		storage.NewHotObjectCache(100),
		nil,
		nil,
		cold,
		embedder,
		schemas.DefaultAlgorithmConfig().HotTierSalienceThreshold,
	)

	dp, err := NewTieredDataPlaneWithEmbedderAndConfig(
		tieredObjs,
		embedder,
		schemas.DefaultAlgorithmConfig(),
	)
	if err != nil {
		t.Fatalf("NewTieredDataPlaneWithEmbedderAndConfig failed: %v", err)
	}

	out := dp.Search(SearchInput{
		QueryText:   "cold archived test payload",
		TopK:        10,
		IncludeCold: true,
	})

	if len(out.ObjectIDs) != 10 {
		t.Fatalf("expected topK=10 results, got %d", len(out.ObjectIDs))
	}
	for i, id := range out.ObjectIDs {
		if _, ok := targetIDs[id]; !ok {
			t.Fatalf("unexpected non-target result at rank %d: %s", i, id)
		}
	}
	if out.ColdSearchMode != "vector" {
		t.Fatalf("expected ColdSearchMode=vector, got %q", out.ColdSearchMode)
	}
}

func BenchmarkTieredDataPlane_Search_IncludeCold_10KArchived(b *testing.B) {
	embedder := &fakeQueryEmbedder{vec: []float32{1, 0}}
	cold := storage.NewInMemoryColdStore()

	for i := 0; i < 10000; i++ {
		id := fmt.Sprintf("bench_cold_10k_%05d", i)
		vec := []float32{0, 1}
		if i < 100 {
			vec = []float32{1, 0}
		}
		cold.PutMemory(schemas.Memory{
			MemoryID: id,
			Content:  "cold benchmark payload",
			Version:  int64(i + 1),
		})
		_ = cold.PutMemoryEmbedding(id, vec)
	}

	tieredObjs := storage.NewTieredObjectStoreWithEmbedder(
		storage.NewHotObjectCache(100),
		nil,
		nil,
		cold,
		embedder,
		schemas.DefaultAlgorithmConfig().HotTierSalienceThreshold,
	)
	dp, err := NewTieredDataPlaneWithEmbedderAndConfig(tieredObjs, embedder, schemas.DefaultAlgorithmConfig())
	if err != nil {
		b.Fatalf("NewTieredDataPlaneWithEmbedderAndConfig failed: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		out := dp.Search(SearchInput{
			QueryText:   "cold benchmark payload",
			TopK:        10,
			IncludeCold: true,
		})
		if len(out.ObjectIDs) != 10 {
			b.Fatalf("expected 10 results, got %d", len(out.ObjectIDs))
		}
		b.ReportMetric(float64(time.Since(start).Microseconds())/1000.0, "ms/op-observed")
	}
}

type mockEmbedder struct {
	vec []float32
	err error
}

func (m mockEmbedder) Generate(text string) ([]float32, error) {
	return m.vec, m.err
}

func (m mockEmbedder) Dim() int {
	return len(m.vec)
}

func (m mockEmbedder) Reset() {}
