package dataplane

import (
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"sync"

	"plasmod/retrievalplane"
	"plasmod/src/internal/dataplane/segmentstore"
	"plasmod/src/internal/schemas"
)

const defaultRRFK = 60

// SegmentDataPlane is the first-party retrieval execution module used by ANDB.
//
// Primary search path: hybrid retrieval combining
//   - lexical (segmentstore.Index, pure-Go string match)
//   - dense vectors via CGO Knowhere/HNSW (VectorStore)
//   - sparse / BM25-style via CGO Knowhere SPARSE_INVERTED_INDEX (SparseStore)
//
// Results from each ready channel are fused with Reciprocal Rank Fusion.
// When CGO is unavailable or the index has not been built yet, the absent
// channels are simply skipped — Search degrades gracefully and never fails.
type SegmentDataPlane struct {
	index       *segmentstore.Index
	vecStore    *VectorStore
	sparseStore *SparseStore
	embedder    EmbeddingGenerator
	rrfK        int
	segMu       sync.RWMutex
	segments    map[string][]string
}

// NewSegmentDataPlane creates a SegmentDataPlane that performs only lexical search.
// Used when the CGO Knowhere library is unavailable.
func NewSegmentDataPlane() *SegmentDataPlane {
	return NewSegmentDataPlaneWithConfig(schemas.DefaultAlgorithmConfig())
}

func NewSegmentDataPlaneWithConfig(cfg schemas.AlgorithmConfig) *SegmentDataPlane {
	// SparseStore is text-only and needs no embedder, so we always attempt
	// to construct it; Ready()=false will keep it out of fusion when the
	// CGO library is unavailable.
	sparseStore, _ := NewSparseStore(SparseStoreConfig{})
	return &SegmentDataPlane{
		index:       segmentstore.NewIndex(),
		sparseStore: sparseStore,
		rrfK:        normalizeRRFK(cfg),
		segments:    map[string][]string{},
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
	sparseStore, _ := NewSparseStore(SparseStoreConfig{})
	return &SegmentDataPlane{
		index:       segmentstore.NewIndex(),
		vecStore:    vecStore,
		sparseStore: sparseStore,
		embedder:    embedder,
		rrfK:        normalizeRRFK(cfg),
		segments:    map[string][]string{},
	}, nil
}

type errEmbedderNil struct{}

func (e *errEmbedderNil) Error() string { return "embedder is nil" }

// Flush builds the vector and sparse indexes from accumulated documents.
// Also resets IDF counters so the next ingest batch starts fresh.
func (p *SegmentDataPlane) Flush() error {
	if p.vecStore != nil && p.embedder != nil {
		if err := p.vecStore.Build(); err != nil {
			return err
		}
		_ = p.prebuildDefaultWarmSegment()
		p.embedder.Reset()
	}
	if p.sparseStore != nil {
		if err := p.sparseStore.Build(); err != nil {
			return err
		}
	}
	return nil
}

func (p *SegmentDataPlane) prebuildDefaultWarmSegment() error {
	if p.vecStore == nil {
		return nil
	}
	ids, vectors, dim := p.vecStore.Snapshot()
	if len(ids) == 0 || len(vectors) == 0 || dim <= 0 {
		return nil
	}
	if err := retrievalplane.GlobalSegmentRetriever.BuildSegment("warm.default", vectors, len(ids), dim); err != nil {
		return err
	}
	p.segMu.Lock()
	p.segments["warm.default"] = append([]string(nil), ids...)
	p.segMu.Unlock()
	// Warm segment: run 10 dummy queries to pre-fault HNSW graph pages into memory.
	// SearchRaw bypasses plugin reorder for pure page-fault elimination.
	if err := warmupSegment("warm.default", dim, 10); err != nil {
		log.Printf("[dataplane] warmup warning: %v", err)
	}
	return nil
}

// warmupSegment runs dummy queries to pre-fault the HNSW graph into memory.
// nQueries must be >= 2 to trigger the L2NormSort plugin (min_nq_ = 8 in C++).
func warmupSegment(segmentID string, dim, nQueries int) error {
	dummy := make([]float32, dim*nQueries)
	for i := 0; i < nQueries; i++ {
		if _, _, err := retrievalplane.GlobalSegmentRetriever.SearchRaw(segmentID, dummy, nQueries, 1); err != nil {
			return fmt.Errorf("warmupSegment %s: %w", segmentID, err)
		}
		break // one call with nQueries batch is enough
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

	if p.vecStore != nil {
		if len(record.Embedding) > 0 {
			p.vecStore.AddVector(record.ObjectID, record.Embedding)
		} else if p.embedder != nil {
			p.vecStore.AddText(record.ObjectID, record.Text)
		}
	}
	if p.sparseStore != nil {
		p.sparseStore.AddText(record.ObjectID, record.Text)
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

	ids := make([]string, len(records))
	texts := make([]string, len(records))
	for i, r := range records {
		ids[i] = r.ObjectID
		texts[i] = r.Text
	}
	if p.vecStore != nil && p.embedder != nil {
		p.vecStore.AddTexts(ids, texts)
	}
	if p.sparseStore != nil {
		p.sparseStore.AddTexts(ids, texts)
	}
	return nil
}

// Search is the primary retrieval entry point. It runs three independent
// recall channels and fuses the ranked candidate lists with Reciprocal Rank
// Fusion (RRF):
//
//   - lexical: always-available pure-string match via segmentstore.Index
//   - dense  : CGO Knowhere/HNSW when vecStore is ready and embedder is set
//   - sparse : CGO Knowhere SPARSE_INVERTED_INDEX when sparseStore is ready
//
// Channels that are not ready (CGO unavailable, no docs, no embedder, etc.)
// are silently skipped, and Tier reflects the channels that contributed.
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

	// ── Dense vector search (optional) ────────────────────────────────────────
	vecIDs := []string{}
	if p.vecStore != nil && p.vecStore.Ready() {
		var queryVec []float32

		// Use precomputed query embedding when provided (bypasses embedder).
		if len(input.QueryEmbedding) > 0 {
			queryVec = input.QueryEmbedding
		} else if p.embedder != nil {
			v, err := p.embedder.Generate(input.QueryText)
			if err == nil && len(v) > 0 {
				queryVec = v
			}
		}

		if len(queryVec) > 0 {
			if ids, _, err := p.vecStore.Search(queryVec, input.TopK); err == nil && len(ids) > 0 {
				vecIDs = ids
			}
		}
	}

	// ── Sparse / BM25-style search (optional) ─────────────────────────────────
	sparseIDs := []string{}
	if p.sparseStore != nil && p.sparseStore.Ready() {
		if ids, _, err := p.sparseStore.Search(input.QueryText, input.TopK); err == nil && len(ids) > 0 {
			sparseIDs = ids
		}
	}

	// ── Fusion ────────────────────────────────────────────────────────────────
	// Collect every channel that returned at least one hit.
	type chanList struct {
		name string
		ids  []string
	}
	chans := []chanList{}
	if len(lexIDs) > 0 {
		chans = append(chans, chanList{"lexical", lexIDs})
	}
	if len(vecIDs) > 0 {
		chans = append(chans, chanList{"vector", vecIDs})
	}
	if len(sparseIDs) > 0 {
		chans = append(chans, chanList{"sparse", sparseIDs})
	}

	// Nothing matched anywhere → return empty (preserve trace metadata).
	if len(chans) == 0 {
		return lexOut
	}

	// Single channel → return as-is, label the tier with that channel.
	if len(chans) == 1 {
		return SearchOutput{
			ObjectIDs:       chans[0].ids,
			ScannedSegments: lexOut.ScannedSegments,
			PlannedSegments: lexOut.PlannedSegments,
			Tier:            chans[0].name,
		}
	}

	// Multi-channel → RRF fusion across all participating channels.
	lists := make([][]string, 0, len(chans))
	names := make([]string, 0, len(chans))
	for _, c := range chans {
		lists = append(lists, c.ids)
		names = append(names, c.name)
	}
	fused := fuseRRFN(lists, p.rrfK, input.TopK)
	return SearchOutput{
		ObjectIDs:       fused,
		ScannedSegments: lexOut.ScannedSegments,
		PlannedSegments: lexOut.PlannedSegments,
		Tier:            joinChannels(names),
	}
}

// joinChannels formats the list of contributing channels into a stable
// "a+b+c" tier label (e.g. "lexical+vector+sparse").
func joinChannels(names []string) string {
	if len(names) == 0 {
		return ""
	}
	out := names[0]
	for _, n := range names[1:] {
		out += "+" + n
	}
	return out
}

// fuseRRFN generalises fuseRRF to N ranked lists.
func fuseRRFN(lists [][]string, k int, topK int) []string {
	if k <= 0 {
		k = defaultRRFK
	}
	scores := map[string]float64{}
	seenOrder := make([]string, 0)
	for _, ids := range lists {
		for rank, id := range ids {
			if _, ok := scores[id]; !ok {
				seenOrder = append(seenOrder, id)
			}
			scores[id] += 1.0 / float64(k+rank+1)
		}
	}
	sort.SliceStable(seenOrder, func(i, j int) bool {
		return scores[seenOrder[i]] > scores[seenOrder[j]]
	})
	if topK > 0 && len(seenOrder) > topK {
		return seenOrder[:topK]
	}
	return seenOrder
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

// SetEmbedder enables dense vector search using the provided embedder.
// Call before ingesting records; Flush is needed before Search. Sparse
// search is unaffected by this call (it indexes raw text and is enabled
// independently of the embedder).
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

// IngestVectorsToWarmSegment writes vectors directly into a named warm segment.
// Defaults to HNSW index type.
func (p *SegmentDataPlane) IngestVectorsToWarmSegment(segmentID string, objectIDs []string, vectors [][]float32) (int, error) {
	return p.ingestWithIndexType(segmentID, objectIDs, vectors, "HNSW", 0, 0, 0, 0, "")
}

// IngestVectorsToWarmSegmentWithType writes vectors into a named warm segment
// with the specified ANN index type.
// Valid indexType: "HNSW" | "IVF_FLAT" | "IVF_PQ" | "IVF_SQ8" | "DISKANN".
// For IVF types, pass nlist/nprobe/m/nbits/sqType; 0/empty = use defaults.
func (p *SegmentDataPlane) IngestVectorsToWarmSegmentWithType(
	segmentID string,
	objectIDs []string,
	vectors [][]float32,
	indexType string,
	nlist, nprobe, m, nbits int,
	sqType string,
) (int, error) {
	return p.ingestWithIndexType(segmentID, objectIDs, vectors, indexType, nlist, nprobe, m, nbits, sqType)
}

// IngestFlatVectorsToWarmSegment writes row-major vectors directly into a named
// warm segment without constructing per-vector Go slices.
func (p *SegmentDataPlane) IngestFlatVectorsToWarmSegment(
	segmentID string,
	objectIDs []string,
	flatVectors []float32,
	n, dim int,
) (int, error) {
	return p.ingestFlatWithIndexType(segmentID, objectIDs, flatVectors, n, dim, "HNSW", 0, 0, 0, 0, "")
}

// IngestFlatVectorsToWarmSegmentWithType writes row-major vectors into a named
// warm segment with the specified ANN index type.
func (p *SegmentDataPlane) IngestFlatVectorsToWarmSegmentWithType(
	segmentID string,
	objectIDs []string,
	flatVectors []float32,
	n, dim int,
	indexType string,
	nlist, nprobe, m, nbits int,
	sqType string,
) (int, error) {
	return p.ingestFlatWithIndexType(segmentID, objectIDs, flatVectors, n, dim, indexType, nlist, nprobe, m, nbits, sqType)
}

// ingestWithIndexType is the shared helper that both ingest methods call.
func (p *SegmentDataPlane) ingestWithIndexType(
	segmentID string,
	objectIDs []string,
	vectors [][]float32,
	indexType string,
	nlist, nprobe, m, nbits int,
	sqType string,
) (int, error) {
	if segmentID == "" {
		return 0, fmt.Errorf("segment_id is required")
	}
	if len(vectors) == 0 {
		return 0, fmt.Errorf("vectors is required")
	}
	if len(objectIDs) != len(vectors) {
		return 0, fmt.Errorf("object_ids/vectors length mismatch")
	}
	dim := len(vectors[0])
	if dim <= 0 {
		return 0, fmt.Errorf("vector dim must be > 0")
	}
	flat := make([]float32, len(vectors)*dim)
	for i, vec := range vectors {
		if len(vec) != dim {
			return 0, fmt.Errorf("all vectors must share same dim")
		}
		if objectIDs[i] == "" {
			return 0, fmt.Errorf("object_ids[%d] is empty", i)
		}
		copy(flat[i*dim:(i+1)*dim], vec)
	}

	return p.ingestFlatWithIndexType(segmentID, objectIDs, flat, len(vectors), dim, indexType, nlist, nprobe, m, nbits, sqType)
}

func (p *SegmentDataPlane) ingestFlatWithIndexType(
	segmentID string,
	objectIDs []string,
	flatVectors []float32,
	n, dim int,
	indexType string,
	nlist, nprobe, m, nbits int,
	sqType string,
) (int, error) {
	if segmentID == "" {
		return 0, fmt.Errorf("segment_id is required")
	}
	if n <= 0 {
		return 0, fmt.Errorf("vectors is required")
	}
	if dim <= 0 {
		return 0, fmt.Errorf("vector dim must be > 0")
	}
	if len(objectIDs) != n {
		return 0, fmt.Errorf("object_ids/vectors length mismatch")
	}
	if len(flatVectors) != n*dim {
		return 0, fmt.Errorf("flat vector length mismatch: got %d, want %d", len(flatVectors), n*dim)
	}
	for i, id := range objectIDs {
		if id == "" {
			return 0, fmt.Errorf("object_ids[%d] is empty", i)
		}
	}

	var err error
	switch indexType {
	case "HNSW", "":
		err = retrievalplane.GlobalSegmentRetriever.BuildSegment(segmentID, flatVectors, n, dim)
	default:
		err = retrievalplane.GlobalSegmentRetriever.BuildSegmentWithType(
			segmentID, flatVectors, n, dim,
			indexType, nlist, nprobe, m, nbits, sqType,
		)
	}
	if err != nil {
		return 0, err
	}
	p.segMu.Lock()
	p.segments[segmentID] = append([]string(nil), objectIDs...)
	p.segMu.Unlock()
	return n, nil
}

// SearchWarmSegment runs ANN query against a prebuilt warm segment.
// queryVec is an optional precomputed embedding — when provided, bypasses
// the embedder.  When nil, the embedder generates the vector from queryText.
func (p *SegmentDataPlane) SearchWarmSegment(segmentID, queryText string, topK int, queryVec []float32) ([]string, error) {
	if segmentID == "" {
		return nil, fmt.Errorf("segment_id is required")
	}
	if topK <= 0 {
		topK = 10
	}
	if len(queryVec) == 0 {
		if p.embedder == nil {
			return nil, fmt.Errorf("embedder unavailable")
		}
		v, err := p.embedder.Generate(queryText)
		if err != nil {
			return nil, err
		}
		queryVec = v
	}
	intIDs, _, err := retrievalplane.GlobalSegmentRetriever.Search(segmentID, queryVec, 1, topK)
	if err != nil {
		return nil, err
	}
	p.segMu.RLock()
	ids := p.segments[segmentID]
	p.segMu.RUnlock()
	out := make([]string, 0, len(intIDs))
	for _, idx := range intIDs {
		if idx >= 0 && int(idx) < len(ids) {
			out = append(out, ids[idx])
		}
	}
	return out, nil
}

// UnloadWarmSegment evicts a warm segment from the CGO index manager so a
// subsequent ingest call can rebuild it from scratch (fresh index build).
func (p *SegmentDataPlane) UnloadWarmSegment(segmentID string) error {
	if segmentID == "" {
		return fmt.Errorf("segment_id required")
	}
	if err := retrievalplane.GlobalSegmentRetriever.UnloadSegment(segmentID); err != nil {
		return err
	}
	p.segMu.Lock()
	delete(p.segments, segmentID)
	p.segMu.Unlock()
	return nil
}

// RegisterWarmSegment stores a segment's object-ID list so SearchWarmSegment
// lookups succeed.  Called by the HTTP registration endpoint after a segment
// is built via cgo (plasmod_segment_build) so the HTTP path can find it.
func (p *SegmentDataPlane) RegisterWarmSegment(segmentID string, objectIDs []string) error {
	if segmentID == "" || len(objectIDs) == 0 {
		return fmt.Errorf("RegisterWarmSegment: invalid args")
	}
	p.segMu.Lock()
	p.segments[segmentID] = append([]string(nil), objectIDs...)
	p.segMu.Unlock()
	return nil
}

// SearchWarmSegmentBatch performs batch ANN search against a warm segment.
// Returns raw integer indices (not string IDs) for use by benchmark tools.
// This is the internal fast path used by the HTTP batch endpoint to avoid
// string conversion overhead.
//
// When nq > pluginChunkSize (default 10000), automatically chunks the batch
// to cap request-local result buffers. Keep the default large enough that
// benchmark-sized batches reach the C++ full-batch path in one call.
func (p *SegmentDataPlane) SearchWarmSegmentBatch(segmentID string, nq int, topK int, queries []float32) ([]int64, []float32, error) {
	if segmentID == "" {
		return nil, nil, fmt.Errorf("segment_id is required")
	}
	if nq <= 0 || topK <= 0 || len(queries) == 0 {
		return nil, nil, fmt.Errorf("invalid args: nq=%d topK=%d len(queries)=%d", nq, topK, len(queries))
	}
	if len(queries)%nq != 0 {
		return nil, nil, fmt.Errorf("queries length %d not divisible by nq=%d", len(queries), nq)
	}
	dim := len(queries) / nq
	ids := make([]int64, nq*topK)
	dists := make([]float32, nq*topK)
	chunkSize := pluginChunkSize()
	if nq <= chunkSize {
		if err := retrievalplane.GlobalSegmentRetriever.SearchInto(segmentID, queries, nq, topK, ids, dists); err != nil {
			return nil, nil, err
		}
		return ids, dists, nil
	}
	// Chunked: split into multiple DoSearch calls, writing into final output
	// buffers directly. This keeps memory bounded without per-chunk result
	// allocation and append copies.
	for start := 0; start < nq; start += chunkSize {
		end := start + chunkSize
		if end > nq {
			end = nq
		}
		cq := end - start
		chunk := queries[start*dim : start*dim+cq*dim]
		outIDs := ids[start*topK : end*topK]
		outDists := dists[start*topK : end*topK]
		if err := retrievalplane.GlobalSegmentRetriever.SearchInto(segmentID, chunk, cq, topK, outIDs, outDists); err != nil {
			return nil, nil, fmt.Errorf("chunk [%d:%d]: %w", start, end, err)
		}
	}
	return ids, dists, nil
}

// SearchWarmSegmentSerialBatch performs a server-side serial loop: each query
// is dispatched as nq=1 to preserve online single-query behavior while avoiding
// one HTTP round trip and one Go result allocation per query.
func (p *SegmentDataPlane) SearchWarmSegmentSerialBatch(segmentID string, nq int, topK int, queries []float32) ([]int64, []float32, error) {
	if segmentID == "" {
		return nil, nil, fmt.Errorf("segment_id is required")
	}
	if nq <= 0 || topK <= 0 || len(queries) == 0 {
		return nil, nil, fmt.Errorf("invalid args: nq=%d topK=%d len(queries)=%d", nq, topK, len(queries))
	}
	if len(queries)%nq != 0 {
		return nil, nil, fmt.Errorf("queries length %d not divisible by nq=%d", len(queries), nq)
	}
	ids := make([]int64, nq*topK)
	dists := make([]float32, nq*topK)
	if err := retrievalplane.GlobalSegmentRetriever.SearchSerialInto(segmentID, queries, nq, topK, ids, dists); err != nil {
		return nil, nil, err
	}
	return ids, dists, nil
}

// SearchWarmSegmentBatchObjectIDs runs batch ANN search and maps hits to object id strings.
func (p *SegmentDataPlane) SearchWarmSegmentBatchObjectIDs(segmentID string, nq int, topK int, queries []float32, raw bool) ([][]string, [][]float32, error) {
	var intIDs []int64
	var dists []float32
	var err error
	if raw {
		intIDs, dists, err = p.SearchWarmSegmentBatchRaw(segmentID, nq, topK, queries)
	} else {
		intIDs, dists, err = p.SearchWarmSegmentBatch(segmentID, nq, topK, queries)
	}
	if err != nil {
		return nil, nil, err
	}
	p.segMu.RLock()
	segObjs := p.segments[segmentID]
	p.segMu.RUnlock()
	out := make([][]string, nq)
	outD := make([][]float32, nq)
	for qi := 0; qi < nq; qi++ {
		base := qi * topK
		rowIDs := intIDs[base : base+topK]
		rowD := dists[base : base+topK]
		s := make([]string, 0, topK)
		ds := make([]float32, 0, topK)
		for j, idx := range rowIDs {
			if idx >= 0 && int(idx) < len(segObjs) {
				s = append(s, segObjs[idx])
				if j < len(rowD) {
					ds = append(ds, rowD[j])
				}
			}
		}
		out[qi] = s
		outD[qi] = ds
	}
	return out, outD, nil
}

// pluginChunkSize returns the maximum number of queries per C++ search call.
// Override with PLASMOD_PLUGIN_CHUNK_SIZE for memory-constrained runs.
func pluginChunkSize() int {
	if s := os.Getenv("PLASMOD_PLUGIN_CHUNK_SIZE"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return n
		}
	}
	return 10000
}

// SearchWarmSegmentBatchRaw performs batch ANN search via SearchRaw (no plugin reorder).
// Used by the HTTP query_warm_batch_raw endpoint for the standard Knowhere baseline.
func (p *SegmentDataPlane) SearchWarmSegmentBatchRaw(segmentID string, nq int, topK int, queries []float32) ([]int64, []float32, error) {
	if segmentID == "" {
		return nil, nil, fmt.Errorf("segment_id is required")
	}
	if nq <= 0 || topK <= 0 || len(queries) == 0 {
		return nil, nil, fmt.Errorf("invalid args: nq=%d topK=%d len(queries)=%d", nq, topK, len(queries))
	}
	return retrievalplane.GlobalSegmentRetriever.SearchRaw(segmentID, queries, nq, topK)
}
