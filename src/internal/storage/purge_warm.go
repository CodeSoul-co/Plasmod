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

// Optional capability: delete all edges incident to a given object in one store-level critical section.
// Implemented by in-memory and badger edge stores to reduce read-delete race windows.
type warmEdgeBulkDeleter interface {
	DeleteEdgesByObjectID(objectID string) int
}

// PurgeMemoryWarmOnlyWithStats executes warm-only purge and returns deletion stats.
// GraphEdgeStore does not currently return DeleteEdge errors, so we verify by
// probing GetEdge after each delete and emit warnings for any leftovers.
//
// Known limitation (warm-only degraded path): if new edges are added concurrently after the last
// BulkEdges snapshot, those edges may not be cleaned up by this purge run.
func PurgeMemoryWarmOnlyWithStats(rs RuntimeStorage, memoryID string) PurgeWarmStats {
	var stats PurgeWarmStats
	if rs == nil {
		return stats
	}
	if hc := rs.HotCache(); hc != nil {
		hc.Evict(memoryID)
	}
	if es := rs.Edges(); es != nil {
		if bulk, ok := es.(warmEdgeBulkDeleter); ok {
			stats.EdgeDeleteSucceeded = bulk.DeleteEdgesByObjectID(memoryID)
			residual := es.BulkEdges([]string{memoryID})
			if len(residual) > 0 {
				stats.EdgeDeleteFailed = len(residual)
				log.Printf(
					"purge_warm: bulk delete left residual edges memory_id=%s residual=%d",
					memoryID, len(residual),
				)
			}
		}

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
