package dataplane

import (
	"fmt"
	"sync"

	"andb/retrievalplane"
)

// VectorStore wraps the CGO Knowhere/HNSW retriever (retrievalplane.Retriever)
// and maintains alignment between Go ObjectIDs and the CGO layer's integer indices.
//
// Architecture:
//
//	idArray[int_idx] = ObjectID  ↔  vectors[int_idx*dim:(int_idx+1)*dim] = embedding
//	                          ↔  CGO retriever internal ID (same as int_idx)
//
// When the CGO library is unavailable (CGO_ENABLED=0 or library not built),
// all methods are safe no-ops and Ready() returns false so the caller falls back
// to lexical search transparently.
type VectorStore struct {
	retriever *retrievalplane.Retriever // nil when CGO unavailable
	embedder  EmbeddingGenerator
	dim       int

	// Parallel arrays aligned by index: idArray[i] ↔ vectors[i*dim:(i+1)*dim]
	idArray []string
	vectors []float32

	mu sync.RWMutex
}

// VectorStoreConfig carries optional tuning parameters for the HNSW index.
type VectorStoreConfig struct {
	Dim      int // embedding dimension (must match embedder.Dim())
	HNSWM    int // M parameter (default 16 when 0)
	EfCons   int // efConstruction (default 256 when 0)
	EfSearch int // efSearch (default 64 when 0)
	RRFK     int // RRF k (default 60 when 0)
}

// NewVectorStore creates a VectorStore. When the CGO library is not available,
// all operations become no-ops and Ready() returns false.
func NewVectorStore(embedder EmbeddingGenerator, cfg VectorStoreConfig) (*VectorStore, error) {
	if embedder == nil {
		return nil, fmt.Errorf("embedder cannot be nil")
	}
	dim := cfg.Dim
	if dim <= 0 {
		dim = embedder.Dim()
	}
	if dim <= 0 {
		dim = DefaultEmbeddingDim
	}

	vs := &VectorStore{
		embedder: embedder,
		dim:      dim,
	}

	hnswM := cfg.HNSWM
	if hnswM <= 0 {
		hnswM = 16
	}
	efCons := cfg.EfCons
	if efCons <= 0 {
		efCons = 256
	}
	efSearch := cfg.EfSearch
	if efSearch <= 0 {
		efSearch = 64
	}
	rrfK := cfg.RRFK
	if rrfK <= 0 {
		rrfK = 60
	}

	// NewRetriever returns an error when CGO is disabled or the .dylib/.so is missing.
	r, err := retrievalplane.NewRetriever(vs.dim, hnswM, efCons, efSearch, rrfK)
	if err != nil {
		// Graceful degradation — vector search silently skipped, lexical still works.
		return vs, nil
	}
	vs.retriever = r
	return vs, nil
}

// Dim returns the embedding dimensionality.
func (vs *VectorStore) Dim() int { return vs.dim }

// Ready returns true when the CGO retriever is initialised and a Build()
// call has indexed the accumulated vectors.
func (vs *VectorStore) Ready() bool {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	return vs.retriever != nil && len(vs.idArray) > 0
}

// AddText generates an embedding for text and stores it for the next Build call.
// Thread-safe.
func (vs *VectorStore) AddText(id, text string) {
	if id == "" || text == "" {
		return
	}

	vec, err := vs.embedder.Generate(text)
	if err != nil || len(vec) == 0 {
		return
	}

	vs.mu.Lock()
	vs.idArray = append(vs.idArray, id)
	vs.vectors = append(vs.vectors, vec...)
	vs.mu.Unlock()
}

// AddVector stores a precomputed embedding vector for the next Build call.
// Use this when vectors are already computed (e.g. from deep1B.ibin benchmark data).
// Thread-safe.
func (vs *VectorStore) AddVector(id string, vec []float32) {
	if id == "" || len(vec) == 0 {
		return
	}
	if len(vec) != vs.dim {
		return
	}

	vs.mu.Lock()
	vs.idArray = append(vs.idArray, id)
	vs.vectors = append(vs.vectors, vec...)
	vs.mu.Unlock()
}

// Build sends the accumulated float32 matrix to the CGO retriever.
// After Build the index is ready for Search calls.
// Calling Build again replaces the existing index.
func (vs *VectorStore) Build() error {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	if vs.retriever == nil || len(vs.idArray) == 0 {
		return nil
	}

	n := len(vs.idArray)
	vecs := make([]float32, n*vs.dim)
	copy(vecs, vs.vectors)

	if err := vs.retriever.Build(vecs, n); err != nil {
		return fmt.Errorf("VectorStore.Build: %w", err)
	}
	return nil
}

// Search queries the vector index and returns up to topK (objectID, score) pairs.
// Thread-safe.
func (vs *VectorStore) Search(queryVec []float32, topK int) (ids []string, scores []float32, err error) {
	vs.mu.RLock()
	defer vs.mu.RUnlock()

	if vs.retriever == nil || len(queryVec) == 0 || topK <= 0 || len(vs.idArray) == 0 {
		return nil, nil, nil
	}

	intIDs, floatScores, err := vs.retriever.Search(queryVec, topK, nil)
	if err != nil || len(intIDs) == 0 {
		return nil, nil, err
	}

	ids = make([]string, len(intIDs))
	for i, intID := range intIDs {
		// CGO internal IDs are 0-based and sequential.
		idx := int(intID)
		if idx >= 0 && idx < len(vs.idArray) {
			ids[i] = vs.idArray[idx]
		}
	}
	return ids, floatScores, nil
}

// Close releases the CGO retriever. Safe to call multiple times.
func (vs *VectorStore) Close() error {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	if vs.retriever != nil {
		vs.retriever.Close()
		vs.retriever = nil
	}
	// Keep idArray/vectors so Close+Reopen is not supported (acceptable for demo).
	return nil
}
