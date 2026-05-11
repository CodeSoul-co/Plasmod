package dataplane

import (
	"fmt"
	"sync"

	"plasmod/retrievalplane"
)

// SparseStore wraps the CGO Knowhere SparseRetriever
// (retrievalplane.SparseRetriever) and maintains alignment between Go
// ObjectIDs and the C++ layer's monotonically-increasing internal IDs.
//
// Architecture:
//
//	idArray[int_idx] = ObjectID  ↔  texts[int_idx] = original text
//	                          ↔  C++ posting-list internal id (== int_idx)
//
// This is the BM25-style complement to VectorStore. Where VectorStore
// indexes dense embeddings via HNSW, SparseStore indexes the same documents
// via FNV-hashed term sparse vectors via SPARSE_INVERTED_INDEX/SPARSE_WAND,
// providing keyword/lexical-vector recall.
//
// When the CGO library is unavailable, all methods are safe no-ops and
// Ready() returns false so the caller transparently falls back to lexical
// (string-matching) search.
type SparseStore struct {
	retriever *retrievalplane.SparseRetriever // nil when CGO unavailable
	indexType retrievalplane.SparseIndexType

	// Parallel arrays aligned by index: idArray[i] ↔ docs[i]
	idArray []string
	docs    []retrievalplane.SparseVector

	mu sync.RWMutex
}

// SparseStoreConfig carries optional tuning parameters for the sparse index.
type SparseStoreConfig struct {
	// IndexType: "SPARSE_INVERTED_INDEX" (default) or "SPARSE_WAND".
	IndexType retrievalplane.SparseIndexType
}

// NewSparseStore creates a SparseStore. When the CGO library is not
// available, NewSparseStore still returns a non-nil store whose methods are
// safe no-ops; Ready() will report false so callers degrade to lexical only.
func NewSparseStore(cfg SparseStoreConfig) (*SparseStore, error) {
	itype := cfg.IndexType
	if itype == "" {
		itype = retrievalplane.SparseInvertedIndex
	}

	ss := &SparseStore{indexType: itype}

	r, err := retrievalplane.NewSparseRetriever(itype)
	if err != nil {
		// Graceful degradation: sparse search silently skipped.
		return ss, nil
	}
	// In stub builds NewSparseRetriever returns a non-nil retriever whose
	// operations all yield ErrRetrievalNotAvailable. We still keep it so
	// callers can rely on a single consistent code path; Ready() detects
	// the stub via a build-time probe (see below).
	ss.retriever = r
	return ss, nil
}

// IndexType returns the sparse index variant in use.
func (s *SparseStore) IndexType() retrievalplane.SparseIndexType { return s.indexType }

// Ready returns true when the CGO retriever has been initialised AND a
// Build() call has populated the index with at least one document.
func (s *SparseStore) Ready() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.retriever == nil {
		return false
	}
	// Use Count() instead of IsReady(): IsReady reports "Init succeeded",
	// which is true even on the stub (where Count() returns -1) and on
	// freshly created retrievers. Count > 0 cleanly answers "has data".
	return s.retriever.Count() > 0
}

// AddText buffers a (id, text) pair for the next Build call. Text is
// converted to a sparse vector via the same FNV-1a tokeniser used by the
// query path, ensuring index/query symmetry.
//
// Thread-safe.
func (s *SparseStore) AddText(id, text string) {
	if id == "" || text == "" {
		return
	}
	sv, err := retrievalplane.TextToSparseVector(text)
	if err != nil {
		// Fall through silently — sparse contribution is optional.
		return
	}
	if len(sv.Indices) == 0 {
		return
	}
	s.mu.Lock()
	s.idArray = append(s.idArray, id)
	s.docs = append(s.docs, sv)
	s.mu.Unlock()
}

// AddTexts buffers a batch of (id, text) pairs. Equivalent to calling
// AddText N times but takes the lock once.
//
// Thread-safe.
func (s *SparseStore) AddTexts(ids, texts []string) {
	if len(ids) == 0 || len(ids) != len(texts) {
		return
	}
	pairs := make([]retrievalplane.SparseVector, 0, len(texts))
	keepIDs := make([]string, 0, len(ids))
	for i, t := range texts {
		if ids[i] == "" || t == "" {
			continue
		}
		sv, err := retrievalplane.TextToSparseVector(t)
		if err != nil || len(sv.Indices) == 0 {
			continue
		}
		keepIDs = append(keepIDs, ids[i])
		pairs = append(pairs, sv)
	}
	if len(keepIDs) == 0 {
		return
	}
	s.mu.Lock()
	s.idArray = append(s.idArray, keepIDs...)
	s.docs = append(s.docs, pairs...)
	s.mu.Unlock()
}

// Build sends the accumulated documents to the C++ inverted index.
// Calling Build again replaces the existing index.
func (s *SparseStore) Build() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.retriever == nil || len(s.idArray) == 0 {
		return nil
	}
	docs := make([]retrievalplane.SparseVector, len(s.docs))
	copy(docs, s.docs)
	if err := s.retriever.Build(docs); err != nil {
		return fmt.Errorf("SparseStore.Build: %w", err)
	}
	return nil
}

// Search queries the sparse index with a free-text query string and returns
// up to topK (objectID, score) pairs. The query goes through the same FNV-1a
// tokeniser used at index time.
//
// Thread-safe.
func (s *SparseStore) Search(queryText string, topK int) (ids []string, scores []float32, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.retriever == nil || queryText == "" || topK <= 0 || len(s.idArray) == 0 {
		return nil, nil, nil
	}
	q, err := retrievalplane.TextToSparseVector(queryText)
	if err != nil || len(q.Indices) == 0 {
		return nil, nil, err
	}
	intIDs, floatScores, err := s.retriever.Search(q, topK, nil)
	if err != nil || len(intIDs) == 0 {
		return nil, nil, err
	}
	ids = make([]string, 0, len(intIDs))
	scores = make([]float32, 0, len(intIDs))
	for i, intID := range intIDs {
		idx := int(intID)
		if idx >= 0 && idx < len(s.idArray) {
			ids = append(ids, s.idArray[idx])
			scores = append(scores, floatScores[i])
		}
	}
	return ids, scores, nil
}

// SearchVector is the lower-level entry that takes a pre-tokenised sparse
// query vector. Useful when callers maintain their own tokenisation pipeline.
func (s *SparseStore) SearchVector(query retrievalplane.SparseVector, topK int) (ids []string, scores []float32, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.retriever == nil || topK <= 0 || len(s.idArray) == 0 {
		return nil, nil, nil
	}
	intIDs, floatScores, err := s.retriever.Search(query, topK, nil)
	if err != nil || len(intIDs) == 0 {
		return nil, nil, err
	}
	ids = make([]string, 0, len(intIDs))
	scores = make([]float32, 0, len(intIDs))
	for i, intID := range intIDs {
		idx := int(intID)
		if idx >= 0 && idx < len(s.idArray) {
			ids = append(ids, s.idArray[idx])
			scores = append(scores, floatScores[i])
		}
	}
	return ids, scores, nil
}

// Snapshot returns the buffered (ids, docs) pairs for prebuild workflows.
func (s *SparseStore) Snapshot() (ids []string, docs []retrievalplane.SparseVector) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	idsCopy := append([]string(nil), s.idArray...)
	docsCopy := make([]retrievalplane.SparseVector, len(s.docs))
	for i, d := range s.docs {
		docsCopy[i] = retrievalplane.SparseVector{
			Indices: append([]uint32(nil), d.Indices...),
			Values:  append([]float32(nil), d.Values...),
		}
	}
	return idsCopy, docsCopy
}

// Close releases the C++ retriever. Safe to call multiple times.
func (s *SparseStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.retriever != nil {
		s.retriever.Close()
		s.retriever = nil
	}
	return nil
}
