package segmentstore

import "sync"

type PartitionState int

const (
	PartitionStateGrowing PartitionState = iota
	PartitionStateSealed
)

func (s PartitionState) String() string {
	if s == PartitionStateSealed {
		return "sealed"
	}
	return "growing"
}

type Row struct {
	ObjectID    string
	Text        string
	Attrs       map[string]string
	EventUnixTS int64
}

type PartitionMeta struct {
	ID        string
	Namespace string
	State     PartitionState
	RowCount  int
	MinTS     int64
	MaxTS     int64
}

type Partition struct {
	ID        string
	Namespace string
	State     PartitionState
	Rows      []Row
	MinTS     int64
	MaxTS     int64
	mu        sync.RWMutex
}

func NewGrowingPartition(id string, namespace string) *Partition {
	return &Partition{
		ID:        id,
		Namespace: namespace,
		State:     PartitionStateGrowing,
		Rows:      []Row{},
	}
}

func (p *Partition) Insert(row Row) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.Rows = append(p.Rows, row)
	if row.EventUnixTS > 0 {
		if p.MinTS == 0 || row.EventUnixTS < p.MinTS {
			p.MinTS = row.EventUnixTS
		}
		if row.EventUnixTS > p.MaxTS {
			p.MaxTS = row.EventUnixTS
		}
	}
}

func (p *Partition) Seal() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.State = PartitionStateSealed
}

func (p *Partition) SnapshotRows() []Row {
	p.mu.RLock()
	defer p.mu.RUnlock()

	out := make([]Row, len(p.Rows))
	copy(out, p.Rows)
	return out
}

func (p *Partition) Meta() PartitionMeta {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return PartitionMeta{
		ID:        p.ID,
		Namespace: p.Namespace,
		State:     p.State,
		RowCount:  len(p.Rows),
		MinTS:     p.MinTS,
		MaxTS:     p.MaxTS,
	}
}
