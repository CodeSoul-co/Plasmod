package dataplane

import (
	"andb/src/internal/dataplane/segmentstore"
)

// SegmentDataPlane is the first-party retrieval execution module used by ANDB.
// It is inspired by segment-oriented systems, but its naming reflects the ANDB
// module boundary rather than an external project.
type SegmentDataPlane struct {
	index *segmentstore.Index
}

func NewSegmentDataPlane() *SegmentDataPlane {
	return &SegmentDataPlane{index: segmentstore.NewIndex()}
}

func (p *SegmentDataPlane) Ingest(record IngestRecord) error {
	namespace := record.Namespace
	if namespace == "" {
		namespace = "default"
	}
	p.index.InsertObject(record.ObjectID, record.Text, record.Attributes, namespace, record.EventUnixTS)
	return nil
}

func (p *SegmentDataPlane) Search(input SearchInput) SearchOutput {
	result := p.index.Search(segmentstore.SearchRequest{
		Query:          input.QueryText,
		TopK:           input.TopK,
		Namespace:      input.Namespace,
		MinEventUnixTS: input.TimeFromUnixTS,
		MaxEventUnixTS: input.TimeToUnixTS,
		IncludeGrowing: input.IncludeGrowing,
	})

	ids := make([]string, 0, len(result.Hits))
	for _, hit := range result.Hits {
		ids = append(ids, hit.ObjectID)
	}

	planned := make([]SegmentTrace, 0, len(result.PlannedMeta))
	for _, meta := range result.PlannedMeta {
		planned = append(planned, SegmentTrace{
			ID:        meta.ID,
			Namespace: meta.Namespace,
			State:     meta.State.String(),
			RowCount:  meta.RowCount,
			MinTS:     meta.MinTS,
			MaxTS:     meta.MaxTS,
		})
	}

	return SearchOutput{
		ObjectIDs:       ids,
		ScannedSegments: result.ScannedSegments,
		PlannedSegments: planned,
	}
}
