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
	shards           []*Shard
	objectLocations  map[string]objectLocation
	exec             *Searcher
	planner          *Planner
	sealRowThreshold int
}

type objectLocation struct {
	shard       *Shard
	recordIndex int
}

func NewIndex() *Index {
	shard := NewGrowingShard(fmt.Sprintf("shard_%d", time.Now().UnixNano()), "default")
	return &Index{
		shards:           []*Shard{shard},
		objectLocations:  map[string]objectLocation{},
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

	record := ObjectRecord{ObjectID: objectID, Text: text, Attrs: attrs, EventUnixTS: eventUnixTS}
	key := namespace + "\x00" + objectID
	if location, ok := i.objectLocations[key]; ok {
		location.shard.Replace(location.recordIndex, record)
		return
	}

	shard := i.ensureGrowingShard(namespace)
	recordIndex := shard.Insert(record)
	i.objectLocations[key] = objectLocation{shard: shard, recordIndex: recordIndex}
	if shard.Meta().RowCount >= i.sealRowThreshold {
		shard.Seal()
	}
}

func (i *Index) Search(req SearchRequest) SearchResult {
	i.mu.RLock()
	shards := make([]*Shard, len(i.shards))
	copy(shards, i.shards)
	i.mu.RUnlock()

	plan := i.planner.Build(req, shards)
	return i.exec.Execute(req, plan)
}

func (i *Index) ensureGrowingShard(namespace string) *Shard {
	for idx := len(i.shards) - 1; idx >= 0; idx-- {
		shard := i.shards[idx]
		if shard.Namespace == namespace && shard.State == ShardStateGrowing {
			return shard
		}
	}

	shard := NewGrowingShard(fmt.Sprintf("shard_%d", time.Now().UnixNano()), namespace)
	i.shards = append(i.shards, shard)
	return shard
}
