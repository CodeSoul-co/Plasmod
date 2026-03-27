//go:build retrieval
// +build retrieval

// bridge.go — CGO bridge to libandb_retrieval (Knowhere HNSW).
//
// Build: go build -tags retrieval ./...
// Requires: make cpp (builds cpp/build/libandb_retrieval.dylib)
//
// This file provides the real implementations of Retriever and SegmentRetriever
// by calling the extern "C" functions in cpp/include/andb/retrieval.h via CGO.
// The stub file (bridge_stub.go) is used for non-CGO builds.

package retrievalplane

/*
#cgo CFLAGS: -I${SRCDIR}/../../../../cpp/include
#cgo LDFLAGS: -L${SRCDIR}/../../../../cpp/build -landb_retrieval -Wl,-rpath,${SRCDIR}/../../../../cpp/build

// Use the pure-C header (no C++ includes) so CGO's C compiler can parse it.
#include "andb/andb_c_api.h"
#include <stdlib.h>
*/
import "C"
import (
	"fmt"
	"unsafe"
)

// Version returns the C++ library version string.
func Version() string {
	return C.GoString(C.andb_version())
}

// ── Retriever (single-index, legacy path) ─────────────────────────────────────

// Retriever wraps a single andb::Retriever instance.
type Retriever struct {
	ptr unsafe.Pointer
	dim int
}

// NewRetriever creates a Retriever with the given parameters.
//   dim          : embedding dimension
//   m, efConstr  : HNSW graph parameters (0 → defaults 16/256)
//   rrfK         : Reciprocal Rank Fusion constant (0 → default 60)
//   _ (unused)   : reserved for future use
func NewRetriever(dim, m, efConstr, rrfK, _ int) (*Retriever, error) {
	ptr := C.andb_retriever_create()
	if ptr == nil {
		return nil, fmt.Errorf("andb_retriever_create: returned nil")
	}
	rc := C.andb_retriever_init(
		ptr,
		C.CString("HNSW"),
		C.CString("IP"),
		C.int(dim),
		C.CString("SPARSE_INVERTED_INDEX"),
		C.int(60),
	)
	if rc == 0 {
		C.andb_retriever_destroy(ptr)
		return nil, fmt.Errorf("andb_retriever_init: failed (rc=%d)", rc)
	}
	return &Retriever{ptr: ptr, dim: dim}, nil
}

// Build builds the HNSW index from a flat float32 slice.
// vectors must have length == n * dim.
func (r *Retriever) Build(vectors []float32, n int) error {
	if len(vectors) == 0 || n <= 0 {
		return fmt.Errorf("Build: empty input")
	}
	rc := C.andb_retriever_build(
		r.ptr,
		(*C.float)(unsafe.Pointer(&vectors[0])),
		C.int64_t(n),
		C.int(r.dim),
	)
	if rc == 0 {
		return fmt.Errorf("andb_retriever_build: failed (rc=%d)", rc)
	}
	return nil
}

// Search performs ANN search and returns (ids, distances, error).
// query  : flat float32 slice of length dim
// topk   : number of results
// filter : optional allow-list bitmask (nil = no filter)
func (r *Retriever) Search(query []float32, topk int, filter []byte) ([]int64, []float32, error) {
	if len(query) == 0 || topk <= 0 {
		return nil, nil, fmt.Errorf("Search: invalid arguments")
	}
	outIDs   := make([]int64, topk)
	outDists := make([]float32, topk)

	var filterPtr *C.uint8_t
	var filterSize C.size_t
	if len(filter) > 0 {
		filterPtr  = (*C.uint8_t)(unsafe.Pointer(&filter[0]))
		filterSize = C.size_t(len(filter))
	}

	rc := C.andb_retriever_search(
		r.ptr,
		(*C.float)(unsafe.Pointer(&query[0])),
		C.int(r.dim),
		C.int(topk),
		C.int(0), // for_graph=false
		filterPtr,
		filterSize,
		(*C.int64_t)(unsafe.Pointer(&outIDs[0])),
		(*C.float)(unsafe.Pointer(&outDists[0])),
		C.int(topk),
	)
	if rc < 0 {
		return nil, nil, fmt.Errorf("andb_retriever_search: failed (rc=%d)", rc)
	}
	n := int(rc)
	return outIDs[:n], outDists[:n], nil
}

// Close destroys the underlying C++ Retriever.
func (r *Retriever) Close() {
	if r.ptr != nil {
		C.andb_retriever_destroy(r.ptr)
		r.ptr = nil
	}
}

// ── SegmentRetriever (multi-segment, production path) ─────────────────────────

// SegmentRetriever exposes the SegmentIndexManager singleton.
// segment_id format: "object_type.memory_type.time_bucket.agent"
type SegmentRetriever struct{}

// GlobalSegmentRetriever is the package-level singleton handle.
var GlobalSegmentRetriever = &SegmentRetriever{}

// BuildSegment builds (or rebuilds) an HNSW index for a segment.
// vectors must be a flat float32 slice of length n * dim.
func (s *SegmentRetriever) BuildSegment(segmentID string, vectors []float32, n, dim int) error {
	if len(vectors) == 0 || n <= 0 || dim <= 0 {
		return fmt.Errorf("BuildSegment: invalid arguments (n=%d, dim=%d)", n, dim)
	}
	cs := C.CString(segmentID)
	defer C.free(unsafe.Pointer(cs))
	rc := C.andb_segment_build(
		cs,
		(*C.float)(unsafe.Pointer(&vectors[0])),
		C.int64_t(n),
		C.int(dim),
	)
	if rc != 0 {
		return fmt.Errorf("andb_segment_build(%q): rc=%d", segmentID, rc)
	}
	return nil
}

// Search performs ANN search in a segment without a filter.
// Returns (ids[nq*topk], dists[nq*topk], error).
func (s *SegmentRetriever) Search(segmentID string, query []float32, nq, topk int) ([]int64, []float32, error) {
	return s.SearchWithFilter(segmentID, query, nq, topk, nil)
}

// SearchWithFilter performs ANN search with an optional allow-list bitmask.
// allowList[i/8] bit (i%8) == 1 means vector i is a valid candidate.
func (s *SegmentRetriever) SearchWithFilter(
	segmentID string,
	query     []float32,
	nq, topk  int,
	allowList []byte,
) ([]int64, []float32, error) {
	if len(query) == 0 || nq <= 0 || topk <= 0 {
		return nil, nil, fmt.Errorf("SearchWithFilter: invalid args")
	}
	total   := nq * topk
	outIDs  := make([]int64,   total)
	outDists := make([]float32, total)

	cs := C.CString(segmentID)
	defer C.free(unsafe.Pointer(cs))

	var rc C.int
	if len(allowList) > 0 {
		// allow_count must equal the number of indexed vectors in the segment
		// (Knowhere BitsetView size in bits must match data count).
		numVecs := s.SegmentSize(segmentID)
		if numVecs < 0 {
			numVecs = int64(len(allowList) * 8)
		}
		rc = C.andb_segment_search_filter(
			cs,
			(*C.float)(unsafe.Pointer(&query[0])),
			C.int64_t(nq),
			C.int(topk),
			(*C.uint8_t)(unsafe.Pointer(&allowList[0])),
			C.int64_t(numVecs), // bits = number of indexed vectors
			(*C.int64_t)(unsafe.Pointer(&outIDs[0])),
			(*C.float)(unsafe.Pointer(&outDists[0])),
		)
	} else {
		rc = C.andb_segment_search(
			cs,
			(*C.float)(unsafe.Pointer(&query[0])),
			C.int64_t(nq),
			C.int(topk),
			(*C.int64_t)(unsafe.Pointer(&outIDs[0])),
			(*C.float)(unsafe.Pointer(&outDists[0])),
		)
	}
	if rc != 0 {
		return nil, nil, fmt.Errorf("andb_segment_search(%q): rc=%d", segmentID, rc)
	}
	return outIDs, outDists, nil
}

// UnloadSegment removes a segment index from memory.
func (s *SegmentRetriever) UnloadSegment(segmentID string) error {
	cs := C.CString(segmentID)
	defer C.free(unsafe.Pointer(cs))
	rc := C.andb_segment_unload(cs)
	if rc != 0 {
		return fmt.Errorf("andb_segment_unload(%q): rc=%d", segmentID, rc)
	}
	return nil
}

// HasSegment returns true if the segment is loaded.
func (s *SegmentRetriever) HasSegment(segmentID string) bool {
	cs := C.CString(segmentID)
	defer C.free(unsafe.Pointer(cs))
	return C.andb_segment_exists(cs) == 1
}

// SegmentSize returns the number of vectors in a loaded segment, or -1.
func (s *SegmentRetriever) SegmentSize(segmentID string) int64 {
	cs := C.CString(segmentID)
	defer C.free(unsafe.Pointer(cs))
	return int64(C.andb_segment_size(cs))
}
