//go:build retrieval
// +build retrieval

// Package retrievalplane wires the compiled andb_retrieval C++ library into
// Go via CGO.  No Milvus Go internal packages are required; the bridge calls
// the plain-C API exported by cpp/retrieval/retrieval.cpp.
//
// Prerequisites:
//
//	cd cpp && mkdir -p build && cd build
//	cmake .. -DANDB_WITH_PYBIND=OFF      # add -DANDB_WITH_KNOWHERE=OFF for stub build
//	make -j$(nproc)
//
// The library is expected at cpp/build/libandb_retrieval.so (Linux) or
// cpp/build/libandb_retrieval.dylib (macOS).  Override via CGO_LDFLAGS if
// you build to a different directory.
package retrievalplane

/*
#cgo CFLAGS:  -I${SRCDIR}/../../../../cpp/include
#cgo LDFLAGS: -L${SRCDIR}/../../../../cpp/build -landb_retrieval -Wl,-rpath,${SRCDIR}/../../../../cpp/build

#include <stdint.h>
#include <stdlib.h>

extern const char* andb_version();
extern void*       andb_retriever_create();
extern void        andb_retriever_destroy(void* r);
extern int         andb_retriever_init(void* r,
                       const char* dense_type, const char* metric,
                       int dim,
                       const char* sparse_type, int rrf_k);
extern int         andb_retriever_build(void* r,
                       const float* vecs, long long n, int d);
extern int         andb_retriever_search(void* r,
                       const float* q, int d, int k, int for_graph,
                       const unsigned char* fbs, unsigned long fbs_size,
                       long long* out_ids, float* out_scores, int max_res);
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// Version returns the version string of the compiled andb_retrieval library.
func Version() string {
	return C.GoString(C.andb_version())
}

// Retriever wraps the C++ andb_retrieval library via CGO.
type Retriever struct {
	ptr unsafe.Pointer
	dim int
}

// NewRetriever creates and initialises a Retriever backed by the C++ library.
//   - dim: vector dimension
//   - hnswM, efConstruction, efSearch: HNSW tuning (0 → defaults 16/256/64)
//   - rrfK: RRF k parameter (0 → 60)
func NewRetriever(dim, hnswM, efConstruction, efSearch, rrfK int) (*Retriever, error) {
	ptr := C.andb_retriever_create()
	if ptr == nil {
		return nil, fmt.Errorf("andb_retriever_create returned nil")
	}

	denseType := C.CString("HNSW")
	defer C.free(unsafe.Pointer(denseType))
	metric := C.CString("IP")
	defer C.free(unsafe.Pointer(metric))
	sparseType := C.CString("SPARSE_INVERTED_INDEX")
	defer C.free(unsafe.Pointer(sparseType))

	if C.andb_retriever_init(ptr, denseType, metric, C.int(dim),
		sparseType, C.int(rrfK)) == 0 {
		C.andb_retriever_destroy(ptr)
		return nil, fmt.Errorf("andb_retriever_init failed (dim=%d)", dim)
	}
	return &Retriever{ptr: ptr, dim: dim}, nil
}

// Build indexes the given float32 row-major matrix (shape [n, dim]).
func (r *Retriever) Build(vectors []float32, n int) error {
	if len(vectors) == 0 || n <= 0 {
		return nil
	}
	if C.andb_retriever_build(r.ptr,
		(*C.float)(unsafe.Pointer(&vectors[0])),
		C.longlong(n), C.int(r.dim)) == 0 {
		return fmt.Errorf("andb_retriever_build failed (n=%d dim=%d)", n, r.dim)
	}
	return nil
}

// Search queries the index and returns up to topK (id, score) pairs.
// filterBitset is optional: bit i=1 means vector i is excluded from results.
func (r *Retriever) Search(
	query []float32,
	topK int,
	filterBitset []byte,
) (ids []int64, scores []float32, err error) {
	if len(query) == 0 || topK <= 0 {
		return nil, nil, nil
	}
	outIDs := make([]int64, topK)
	outScores := make([]float32, topK)

	var fbPtr *C.uchar
	var fbSize C.ulong
	if len(filterBitset) > 0 {
		fbPtr = (*C.uchar)(unsafe.Pointer(&filterBitset[0]))
		fbSize = C.ulong(len(filterBitset))
	}

	count := int(C.andb_retriever_search(
		r.ptr,
		(*C.float)(unsafe.Pointer(&query[0])),
		C.int(r.dim),
		C.int(topK),
		C.int(0), // for_graph=false
		fbPtr, fbSize,
		(*C.longlong)(unsafe.Pointer(&outIDs[0])),
		(*C.float)(unsafe.Pointer(&outScores[0])),
		C.int(topK),
	))
	return outIDs[:count], outScores[:count], nil
}

// Close destroys the underlying C++ retriever and releases memory.
func (r *Retriever) Close() {
	if r.ptr != nil {
		C.andb_retriever_destroy(r.ptr)
		r.ptr = nil
	}
}
