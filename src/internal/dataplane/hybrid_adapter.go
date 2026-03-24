package dataplane

import (
	"log"
)

// VectorSearcher is the interface for vector similarity search backends.
// Implemented by MilvusAdapter and HTTPRetrievalAdapter.
type VectorSearcher interface {
	// SearchWithEmbeddingVS performs vector similarity search with a pre-computed query embedding.
	SearchWithEmbeddingVS(embedding []float32, topK int, filter string) ([]string, []float32, error)
	// IngestWithEmbeddingVS ingests a record with its pre-computed embedding.
	IngestWithEmbeddingVS(objectID string, embedding []float32, text string, attributes map[string]string) error
	// Flush ensures all pending writes are persisted.
	Flush() error
}

// HybridDataPlane combines lexical search (SegmentDataPlane) with vector search
// (MilvusAdapter or HTTPRetrievalAdapter) using RRF fusion.
//
// Search strategy:
//  1. If UseVectorSearch=true and QueryEmbedding is provided, run vector search
//  2. Always run lexical search on SegmentDataPlane
//  3. Merge results using Reciprocal Rank Fusion (RRF)
//
// Ingest strategy:
//  1. Always write to SegmentDataPlane for lexical indexing
//  2. If Embedding is provided and vectorSearcher is configured, write to vector store
type HybridDataPlane struct {
	lexical        *TieredDataPlane
	vectorSearcher VectorSearcher
	rrfK           float32 // RRF constant, default 60
}

// HybridConfig holds configuration for HybridDataPlane.
type HybridConfig struct {
	// VectorSearcher is the backend for vector similarity search.
	// If nil, only lexical search is used.
	VectorSearcher VectorSearcher
	// RRFK is the RRF fusion constant. Default is 60.
	RRFK float32
}

// NewHybridDataPlane creates a new hybrid data plane combining lexical and vector search.
func NewHybridDataPlane(cfg HybridConfig) *HybridDataPlane {
	rrfK := cfg.RRFK
	if rrfK <= 0 {
		rrfK = 60
	}
	return &HybridDataPlane{
		lexical:        NewTieredDataPlane(),
		vectorSearcher: cfg.VectorSearcher,
		rrfK:           rrfK,
	}
}

// LexicalPlane exposes the underlying TieredDataPlane for direct access.
func (h *HybridDataPlane) LexicalPlane() *TieredDataPlane {
	return h.lexical
}

// SetVectorSearcher sets or replaces the vector search backend.
func (h *HybridDataPlane) SetVectorSearcher(vs VectorSearcher) {
	h.vectorSearcher = vs
}

// Flush syncs both lexical and vector stores.
func (h *HybridDataPlane) Flush() error {
	if err := h.lexical.Flush(); err != nil {
		return err
	}
	if h.vectorSearcher != nil {
		if err := h.vectorSearcher.Flush(); err != nil {
			log.Printf("warning: vector searcher flush failed: %v", err)
		}
	}
	return nil
}

// Ingest writes to both lexical and vector stores.
func (h *HybridDataPlane) Ingest(record IngestRecord) error {
	// Always write to lexical store
	if err := h.lexical.Ingest(record); err != nil {
		return err
	}

	// Write to vector store if embedding is provided and searcher is configured
	if h.vectorSearcher != nil && len(record.Embedding) > 0 {
		if err := h.vectorSearcher.IngestWithEmbeddingVS(
			record.ObjectID,
			record.Embedding,
			record.Text,
			record.Attributes,
		); err != nil {
			log.Printf("warning: vector ingest failed for %s: %v", record.ObjectID, err)
		}
	}

	return nil
}

// Search performs hybrid search combining lexical and vector results.
func (h *HybridDataPlane) Search(input SearchInput) SearchOutput {
	// Always run lexical search
	lexicalOutput := h.lexical.Search(input)

	// If vector search is not requested or not available, return lexical results
	if !input.UseVectorSearch || h.vectorSearcher == nil || len(input.QueryEmbedding) == 0 {
		return lexicalOutput
	}

	// Run vector search
	vectorIDs, vectorScores, err := h.vectorSearcher.SearchWithEmbeddingVS(
		input.QueryEmbedding,
		input.TopK,
		"", // no filter for now
	)
	if err != nil {
		log.Printf("warning: vector search failed, using lexical only: %v", err)
		return lexicalOutput
	}

	// Merge results using RRF
	merged := h.rrfMerge(lexicalOutput.ObjectIDs, vectorIDs, vectorScores, input.TopK)

	return SearchOutput{
		ObjectIDs:       merged,
		ScannedSegments: lexicalOutput.ScannedSegments,
		PlannedSegments: lexicalOutput.PlannedSegments,
		Tier:            lexicalOutput.Tier + "+vector",
	}
}

// rrfMerge combines lexical and vector results using Reciprocal Rank Fusion.
// RRF score = sum(1 / (k + rank_i)) for each result list containing the document.
func (h *HybridDataPlane) rrfMerge(lexicalIDs []string, vectorIDs []string, vectorScores []float32, topK int) []string {
	scores := make(map[string]float32)

	// Add lexical scores (rank-based)
	for rank, id := range lexicalIDs {
		scores[id] += 1.0 / (h.rrfK + float32(rank+1))
	}

	// Add vector scores (rank-based, with optional score weighting)
	for rank, id := range vectorIDs {
		rrfScore := 1.0 / (h.rrfK + float32(rank+1))
		// Optionally weight by vector similarity score
		if rank < len(vectorScores) && vectorScores[rank] > 0 {
			rrfScore *= (1.0 + vectorScores[rank]) / 2.0 // blend RRF with similarity
		}
		scores[id] += rrfScore
	}

	// Sort by score descending
	type scoredID struct {
		id    string
		score float32
	}
	sorted := make([]scoredID, 0, len(scores))
	for id, score := range scores {
		sorted = append(sorted, scoredID{id, score})
	}
	// Simple bubble sort for small result sets
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].score > sorted[i].score {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	// Extract top-K IDs
	result := make([]string, 0, topK)
	for i := 0; i < len(sorted) && (topK <= 0 || i < topK); i++ {
		result = append(result, sorted[i].id)
	}

	return result
}
