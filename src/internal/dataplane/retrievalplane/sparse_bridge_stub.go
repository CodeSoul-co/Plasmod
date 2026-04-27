//go:build !retrieval
// +build !retrieval

// Stub implementation of the sparse retriever for builds without the
// `retrieval` build tag (i.e. when libplasmod_retrieval.{so,dylib} is not
// available). All operations return ErrRetrievalNotAvailable.
package retrievalplane

// SparseIndexType identifies the sparse index algorithm.
type SparseIndexType string

const (
	// SparseInvertedIndex: standard inverted-list scoring.
	SparseInvertedIndex SparseIndexType = "SPARSE_INVERTED_INDEX"
	// SparseWAND: Weak-AND pruning.
	SparseWAND SparseIndexType = "SPARSE_WAND"
)

// SparseVector mirrors the CGO type so callers can compile against the same
// API regardless of build tags.
type SparseVector struct {
	Indices []uint32
	Values  []float32
}

// SparseRetriever is a no-op stub.
type SparseRetriever struct {
	indexType SparseIndexType
}

// NewSparseRetriever returns a stub that records the requested index type.
// All subsequent operations return ErrRetrievalNotAvailable.
func NewSparseRetriever(indexType SparseIndexType) (*SparseRetriever, error) {
	if indexType == "" {
		indexType = SparseInvertedIndex
	}
	return &SparseRetriever{indexType: indexType}, nil
}

// IndexType returns the index type recorded at construction time.
func (s *SparseRetriever) IndexType() SparseIndexType { return s.indexType }

// Close is a no-op on the stub.
func (s *SparseRetriever) Close() {}

// Build returns ErrRetrievalNotAvailable.
func (s *SparseRetriever) Build(_ []SparseVector) error { return ErrRetrievalNotAvailable }

// Add returns ErrRetrievalNotAvailable.
func (s *SparseRetriever) Add(_ []SparseVector) error { return ErrRetrievalNotAvailable }

// Search returns ErrRetrievalNotAvailable.
func (s *SparseRetriever) Search(_ SparseVector, _ int, _ []byte) ([]int64, []float32, error) {
	return nil, nil, ErrRetrievalNotAvailable
}

// Count always returns -1 on the stub.
func (s *SparseRetriever) Count() int64 { return -1 }

// IsReady always returns false on the stub.
func (s *SparseRetriever) IsReady() bool { return false }

// Save returns ErrRetrievalNotAvailable.
func (s *SparseRetriever) Save(_ string) error { return ErrRetrievalNotAvailable }

// Load returns ErrRetrievalNotAvailable.
func (s *SparseRetriever) Load(_ string) error { return ErrRetrievalNotAvailable }

// TextToSparseVector returns ErrRetrievalNotAvailable on the stub.
func TextToSparseVector(_ string) (SparseVector, error) {
	return SparseVector{}, ErrRetrievalNotAvailable
}
