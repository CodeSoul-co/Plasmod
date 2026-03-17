package segmentstore

import "sync"

// ShardState indicates whether a Shard is still accepting writes or has been
// sealed for read-only access.
type ShardState int

const (
	ShardStateGrowing ShardState = iota
	ShardStateSealed
)

func (s ShardState) String() string {
	if s == ShardStateSealed {
		return "sealed"
	}
	return "growing"
}

// ObjectRecord is a single entry written into a Shard.  It maps directly to
// one canonical object (Event, Memory, Artifact, …) at ingest time.
type ObjectRecord struct {
	ObjectID    string
	Text        string
	Attrs       map[string]string
	EventUnixTS int64
}

// ShardMeta is a lightweight descriptor for a Shard that can be passed around
// without holding the shard's read lock.
type ShardMeta struct {
	ID        string
	Namespace string
	State     ShardState
	RowCount  int
	MinTS     int64
	MaxTS     int64
}

// Shard is the physical unit of storage inside the segment index.  A CogDB
// segment is partitioned into one or more Shards keyed by namespace.
// Growing shards accept writes; sealed shards are read-only.
type Shard struct {
	ID        string
	Namespace string
	State     ShardState
	Records   []ObjectRecord
	MinTS     int64
	MaxTS     int64
	mu        sync.RWMutex
}

func NewGrowingShard(id string, namespace string) *Shard {
	return &Shard{
		ID:        id,
		Namespace: namespace,
		State:     ShardStateGrowing,
		Records:   []ObjectRecord{},
	}
}

func (s *Shard) Insert(rec ObjectRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Records = append(s.Records, rec)
	if rec.EventUnixTS > 0 {
		if s.MinTS == 0 || rec.EventUnixTS < s.MinTS {
			s.MinTS = rec.EventUnixTS
		}
		if rec.EventUnixTS > s.MaxTS {
			s.MaxTS = rec.EventUnixTS
		}
	}
}

func (s *Shard) Seal() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.State = ShardStateSealed
}

func (s *Shard) SnapshotRecords() []ObjectRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]ObjectRecord, len(s.Records))
	copy(out, s.Records)
	return out
}

func (s *Shard) Meta() ShardMeta {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return ShardMeta{
		ID:        s.ID,
		Namespace: s.Namespace,
		State:     s.State,
		RowCount:  len(s.Records),
		MinTS:     s.MinTS,
		MaxTS:     s.MaxTS,
	}
}
