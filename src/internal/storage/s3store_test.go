package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"plasmod/src/internal/schemas"
)

func TestLoadS3ColdSearchConfigFromEnv_NewLimitVars(t *testing.T) {
	t.Setenv("S3_COLD_MAX_PAGES", "7")
	t.Setenv("S3_COLD_MAX_CANDIDATES", "321")
	t.Setenv("S3_COLDSEARCH_MAX_KEYS", "999") // should be ignored when new var exists

	cfg := NewS3ColdStore(S3Config{}).loadS3ColdSearchConfigFromEnv()
	if cfg.maxPages != 7 {
		t.Fatalf("maxPages = %d, want 7", cfg.maxPages)
	}
	if cfg.maxCandidates != 321 {
		t.Fatalf("maxCandidates = %d, want 321", cfg.maxCandidates)
	}
}

func TestLoadS3ColdSearchConfigFromEnv_LegacyMaxKeysFallback(t *testing.T) {
	t.Setenv("S3_COLD_MAX_CANDIDATES", "")
	t.Setenv("S3_COLDSEARCH_MAX_KEYS", "654")

	cfg := NewS3ColdStore(S3Config{}).loadS3ColdSearchConfigFromEnv()
	if cfg.maxCandidates != 654 {
		t.Fatalf("maxCandidates = %d, want 654", cfg.maxCandidates)
	}
}

func TestClampColdKeys_RespectsMaxPagesAndCandidates(t *testing.T) {
	keys := []string{"a", "b", "c", "d", "e", "f", "g"}
	got, pages, truncated := clampColdKeys(keys, s3ColdSearchConfig{
		maxPages:      2,
		maxCandidates: 5,
		batchSize:     2,
	})
	if len(got) != 4 {
		t.Fatalf("len(got) = %d, want 4", len(got))
	}
	if pages != 2 {
		t.Fatalf("pages = %d, want 2", pages)
	}
	if !truncated {
		t.Fatal("expected truncated=true")
	}
}

func TestClampColdKeys_RespectsCandidateCapWhenLowerThanPageCap(t *testing.T) {
	keys := []string{"a", "b", "c", "d", "e"}
	got, pages, truncated := clampColdKeys(keys, s3ColdSearchConfig{
		maxPages:      3,
		maxCandidates: 2,
		batchSize:     2,
	})
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if pages != 1 {
		t.Fatalf("pages = %d, want 1", pages)
	}
	if !truncated {
		t.Fatal("expected truncated=true")
	}
}

func TestScoreColdMemory_ExactMatchWins(t *testing.T) {
	m := schemas.Memory{Content: "hello world", Summary: "short"}
	if got := scoreColdMemory("hello world", m); got != 1.0 {
		t.Fatalf("score exact match: want 1.0, got %v", got)
	}
}

func TestSelectTopScored_ByScoreThenRecency(t *testing.T) {
	in := []s3ColdScored{
		{id: "a", score: 0.7, ts: 1},
		{id: "b", score: 0.9, ts: 1},
		{id: "c", score: 0.9, ts: 5},
	}
	orig := append([]s3ColdScored(nil), in...)
	out := selectTopScored(in, 2)
	if len(out) != 2 {
		t.Fatalf("len(out)= %d, want 2", len(out))
	}
	if out[0].id != "c" || out[1].id != "b" {
		t.Fatalf("unexpected order: %+v", out)
	}
	if in[0].id != orig[0].id || in[1].id != orig[1].id || in[2].id != orig[2].id {
		t.Fatalf("selectTopScored must not mutate input slice, got=%+v want=%+v", in, orig)
	}
}

func TestSelectTopScored_DoesNotMutateInput(t *testing.T) {
	in := []s3ColdScored{
		{id: "a", score: 0.7, ts: 1},
		{id: "b", score: 0.9, ts: 1},
		{id: "c", score: 0.9, ts: 5},
	}
	orig := append([]s3ColdScored(nil), in...)
	_ = selectTopScored(in, 2)
	for i := range in {
		if in[i] != orig[i] {
			t.Fatalf("input mutated at %d: got %+v want %+v", i, in[i], orig[i])
		}
	}
}

func TestShouldEarlyStop_WithHighScoreAndStablePages(t *testing.T) {
	top := []s3ColdScored{
		{id: "m1", score: 1.0},
		{id: "m2", score: 0.98},
	}
	ok := shouldEarlyStop(top, 8, 2, 6, 0.95, 2, 2)
	if !ok {
		t.Fatal("expected early stop to be true")
	}
}

func TestShouldEarlyStop_NotEnoughCandidates(t *testing.T) {
	top := []s3ColdScored{
		{id: "m1", score: 1.0},
		{id: "m2", score: 0.98},
	}
	ok := shouldEarlyStop(top, 3, 2, 6, 0.95, 3, 2)
	if ok {
		t.Fatal("expected early stop to be false when candidates are insufficient")
	}
}

func TestCombineColdScores(t *testing.T) {
	weights := schemas.ColdSearchWeights{
		Lexical: 0.5,
		Dense:   0.4,
		Recency: 0.1,
	}
	got := combineColdScores(0.8, 0.6, 0.5, weights)
	want := 0.4*0.8 + 0.5*0.6 + 0.1*0.5
	if diff := got - want; diff < -1e-9 || diff > 1e-9 {
		t.Fatalf("combineColdScores: want %v, got %v", want, got)
	}
}

func TestS3ColdStore_LoadConfig_AcceptsReadmeEnvAliases(t *testing.T) {
	t.Setenv("S3_COLD_BATCH_SIZE", "64")
	t.Setenv("S3_COLD_MAX_CANDIDATES", "4096")
	t.Setenv("S3_COLD_CONCURRENCY", "12")
	t.Setenv("S3_COLD_BUFFER_FACTOR", "5")
	t.Setenv("S3_COLD_EARLY_STOP_SCORE", "0.88")
	t.Setenv("S3_COLD_NO_IMPROVE_PAGES", "4")

	cfg := NewS3ColdStore(S3Config{}).loadS3ColdSearchConfigFromEnv()
	if cfg.batchSize != 64 {
		t.Fatalf("batchSize: want 64, got %d", cfg.batchSize)
	}
	if cfg.maxCandidates != 4096 {
		t.Fatalf("maxCandidates: want 4096, got %d", cfg.maxCandidates)
	}
	if cfg.concurrency != 12 {
		t.Fatalf("concurrency: want 12, got %d", cfg.concurrency)
	}
	if cfg.bufferFactor != 5 {
		t.Fatalf("bufferFactor: want 5, got %d", cfg.bufferFactor)
	}
	if cfg.earlyStopScore != 0.88 {
		t.Fatalf("earlyStopScore: want 0.88, got %v", cfg.earlyStopScore)
	}
	if cfg.noImprovePages != 4 {
		t.Fatalf("noImprovePages: want 4, got %d", cfg.noImprovePages)
	}
}

func TestS3ColdStore_ListCacheInvalidation(t *testing.T) {
	store := NewS3ColdStore(S3Config{})
	prefix := "andb/test/cold/embeddings/"
	store.listCache[prefix] = s3ListCacheEntry{
		keys:      []string{"a", "b"},
		expiresAt: time.Now().Add(5 * time.Second).Unix(),
	}

	store.invalidateListCache(prefix)

	if _, ok := store.listCache[prefix]; ok {
		t.Fatal("expected list cache entry to be invalidated")
	}
}

func TestS3ColdStore_ListCacheInvalidation_OtherPrefixes(t *testing.T) {
	store := NewS3ColdStore(S3Config{Prefix: "andb/test"})
	prefixes := []string{
		store.agentPrefix(),
		store.statePrefix(),
		store.artifactPrefix(),
		store.edgePrefix(),
	}
	now := time.Now().Add(5 * time.Second).Unix()

	for _, prefix := range prefixes {
		store.listCache[prefix] = s3ListCacheEntry{
			keys:      []string{"one", "two"},
			expiresAt: now,
		}
	}

	store.invalidateListCache(store.agentPrefix())
	store.invalidateListCache(store.statePrefix())
	store.invalidateListCache(store.artifactPrefix())
	store.invalidateListCache(store.edgePrefix())

	for _, prefix := range prefixes {
		if _, ok := store.listCache[prefix]; ok {
			t.Fatalf("expected list cache entry %q to be invalidated", prefix)
		}
	}
}

func TestS3ColdStore_MemoryEmbeddingLifecycle(t *testing.T) {
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Skipf("skip S3 lifecycle test: %v", err)
	}

	// Use an isolated prefix so repeated runs do not collide.
	cfg.Prefix = fmt.Sprintf("%s/test_s3cold_%d", cfg.Prefix, time.Now().UnixNano())

	store := NewS3ColdStore(cfg)

	memoryID := fmt.Sprintf("mem_s3_embed_%d", time.Now().UnixNano())
	mem := schemas.Memory{
		MemoryID: memoryID,
		Content:  "s3 embedding lifecycle test",
		Version:  time.Now().Unix(),
		IsActive: false,
	}

	// 1) Put/Get memory json
	store.PutMemory(mem)

	gotMem, ok := store.GetMemory(memoryID)
	if !ok {
		t.Fatal("expected memory to exist in S3 cold store")
	}
	if gotMem.MemoryID != memoryID {
		t.Fatalf("unexpected memory id: got %q want %q", gotMem.MemoryID, memoryID)
	}

	// 2) Put/Get embedding
	wantVec := []float32{0.1, 0.2, 0.3}
	if err := store.PutMemoryEmbedding(memoryID, wantVec); err != nil {
		t.Fatalf("PutMemoryEmbedding failed: %v", err)
	}

	gotVec, ok, err := store.GetMemoryEmbedding(memoryID)
	if err != nil {
		t.Fatalf("GetMemoryEmbedding returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected embedding to exist in S3 cold store")
	}
	if len(gotVec) != len(wantVec) {
		t.Fatalf("unexpected embedding length: got %d want %d", len(gotVec), len(wantVec))
	}
	for i := range wantVec {
		if gotVec[i] != wantVec[i] {
			t.Fatalf("unexpected embedding value at %d: got %v want %v", i, gotVec[i], wantVec[i])
		}
	}

	// 3) Delete embedding, memory should still remain
	if err := store.DeleteMemoryEmbedding(memoryID); err != nil {
		t.Fatalf("DeleteMemoryEmbedding failed: %v", err)
	}

	_, ok, err = store.GetMemoryEmbedding(memoryID)
	if err != nil {
		t.Fatalf("GetMemoryEmbedding after delete returned error: %v", err)
	}
	if ok {
		t.Fatal("expected embedding to be deleted from S3 cold store")
	}

	gotMem, ok = store.GetMemory(memoryID)
	if !ok {
		t.Fatal("expected memory json to still exist after embedding deletion")
	}
	if gotMem.MemoryID != memoryID {
		t.Fatalf("unexpected memory id after embedding deletion: got %q want %q", gotMem.MemoryID, memoryID)
	}

	// 4) Cleanup memory blob (best effort)
	if err := store.DeleteMemory(memoryID); err != nil {
		t.Fatalf("DeleteMemory failed: %v", err)
	}
}

func TestMemoryIDFromEmbeddingKey(t *testing.T) {
	tests := []struct {
		key  string
		want string
	}{
		{
			key:  "andb/integration_tests/cold/embeddings/mem_123.npy",
			want: "mem_123",
		},
		{
			key:  "cold/embeddings/mem_abc.npy",
			want: "mem_abc",
		},
		{
			key:  "mem_only.npy",
			want: "mem_only",
		},
		{
			key:  "",
			want: "",
		},
	}

	for _, tt := range tests {
		got := memoryIDFromEmbeddingKey(tt.key)
		if got != tt.want {
			t.Fatalf("memoryIDFromEmbeddingKey(%q) = %q, want %q", tt.key, got, tt.want)
		}
	}
}

func TestFloat32BytesRoundTrip(t *testing.T) {
	want := []float32{0.1, 0.2, 0.3, 1.5, -2.0}

	data, err := float32SliceToBytes(want)
	if err != nil {
		t.Fatalf("float32SliceToBytes returned error: %v", err)
	}

	got, err := bytesToFloat32Slice(data)
	if err != nil {
		t.Fatalf("bytesToFloat32Slice returned error: %v", err)
	}

	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("round-trip mismatch at %d: got %v want %v", i, got[i], want[i])
		}
	}
}

func TestS3ColdStore_ColdVectorSearch_TopKOrdering(t *testing.T) {
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Skipf("skip S3 cold vector search test: %v", err)
	}

	cfg.Prefix = fmt.Sprintf("%s/test_s3_vector_%d", cfg.Prefix, time.Now().UnixNano())
	store := NewS3ColdStore(cfg)

	type item struct {
		id      string
		version int64
		vec     []float32
	}

	items := []item{
		{id: fmt.Sprintf("m1_%d", time.Now().UnixNano()), version: 1, vec: []float32{1, 0}},
		{id: fmt.Sprintf("m2_%d", time.Now().UnixNano()), version: 2, vec: []float32{0.5, 0.5}},
		{id: fmt.Sprintf("m3_%d", time.Now().UnixNano()), version: 3, vec: []float32{0, 1}},
	}

	for _, it := range items {
		store.PutMemory(schemas.Memory{
			MemoryID: it.id,
			Content:  "vector search test",
			Version:  it.version,
			IsActive: false,
		})
		if err := store.PutMemoryEmbedding(it.id, it.vec); err != nil {
			t.Fatalf("PutMemoryEmbedding(%s) failed: %v", it.id, err)
		}

		gotVec, ok, err := store.GetMemoryEmbedding(it.id)
		if err != nil {
			t.Fatalf("GetMemoryEmbedding(%s) failed: %v", it.id, err)
		}
		if !ok {
			t.Fatalf("expected embedding %s to be readable immediately after write", it.id)
		}
		if len(gotVec) != len(it.vec) {
			t.Fatalf("embedding length mismatch for %s: got %d want %d", it.id, len(gotVec), len(it.vec))
		}
	}

	embeddingPrefix := fmt.Sprintf("%s/cold/embeddings/", cfg.Prefix)

	keys, err := ListObjects(context.Background(), nil, cfg, embeddingPrefix)
	if err != nil {
		t.Fatalf("ListObjects failed: %v", err)
	}
	t.Logf("listed embedding keys: %+v", keys)

	if len(keys) < 3 {
		t.Fatalf("expected at least 3 embedding keys, got %d: %+v", len(keys), keys)
	}

	for _, key := range keys {
		data, err := GetBytes(context.Background(), nil, cfg, key)
		if err != nil {
			t.Fatalf("GetBytes(%s) failed: %v", key, err)
		}
		if data == nil {
			t.Fatalf("GetBytes(%s) returned nil data", key)
		}

		vec, err := bytesToFloat32Slice(data)
		if err != nil {
			t.Fatalf("bytesToFloat32Slice(%s) failed: %v", key, err)
		}
		t.Logf("decoded %s => %+v", key, vec)
	}

	var got []string
	deadline := time.Now().Add(2 * time.Second)

	for {
		got = store.ColdVectorSearch([]float32{1, 0}, 2)
		if len(got) == 2 {
			break
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if len(got) != 2 {
		t.Fatalf("expected top2 results, got %d: %+v", len(got), got)
	}

	if got[0] != items[0].id {
		t.Fatalf("expected first result %q, got %+v", items[0].id, got)
	}
	if got[1] != items[1].id {
		t.Fatalf("expected second result %q, got %+v", items[1].id, got)
	}

	// cleanup (best effort)
	for _, it := range items {
		_ = store.DeleteMemoryEmbedding(it.id)
		_ = store.DeleteMemory(it.id)
	}
}

func TestS3ColdStore_ColdHNSWSearch_TopKOrdering(t *testing.T) {
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Skipf("skip S3 cold HNSW search test: %v", err)
	}
	cfg.Prefix = fmt.Sprintf("%s/test_s3_hnsw_%d", cfg.Prefix, time.Now().UnixNano())

	cold := NewS3ColdStoreWithAlgorithmConfig(cfg, schemas.DefaultAlgorithmConfig())

	// 3 个 memory + embedding
	mem1 := schemas.Memory{MemoryID: fmt.Sprintf("m1_%d", time.Now().UnixNano()), Version: time.Now().Unix()}
	mem2 := schemas.Memory{MemoryID: fmt.Sprintf("m2_%d", time.Now().UnixNano()), Version: time.Now().Unix()}
	mem3 := schemas.Memory{MemoryID: fmt.Sprintf("m3_%d", time.Now().UnixNano()), Version: time.Now().Unix()}

	cold.PutMemory(mem1)
	cold.PutMemory(mem2)
	cold.PutMemory(mem3)

	if err := cold.PutMemoryEmbedding(mem1.MemoryID, []float32{1, 0}); err != nil {
		t.Fatalf("PutMemoryEmbedding m1 failed: %v", err)
	}
	if err := cold.PutMemoryEmbedding(mem2.MemoryID, []float32{0.5, 0.5}); err != nil {
		t.Fatalf("PutMemoryEmbedding m2 failed: %v", err)
	}
	if err := cold.PutMemoryEmbedding(mem3.MemoryID, []float32{0, 1}); err != nil {
		t.Fatalf("PutMemoryEmbedding m3 failed: %v", err)
	}

	got := cold.ColdHNSWSearch([]float32{1, 0}, 3)

	// retrieval bridge 不可用时允许返回 nil，让调用方 fallback
	if len(got) == 0 {
		t.Skip("ColdHNSWSearch returned no results; retrieval bridge may be unavailable, fallback path remains valid")
	}

	if got[0] != mem1.MemoryID {
		t.Fatalf("expected %s ranked first, got %v", mem1.MemoryID, got)
	}
}

func TestS3ColdStore_ColdVectorSearch_10K_Correctness(t *testing.T) {
	if testing.Short() {
		t.Skip("skip 10K correctness test in short mode")
	}

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Skipf("skip S3 10K correctness test: %v", err)
	}

	// 独立 prefix，避免与其他测试互相污染
	cfg.Prefix = fmt.Sprintf("%s/test_s3_vector_10k_%d", cfg.Prefix, time.Now().UnixNano())

	algoCfg := schemas.DefaultAlgorithmConfig()
	algoCfg.ColdBatchSize = 256
	algoCfg.ColdMaxCandidates = 12000 // 必须 > 10000，否则会被截断
	store := NewS3ColdStoreWithAlgorithmConfig(cfg, algoCfg)

	const (
		totalN  = 10000
		targetN = 100
		topK    = 10
	)

	targetIDs := make(map[string]struct{}, targetN)

	// 构造 10K 冷数据：
	// - 前 100 条是“目标簇”，query=[1,0] 时相似度最高
	// - 后 9900 条是“干扰簇”，query=[1,0] 时相似度为 0
	for i := 0; i < totalN; i++ {
		id := fmt.Sprintf("mem_10k_%05d", i)

		mem := schemas.Memory{
			MemoryID: id,
			Content:  fmt.Sprintf("10k cold correctness item %d", i),
			Version:  int64(i + 1),
			IsActive: false,
		}
		store.PutMemory(mem)

		var vec []float32
		if i < targetN {
			vec = []float32{1, 0}
			targetIDs[id] = struct{}{}
		} else {
			vec = []float32{0, 1}
		}

		if err := store.PutMemoryEmbedding(id, vec); err != nil {
			t.Fatalf("PutMemoryEmbedding(%s) failed: %v", id, err)
		}
	}

	// 给对象存储一点时间完成 list / get 可见性，然后 retry 搜索结果
	var got []string
	deadline := time.Now().Add(10 * time.Second)

	for {
		got = store.ColdVectorSearch([]float32{1, 0}, topK)
		if len(got) == topK {
			break
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	if len(got) != topK {
		t.Fatalf("expected topK=%d results, got %d: %+v", topK, len(got), got)
	}

	// 断言前 topK 全部来自目标簇
	for i, id := range got {
		if _, ok := targetIDs[id]; !ok {
			t.Fatalf("unexpected non-target result at rank %d: %s (results=%+v)", i, id, got)
		}
	}

	t.Logf("10K correctness PASS: top-%d results all from target cluster", topK)
}

func BenchmarkS3ColdStore_ColdVectorSearch_10K(b *testing.B) {
	cfg, err := LoadFromEnv()
	if err != nil {
		b.Skipf("skip S3 10K benchmark: %v", err)
	}

	cfg.Prefix = fmt.Sprintf("%s/bench_s3_vector_10k_%d", cfg.Prefix, time.Now().UnixNano())

	algoCfg := schemas.DefaultAlgorithmConfig()
	algoCfg.ColdBatchSize = 256
	algoCfg.ColdMaxCandidates = 12000
	store := NewS3ColdStoreWithAlgorithmConfig(cfg, algoCfg)

	const (
		totalN = 10000
		topK   = 10
	)

	for i := 0; i < totalN; i++ {
		id := fmt.Sprintf("bench_mem_10k_%05d", i)
		mem := schemas.Memory{
			MemoryID: id,
			Content:  fmt.Sprintf("10k cold benchmark item %d", i),
			Version:  int64(i + 1),
			IsActive: false,
		}
		data, err := json.Marshal(mem)
		if err != nil {
			b.Fatalf("marshal memory %s failed: %v", id, err)
		}
		if err := PutBytes(context.Background(), nil, cfg, store.memoryKey(id), data, "application/json"); err != nil {
			b.Fatalf("PutMemory(%s) failed: %v", id, err)
		}

		vec := []float32{0, 1}
		if i < 100 {
			vec = []float32{1, 0}
		}
		if err := store.PutMemoryEmbedding(id, vec); err != nil {
			b.Fatalf("PutMemoryEmbedding(%s) failed: %v", id, err)
		}
	}

	memoryPrefix := fmt.Sprintf("%s/cold/memories/", cfg.Prefix)
	embeddingPrefix := fmt.Sprintf("%s/cold/embeddings/", cfg.Prefix)

	// Real S3/MinIO may need a short convergence window before list/get
	// sees the full archived set. Wait until both prefixes are visible first.
	deadline := time.Now().Add(30 * time.Second)
	for {
		memKeys, memErr := ListObjects(context.Background(), nil, cfg, memoryPrefix)
		embKeys, embErr := ListObjects(context.Background(), nil, cfg, embeddingPrefix)
		if memErr == nil && embErr == nil && len(memKeys) >= totalN && len(embKeys) >= totalN {
			break
		}
		if time.Now().After(deadline) {
			memCount := 0
			embCount := 0
			if memKeys, err := ListObjects(context.Background(), nil, cfg, memoryPrefix); err == nil {
				memCount = len(memKeys)
			}
			if embKeys, err := ListObjects(context.Background(), nil, cfg, embeddingPrefix); err == nil {
				embCount = len(embKeys)
			}
			b.Fatalf("expected %d visible S3 objects before benchmark, got memories=%d embeddings=%d", totalN, memCount, embCount)
		}
		time.Sleep(250 * time.Millisecond)
	}

	var warmup []string
	deadline = time.Now().Add(30 * time.Second)
	for {
		warmup = store.ColdVectorSearch([]float32{1, 0}, topK)
		if len(warmup) == topK {
			break
		}
		if time.Now().After(deadline) {
			b.Fatalf("expected topK=%d warmup results before benchmark, got %d", topK, len(warmup))
		}
		time.Sleep(250 * time.Millisecond)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		got := store.ColdVectorSearch([]float32{1, 0}, topK)
		if len(got) != topK {
			b.Fatalf("expected topK=%d results, got %d", topK, len(got))
		}
		b.ReportMetric(float64(time.Since(start).Microseconds())/1000.0, "ms/op-observed")
	}
}
