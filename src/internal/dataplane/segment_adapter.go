package dataplane

import (
	"sort"

	"plasmod/src/internal/dataplane/segmentstore"
	"plasmod/src/internal/schemas"
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
	return NewSegmentDataPlaneWithConfig(schemas.DefaultAlgorithmConfig())
}

func NewSegmentDataPlaneWithConfig(cfg schemas.AlgorithmConfig) *SegmentDataPlane {
	return &SegmentDataPlane{
		index: segmentstore.NewIndex(),
		rrfK:  normalizeRRFK(cfg),
	}
}

// NewSegmentDataPlaneWithEmbedder creates a SegmentDataPlane with vector search enabled.
// The embedder generates float32 vectors for indexing and querying.
// VectorStore wraps the CGO Knowhere retriever (gracefully degrades when unavailable).
func NewSegmentDataPlaneWithEmbedder(embedder EmbeddingGenerator) (*SegmentDataPlane, error) {
	return NewSegmentDataPlaneWithEmbedderAndConfig(embedder, schemas.DefaultAlgorithmConfig())
}

func NewSegmentDataPlaneWithEmbedderAndConfig(embedder EmbeddingGenerator, cfg schemas.AlgorithmConfig) (*SegmentDataPlane, error) {
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
		rrfK:     normalizeRRFK(cfg),
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

func normalizeRRFK(cfg schemas.AlgorithmConfig) int {
	if cfg.RRFK > 0 {
		return cfg.RRFK
	}
	return defaultRRFK
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

// BatchIngest writes multiple records to the lexical index and, if an embedder
// is set, generates all embeddings via a single BatchGenerate call instead of
// N individual Generate calls.
func (p *SegmentDataPlane) BatchIngest(records []IngestRecord) error {
	if len(records) == 0 {
		return nil
	}
	for i := range records {
		if records[i].Namespace == "" {
			records[i].Namespace = "default"
		}
		p.index.InsertObject(records[i].ObjectID, records[i].Text, records[i].Attributes, records[i].Namespace, records[i].EventUnixTS)
	}

	if p.vecStore == nil || p.embedder == nil {
		return nil
	}

	ids := make([]string, len(records))
	texts := make([]string, len(records))
	for i, r := range records {
		ids[i] = r.ObjectID
		texts[i] = r.Text
	}
	p.vecStore.AddTexts(ids, texts)
	return nil
}

// Search is the primary retrieval entry point.
//
//   - Vector mode (CGO Knowhere/HNSW): used when vecStore is ready.
//     Embeds QueryText via TfidfEmbedder → VectorStore.Search → CGO retriever.
//   - Lexical fallback mode: used when vecStore is unavailable, not yet built,
//     or the embedder fails. Pure string match via segmentstore.Index.
func (p *SegmentDataPlane) Search(input SearchInput) SearchOutput {
	// ── Lexical search (always available) ─────────────────────────────────────
	lexResult := p.index.Search(segmentstore.SearchRequest{
		Query:          input.QueryText,
		TopK:           input.TopK,
		Namespace:      input.Namespace,
		MinEventUnixTS: input.TimeFromUnixTS,
		MaxEventUnixTS: input.TimeToUnixTS,
		IncludeGrowing: input.IncludeGrowing,
	})

	lexIDs := make([]string, 0, len(lexResult.Hits))
	for _, hit := range lexResult.Hits {
		lexIDs = append(lexIDs, hit.ObjectID)
	}

	lexOut := SearchOutput{
		ObjectIDs:       lexIDs,
		ScannedSegments: lexResult.ScannedShards,
		PlannedSegments: p.plannedSegments(lexResult.ShardMetas),
		Tier:            "lexical",
	}

	// ── Vector search (optional) ──────────────────────────────────────────────
	vecIDs := []string{}
	if p.vecStore != nil && p.vecStore.Ready() && p.embedder != nil {
		queryVec, err := p.embedder.Generate(input.QueryText)
		if err == nil && len(queryVec) > 0 {
			if ids, _, err := p.vecStore.Search(queryVec, input.TopK); err == nil && len(ids) > 0 {
				vecIDs = ids
			}
		}
	}

	// No vector results → lexical only
	if len(vecIDs) == 0 {
		return lexOut
	}

	// No lexical results → vector only
	if len(lexIDs) == 0 {
		return SearchOutput{
			ObjectIDs:       vecIDs,
			ScannedSegments: lexOut.ScannedSegments,
			PlannedSegments: lexOut.PlannedSegments,
			Tier:            "vector",
		}
	}

	// Hybrid fusion via RRF
	fused := fuseRRF(lexIDs, vecIDs, p.rrfK, input.TopK)
	return SearchOutput{
		ObjectIDs:       fused,
		ScannedSegments: lexOut.ScannedSegments,
		PlannedSegments: lexOut.PlannedSegments,
		Tier:            "lexical+vector",
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

func fuseRRF(lexIDs, vecIDs []string, k int, topK int) []string {
	if k <= 0 {
		k = defaultRRFK
	}

	scores := map[string]float64{}
	seenOrder := make([]string, 0, len(lexIDs)+len(vecIDs))

	addRanked := func(ids []string) {
		for rank, id := range ids {
			if _, ok := scores[id]; !ok {
				seenOrder = append(seenOrder, id)
			}
			scores[id] += 1.0 / float64(k+rank+1)
		}
	}

	addRanked(lexIDs)
	addRanked(vecIDs)

	sort.SliceStable(seenOrder, func(i, j int) bool {
		return scores[seenOrder[i]] > scores[seenOrder[j]]
	})

	if topK > 0 && len(seenOrder) > topK {
		return seenOrder[:topK]
	}
	return seenOrder
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
