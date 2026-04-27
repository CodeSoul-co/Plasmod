//go:build retrieval
// +build retrieval

// sparse_bridge.go — CGO bridge to libplasmod_retrieval Sparse retriever.
//
// Build: go build -tags retrieval ./...
// Requires: cpp/build/libplasmod_retrieval.{so,dylib}
//
// Wraps plasmod::SparseRetriever (SPARSE_INVERTED_INDEX / SPARSE_WAND).
// The C-level API takes CSR-flattened sparse batches:
//   docLengths[i] = nnz of doc i
//   indicesFlat   = concat of per-doc index arrays
//   valuesFlat    = concat of per-doc value arrays
// avoiding arrays-of-arrays through CGO.
//
// Typical usage:
//
//	sr, err := NewSparseRetriever(SparseInvertedIndex)
//	if err != nil { ... }
//	defer sr.Close()
//
//	// Build from a corpus.
//	docs := []SparseVector{
//	    {Indices: []uint32{1, 7, 42}, Values: []float32{0.5, 0.3, 0.2}},
//	    ...
//	}
//	if err := sr.Build(docs); err != nil { ... }
//
//	// Search.
//	q := SparseVector{Indices: []uint32{7, 42}, Values: []float32{1, 1}}
//	ids, scores, err := sr.Search(q, 10, nil)
package retrievalplane

/*
#cgo CFLAGS: -I${SRCDIR}/../../../cpp/include
#cgo LDFLAGS: -L${SRCDIR}/../../../cpp/build -lplasmod_retrieval -Wl,-rpath,${SRCDIR}/../../../cpp/build

#include "plasmod/plasmod_c_api.h"
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// SparseIndexType identifies the sparse index algorithm. Currently both names
// map to plasmod's in-memory inverted index; the wrapper records the type for
// metadata and forward-compat with future Knowhere SPARSE_WAND backends.
type SparseIndexType string

const (
	// SparseInvertedIndex: standard inverted-list scoring (sum of q·d term IPs).
	SparseInvertedIndex SparseIndexType = "SPARSE_INVERTED_INDEX"
	// SparseWAND: Weak-AND pruning over the same posting lists.
	SparseWAND SparseIndexType = "SPARSE_WAND"
)

// SparseVector represents a single sparse vector in CSR form.
// Indices and Values must have equal length. Indices are interpreted modulo
// the index dimension chosen by the underlying C++ implementation (30000 for
// the FNV-hashed text path).
type SparseVector struct {
	Indices []uint32
	Values  []float32
}

// SparseRetriever is the Go-side handle to a plasmod::SparseRetriever.
// It is NOT safe for concurrent Build/Add/Search; callers should serialise
// writes. Read-only Search calls are safe to issue from a single goroutine
// at a time only — the underlying C++ inverted index uses non-thread-safe
// std::unordered_map accumulators.
type SparseRetriever struct {
	ptr       unsafe.Pointer
	indexType SparseIndexType
}

// NewSparseRetriever allocates a SparseRetriever and initialises it with the
// given index type. Pass an empty type to default to SparseInvertedIndex.
func NewSparseRetriever(indexType SparseIndexType) (*SparseRetriever, error) {
	if indexType == "" {
		indexType = SparseInvertedIndex
	}
	ptr := C.plasmod_sparse_create()
	if ptr == nil {
		return nil, fmt.Errorf("plasmod_sparse_create: returned nil")
	}
	cType := C.CString(string(indexType))
	defer C.free(unsafe.Pointer(cType))

	if rc := C.plasmod_sparse_init(ptr, cType); rc == 0 {
		C.plasmod_sparse_destroy(ptr)
		return nil, fmt.Errorf("plasmod_sparse_init(%s): failed", indexType)
	}
	return &SparseRetriever{ptr: ptr, indexType: indexType}, nil
}

// IndexType returns the sparse index variant the retriever was created with.
func (s *SparseRetriever) IndexType() SparseIndexType { return s.indexType }

// Close releases the underlying C++ retriever. After Close the SparseRetriever
// is unusable; further calls return an error.
func (s *SparseRetriever) Close() {
	if s.ptr != nil {
		C.plasmod_sparse_destroy(s.ptr)
		s.ptr = nil
	}
}

// Build replaces the index contents with the given documents.
// Caller must not mutate `docs` during the call.
func (s *SparseRetriever) Build(docs []SparseVector) error {
	if s.ptr == nil {
		return fmt.Errorf("SparseRetriever: closed")
	}
	if len(docs) == 0 {
		return fmt.Errorf("SparseRetriever.Build: empty corpus")
	}
	lens, idxFlat, valFlat, err := flattenCSR(docs)
	if err != nil {
		return fmt.Errorf("SparseRetriever.Build: %w", err)
	}
	rc := C.plasmod_sparse_build(
		s.ptr,
		C.int64_t(len(docs)),
		csrLengthsPtr(lens),
		csrIndicesPtr(idxFlat),
		csrValuesPtr(valFlat),
	)
	if rc == 0 {
		return fmt.Errorf("plasmod_sparse_build: failed")
	}
	return nil
}

// Add appends documents to the index without rebuilding.
func (s *SparseRetriever) Add(docs []SparseVector) error {
	if s.ptr == nil {
		return fmt.Errorf("SparseRetriever: closed")
	}
	if len(docs) == 0 {
		return nil
	}
	lens, idxFlat, valFlat, err := flattenCSR(docs)
	if err != nil {
		return fmt.Errorf("SparseRetriever.Add: %w", err)
	}
	rc := C.plasmod_sparse_add(
		s.ptr,
		C.int64_t(len(docs)),
		csrLengthsPtr(lens),
		csrIndicesPtr(idxFlat),
		csrValuesPtr(valFlat),
	)
	if rc == 0 {
		return fmt.Errorf("plasmod_sparse_add: failed")
	}
	return nil
}

// Search performs sparse top-k retrieval. filterBitset is an optional ban-list:
// for each indexed doc id `i`, bit `i % 8` of byte `i / 8` set to 1 means the
// document is filtered OUT (not returned).
//
// Returns ids and scores ordered by descending score. Length is ≤ topK.
func (s *SparseRetriever) Search(query SparseVector, topK int, filterBitset []byte) ([]int64, []float32, error) {
	if s.ptr == nil {
		return nil, nil, fmt.Errorf("SparseRetriever: closed")
	}
	if topK <= 0 {
		return nil, nil, fmt.Errorf("SparseRetriever.Search: topK must be > 0")
	}
	if len(query.Indices) != len(query.Values) {
		return nil, nil, fmt.Errorf("SparseRetriever.Search: query indices/values length mismatch")
	}

	outIDs := make([]int64, topK)
	outScores := make([]float32, topK)

	var qIdxPtr *C.uint32_t
	var qValPtr *C.float
	if len(query.Indices) > 0 {
		qIdxPtr = (*C.uint32_t)(unsafe.Pointer(&query.Indices[0]))
		qValPtr = (*C.float)(unsafe.Pointer(&query.Values[0]))
	}

	var fPtr *C.uint8_t
	var fSize C.size_t
	if len(filterBitset) > 0 {
		fPtr = (*C.uint8_t)(unsafe.Pointer(&filterBitset[0]))
		fSize = C.size_t(len(filterBitset))
	}

	rc := C.plasmod_sparse_search(
		s.ptr,
		C.int32_t(len(query.Indices)),
		qIdxPtr,
		qValPtr,
		C.int(topK),
		fPtr,
		fSize,
		(*C.int64_t)(unsafe.Pointer(&outIDs[0])),
		(*C.float)(unsafe.Pointer(&outScores[0])),
	)
	if rc < 0 {
		return nil, nil, fmt.Errorf("plasmod_sparse_search: rc=%d", int(rc))
	}
	n := int(rc)
	return outIDs[:n], outScores[:n], nil
}

// Count returns the number of indexed documents.
func (s *SparseRetriever) Count() int64 {
	if s.ptr == nil {
		return -1
	}
	return int64(C.plasmod_sparse_count(s.ptr))
}

// IsReady reports whether the retriever has been successfully initialised.
// NOTE: returns true after Init even if no documents have been added yet.
// Use Count() > 0 to distinguish empty vs populated indexes.
func (s *SparseRetriever) IsReady() bool {
	if s.ptr == nil {
		return false
	}
	return C.plasmod_sparse_is_ready(s.ptr) == 1
}

// Save serialises the index to a file path.
func (s *SparseRetriever) Save(path string) error {
	if s.ptr == nil {
		return fmt.Errorf("SparseRetriever: closed")
	}
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	if rc := C.plasmod_sparse_save(s.ptr, cPath); rc == 0 {
		return fmt.Errorf("plasmod_sparse_save(%q): failed", path)
	}
	return nil
}

// Load deserialises an index file into this retriever, replacing any current
// state. The retriever must already be Init'd (NewSparseRetriever does this).
func (s *SparseRetriever) Load(path string) error {
	if s.ptr == nil {
		return fmt.Errorf("SparseRetriever: closed")
	}
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	if rc := C.plasmod_sparse_load(s.ptr, cPath); rc == 0 {
		return fmt.Errorf("plasmod_sparse_load(%q): failed", path)
	}
	return nil
}

// TextToSparseVector tokenises text using FNV-1a hashing and returns the
// resulting TF-normalised sparse vector. The mapping is identical to the one
// used by the C++ index, so query vectors produced this way score correctly
// against documents indexed via the same function.
//
// initialCapacity is a hint for the output buffer; the function will retry
// with a larger buffer automatically if needed.
func TextToSparseVector(text string) (SparseVector, error) {
	if text == "" {
		return SparseVector{}, nil
	}
	cText := C.CString(text)
	defer C.free(unsafe.Pointer(cText))

	// Conservative initial capacity: number of bytes is an upper bound for
	// distinct tokens. Cap to avoid huge first allocation.
	initial := len(text)
	if initial < 16 {
		initial = 16
	}
	if initial > 4096 {
		initial = 4096
	}

	for attempt := 0; attempt < 2; attempt++ {
		idxBuf := make([]uint32, initial)
		valBuf := make([]float32, initial)
		var outLen C.int32_t

		rc := C.plasmod_sparse_text_to_vector(
			cText,
			C.int32_t(initial),
			(*C.uint32_t)(unsafe.Pointer(&idxBuf[0])),
			(*C.float)(unsafe.Pointer(&valBuf[0])),
			&outLen,
		)
		n := int(outLen)
		if rc == 1 {
			return SparseVector{
				Indices: append([]uint32(nil), idxBuf[:n]...),
				Values:  append([]float32(nil), valBuf[:n]...),
			}, nil
		}
		// Overflow: retry once with the requested length.
		if n > initial {
			initial = n
			continue
		}
		return SparseVector{}, fmt.Errorf("plasmod_sparse_text_to_vector: failed (rc=%d, len=%d)", int(rc), n)
	}
	return SparseVector{}, fmt.Errorf("plasmod_sparse_text_to_vector: retry capacity exhausted")
}

// ── helpers ──────────────────────────────────────────────────────────────────

// flattenCSR validates and concatenates per-doc sparse arrays.
func flattenCSR(docs []SparseVector) ([]int32, []uint32, []float32, error) {
	lens := make([]int32, len(docs))
	total := 0
	for i, d := range docs {
		if len(d.Indices) != len(d.Values) {
			return nil, nil, nil, fmt.Errorf("doc %d: indices/values length mismatch (%d vs %d)",
				i, len(d.Indices), len(d.Values))
		}
		lens[i] = int32(len(d.Indices))
		total += len(d.Indices)
	}
	idxFlat := make([]uint32, 0, total)
	valFlat := make([]float32, 0, total)
	for _, d := range docs {
		idxFlat = append(idxFlat, d.Indices...)
		valFlat = append(valFlat, d.Values...)
	}
	return lens, idxFlat, valFlat, nil
}

// Pointer helpers that return nil for empty slices (CGO safe).
func csrLengthsPtr(lens []int32) *C.int32_t {
	if len(lens) == 0 {
		return nil
	}
	return (*C.int32_t)(unsafe.Pointer(&lens[0]))
}

func csrIndicesPtr(idx []uint32) *C.uint32_t {
	if len(idx) == 0 {
		return nil
	}
	return (*C.uint32_t)(unsafe.Pointer(&idx[0]))
}

func csrValuesPtr(val []float32) *C.float {
	if len(val) == 0 {
		return nil
	}
	return (*C.float)(unsafe.Pointer(&val[0]))
}
