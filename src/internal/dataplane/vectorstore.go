package dataplane

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"plasmod/retrievalplane"
)

// BatchEmbeddingGenerator is an optional extension of EmbeddingGenerator.
// Implementations that support parallel/batched GPU inference should satisfy this
// interface.  VectorStore.AddTexts will prefer BatchGenerate over N×Generate when
// available.
type BatchEmbeddingGenerator interface {
	EmbeddingGenerator
	BatchGenerate(ctx context.Context, texts []string) ([][]float32, error)
}

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
	idIndex map[string]int

	mu sync.RWMutex

	// rebuildMu serialises HNSW index builds.  Concurrent callers are queued, not
	// dropped.  This prevents O(n²) rebuild storms under concurrent write load.
	rebuildMu sync.Mutex

	// indexGen increments each time a rebuild completes.  Callers of Search can
	// snapshot it before/after to detect whether the index changed.
	indexGen atomic.Int64
}

// VectorStoreConfig carries optional tuning parameters for the HNSW index.
type VectorStoreConfig struct {
	Dim      int    // embedding dimension (must match embedder.Dim())
	HNSWM    int    // M parameter (default 16 when 0)
	EfCons   int    // efConstruction (default 256 when 0)
	EfSearch int    // efSearch (default 64 when 0)
	RRFK     int    // RRF k (default 60 when 0)
	Metric   string // distance metric: "IP" (default), "L2", "COSINE"
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
		idIndex:  map[string]int{},
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
		efSearch = 256 // default 256 for better recall; 64 is too conservative
	}
	rrfK := cfg.RRFK
	if rrfK <= 0 {
		rrfK = 60
	}

	metric := cfg.Metric
	if metric == "" {
		metric = "IP"
	}

	// NewRetrieverWithMetric returns an error when CGO is disabled or the .dylib/.so is missing.
	r, err := retrievalplane.NewRetrieverWithMetric(vs.dim, hnswM, efCons, rrfK, metric)
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
	vs.upsertVectorLocked(id, vec)
	vs.mu.Unlock()
}

// AddTexts generates embeddings for a batch of (id, text) pairs and stores
// them for the next Build call.  When the embedder satisfies
// BatchEmbeddingGenerator, a single BatchGenerate RPC is issued; otherwise
// individual Generate calls are made.  Thread-safe.
func (vs *VectorStore) AddTexts(ids, texts []string) error {
	if len(ids) == 0 || len(ids) != len(texts) {
		return fmt.Errorf("ids/texts must be non-empty and have equal length")
	}

	var vecs [][]float32
	if bg, ok := vs.embedder.(BatchEmbeddingGenerator); ok {
		var err error
		vecs, err = bg.BatchGenerate(context.Background(), texts)
		if err != nil || len(vecs) != len(ids) {
			if err != nil {
				return fmt.Errorf("batch generate embeddings: %w", err)
			}
			return fmt.Errorf("batch generate embeddings: got %d vectors for %d texts", len(vecs), len(ids))
		}
	} else {
		vecs = make([][]float32, len(texts))
		for i, t := range texts {
			v, err := vs.embedder.Generate(t)
			if err != nil || len(v) == 0 {
				if err != nil {
					return fmt.Errorf("generate embedding for %q: %w", ids[i], err)
				}
				return fmt.Errorf("generate embedding for %q returned an empty vector", ids[i])
			}
			vecs[i] = v
		}
	}

	vs.mu.Lock()
	for i, id := range ids {
		if id == "" || len(vecs[i]) == 0 {
			vs.mu.Unlock()
			return fmt.Errorf("empty id or vector at batch index %d", i)
		}
		if len(vecs[i]) != vs.dim {
			vs.mu.Unlock()
			return fmt.Errorf("embedding dimension mismatch for %q: got %d want %d", id, len(vecs[i]), vs.dim)
		}
		vs.upsertVectorLocked(id, vecs[i])
	}
	vs.mu.Unlock()
	return nil
}

// AddVector stores a precomputed embedding vector for the next Build call.
// Use this when vectors are already computed (e.g. from deep1B.ibin benchmark data).
// Thread-safe.
func (vs *VectorStore) AddVector(id string, vec []float32) {
	if id == "" || len(vec) == 0 {
		return
	}

	vs.mu.Lock()
	vs.upsertVectorLocked(id, vec)
	vs.mu.Unlock()
}

func (vs *VectorStore) upsertVectorLocked(id string, vec []float32) {
	if id == "" || len(vec) != vs.dim {
		return
	}
	if vs.idIndex == nil {
		vs.idIndex = map[string]int{}
	}
	if idx, ok := vs.idIndex[id]; ok {
		start := idx * vs.dim
		copy(vs.vectors[start:start+vs.dim], vec)
		return
	}
	vs.idIndex[id] = len(vs.idArray)
	vs.idArray = append(vs.idArray, id)
	vs.vectors = append(vs.vectors, vec...)
}

// Build sends the accumulated float32 matrix to the CGO retriever.
// After Build the index is ready for Search calls.
// Calling Build again replaces the existing index.
//
// Concurrent callers are serialised via rebuildMu — only one rebuild runs at a
// time; others block until it finishes and then use the freshly-built index.
// This eliminates the O(n²) rebuild storm that occurred when N concurrent writes
// each triggered their own independent full-index rebuild.
func (vs *VectorStore) Build() error {
	// Serialize rebuilds; wait for any in-flight rebuild to finish first.
	vs.rebuildMu.Lock()
	defer vs.rebuildMu.Unlock()

	vs.mu.Lock()
	if vs.retriever == nil || len(vs.idArray) == 0 {
		vs.mu.Unlock()
		return nil
	}
	n := len(vs.idArray)
	vecs := make([]float32, n*vs.dim)
	copy(vecs, vs.vectors)
	vs.mu.Unlock()

	if err := vs.retriever.Build(vecs, n); err != nil {
		return fmt.Errorf("VectorStore.Build: %w", err)
	}
	vs.indexGen.Add(1)
	return nil
}

// Snapshot returns copies of buffered ids/vectors for prebuild workflows.
func (vs *VectorStore) Snapshot() (ids []string, vectors []float32, dim int) {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	return append([]string(nil), vs.idArray...), append([]float32(nil), vs.vectors...), vs.dim
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
