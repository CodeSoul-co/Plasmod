//go:build retrieval
// +build retrieval

package retrievalplane

import (
	"math/rand"
	"testing"
)

// TestRetriever_IVFFlat_BuildSearch validates that the new index_type switch
// correctly drives faiss IVF_FLAT through Knowhere's IndexFactory.
func TestRetriever_IVFFlat_BuildSearch(t *testing.T) {
	const (
		dim   = 16
		nDocs = 256
		topK  = 5
	)

	r, err := NewRetrieverWithIndexType(dim, IndexIVFFlat, "L2")
	if err != nil {
		t.Fatalf("NewRetrieverWithIndexType(IVF_FLAT): %v", err)
	}
	defer r.Close()

	// Build a corpus with one cluster around vector "anchor" plus noise.
	rng := rand.New(rand.NewSource(42))
	flat := make([]float32, nDocs*dim)
	for i := 0; i < nDocs; i++ {
		for j := 0; j < dim; j++ {
			flat[i*dim+j] = rng.Float32()
		}
	}
	// Pin doc 0 to a known anchor vector so the query (== anchor) finds it.
	for j := 0; j < dim; j++ {
		flat[j] = float32(j) / float32(dim)
	}

	if err := r.Build(flat, nDocs); err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Query with the anchor — doc 0 should be the closest.
	q := make([]float32, dim)
	for j := 0; j < dim; j++ {
		q[j] = float32(j) / float32(dim)
	}
	ids, dists, err := r.Search(q, topK, nil)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(ids) == 0 {
		t.Fatalf("Search returned 0 results")
	}
	t.Logf("ids=%v dists=%v", ids, dists)
	if ids[0] != 0 {
		t.Errorf("top match = %d, want 0", ids[0])
	}
}

// TestRetriever_IVFFlat_VsHNSW spot-checks that both backends accept the
// same input and return non-empty results.
func TestRetriever_IVFFlat_VsHNSW(t *testing.T) {
	const dim = 8
	flat := make([]float32, 32*dim)
	rng := rand.New(rand.NewSource(7))
	for i := range flat {
		flat[i] = rng.Float32()
	}
	q := make([]float32, dim)
	copy(q, flat[:dim])

	for _, idxType := range []string{IndexHNSW, IndexIVFFlat} {
		idxType := idxType
		t.Run(idxType, func(t *testing.T) {
			r, err := NewRetrieverWithIndexType(dim, idxType, "L2")
			if err != nil {
				t.Fatalf("NewRetrieverWithIndexType(%s): %v", idxType, err)
			}
			defer r.Close()
			if err := r.Build(flat, 32); err != nil {
				t.Fatalf("Build: %v", err)
			}
			ids, _, err := r.Search(q, 3, nil)
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			if len(ids) == 0 {
				t.Fatalf("Search returned 0 results for %s", idxType)
			}
		})
	}
}

// TestRetriever_UnknownIndexType ensures unsupported index types fail fast
// with a clear error rather than silently mis-configuring.
func TestRetriever_UnknownIndexType(t *testing.T) {
	_, err := NewRetrieverWithIndexType(8, "BOGUS_INDEX", "IP")
	if err == nil {
		t.Fatalf("expected error for BOGUS_INDEX, got nil")
	}
}

// TestRetriever_DISKANN_ApiSurface verifies the DISKANN path is reachable
// via the Go API and fails cleanly (no panic / SIGABRT) regardless of
// whether the backend is compiled in.
//
// Current state:
//   - ANDB_KNOWHERE_DISKANN=OFF : IndexFactory rejects "DISKANN" → Init returns false
//   - ANDB_KNOWHERE_DISKANN=ON  : dense.cpp explicitly returns false because
//     full FileManager runtime wiring is a follow-up (see comment in
//     dense.cpp DISKANN branch).
//
// In both cases we expect a non-nil error, NOT a successful retriever.
// This test guards against accidental future breakage of either path
// (e.g. a regression that lets DISKANN slip through and SIGABRT the
// process inside the C++ assert).
func TestRetriever_DISKANN_ApiSurface(t *testing.T) {
	r, err := NewRetrieverWithIndexType(16, IndexDISKANN, "L2")
	if err == nil {
		// If somebody adds full runtime wiring later, expand this test
		// into a proper Build/Search round-trip instead of failing.
		r.Close()
		t.Fatalf("DISKANN init unexpectedly succeeded — full FileManager " +
			"wiring landed; please replace this test with a real " +
			"Build/Search round-trip")
	}
	t.Logf("DISKANN init returned expected error: %v", err)
}
