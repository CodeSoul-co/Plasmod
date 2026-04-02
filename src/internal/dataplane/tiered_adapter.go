package dataplane

import (
	"sort"

	"andb/src/internal/dataplane/segmentstore"
	"andb/src/internal/schemas"
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
	hot              *segmentstore.Index
	warm             *SegmentDataPlane
	embedder         EmbeddingGenerator
	coldSearch       func(query string, topK int) []string
	coldVectorSearch func(queryVec []float32, topK int) []string
	coldHNSWSearch   func(queryVec []float32, topK int) []string
	coldWrite        func(memoryID, text string, attrs map[string]string, ns string, ts int64)
	rrfK             int
}

func normalizeTieredRRFK(cfg schemas.AlgorithmConfig) int {
	if cfg.RRFK > 0 {
		return cfg.RRFK
	}
	return defaultRRFK
}

// NewTieredDataPlane constructs a TieredDataPlane backed by the given TieredObjectStore.
// The warm tier performs only lexical search.  To enable hybrid (lexical+vector) warm
// search, use NewTieredDataPlaneWithEmbedder instead.
func NewTieredDataPlane(tieredObjs *storage.TieredObjectStore) *TieredDataPlane {
	return NewTieredDataPlaneWithConfig(tieredObjs, schemas.DefaultAlgorithmConfig())
}

func NewTieredDataPlaneWithConfig(tieredObjs *storage.TieredObjectStore, cfg schemas.AlgorithmConfig) *TieredDataPlane {
	if tieredObjs == nil {
		tieredObjs = storage.NewTieredObjectStore(storage.NewHotObjectCache(0), nil, nil, nil)
	}
	objs := tieredObjs
	return &TieredDataPlane{
		hot:      segmentstore.NewIndex(),
		warm:     NewSegmentDataPlaneWithConfig(cfg),
		embedder: nil,
		coldSearch: func(query string, topK int) []string {
			return objs.ColdSearch(query, topK)
		},
		coldVectorSearch: func(queryVec []float32, topK int) []string {
			return objs.ColdVectorSearch(queryVec, topK)
		},
		coldHNSWSearch: func(queryVec []float32, topK int) []string {
			return objs.ColdHNSWSearch(queryVec, topK)
		},
		coldWrite: func(memoryID, text string, attrs map[string]string, ns string, ts int64) {
			objs.ArchiveColdRecord(memoryID, text, attrs, ns, ts)
		},
		rrfK: normalizeTieredRRFK(cfg),
	}
}

// NewTieredDataPlaneWithEmbedder constructs a TieredDataPlane with hybrid warm search.
// The embedder generates float32 vectors (e.g. TF-IDF or LLM-based) that are indexed
// in the CGO Knowhere/HNSW retriever for dense vector search.
// The VectorStore gracefully degrades to lexical-only when CGO is unavailable.
func NewTieredDataPlaneWithEmbedder(tieredObjs *storage.TieredObjectStore, embedder EmbeddingGenerator) (*TieredDataPlane, error) {
	return NewTieredDataPlaneWithEmbedderAndConfig(tieredObjs, embedder, schemas.DefaultAlgorithmConfig())
}

func NewTieredDataPlaneWithEmbedderAndConfig(tieredObjs *storage.TieredObjectStore, embedder EmbeddingGenerator, cfg schemas.AlgorithmConfig) (*TieredDataPlane, error) {
	if tieredObjs == nil {
		tieredObjs = storage.NewTieredObjectStore(storage.NewHotObjectCache(0), nil, nil, nil)
	}
	if embedder == nil {
		return NewTieredDataPlaneWithConfig(tieredObjs, cfg), nil
	}
	warm, err := NewSegmentDataPlaneWithEmbedderAndConfig(embedder, cfg)
	if err != nil {
		return nil, err
	}
	return &TieredDataPlane{
		hot:      segmentstore.NewIndex(),
		warm:     warm,
		embedder: embedder,
		coldSearch: func(query string, topK int) []string {
			return tieredObjs.ColdSearch(query, topK)
		},
		coldVectorSearch: func(queryVec []float32, topK int) []string {
			return tieredObjs.ColdVectorSearch(queryVec, topK)
		},
		coldHNSWSearch: func(queryVec []float32, topK int) []string {
			return tieredObjs.ColdHNSWSearch(queryVec, topK)
		},
		coldWrite: func(memoryID, text string, attrs map[string]string, ns string, ts int64) {
			tieredObjs.ArchiveColdRecord(memoryID, text, attrs, ns, ts)
		},
		rrfK: normalizeTieredRRFK(cfg),
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

// BatchIngest writes multiple records to both hot and warm tiers.
// For the warm tier, embeddings are computed via a single BatchGenerate call
// when the embedder supports it, rather than N individual Generate calls.
func (t *TieredDataPlane) BatchIngest(records []IngestRecord) error {
	if len(records) == 0 {
		return nil
	}
	for _, r := range records {
		ns := r.Namespace
		if ns == "" {
			ns = "default"
		}
		t.hot.InsertObject(r.ObjectID, r.Text, r.Attributes, ns, r.EventUnixTS)
	}
	return t.warm.BatchIngest(records)
}

func (t *TieredDataPlane) resolveColdIDs(input SearchInput) []string {
	if t.embedder != nil {
		queryVec, err := t.embedder.Generate(input.QueryText)
		if err == nil && len(queryVec) > 0 {
			if t.coldHNSWSearch != nil {
				ids := t.coldHNSWSearch(queryVec, input.TopK)
				if len(ids) > 0 {
					return ids
				}
			}
			if t.coldVectorSearch != nil {
				return t.coldVectorSearch(queryVec, input.TopK)
			}
		}
	}
	if t.coldSearch != nil {
		return t.coldSearch(input.QueryText, input.TopK)
	}
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

	hotOut := t.hotToOutput(hotResult)

	// Early return only when hot fully satisfies the request and cold is not needed.
	if len(hotResult.Hits) >= input.TopK && input.TopK > 0 {
		hotOut.Tier = "hot"
		if !input.IncludeCold {
			return hotOut
		}
		// Caller asked for cold tier: merge even when hot already satisfies TopK,
		// otherwise archived hits would never be consulted on a full hot page.
		coldIDs := t.resolveColdIDs(input)
		coldOutput := SearchOutput{ObjectIDs: coldIDs, Tier: "cold"}
		merged := mergeOutputs(hotOut, coldOutput, input.TopK)
		merged.Tier = "hot+cold"
		return merged
	}

	// Warm tier (lexical or lexical+vector depending on embedder/vector readiness).
	warmOut := t.warm.Search(input)

	// Collect candidate ranked lists for fusion.
	candidateLists := [][]string{}
	if len(hotOut.ObjectIDs) > 0 {
		candidateLists = append(candidateLists, hotOut.ObjectIDs)
	}
	if len(warmOut.ObjectIDs) > 0 {
		candidateLists = append(candidateLists, warmOut.ObjectIDs)
	}

	tierLabel := "hot+warm"

	// Cold tier is consulted only when explicitly requested.
	coldOut := SearchOutput{}
	if input.IncludeCold {
		coldIDs := t.resolveColdIDs(input)
		coldOut = SearchOutput{ObjectIDs: coldIDs, Tier: "cold"}
		if len(coldOut.ObjectIDs) > 0 {
			candidateLists = append(candidateLists, coldOut.ObjectIDs)
		}
		tierLabel = "hot+warm+cold"
	}

	// Fuse ranked candidate lists using RRF.
	fusedIDs := rrfFuseMany(candidateLists, t.rrfK, input.TopK)

	// Merge trace metadata from all tiers.
	scanned := append([]string{}, hotOut.ScannedSegments...)
	scanned = append(scanned, warmOut.ScannedSegments...)
	scanned = append(scanned, coldOut.ScannedSegments...)

	planned := append([]SegmentTrace{}, hotOut.PlannedSegments...)
	planned = append(planned, warmOut.PlannedSegments...)
	planned = append(planned, coldOut.PlannedSegments...)

	return SearchOutput{
		ObjectIDs:       fusedIDs,
		ScannedSegments: scanned,
		PlannedSegments: planned,
		Tier:            tierLabel,
	}
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

// rrfFuseMany fuses multiple ranked candidate lists using Reciprocal Rank Fusion.
// Each input list is assumed to be ranked from best to worst.
func rrfFuseMany(lists [][]string, k int, topK int) []string {
	if k <= 0 {
		k = defaultRRFK
	}

	scores := map[string]float64{}
	order := make([]string, 0)

	for _, ids := range lists {
		for rank, id := range ids {
			if _, ok := scores[id]; !ok {
				order = append(order, id)
			}
			scores[id] += 1.0 / float64(k+rank+1)
		}
	}

	sort.SliceStable(order, func(i, j int) bool {
		return scores[order[i]] > scores[order[j]]
	})

	if topK > 0 && len(order) > topK {
		return order[:topK]
	}
	return order
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
