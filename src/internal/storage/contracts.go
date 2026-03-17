package storage

import "time"

type SegmentRecord struct {
	SegmentID       string    `json:"segment_id"`
	ObjectType      string 	  `json:"object_type"`
	Namespace       string    `json:"namespace"`
	TimeBucket      string    `json:"time_bucket"`
	EmbeddingFamily string    `json:"embedding_family"`
	StorageRef      string    `json:"storage_ref"`
	IndexRef        string    `json:"index_ref"`
	RowCount        int       `json:"row_count"`
	MinTS           string    `json:"min_ts"`
	MaxTS           string    `json:"max_ts"`
	Tier            string    `json:"tier"`
	UpdatedAt       time.Time `json:"updated_at"`
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
