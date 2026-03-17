package dataplane

// IngestRecord is the normalised unit of data written to the retrieval plane
// after an Event has been processed by the Materializer.
type IngestRecord struct {
	ObjectID    string
	Text        string
	Namespace   string
	Attributes  map[string]string
	EventUnixTS int64
}

// SearchInput is the query descriptor passed from the semantic layer to the
// data plane.  All fields are optional; zero values mean "no constraint".
type SearchInput struct {
	QueryText      string
	TopK           int
	Namespace      string
	Constraints    []string
	TimeFromUnixTS int64
	TimeToUnixTS   int64
	// IncludeGrowing includes shards still accepting writes (growing state).
	IncludeGrowing bool
	// IncludeCold extends the search to the cold/archived tier.  Set by
	// time-travel and historical evidence queries.  Comes with extra latency.
	IncludeCold bool
}

// SegmentTrace describes one physical shard that was evaluated during search.
type SegmentTrace struct {
	ID        string `json:"id"`
	Namespace string `json:"namespace"`
	State     string `json:"state"`
	RowCount  int    `json:"row_count"`
	MinTS     int64  `json:"min_ts"`
	MaxTS     int64  `json:"max_ts"`
}

// SearchOutput is the result returned by a DataPlane search.
type SearchOutput struct {
	ObjectIDs       []string
	ScannedSegments []string
	PlannedSegments []SegmentTrace
	// Tier indicates which tier(s) were hit ("hot", "warm", "cold", "hot+warm", …).
	Tier string
}

// DataPlane is the interface satisfied by all retrieval execution modules
// (SegmentDataPlane, TieredDataPlane, or a Milvus-backed adapter).
type DataPlane interface {
	Ingest(record IngestRecord) error
	Search(input SearchInput) SearchOutput
	// Flush forces any buffered hot-tier writes to be persisted to the warm
	// tier.  Implementations that do not buffer are allowed to return nil.
	Flush() error
}
