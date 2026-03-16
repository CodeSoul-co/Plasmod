package storage

import "time"

type SegmentRecord struct {
	SegmentID string    `json:"segment_id"`
	Namespace string    `json:"namespace"`
	RowCount  int       `json:"row_count"`
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
