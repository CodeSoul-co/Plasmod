package storage

import (
	"context"
	"fmt"
	"testing"
	"time"

	"andb/src/internal/schemas"
)

func TestLoadS3ColdSearchConfigFromEnv_NewLimitVars(t *testing.T) {
	t.Setenv("S3_COLD_MAX_PAGES", "7")
	t.Setenv("S3_COLD_MAX_CANDIDATES", "321")
	t.Setenv("S3_COLDSEARCH_MAX_KEYS", "999") // should be ignored when new var exists

	cfg := loadS3ColdSearchConfigFromEnv()
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

	cfg := loadS3ColdSearchConfigFromEnv()
	if cfg.maxCandidates != 654 {
		t.Fatalf("maxCandidates = %d, want 654", cfg.maxCandidates)
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
