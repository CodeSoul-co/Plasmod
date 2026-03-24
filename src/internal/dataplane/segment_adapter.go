package dataplane

import (
	"andb/src/internal/dataplane/segmentstore"
)

const defaultRRFK = 60

// SegmentDataPlane is the first-party retrieval execution module used by ANDB.
//
// Primary search path: pure vector search via CGO Knowhere/HNSW (VectorStore).
// When the CGO library is unavailable, unavailable, or not yet built,
// Search falls back to pure lexical search (segmentstore.Index) transparently.
// RRF fusion (lexical + vector) is NOT performed — they are mutually exclusive
// modes, not complementary.
type SegmentDataPlane struct {
	index    *segmentstore.Index
	vecStore *VectorStore
	embedder EmbeddingGenerator
	rrfK     int
}

// NewSegmentDataPlane creates a SegmentDataPlane that performs only lexical search.
// Used when the CGO Knowhere library is unavailable.
func NewSegmentDataPlane() *SegmentDataPlane {
	return &SegmentDataPlane{
		index: segmentstore.NewIndex(),
		rrfK:  defaultRRFK,
	}
}

// NewSegmentDataPlaneWithEmbedder creates a SegmentDataPlane with vector search enabled.
// The embedder generates float32 vectors for indexing and querying.
// VectorStore wraps the CGO Knowhere retriever (gracefully degrades when unavailable).
func NewSegmentDataPlaneWithEmbedder(embedder EmbeddingGenerator) (*SegmentDataPlane, error) {
	if embedder == nil {
		return nil, &errEmbedderNil{}
	}
	dim := embedder.Dim()
	if dim <= 0 {
		dim = DefaultEmbeddingDim
	}
	vecStore, err := NewVectorStore(embedder, VectorStoreConfig{Dim: dim})
	if err != nil {
		return nil, err
	}
	return &SegmentDataPlane{
		index:    segmentstore.NewIndex(),
		vecStore: vecStore,
		embedder: embedder,
		rrfK:     defaultRRFK,
	}, nil
}

type errEmbedderNil struct{}

func (e *errEmbedderNil) Error() string { return "embedder is nil" }

// Flush builds the vector index from accumulated embeddings.
// Also resets IDF counters so the next ingest batch starts fresh.
func (p *SegmentDataPlane) Flush() error {
	if p.vecStore != nil && p.embedder != nil {
		_ = p.vecStore.Build()
		p.embedder.Reset()
	}
	return nil
}

// Ingest writes a record to the lexical index and, if an embedder is set,
// also generates and stores a vector embedding for HNSW indexing.
func (p *SegmentDataPlane) Ingest(record IngestRecord) error {
	namespace := record.Namespace
	if namespace == "" {
		namespace = "default"
	}
	p.index.InsertObject(record.ObjectID, record.Text, record.Attributes, namespace, record.EventUnixTS)

	if p.vecStore != nil && p.embedder != nil {
		p.vecStore.AddText(record.ObjectID, record.Text)
	}
	return nil
}

// Search is the primary retrieval entry point.
//
//   - Vector mode (CGO Knowhere/HNSW): used when vecStore is ready.
//     Embeds QueryText via TfidfEmbedder → VectorStore.Search → CGO retriever.
//   - Lexical fallback mode: used when vecStore is unavailable, not yet built,
//     or the embedder fails. Pure string match via segmentstore.Index.
func (p *SegmentDataPlane) Search(input SearchInput) SearchOutput {
	// ── Vector search (primary) ───────────────────────────────────────────────
	if p.vecStore != nil && p.vecStore.Ready() {
		queryVec, err := p.embedder.Generate(input.QueryText)
		if err == nil && len(queryVec) > 0 {
			vecIDs, _, err := p.vecStore.Search(queryVec, input.TopK)
			if err == nil && len(vecIDs) > 0 {
				return SearchOutput{
					ObjectIDs: vecIDs,
					Tier:      "vector",
				}
			}
		}
		// Vector search returned empty or error — fall through to lexical.
	}

	// ── Lexical fallback (temporary — active while CGO library is unavailable) ──
	lexResult := p.index.Search(segmentstore.SearchRequest{
		Query:          input.QueryText,
		TopK:           input.TopK,
		Namespace:      input.Namespace,
		MinEventUnixTS: input.TimeFromUnixTS,
		MaxEventUnixTS: input.TimeToUnixTS,
		IncludeGrowing: input.IncludeGrowing,
	})

	ids := make([]string, 0, len(lexResult.Hits))
	for _, hit := range lexResult.Hits {
		ids = append(ids, hit.ObjectID)
	}
	return SearchOutput{
		ObjectIDs:       ids,
		ScannedSegments: lexResult.ScannedShards,
		PlannedSegments: p.plannedSegments(lexResult.ShardMetas),
		Tier:            "lexical",
	}
}

func (p *SegmentDataPlane) plannedSegments(metas []segmentstore.ShardMeta) []SegmentTrace {
	out := make([]SegmentTrace, 0, len(metas))
	for _, m := range metas {
		out = append(out, SegmentTrace{
			ID:       m.ID,
			State:    m.State.String(),
			RowCount: m.RowCount,
			MinTS:    m.MinTS,
			MaxTS:    m.MaxTS,
		})
	}
	return out
}

// SetEmbedder enables vector search using the provided embedder.
// Call before ingesting records; Flush is needed before Search.
func (p *SegmentDataPlane) SetEmbedder(embedder EmbeddingGenerator) error {
	if embedder == nil {
		p.vecStore = nil
		p.embedder = nil
		return nil
	}
	dim := embedder.Dim()
	if dim <= 0 {
		dim = DefaultEmbeddingDim
	}
	vs, err := NewVectorStore(embedder, VectorStoreConfig{Dim: dim})
	if err != nil {
		return err
	}
	p.vecStore = vs
	p.embedder = embedder
	return nil
}
