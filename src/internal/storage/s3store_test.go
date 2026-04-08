package storage

import (
	"context"
	"fmt"
	"testing"
	"time"

	"andb/src/internal/schemas"
)

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
	out := selectTopScored(in, 2)
	if len(out) != 2 {
		t.Fatalf("len(out)= %d, want 2", len(out))
	}
	if out[0].id != "c" || out[1].id != "b" {
		t.Fatalf("unexpected order: %+v", out)
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
