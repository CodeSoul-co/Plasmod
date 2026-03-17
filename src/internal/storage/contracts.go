package storage

import "time"

type SegmentRecord struct {
	SegmentID string `json:"segment_id"`
	// ObjectType is the canonical object category materialized into this retrieval segment (e.g. memory/event/artifact).
	ObjectType string `json:"object_type"`
	Namespace  string `json:"namespace"`
	// TimeBucket is a coarse temporal bucket identifier for segment partitioning (e.g. 2026w11, 2026-03-17).
	TimeBucket string `json:"time_bucket"`
	// EmbeddingFamily identifies the embedding family/index family used by this segment, if applicable.
	EmbeddingFamily string `json:"embedding_family"`
	// StorageRef points to the underlying persisted segment payload.
	StorageRef string `json:"storage_ref"`
	// IndexRef points to the segment's index metadata/payload reference.
	IndexRef string `json:"index_ref"`
	RowCount int    `json:"row_count"`
	// MinTS/MaxTS represent the temporal range covered by this segment (RFC3339 recommended).
	MinTS string `json:"min_ts"`
	MaxTS string `json:"max_ts"`
	// Tier indicates hot/cold tier placement.
	Tier string `json:"tier"`
	UpdatedAt time.Time `json:"updated_at"`
}

type IndexRecord struct {
	Namespace string    `json:"namespace"`
	Indexed   int       `json:"indexed"`
	UpdatedAt time.Time `json:"updated_at"`
}

type SegmentStore interface {
	Upsert(record SegmentRecord)
	List(namespace string) []SegmentRecord
}

type IndexStore interface {
	Upsert(record IndexRecord)
	List() []IndexRecord
}

type RuntimeStorage interface {
	Segments() SegmentStore
	Indexes() IndexStore
}
