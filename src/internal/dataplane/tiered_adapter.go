package dataplane

import (
	"andb/src/internal/dataplane/segmentstore"
)

// TieredDataPlane implements the three-tier search path:
//
//	Hot  → HotSegmentIndex  (bounded in-memory, growing shards of current scope)
//	Warm → SegmentDataPlane (full in-memory, all shards)
//	Cold → ColdSegmentIndex (archived sealed shards, simulated disk latency)
//
// A query first hits the hot index.  If it returns enough results (>= TopK)
// the result is returned immediately.  Otherwise the warm index is searched and
// the merged result set is returned.  The cold tier is consulted only when the
// caller sets IncludeCold = true (time-travel or historical evidence queries).
type TieredDataPlane struct {
	hot  *segmentstore.Index
	warm *SegmentDataPlane
	cold *SegmentDataPlane
}

func NewTieredDataPlane() *TieredDataPlane {
	return &TieredDataPlane{
		hot:  segmentstore.NewIndex(),
		warm: NewSegmentDataPlane(),
		cold: NewSegmentDataPlane(),
	}
}

// HotIndex exposes the raw hot-tier index so the node manager and bootstrap can
// register dedicated InMemoryDataNode / InMemoryIndexNode instances against it.
func (t *TieredDataPlane) HotIndex() *segmentstore.Index { return t.hot }

// WarmPlane exposes the warm-tier plane for node registration.
func (t *TieredDataPlane) WarmPlane() *SegmentDataPlane { return t.warm }

// ColdPlane exposes the cold-tier plane.
func (t *TieredDataPlane) ColdPlane() *SegmentDataPlane { return t.cold }

// Flush syncs the hot-tier index state to the warm plane.
func (t *TieredDataPlane) Flush() error {
	_ = t.warm.Flush()
	_ = t.cold.Flush()
	return nil
}

// Ingest writes to the hot tier first; the object is promoted to warm on the
// next background compaction cycle (modelled by always writing to both here).
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
//  3. Cold plane — archived (only when IncludeCold flag set)
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

	// cold fallback
	coldOutput := t.cold.Search(input)
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
