package storage

import "log"

// PurgeMemoryWarmOnly removes a memory from the hot cache, warm graph edges, and warm ObjectStore.
// It does not touch the cold tier (embeddings or cold-stored Memory blobs may remain orphaned until
// a full TieredObjectStore.HardDeleteMemory or cold GC runs). Use when Runtime has no TieredObjectStore.
func PurgeMemoryWarmOnly(rs RuntimeStorage, memoryID string) {
	PurgeMemoryWarmOnlyWithStats(rs, memoryID)
}

type PurgeWarmStats struct {
	EdgeDeleteSucceeded int
	EdgeDeleteFailed    int
	EdgeDeleteRetried   int
}

// PurgeMemoryWarmOnlyWithStats executes warm-only purge and returns deletion stats.
// GraphEdgeStore does not currently return DeleteEdge errors, so we verify by
// probing GetEdge after each delete and emit warnings for any leftovers.
func PurgeMemoryWarmOnlyWithStats(rs RuntimeStorage, memoryID string) PurgeWarmStats {
	var stats PurgeWarmStats
	if rs == nil {
		return stats
	}
	if hc := rs.HotCache(); hc != nil {
		hc.Evict(memoryID)
	}
	if es := rs.Edges(); es != nil {
		seen := map[string]struct{}{}
		for pass := 0; pass < 2; pass++ {
			edges := es.BulkEdges([]string{memoryID})
			if pass > 0 {
				stats.EdgeDeleteRetried += len(edges)
			}
			if len(edges) == 0 {
				break
			}
			for _, e := range edges {
				if _, ok := seen[e.EdgeID]; ok {
					continue
				}
				seen[e.EdgeID] = struct{}{}
				es.DeleteEdge(e.EdgeID)
				if _, ok := es.GetEdge(e.EdgeID); ok {
					stats.EdgeDeleteFailed++
					log.Printf("purge_warm: delete edge failed edge_id=%s memory_id=%s", e.EdgeID, memoryID)
					continue
				}
				stats.EdgeDeleteSucceeded++
			}
		}
		if stats.EdgeDeleteFailed > 0 {
			log.Printf(
				"purge_warm: completed with edge delete failures memory_id=%s edge_delete_succeeded=%d edge_delete_failed=%d edge_delete_retried=%d",
				memoryID, stats.EdgeDeleteSucceeded, stats.EdgeDeleteFailed, stats.EdgeDeleteRetried,
			)
		}
	}
	if os := rs.Objects(); os != nil {
		os.DeleteMemory(memoryID)
	}
	return stats
}
