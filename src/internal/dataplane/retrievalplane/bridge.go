//go:build retrieval
// +build retrieval

// bridge.go — CGO bridge to libplasmod_retrieval (Knowhere HNSW).
//
// Build: go build -tags retrieval ./...
// Requires: make cpp-with-knowhere (builds cpp/build/libplasmod_retrieval.dylib)
//
// This file provides the real implementations of Retriever and SegmentRetriever
// by calling the extern "C" functions in cpp/include/plasmod/plasmod_c_api.h via CGO.
// The stub file (bridge_stub.go) is used for non-CGO builds.

package retrievalplane

/*
#cgo CFLAGS: -I${SRCDIR}/../../../../cpp/include
#cgo LDFLAGS: -L${SRCDIR}/../../../../cpp/build -lplasmod_retrieval -Wl,-rpath,${SRCDIR}/../../../../cpp/build

// Use the pure-C header (no C++ includes) so CGO's C compiler can parse it.
#include "plasmod/plasmod_c_api.h"
#include <stdlib.h>
*/
import "C"
import (
	"fmt"
	"sync"
	"unsafe"
)

// segIDCache caches a heap-allocated C string per Go segment ID so the hot
// search path does not pay a malloc + free + memcpy every call (each
// C.CString is a malloc, and the deferred C.free is another lock on the C
// allocator).  Entries live for the process lifetime; the working set of
// segments is small in practice (typically O(10s)).
var (
	segIDMu    sync.RWMutex
	segIDCache = map[string]*C.char{}
)

func cachedSegmentCString(id string) *C.char {
	segIDMu.RLock()
	if p, ok := segIDCache[id]; ok {
		segIDMu.RUnlock()
		return p
	}
	segIDMu.RUnlock()
	segIDMu.Lock()
	defer segIDMu.Unlock()
	if p, ok := segIDCache[id]; ok {
		return p
	}
	p := C.CString(id)
	segIDCache[id] = p
	return p
}

// Version returns the C++ library version string.
func Version() string {
	return C.GoString(C.plasmod_version())
}

// ── Retriever (single-index, legacy path) ─────────────────────────────────────

// Retriever wraps a single andb::Retriever instance.
type Retriever struct {
	ptr unsafe.Pointer
	dim int
}

// NewRetriever creates a Retriever with the given parameters.
//
//	dim          : embedding dimension
//	m, efConstr  : HNSW graph parameters (0 → defaults 16/256)
//	rrfK         : Reciprocal Rank Fusion constant (0 → default 60)
//	_ (unused)   : reserved for future use
func NewRetriever(dim, m, efConstr, rrfK, _ int) (*Retriever, error) {
	return NewRetrieverWithMetric(dim, m, efConstr, rrfK, "IP")
}

// NewRetrieverWithMetric creates a Retriever with configurable distance metric.
// Supported metrics: "IP" (inner product), "L2" (Euclidean), "COSINE".
// Index type defaults to "HNSW".
func NewRetrieverWithMetric(dim, m, efConstr, rrfK int, metric string) (*Retriever, error) {
	return NewRetrieverWithIndexType(dim, "HNSW", metric)
}

// Supported dense index types exposed by the C++ retrieval library.
// Build-time gates:
//   - HNSW     : always available (knowhere_hnsw, MIT)
//   - IVF_FLAT : ANDB_KNOWHERE_FAISS=ON (faiss IVF, needs OpenBLAS)
//   - DISKANN  : ANDB_KNOWHERE_DISKANN=ON (Microsoft DiskANN, needs
//                OpenBLAS + libaio on Linux)
const (
	IndexHNSW    = "HNSW"
	IndexIVFFlat = "IVF_FLAT"
	IndexDISKANN = "DISKANN"
)

// NewRetrieverWithIndexType creates a Retriever for the given (indexType,
// metric) pair. indexType must be one of:
//   - "HNSW"     : in-memory HNSW (always available)
//   - "IVF_FLAT" : faiss IVF clustering (build with ANDB_KNOWHERE_FAISS=ON)
//   - "DISKANN"  : disk-resident DiskANN (build with ANDB_KNOWHERE_DISKANN=ON)
// An empty string defaults to "HNSW". metric defaults to "IP".
//
// IVF-specific tuning parameters (nlist, nprobe) currently use the C++
// defaults (nlist=128, nprobe=8). Expose them through this Go API only when
// a future call site needs to override them. DISKANN parameters are taken
// from the Knowhere defaults (search_list, max_degree).
//
// Returns an error if the requested backend was not compiled into the
// shared library; callers may detect this and fall back to HNSW.
func NewRetrieverWithIndexType(dim int, indexType, metric string) (*Retriever, error) {
	if dim <= 0 {
		return nil, fmt.Errorf("NewRetrieverWithIndexType: dim must be > 0")
	}
	if indexType == "" {
		indexType = IndexHNSW
	}
	if metric == "" {
		metric = "IP"
	}

	ptr := C.plasmod_retriever_create()
	if ptr == nil {
		return nil, fmt.Errorf("plasmod_retriever_create: returned nil")
	}
	cIdx := C.CString(indexType)
	defer C.free(unsafe.Pointer(cIdx))
	cMetric := C.CString(metric)
	defer C.free(unsafe.Pointer(cMetric))
	cReserved := C.CString("SPARSE_INVERTED_INDEX")
	defer C.free(unsafe.Pointer(cReserved))

	rc := C.plasmod_retriever_init(
		ptr,
		cIdx,
		cMetric,
		C.int(dim),
		cReserved,
		C.int(60),
	)
	if rc == 0 {
		C.plasmod_retriever_destroy(ptr)
		return nil, fmt.Errorf("plasmod_retriever_init(%s,%s): failed", indexType, metric)
	}
	return &Retriever{ptr: ptr, dim: dim}, nil
}

// Build builds the HNSW index from a flat float32 slice.
// vectors must have length == n * dim.
func (r *Retriever) Build(vectors []float32, n int) error {
	if len(vectors) == 0 || n <= 0 {
		return fmt.Errorf("Build: empty input")
	}
	rc := C.plasmod_retriever_build(
		r.ptr,
		(*C.float)(unsafe.Pointer(&vectors[0])),
		C.int64_t(n),
		C.int(r.dim),
	)
	if rc == 0 {
		return fmt.Errorf("plasmod_retriever_build: failed (rc=%d)", rc)
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
	outIDs := make([]int64, topk)
	outDists := make([]float32, topk)

	var filterPtr *C.uint8_t
	var filterSize C.size_t
	if len(filter) > 0 {
		filterPtr = (*C.uint8_t)(unsafe.Pointer(&filter[0]))
		filterSize = C.size_t(len(filter))
	}

	rc := C.plasmod_retriever_search(
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
		return nil, nil, fmt.Errorf("plasmod_retriever_search: failed (rc=%d)", rc)
	}
	n := int(rc)
	return outIDs[:n], outDists[:n], nil
}

// Close destroys the underlying C++ Retriever.
func (r *Retriever) Close() {
	if r.ptr != nil {
		C.plasmod_retriever_destroy(r.ptr)
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
	rc := C.plasmod_segment_build(
		cs,
		(*C.float)(unsafe.Pointer(&vectors[0])),
		C.int64_t(n),
		C.int(dim),
	)
	if rc != 0 {
		return fmt.Errorf("plasmod_segment_build(%q): rc=%d", segmentID, rc)
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
	query []float32,
	nq, topk int,
	allowList []byte,
) ([]int64, []float32, error) {
	if len(query) == 0 || nq <= 0 || topk <= 0 {
		return nil, nil, fmt.Errorf("SearchWithFilter: invalid args")
	}
	total := nq * topk
	outIDs := make([]int64, total)
	outDists := make([]float32, total)

	cs := cachedSegmentCString(segmentID)

	var rc C.int
	if len(allowList) > 0 {
		// allow_count must equal the number of indexed vectors in the segment
		// (Knowhere BitsetView size in bits must match data count).
		numVecs := s.SegmentSize(segmentID)
		if numVecs < 0 {
			numVecs = int64(len(allowList) * 8)
		}
		rc = C.plasmod_segment_search_filter(
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
		rc = C.plasmod_segment_search(
			cs,
			(*C.float)(unsafe.Pointer(&query[0])),
			C.int64_t(nq),
			C.int(topk),
			(*C.int64_t)(unsafe.Pointer(&outIDs[0])),
			(*C.float)(unsafe.Pointer(&outDists[0])),
		)
	}
	if rc != 0 {
		return nil, nil, fmt.Errorf("plasmod_segment_search(%q): rc=%d", segmentID, rc)
	}
	return outIDs, outDists, nil
}

// UnloadSegment removes a segment index from memory.
func (s *SegmentRetriever) UnloadSegment(segmentID string) error {
	cs := cachedSegmentCString(segmentID)
	rc := C.plasmod_segment_unload(cs)
	if rc != 0 {
		return fmt.Errorf("plasmod_segment_unload(%q): rc=%d", segmentID, rc)
	}
	// Drop and free the cached C-string so the segment ID can be reused
	// without leaking, and so a future BuildSegment with the same name
	// re-registers a fresh entry.
	segIDMu.Lock()
	if p, ok := segIDCache[segmentID]; ok {
		delete(segIDCache, segmentID)
		C.free(unsafe.Pointer(p))
	}
	segIDMu.Unlock()
	return nil
}

// HasSegment returns true if the segment is loaded.
func (s *SegmentRetriever) HasSegment(segmentID string) bool {
	cs := C.CString(segmentID)
	defer C.free(unsafe.Pointer(cs))
	return C.plasmod_segment_exists(cs) == 1
}

// SegmentSize returns the number of vectors in a loaded segment, or -1.
func (s *SegmentRetriever) SegmentSize(segmentID string) int64 {
	cs := C.CString(segmentID)
	defer C.free(unsafe.Pointer(cs))
	return int64(C.plasmod_segment_size(cs))
}

// RegisterWarmSegment exposes a built cgo segment to the HTTP server's
// SegmentDataPlane.segments map so that SearchWarmSegment lookups succeed.
// After BuildSegment + this call, the segment is visible to the HTTP path.
func (s *SegmentRetriever) RegisterWarmSegment(segmentID string, objectIDs []string) error {
	if segmentID == "" || len(objectIDs) == 0 {
		return fmt.Errorf("RegisterWarmSegment: invalid args")
	}
	cs := C.CString(segmentID)
	defer C.free(unsafe.Pointer(cs))

	// Build array of C strings
	cIDs := make([]*C.char, len(objectIDs))
	for i, id := range objectIDs {
		cIDs[i] = C.CString(id)
	}
	for i := range cIDs {
		defer C.free(unsafe.Pointer(cIDs[i]))
	}

	// Slice of pointers for passing to C
	cIDPtrs := (*[200000]*C.char)(unsafe.Pointer(&cIDs[0]))

	rc := C.plasmod_segment_register_warm(cs, cIDPtrs, C.int64_t(len(objectIDs)))
	if rc != 0 {
		return fmt.Errorf("plasmod_segment_register_warm(%q, n=%d): rc=%d", segmentID, len(objectIDs), rc)
	}
	return nil
}
