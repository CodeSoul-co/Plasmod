//go:build !retrieval
// +build !retrieval

// Stub implementation used when the C++ retrieval library is not compiled.
// This is the default; use `-tags retrieval` (after running `make cpp`) to enable
// the CGO Knowhere/HNSW retriever.
package retrievalplane

import "fmt"

// ErrRetrievalNotAvailable is returned by all stub methods when the CGO bridge
// is not compiled in (build tag `retrieval` absent).
var ErrRetrievalNotAvailable = fmt.Errorf("plasmod_retrieval: CGO_ENABLED=0, C++ bridge unavailable")

// Version returns a placeholder when CGO is disabled.
func Version() string { return "stub (CGO_ENABLED=0)" }

// ── Retriever stub ────────────────────────────────────────────────────────────

// Retriever is a no-op stub used when the CGO bridge is not compiled in.
type Retriever struct{ dim int }

// NewRetriever returns a stub retriever that records the dimension.
func NewRetriever(dim, _, _, _, _ int) (*Retriever, error) {
	return &Retriever{dim: dim}, nil
}

// NewRetrieverWithMetric returns a stub retriever (metric ignored).
func NewRetrieverWithMetric(dim, _, _, _ int, _ string) (*Retriever, error) {
	return &Retriever{dim: dim}, nil
}

// Supported dense index types (mirrored from the CGO build for API parity).
const (
	IndexHNSW    = "HNSW"
	IndexIVFFlat = "IVF_FLAT"
	IndexDISKANN = "DISKANN"
)

// NewRetrieverWithIndexType is a stub that records dim and ignores indexType/metric.
func NewRetrieverWithIndexType(dim int, _ string, _ string) (*Retriever, error) {
	return &Retriever{dim: dim}, nil
}

// Build is a no-op on the stub retriever.
func (r *Retriever) Build(_ []float32, _ int) error { return nil }

// Search always returns ErrRetrievalNotAvailable when CGO is disabled.
func (r *Retriever) Search(_ []float32, _ int, _ []byte) ([]int64, []float32, error) {
	return nil, nil, ErrRetrievalNotAvailable
}

// Close is a no-op on the stub retriever.
func (r *Retriever) Close() {}

// ── SegmentRetriever stub ─────────────────────────────────────────────────────

// SegmentRetriever is a no-op stub for the multi-segment index manager.
// All methods return ErrRetrievalNotAvailable.
type SegmentRetriever struct{}

// GlobalSegmentRetriever is the package-level singleton stub.
var GlobalSegmentRetriever = &SegmentRetriever{}

// BuildSegment is a no-op stub.
func (s *SegmentRetriever) BuildSegment(_ string, _ []float32, _, _ int) error {
	return ErrRetrievalNotAvailable
}

// Search returns ErrRetrievalNotAvailable.
func (s *SegmentRetriever) Search(_ string, _ []float32, _, _ int) ([]int64, []float32, error) {
	return nil, nil, ErrRetrievalNotAvailable
}

// SearchWithFilter returns ErrRetrievalNotAvailable.
func (s *SegmentRetriever) SearchWithFilter(_ string, _ []float32, _, _ int, _ []byte) ([]int64, []float32, error) {
	return nil, nil, ErrRetrievalNotAvailable
}

// UnloadSegment returns ErrRetrievalNotAvailable.
func (s *SegmentRetriever) UnloadSegment(_ string) error {
	return ErrRetrievalNotAvailable
}

// HasSegment always returns false.
func (s *SegmentRetriever) HasSegment(_ string) bool { return false }

// SegmentSize always returns -1.
func (s *SegmentRetriever) SegmentSize(_ string) int64 { return -1 }
