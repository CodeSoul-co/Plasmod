package dataplane

type SegmentSpec struct {
	ObjectType      string
	Namespace       string
	TimeBucket      string
	EmbeddingFamily string
	Tier            string
}
