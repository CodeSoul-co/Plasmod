package dataplane

type IngestRecord struct {
	ObjectID    string
	Text        string
	Namespace   string
	Attributes  map[string]string
	EventUnixTS int64
}

type SearchInput struct {
	QueryText      string
	TopK           int
	Namespace      string
	Constraints    []string
	TimeFromUnixTS int64
	TimeToUnixTS   int64
	IncludeGrowing bool
}

type SegmentTrace struct {
	ID        string `json:"id"`
	Namespace string `json:"namespace"`
	State     string `json:"state"`
	RowCount  int    `json:"row_count"`
	MinTS     int64  `json:"min_ts"`
	MaxTS     int64  `json:"max_ts"`
}

type SearchOutput struct {
	ObjectIDs       []string
	ScannedSegments []string
	PlannedSegments []SegmentTrace
}

type DataPlane interface {
	Ingest(record IngestRecord) error
	Search(input SearchInput) SearchOutput
}
