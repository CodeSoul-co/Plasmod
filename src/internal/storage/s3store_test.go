package storage

import (
	"testing"

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
