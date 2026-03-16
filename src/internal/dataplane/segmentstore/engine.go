package segmentstore

import (
	"fmt"
	"sync"
	"time"
)

const defaultSealRowThreshold = 1024

// Index is the in-process retrieval index used by the ANDB segment data plane.
type Index struct {
	mu               sync.RWMutex
	segments         []*Partition
	exec             *Searcher
	planner          *Planner
	sealRowThreshold int
}

func NewIndex() *Index {
	partition := NewGrowingPartition(fmt.Sprintf("seg_%d", time.Now().UnixNano()), "default")
	return &Index{
		segments:         []*Partition{partition},
		exec:             NewSearcher(),
		planner:          NewPlanner(),
		sealRowThreshold: defaultSealRowThreshold,
	}
}

func (i *Index) InsertObject(objectID string, text string, attrs map[string]string, namespace string, eventUnixTS int64) {
	i.mu.Lock()
	defer i.mu.Unlock()

	if namespace == "" {
		namespace = "default"
	}

	partition := i.ensureGrowingPartition(namespace)
	partition.Insert(Row{ObjectID: objectID, Text: text, Attrs: attrs, EventUnixTS: eventUnixTS})
	if partition.Meta().RowCount >= i.sealRowThreshold {
		partition.Seal()
	}
}

func (i *Index) Search(req SearchRequest) SearchResult {
	i.mu.RLock()
	partitions := make([]*Partition, len(i.segments))
	copy(partitions, i.segments)
	i.mu.RUnlock()

	plan := i.planner.Build(req, partitions)
	return i.exec.Execute(req, plan)
}

func (i *Index) ensureGrowingPartition(namespace string) *Partition {
	for idx := len(i.segments) - 1; idx >= 0; idx-- {
		partition := i.segments[idx]
		if partition.Namespace == namespace && partition.State == PartitionStateGrowing {
			return partition
		}
	}

	partition := NewGrowingPartition(fmt.Sprintf("seg_%d", time.Now().UnixNano()), namespace)
	i.segments = append(i.segments, partition)
	return partition
}
