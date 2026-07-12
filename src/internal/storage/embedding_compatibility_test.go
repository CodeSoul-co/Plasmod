package storage

import "testing"

func TestCheckEmbeddingCompatibility(t *testing.T) {
	target := EmbeddingSpec{Family: "gguf:nomic-embed", Dim: 768}
	report := CheckEmbeddingCompatibility([]SegmentRecord{
		{SegmentID: "current", EmbeddingFamily: target.Family, EmbeddingDim: target.Dim},
		{SegmentID: "old-model", EmbeddingFamily: "tfidf", EmbeddingDim: 256},
		{SegmentID: "legacy", EmbeddingFamily: "tfidf"},
	}, target)
	if report.Compatible() {
		t.Fatal("expected incompatible report")
	}
	if report.Checked != 3 || report.Incompatible != 2 || report.Legacy != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if err := report.Error(); err == nil {
		t.Fatal("expected compatibility error")
	}
}

func TestCheckEmbeddingCompatibilityAcceptsExactSpec(t *testing.T) {
	target := EmbeddingSpec{Family: "tfidf", Dim: 256}
	report := CheckEmbeddingCompatibility([]SegmentRecord{{
		SegmentID:       "current",
		EmbeddingFamily: target.Family,
		EmbeddingDim:    target.Dim,
	}}, target)
	if !report.Compatible() {
		t.Fatalf("expected compatible report: %+v", report)
	}
}
