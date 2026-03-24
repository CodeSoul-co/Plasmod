package dataplane

import (
	"andb/src/internal/dataplane/segmentstore"
	"andb/src/internal/storage"
)

// TieredDataPlane implements the three-tier search path:
//
//	Hot  → HotSegmentIndex  (bounded in-memory, growing shards of current scope)
//	Warm → SegmentDataPlane (full in-memory, all shards)
//	Cold → ColdObjectStore  (S3 or in-memory simulation, via TieredObjectStore)
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
// The hot and warm tiers are identical to the previous implementation; the cold tier
// now uses TieredObjectStore.ColdSearch for IncludeCold queries and receives ingest
// records via TieredObjectStore's cold-write path.
func NewTieredDataPlane(tieredObjs *storage.TieredObjectStore) *TieredDataPlane {
	return &TieredDataPlane{
		hot:  segmentstore.NewIndex(),
		warm: NewSegmentDataPlane(),
		coldSearch: func(query string, topK int) []string {
			return tieredObjs.ColdSearch(query, topK)
		},
		coldWrite: func(memoryID, text string, attrs map[string]string, ns string, ts int64) {
			// TieredObjectStore.ArchiveColdRecord buffers the record in the cold store.
			tieredObjs.ArchiveColdRecord(memoryID, text, attrs, ns, ts)
		},
	}
}

// HotIndex exposes the raw hot-tier index so the node manager and bootstrap can
// register dedicated InMemoryDataNode / InMemoryIndexNode instances against it.
func (t *TieredDataPlane) HotIndex() *segmentstore.Index { return t.hot }

// WarmPlane exposes the warm-tier plane for node registration.
func (t *TieredDataPlane) WarmPlane() *SegmentDataPlane { return t.warm }

// Flush syncs the hot-tier index state to the warm plane.
// Cold writes are flushed asynchronously and do not require an explicit Flush call.
func (t *TieredDataPlane) Flush() error {
	_ = t.warm.Flush()
	return nil
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
//  1. Hot index — fast, bounded
//  2. Warm plane — full in-memory (only when hot is insufficient)
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

	// warm fallback — merge with hot results
	warmOutput := t.warm.Search(input)
	merged := mergeOutputs(t.hotToOutput(hotResult), warmOutput, input.TopK)
	merged.Tier = "hot+warm"

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
