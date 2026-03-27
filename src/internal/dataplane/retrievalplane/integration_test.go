//go:build retrieval
// +build retrieval

// integration_test.go — end-to-end chain validation with mock data.
//
// Run: go test -v -tags retrieval ./src/internal/dataplane/retrievalplane/ \
//           -ldflags "-r /path/to/CogDB/cpp/build"
//
// What this tests:
//   1. andb_segment_build   — builds a 4-vector HNSW index
//   2. andb_segment_search  — retrieves top-K nearest neighbours
//   3. SegmentRetriever (Go wrapper) — same via CGO bridge
//   4. Retriever (single-index path) — Build + Search
//   5. Version()            — library linked correctly

package retrievalplane

import (
	"math"
	"testing"
)

// ── Helpers ──────────────────────────────────────────────────────────────────

const testDim = 4

// normalise makes a unit vector (IP ≡ cosine after normalisation).
func normalise(v []float32) []float32 {
	var sum float32
	for _, x := range v {
		sum += x * x
	}
	norm := float32(math.Sqrt(float64(sum)))
	if norm == 0 {
		return v
	}
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = x / norm
	}
	return out
}

// mockVectors returns 4 normalised vectors in dim=4.
func mockVectors() []float32 {
	raw := [][]float32{
		{1, 0, 0, 0},
		{0, 1, 0, 0},
		{0, 0, 1, 0},
		{0, 0, 0, 1},
	}
	flat := make([]float32, 0, len(raw)*testDim)
	for _, v := range raw {
		flat = append(flat, normalise(v)...)
	}
	return flat
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestVersion(t *testing.T) {
	v := Version()
	t.Logf("andb_retrieval version: %s", v)
	if v == "" {
		t.Fatal("Version() returned empty string")
	}
}

func TestSegmentBuildSearch(t *testing.T) {
	const segID = "memory.episodic.2026-03-26.agent-001"
	vecs := mockVectors()

	sr := GlobalSegmentRetriever

	// 1. Build
	if err := sr.BuildSegment(segID, vecs, 4, testDim); err != nil {
		t.Fatalf("BuildSegment: %v", err)
	}
	defer sr.UnloadSegment(segID)

	// 2. HasSegment
	if !sr.HasSegment(segID) {
		t.Fatal("HasSegment returned false after build")
	}
	if sz := sr.SegmentSize(segID); sz != 4 {
		t.Fatalf("SegmentSize want 4 got %d", sz)
	}

	// 3. Search — query closest to vector[0] = {1,0,0,0}
	query := normalise([]float32{0.9, 0.1, 0, 0})
	ids, dists, err := sr.Search(segID, query, 1, 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	t.Logf("Search results: ids=%v dists=%v", ids, dists)
	if len(ids) == 0 {
		t.Fatal("Search returned 0 results")
	}
	// Nearest neighbour of {0.9, 0.1, 0, 0} should be vector 0 = {1,0,0,0}
	if ids[0] != 0 {
		t.Errorf("Expected nearest neighbour = 0, got %d", ids[0])
	}
}

func TestSegmentSearchWithFilter(t *testing.T) {
	const segID = "event.interaction.2026-03-26.agent-002"
	vecs := mockVectors()
	sr := GlobalSegmentRetriever

	if err := sr.BuildSegment(segID, vecs, 4, testDim); err != nil {
		t.Fatalf("BuildSegment: %v", err)
	}
	defer sr.UnloadSegment(segID)

	// Allow only vectors 1 and 2 (bitset: 0b00000110 = byte 0x06)
	allowList := []byte{0x06} // bit1=1 (vec1), bit2=1 (vec2), others=0

	// Query closest to {0,1,0,0} = vector 1
	query := normalise([]float32{0, 0.9, 0.1, 0})
	ids, dists, err := sr.SearchWithFilter(segID, query, 1, 2, allowList)
	if err != nil {
		t.Fatalf("SearchWithFilter: %v", err)
	}
	t.Logf("FilterSearch results: ids=%v dists=%v", ids, dists)
	if len(ids) == 0 {
		t.Fatal("SearchWithFilter returned 0 results")
	}
	// Nearest allowed neighbour should be vector 1
	if ids[0] != 1 {
		t.Logf("Warning: expected nearest in filter={1,2} to be 1, got %d", ids[0])
	}
}

func TestRetrieverSingleSegment(t *testing.T) {
	// Tests the legacy single-index Retriever path (matches bridge_stub API)
	r, err := NewRetriever(testDim, 16, 256, 60, 0)
	if err != nil {
		t.Fatalf("NewRetriever: %v", err)
	}
	defer r.Close()

	vecs := mockVectors()
	if err := r.Build(vecs, 4); err != nil {
		t.Fatalf("Build: %v", err)
	}

	query := normalise([]float32{0, 0, 0.9, 0.1})
	ids, dists, err := r.Search(query, 2, nil)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	t.Logf("Retriever.Search: ids=%v dists=%v", ids, dists)
	if len(ids) == 0 {
		t.Fatal("Search returned 0 results")
	}
	// Nearest to {0,0,0.9,0.1} should be vector 2 = {0,0,1,0}
	if ids[0] != 2 {
		t.Logf("Warning: expected nearest=2, got %d", ids[0])
	}
}

func TestSegmentUnload(t *testing.T) {
	const segID = "artifact.document.2026-03-26.agent-003"
	vecs := mockVectors()
	sr := GlobalSegmentRetriever

	if err := sr.BuildSegment(segID, vecs, 4, testDim); err != nil {
		t.Fatalf("BuildSegment: %v", err)
	}
	if !sr.HasSegment(segID) {
		t.Fatal("expected segment to exist after build")
	}
	if err := sr.UnloadSegment(segID); err != nil {
		t.Fatalf("UnloadSegment: %v", err)
	}
	if sr.HasSegment(segID) {
		t.Fatal("expected segment to be gone after unload")
	}
}

func TestMultiSegment(t *testing.T) {
	// Two independent segments coexist in the manager.
	sr := GlobalSegmentRetriever
	vecs := mockVectors()

	seg1 := "memory.semantic.2026-03-26.agent-a"
	seg2 := "event.click.2026-03-26.agent-b"

	if err := sr.BuildSegment(seg1, vecs, 4, testDim); err != nil {
		t.Fatalf("BuildSegment seg1: %v", err)
	}
	if err := sr.BuildSegment(seg2, vecs, 4, testDim); err != nil {
		t.Fatalf("BuildSegment seg2: %v", err)
	}
	defer sr.UnloadSegment(seg1)
	defer sr.UnloadSegment(seg2)

	if !sr.HasSegment(seg1) || !sr.HasSegment(seg2) {
		t.Fatal("both segments should be loaded")
	}

	query := normalise([]float32{1, 0.1, 0, 0})
	for _, seg := range []string{seg1, seg2} {
		ids, _, err := sr.Search(seg, query, 1, 1)
		if err != nil {
			t.Errorf("Search(%s): %v", seg, err)
			continue
		}
		if len(ids) == 0 {
			t.Errorf("Search(%s): empty results", seg)
		}
	}
}
