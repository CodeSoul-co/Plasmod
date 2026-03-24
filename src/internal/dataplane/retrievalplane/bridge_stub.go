//go:build !retrieval
// +build !retrieval

// Stub implementation used when the C++ retrieval library is not compiled.
// This is the default; use `-tags retrieval` (after running `make cpp`) to enable
// the CGO Knowhere/HNSW retriever.
package retrievalplane

import "fmt"

// Version returns a placeholder when CGO is disabled.
func Version() string { return "stub (CGO_ENABLED=0)" }

// Retriever is a no-op stub used when the CGO bridge is not compiled in.
type Retriever struct{ dim int }

// NewRetriever returns a stub retriever that records the dimension.
func NewRetriever(dim, _, _, _, _ int) (*Retriever, error) {
	return &Retriever{dim: dim}, nil
}

// Build is a no-op on the stub retriever.
func (r *Retriever) Build(_ []float32, _ int) error { return nil }

// Search always returns an error when CGO is disabled.
func (r *Retriever) Search(_ []float32, _ int, _ []byte) ([]int64, []float32, error) {
	return nil, nil, fmt.Errorf("andb_retrieval: CGO_ENABLED=0, C++ bridge unavailable")
}

// Close is a no-op on the stub retriever.
func (r *Retriever) Close() {}
