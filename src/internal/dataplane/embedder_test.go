package dataplane

import (
	"testing"
)

func TestTfidfEmbedder_Dim(t *testing.T) {
	e := NewTfidfEmbedder(128)
	if e.Dim() != 128 {
		t.Errorf("expected dim 128, got %d", e.Dim())
	}
}

func TestTfidfEmbedder_Generate(t *testing.T) {
	e := NewTfidfEmbedder(256)

	// Non-empty text
	vec, err := e.Generate("hello world example text")
	if err != nil {
		t.Fatalf("Generate non-empty: unexpected error: %v", err)
	}
	if len(vec) != 256 {
		t.Fatalf("expected vector dim 256, got %d", len(vec))
	}

	// Empty text returns zero vector
	vecEmpty, err := e.Generate("")
	if err != nil {
		t.Fatalf("Generate empty: unexpected error: %v", err)
	}
	if len(vecEmpty) != 256 {
		t.Fatalf("expected zero vector dim 256, got %d", len(vecEmpty))
	}
	allZero := true
	for _, v := range vecEmpty {
		if v != 0 {
			allZero = false
			break
		}
	}
	if !allZero {
		t.Error("empty text should produce all-zero vector")
	}
}

func TestTfidfEmbedder_GenerateDeterministic(t *testing.T) {
	e := NewTfidfEmbedder(256)

	// Observe some documents first
	e.ObserveTokens("machine learning neural network")
	e.ObserveTokens("deep learning transformer model")
	e.ObserveTokens("machine learning language model")

	vec1, _ := e.Generate("machine learning")
	vec2, _ := e.Generate("machine learning")

	if len(vec1) != len(vec2) {
		t.Fatal("vectors must have same length")
	}
	for i := range vec1 {
		if vec1[i] != vec2[i] {
			t.Fatalf("Generate is not deterministic at index %d: %f vs %f", i, vec1[i], vec2[i])
		}
	}
}

func TestTfidfEmbedder_ObserveTokens(t *testing.T) {
	e := NewTfidfEmbedder(256)

	if e.totalDocs != 0 {
		t.Errorf("initial totalDocs should be 0, got %d", e.totalDocs)
	}

	e.ObserveTokens("hello world")
	if e.totalDocs != 1 {
		t.Errorf("after 1 ObserveTokens: totalDocs should be 1, got %d", e.totalDocs)
	}

	e.ObserveTokens("hello machine")
	if e.totalDocs != 2 {
		t.Errorf("after 2 ObserveTokens: totalDocs should be 2, got %d", e.totalDocs)
	}

	// Empty text should not increment counter
	e.ObserveTokens("")
	if e.totalDocs != 2 {
		t.Errorf("after empty ObserveTokens: totalDocs should stay 2, got %d", e.totalDocs)
	}
}

func TestTfidfEmbedder_Reset(t *testing.T) {
	e := NewTfidfEmbedder(256)

	e.ObserveTokens("document one")
	e.ObserveTokens("document two")

	if e.totalDocs != 2 {
		t.Fatalf("before reset: totalDocs should be 2, got %d", e.totalDocs)
	}

	e.Reset()

	if e.totalDocs != 0 {
		t.Errorf("after reset: totalDocs should be 0, got %d", e.totalDocs)
	}
}

func TestTfidfEmbedder_ResetClearsDocFreq(t *testing.T) {
	e := NewTfidfEmbedder(256)

	e.ObserveTokens("hello world")
	e.ObserveTokens("hello machine")

	e.Reset()

	e.mu.Lock()
	defer e.mu.Unlock()
	for _, df := range e.docFreq {
		if df != 0 {
			t.Errorf("after reset: all docFreq should be 0, found non-zero")
			break
		}
	}
}

func TestRrfFuse_BothNonEmpty(t *testing.T) {
	lexical := []string{"a", "b", "c"}
	vecIDs := []string{"b", "d", "a"}
	vecScores := []float64{0.9, 0.8, 0.7}

	result := rrfFuse(lexical, vecIDs, vecScores, 5, 60)

	if len(result) == 0 {
		t.Fatal("rrfFuse should return results")
	}

	// "a" and "b" appear in both lists → should rank high
	// Check deduplication: "a" and "b" should appear at most once
	seen := make(map[string]int)
	for _, id := range result {
		seen[id]++
		if seen[id] > 1 {
			t.Errorf("duplicate id in result: %q", id)
		}
	}
}

func TestRrfFuse_LexicalOnly(t *testing.T) {
	lexical := []string{"a", "b", "c"}

	result := rrfFuse(lexical, nil, nil, 3, 60)

	if len(result) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result))
	}
	// Should preserve lexical order roughly (with small RRF variations)
	if result[0] != "a" {
		t.Errorf("expected first result 'a', got %q", result[0])
	}
}

func TestRrfFuse_VectorOnly(t *testing.T) {
	vecIDs := []string{"x", "y", "z"}
	vecScores := []float64{0.9, 0.8, 0.7}

	result := rrfFuse(nil, vecIDs, vecScores, 3, 60)

	if len(result) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result))
	}
}

func TestRrfFuse_TopKLimit(t *testing.T) {
	lexical := []string{"a", "b", "c", "d", "e"}

	result := rrfFuse(lexical, nil, nil, 2, 60)

	if len(result) != 2 {
		t.Fatalf("expected topK=2 results, got %d", len(result))
	}
}

func TestRrfFuse_DefaultTopK(t *testing.T) {
	lexical := []string{}

	result := rrfFuse(lexical, nil, nil, 0, 60)

	// With topK=0, default should be 10
	if len(result) > 10 {
		t.Fatalf("with topK=0, should use default 10, got %d", len(result))
	}
}
