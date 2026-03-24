package dataplane

import (
	"andb/src/internal/dataplane/segmentstore"
	"andb/src/internal/storage"
)

// TieredDataPlane implements the three-tier search path with optional hybrid vector search:
//
//	Hot  → segmentstore.Index   (fast in-memory, lexical, bounded)
//	Warm → SegmentDataPlane     (full in-memory, hybrid when embedder is set)
//	Cold → ColdObjectStore      (S3 or in-memory, via TieredObjectStore)
//
// When an EmbeddingGenerator is provided, the warm tier performs hybrid search
// (lexical + CGO Knowhere/HNSW via RRF).  The hot tier stays lexical-only since
// its shard sealing is driven by row-count, not embedding cardinality.
//
// A query first hits the hot index.  If it returns enough results (>= TopK)
// the result is returned immediately.  Otherwise the warm index is searched and
// the merged result set is returned.  The cold tier is consulted only when the
// caller sets IncludeCold = true (time-travel or historical evidence queries).
//
// The cold tier is backed by TieredObjectStore, which routes reads/writes through
// the storage.ColdObjectStore interface (S3ColdStore or InMemoryColdStore).
type TieredDataPlane struct {
	hot        *segmentstore.Index
	warm       *SegmentDataPlane
	coldSearch func(query string, topK int) []string // delegates to TieredObjectStore.ColdSearch
	coldWrite  func(memoryID, text string, attrs map[string]string, ns string, ts int64)
}

// NewTieredDataPlane constructs a TieredDataPlane backed by the given TieredObjectStore.
// The warm tier performs only lexical search.  To enable hybrid (lexical+vector) warm
// search, use NewTieredDataPlaneWithEmbedder instead.
func NewTieredDataPlane(tieredObjs *storage.TieredObjectStore) *TieredDataPlane {
	return &TieredDataPlane{
		hot:  segmentstore.NewIndex(),
		warm: NewSegmentDataPlane(),
		coldSearch: func(query string, topK int) []string {
			return tieredObjs.ColdSearch(query, topK)
		},
		coldWrite: func(memoryID, text string, attrs map[string]string, ns string, ts int64) {
			tieredObjs.ArchiveColdRecord(memoryID, text, attrs, ns, ts)
		},
	}
}

// NewTieredDataPlaneWithEmbedder constructs a TieredDataPlane with hybrid warm search.
// The embedder generates float32 vectors (e.g. TF-IDF or LLM-based) that are indexed
// in the CGO Knowhere/HNSW retriever for dense vector search.
// The VectorStore gracefully degrades to lexical-only when CGO is unavailable.
func NewTieredDataPlaneWithEmbedder(tieredObjs *storage.TieredObjectStore, embedder EmbeddingGenerator) (*TieredDataPlane, error) {
	if embedder == nil {
		return NewTieredDataPlane(tieredObjs), nil
	}
	warm, err := NewSegmentDataPlaneWithEmbedder(embedder)
	if err != nil {
		return nil, err
	}
	return &TieredDataPlane{
		hot:  segmentstore.NewIndex(),
		warm: warm,
		coldSearch: func(query string, topK int) []string {
			return tieredObjs.ColdSearch(query, topK)
		},
		coldWrite: func(memoryID, text string, attrs map[string]string, ns string, ts int64) {
			tieredObjs.ArchiveColdRecord(memoryID, text, attrs, ns, ts)
		},
	}, nil
}

// HotIndex exposes the raw hot-tier index so the node manager and bootstrap can
// register dedicated InMemoryDataNode / InMemoryIndexNode instances against it.
func (t *TieredDataPlane) HotIndex() *segmentstore.Index { return t.hot }

// WarmPlane exposes the warm-tier plane for node registration.
func (t *TieredDataPlane) WarmPlane() *SegmentDataPlane { return t.warm }

// Flush flushes the hot-tier index state to the warm plane and builds the
// vector index (when hybrid mode is enabled).
func (t *TieredDataPlane) Flush() error {
	return t.warm.Flush()
}

// Ingest writes to the hot tier and warm tier immediately.
// Cold-tier persistence is deferred to ArchiveColdRecord, which the caller
// (typically Runtime.SubmitIngest via TieredObjectStore) should invoke when
// an object transitions from hot or warm to cold (e.g. on TTL expiry).
func (t *TieredDataPlane) Ingest(record IngestRecord) error {
	_ = t.warm.Ingest(record)
	t.hot.InsertObject(
		record.ObjectID,
		record.Text,
		record.Attributes,
		record.Namespace,
		record.EventUnixTS,
	)
	return nil
}

// Search executes the tiered search:
//  1. Hot index — fast, bounded (lexical only)
//  2. Warm plane — full in-memory (lexical, or hybrid if embedder is set)
//  3. Cold tier — archived (only when IncludeCold flag set, via TieredObjectStore)
func (t *TieredDataPlane) Search(input SearchInput) SearchOutput {
	hotResult := t.hot.Search(segmentstore.SearchRequest{
		Query:          input.QueryText,
		TopK:           input.TopK,
		Namespace:      input.Namespace,
		MinEventUnixTS: input.TimeFromUnixTS,
		MaxEventUnixTS: input.TimeToUnixTS,
		IncludeGrowing: true,
	})

	if len(hotResult.Hits) >= input.TopK && input.TopK > 0 {
		out := t.hotToOutput(hotResult)
		out.Tier = "hot"
		return out
	}

	// warm fallback — merge with hot results (warm may be hybrid)
	warmOutput := t.warm.Search(input)
	merged := mergeOutputs(t.hotToOutput(hotResult), warmOutput, input.TopK)
	if warmOutput.Tier == "lexical+vector" {
		merged.Tier = "hot+warm"
	} else {
		merged.Tier = "hot+warm"
	}

	if !input.IncludeCold {
		return merged
	}

	// cold fallback — delegate to TieredObjectStore.ColdSearch
	coldIDs := t.coldSearch(input.QueryText, input.TopK)
	coldOutput := SearchOutput{ObjectIDs: coldIDs, Tier: "cold"}
	out := mergeOutputs(merged, coldOutput, input.TopK)
	out.Tier = "hot+warm+cold"
	return out
}

func (t *TieredDataPlane) hotToOutput(r segmentstore.SearchResult) SearchOutput {
	ids := make([]string, 0, len(r.Hits))
	for _, h := range r.Hits {
		ids = append(ids, h.ObjectID)
	}
	planned := make([]SegmentTrace, 0, len(r.ShardMetas))
	for _, m := range r.ShardMetas {
		planned = append(planned, SegmentTrace{
			ID:       m.ID,
			State:    m.State.String(),
			RowCount: m.RowCount,
			MinTS:    m.MinTS,
			MaxTS:    m.MaxTS,
		})
	}
	return SearchOutput{
		ObjectIDs:       ids,
		ScannedSegments: r.ScannedShards,
		PlannedSegments: planned,
	}
}

// mergeOutputs deduplicates and merges two SearchOutputs up to topK results.
func mergeOutputs(a, b SearchOutput, topK int) SearchOutput {
	seen := map[string]bool{}
	ids := make([]string, 0, len(a.ObjectIDs)+len(b.ObjectIDs))
	for _, id := range a.ObjectIDs {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for _, id := range b.ObjectIDs {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if topK > 0 && len(ids) > topK {
		ids = ids[:topK]
	}
	segs := append(a.ScannedSegments, b.ScannedSegments...)
	planned := append(a.PlannedSegments, b.PlannedSegments...)
	return SearchOutput{ObjectIDs: ids, ScannedSegments: segs, PlannedSegments: planned}
}
