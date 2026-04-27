//go:build retrieval
// +build retrieval

// sparse_bridge_test.go — integration tests for the SparseRetriever CGO bridge.
//
// Run: go test -v -tags retrieval ./src/internal/dataplane/retrievalplane/ \
//           -run TestSparse
//
// Requires libplasmod_retrieval.{so,dylib} on the loader path.

package retrievalplane

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSparseRetriever_BuildSearch validates Build + Search end-to-end.
func TestSparseRetriever_BuildSearch(t *testing.T) {
	sr, err := NewSparseRetriever(SparseInvertedIndex)
	if err != nil {
		t.Fatalf("NewSparseRetriever: %v", err)
	}
	defer sr.Close()

	docs := []SparseVector{
		// doc 0: term 1 strong, term 7 weak
		{Indices: []uint32{1, 7}, Values: []float32{1.0, 0.1}},
		// doc 1: term 2 strong, term 7 weak
		{Indices: []uint32{2, 7}, Values: []float32{1.0, 0.1}},
		// doc 2: term 7 strong, others zero
		{Indices: []uint32{7}, Values: []float32{1.0}},
	}
	if err := sr.Build(docs); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got, want := sr.Count(), int64(3); got != want {
		t.Errorf("Count = %d, want %d", got, want)
	}
	if !sr.IsReady() {
		t.Errorf("IsReady = false after Build")
	}

	// Query: pure term 7 — doc 2 should rank first.
	q := SparseVector{Indices: []uint32{7}, Values: []float32{1.0}}
	ids, scores, err := sr.Search(q, 3, nil)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(ids) == 0 {
		t.Fatalf("Search returned 0 results")
	}
	if ids[0] != 2 {
		t.Errorf("Top doc = %d, want 2 (ids=%v scores=%v)", ids[0], ids, scores)
	}

	// Query: pure term 1 — doc 0 should rank first.
	q2 := SparseVector{Indices: []uint32{1}, Values: []float32{1.0}}
	ids, _, err = sr.Search(q2, 3, nil)
	if err != nil {
		t.Fatalf("Search term 1: %v", err)
	}
	if len(ids) == 0 || ids[0] != 0 {
		t.Errorf("Top doc for term 1 = %v, want 0", ids)
	}
}

// TestSparseRetriever_AddIncremental validates that Add appends without
// resetting the existing index.
func TestSparseRetriever_AddIncremental(t *testing.T) {
	sr, err := NewSparseRetriever(SparseInvertedIndex)
	if err != nil {
		t.Fatalf("NewSparseRetriever: %v", err)
	}
	defer sr.Close()

	first := []SparseVector{
		{Indices: []uint32{1}, Values: []float32{1.0}},
	}
	if err := sr.Build(first); err != nil {
		t.Fatalf("Build: %v", err)
	}
	more := []SparseVector{
		{Indices: []uint32{1, 2}, Values: []float32{0.5, 1.0}},
		{Indices: []uint32{2}, Values: []float32{1.0}},
	}
	if err := sr.Add(more); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got, want := sr.Count(), int64(3); got != want {
		t.Errorf("Count after Add = %d, want %d", got, want)
	}
}

// TestSparseRetriever_Filter validates that the filter bitset masks results.
func TestSparseRetriever_Filter(t *testing.T) {
	sr, err := NewSparseRetriever(SparseInvertedIndex)
	if err != nil {
		t.Fatalf("NewSparseRetriever: %v", err)
	}
	defer sr.Close()

	docs := []SparseVector{
		{Indices: []uint32{42}, Values: []float32{1.0}},
		{Indices: []uint32{42}, Values: []float32{0.5}},
		{Indices: []uint32{42}, Values: []float32{0.25}},
	}
	if err := sr.Build(docs); err != nil {
		t.Fatalf("Build: %v", err)
	}

	q := SparseVector{Indices: []uint32{42}, Values: []float32{1.0}}

	// First: no filter — top doc should be 0.
	ids, _, err := sr.Search(q, 5, nil)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(ids) == 0 || ids[0] != 0 {
		t.Errorf("unfiltered top = %v, want 0", ids)
	}

	// Now filter doc 0: bit 0 set → masked.
	mask := []byte{0x01} // doc 0 filtered
	ids, _, err = sr.Search(q, 5, mask)
	if err != nil {
		t.Fatalf("Search (filtered): %v", err)
	}
	for _, id := range ids {
		if id == 0 {
			t.Errorf("filtered doc 0 still in results: %v", ids)
		}
	}
	if len(ids) == 0 || ids[0] != 1 {
		t.Errorf("filtered top = %v, want 1", ids)
	}
}

// TestSparseRetriever_TextToSparseVector validates the FNV-1a tokeniser
// produces consistent output between document and query paths.
func TestSparseRetriever_TextToSparseVector(t *testing.T) {
	sv, err := TextToSparseVector("hello world hello")
	if err != nil {
		t.Fatalf("TextToSparseVector: %v", err)
	}
	if len(sv.Indices) != len(sv.Values) {
		t.Fatalf("indices/values length mismatch: %d vs %d", len(sv.Indices), len(sv.Values))
	}
	if len(sv.Indices) != 2 { // hello + world
		t.Errorf("expected 2 distinct tokens, got %d", len(sv.Indices))
	}

	empty, err := TextToSparseVector("")
	if err != nil {
		t.Fatalf("TextToSparseVector(\"\"): %v", err)
	}
	if len(empty.Indices) != 0 {
		t.Errorf("empty text → expected 0 indices, got %d", len(empty.Indices))
	}

	// Round-trip: index a doc generated from text, query with the same text,
	// expect a hit.
	sr, err := NewSparseRetriever(SparseInvertedIndex)
	if err != nil {
		t.Fatalf("NewSparseRetriever: %v", err)
	}
	defer sr.Close()

	doc, _ := TextToSparseVector("the quick brown fox")
	if err := sr.Build([]SparseVector{doc}); err != nil {
		t.Fatalf("Build: %v", err)
	}

	q, _ := TextToSparseVector("brown fox")
	ids, scores, err := sr.Search(q, 1, nil)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(ids) != 1 || ids[0] != 0 {
		t.Errorf("text round-trip failed: ids=%v scores=%v", ids, scores)
	}
	if scores[0] <= 0 {
		t.Errorf("expected positive score, got %v", scores[0])
	}
}

// TestSparseRetriever_SaveLoad validates persistence round-trip.
func TestSparseRetriever_SaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sparse.idx")

	// Build + save.
	src, err := NewSparseRetriever(SparseInvertedIndex)
	if err != nil {
		t.Fatalf("NewSparseRetriever: %v", err)
	}
	docs := []SparseVector{
		{Indices: []uint32{1, 2}, Values: []float32{0.7, 0.3}},
		{Indices: []uint32{2, 3}, Values: []float32{0.5, 0.5}},
	}
	if err := src.Build(docs); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := src.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	src.Close()

	// Verify file written.
	if info, err := os.Stat(path); err != nil || info.Size() == 0 {
		t.Fatalf("expected non-empty file at %s, err=%v", path, err)
	}

	// Load into fresh instance, query.
	dst, err := NewSparseRetriever(SparseInvertedIndex)
	if err != nil {
		t.Fatalf("NewSparseRetriever (load): %v", err)
	}
	defer dst.Close()
	if err := dst.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := dst.Count(), int64(2); got != want {
		t.Errorf("Count after Load = %d, want %d", got, want)
	}

	q := SparseVector{Indices: []uint32{1}, Values: []float32{1.0}}
	ids, _, err := dst.Search(q, 2, nil)
	if err != nil {
		t.Fatalf("Search after Load: %v", err)
	}
	if len(ids) == 0 || ids[0] != 0 {
		t.Errorf("Search after Load: top=%v, want 0", ids)
	}
}

// TestSparseRetriever_ClosedHandle ensures methods on a closed retriever
// fail cleanly without panicking.
func TestSparseRetriever_ClosedHandle(t *testing.T) {
	sr, err := NewSparseRetriever(SparseInvertedIndex)
	if err != nil {
		t.Fatalf("NewSparseRetriever: %v", err)
	}
	sr.Close()

	if err := sr.Build(nil); err == nil {
		t.Errorf("Build on closed handle: expected error")
	}
	if _, _, err := sr.Search(SparseVector{}, 1, nil); err == nil {
		t.Errorf("Search on closed handle: expected error")
	}
	if sr.IsReady() {
		t.Errorf("IsReady on closed handle: expected false")
	}
	if sr.Count() != -1 {
		t.Errorf("Count on closed handle: expected -1, got %d", sr.Count())
	}
}

// TestSparseRetriever_WANDVariant validates that SparseWAND can be created and
// behaves like SparseInvertedIndex (current C++ impl shares posting lists).
func TestSparseRetriever_WANDVariant(t *testing.T) {
	sr, err := NewSparseRetriever(SparseWAND)
	if err != nil {
		t.Fatalf("NewSparseRetriever(WAND): %v", err)
	}
	defer sr.Close()
	if got, want := sr.IndexType(), SparseWAND; got != want {
		t.Errorf("IndexType = %s, want %s", got, want)
	}
	docs := []SparseVector{
		{Indices: []uint32{5}, Values: []float32{1.0}},
	}
	if err := sr.Build(docs); err != nil {
		t.Fatalf("Build (WAND): %v", err)
	}
	q := SparseVector{Indices: []uint32{5}, Values: []float32{1.0}}
	ids, _, err := sr.Search(q, 1, nil)
	if err != nil {
		t.Fatalf("Search (WAND): %v", err)
	}
	if len(ids) != 1 || ids[0] != 0 {
		t.Errorf("WAND search: ids=%v, want [0]", ids)
	}
}
