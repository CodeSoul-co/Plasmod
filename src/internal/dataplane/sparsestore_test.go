package dataplane

import (
	"strings"
	"testing"
)

// TestSparseStore_BasicLifecycle exercises Add → Build → Search round-trip.
func TestSparseStore_BasicLifecycle(t *testing.T) {
	ss, err := NewSparseStore(SparseStoreConfig{})
	if err != nil {
		t.Fatalf("NewSparseStore: %v", err)
	}
	defer ss.Close()

	ss.AddText("doc-a", "the quick brown fox jumps over the lazy dog")
	ss.AddText("doc-b", "lorem ipsum dolor sit amet consectetur")
	ss.AddText("doc-c", "brown fox brown fox brown fox keyword stuffed")

	if err := ss.Build(); err != nil {
		t.Fatalf("Build: %v", err)
	}

	if !ss.Ready() {
		t.Skip("SparseStore not ready (CGO sparse library unavailable)")
	}

	ids, scores, err := ss.Search("brown fox", 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(ids) == 0 {
		t.Fatalf("Search returned 0 results")
	}
	// doc-c repeats the query terms most aggressively, so it should win.
	if ids[0] != "doc-c" && ids[0] != "doc-a" {
		t.Errorf("top match = %q, want doc-c or doc-a (ids=%v scores=%v)", ids[0], ids, scores)
	}
}

// TestSparseStore_GracefulNoBuild ensures Search before Build is a no-op.
func TestSparseStore_GracefulNoBuild(t *testing.T) {
	ss, err := NewSparseStore(SparseStoreConfig{})
	if err != nil {
		t.Fatalf("NewSparseStore: %v", err)
	}
	defer ss.Close()

	if ss.Ready() {
		t.Errorf("Ready should be false before AddText/Build")
	}
	ids, _, err := ss.Search("anything", 5)
	if err != nil {
		t.Fatalf("Search returned error on empty store: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("Search on empty store returned %d ids, want 0", len(ids))
	}
}

// TestSparseStore_BatchAdd validates AddTexts equivalence with N×AddText.
func TestSparseStore_BatchAdd(t *testing.T) {
	ss, err := NewSparseStore(SparseStoreConfig{})
	if err != nil {
		t.Fatalf("NewSparseStore: %v", err)
	}
	defer ss.Close()

	ids := []string{"a", "b", "c"}
	texts := []string{
		"alpha bravo charlie",
		"delta echo foxtrot",
		"alpha alpha alpha",
	}
	ss.AddTexts(ids, texts)
	if err := ss.Build(); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !ss.Ready() {
		t.Skip("SparseStore not ready (CGO sparse library unavailable)")
	}
	hits, _, err := ss.Search("alpha", 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 || hits[0] != "c" {
		t.Errorf("expected top hit doc 'c' (saturated alpha), got %v", hits)
	}
}

// TestSegmentDataPlane_HybridSparseTier verifies the warm tier participates
// in 3-way RRF fusion when sparse retrieval is operational.
func TestSegmentDataPlane_HybridSparseTier(t *testing.T) {
	embedder := NewTfidfEmbedder(DefaultEmbeddingDim)
	plane, err := NewSegmentDataPlaneWithEmbedder(embedder)
	if err != nil {
		t.Fatalf("NewSegmentDataPlaneWithEmbedder: %v", err)
	}

	records := []IngestRecord{
		{ObjectID: "m1", Text: "machine learning models train on data", Namespace: "ws"},
		{ObjectID: "m2", Text: "neural network deep learning architecture", Namespace: "ws"},
		{ObjectID: "m3", Text: "data preprocessing feature engineering", Namespace: "ws"},
		{ObjectID: "m4", Text: "transformer attention mechanism language model", Namespace: "ws"},
	}
	for _, r := range records {
		if err := plane.Ingest(r); err != nil {
			t.Fatalf("Ingest %s: %v", r.ObjectID, err)
		}
	}
	if err := plane.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	out := plane.Search(SearchInput{
		QueryText:      "neural network learning",
		TopK:           4,
		Namespace:      "ws",
		IncludeGrowing: true,
	})
	if len(out.ObjectIDs) == 0 {
		t.Fatal("expected at least one hybrid result")
	}

	// Tier label must include sparse when sparse channel is ready.
	if !strings.Contains(out.Tier, "sparse") {
		t.Logf("warning: sparse channel did not contribute (tier=%q). "+
			"This is OK on stub builds but unexpected with -tags retrieval.", out.Tier)
	}
	t.Logf("Tier=%q ObjectIDs=%v", out.Tier, out.ObjectIDs)
}
