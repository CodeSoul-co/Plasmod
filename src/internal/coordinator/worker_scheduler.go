package coordinator

import "sync"

// WorkerType enumerates all 14 worker kinds from spec section 16.4.
type WorkerType string

const (
	WorkerTypeIngest               WorkerType = "ingest"
	WorkerTypeObjectMaterialization WorkerType = "object_materialization"
	WorkerTypeMemoryExtraction     WorkerType = "memory_extraction"
	WorkerTypeMemoryConsolidation  WorkerType = "memory_consolidation"
	WorkerTypeStateMaterialization WorkerType = "state_materialization"
	WorkerTypeToolTrace            WorkerType = "tool_trace"
	WorkerTypeSummarization        WorkerType = "summarization"
	WorkerTypeReflectionPolicy     WorkerType = "reflection_policy"
	WorkerTypeCommunication        WorkerType = "communication"
	WorkerTypeIndexBuild           WorkerType = "index_build"
	WorkerTypeGraphRelation        WorkerType = "graph_relation"
	WorkerTypeProofTrace           WorkerType = "proof_trace"
	WorkerTypeConflictMerge        WorkerType = "conflict_merge"
	WorkerTypeQuery                WorkerType = "query"
	WorkerTypeMicroBatchScheduler  WorkerType = "micro_batch_scheduler"
)

// workerStat tracks lightweight scheduling metadata per worker type.
type workerStat struct {
	Dispatched int64
	Active     int
}

// WorkerScheduler coordinates the lifecycle of stateless worker nodes and
// tracks dispatch counts per worker type.
type WorkerScheduler struct {
	mu    sync.RWMutex
	stats map[WorkerType]*workerStat
}

func NewWorkerScheduler() *WorkerScheduler {
	s := &WorkerScheduler{stats: map[WorkerType]*workerStat{}}
	for _, wt := range []WorkerType{
		WorkerTypeIngest, WorkerTypeObjectMaterialization, WorkerTypeMemoryExtraction,
		WorkerTypeMemoryConsolidation, WorkerTypeStateMaterialization, WorkerTypeToolTrace,
		WorkerTypeSummarization, WorkerTypeReflectionPolicy, WorkerTypeCommunication,
		WorkerTypeIndexBuild, WorkerTypeGraphRelation, WorkerTypeProofTrace,
		WorkerTypeConflictMerge, WorkerTypeQuery, WorkerTypeMicroBatchScheduler,
	} {
		s.stats[wt] = &workerStat{}
	}
	return s
}

// Dispatch records a task dispatch for the given worker type.
func (s *WorkerScheduler) Dispatch(wt WorkerType) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if stat, ok := s.stats[wt]; ok {
		stat.Dispatched++
		stat.Active++
	}
}

// Complete records the completion of a dispatched task.
func (s *WorkerScheduler) Complete(wt WorkerType) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if stat, ok := s.stats[wt]; ok && stat.Active > 0 {
		stat.Active--
	}
}

// Stats returns a snapshot of dispatch/active counts per worker type.
func (s *WorkerScheduler) Stats() map[string]map[string]int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]map[string]int64, len(s.stats))
	for wt, stat := range s.stats {
		out[string(wt)] = map[string]int64{
			"dispatched": stat.Dispatched,
			"active":     int64(stat.Active),
		}
	}
	return out
}
