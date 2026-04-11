package cognitive

import (
	"plasmod/src/internal/schemas"
)

// defaultAlgorithm is a no-op MemoryManagementAlgorithm used when no custom
// algorithm plugin is configured.  It is a valid, functional stub that returns
// empty slices for all lifecycle operations and satisfies the interface.
//
// Real deployments should replace this with a concrete algorithm (e.g. MemoryBank,
// decay-only scheduler, graph builder) by passing it to CreateAlgorithmDispatchWorker.
type defaultAlgorithm struct{}

func (d *defaultAlgorithm) AlgorithmID() string { return "noop" }

func (d *defaultAlgorithm) Ingest(memories []schemas.Memory, _ schemas.AlgorithmContext) []schemas.MemoryAlgorithmState {
	return nil
}

func (d *defaultAlgorithm) Update(memories []schemas.Memory, signals map[string]float64) []schemas.MemoryAlgorithmState {
	return nil
}

func (d *defaultAlgorithm) Recall(_ string, candidates []schemas.Memory, _ schemas.AlgorithmContext) []schemas.ScoredMemory {
	// Fall back to identity ranking when no algorithm is configured.
	r := make([]schemas.ScoredMemory, 0, len(candidates))
	for _, m := range candidates {
		r = append(r, schemas.ScoredMemory{Memory: m, Score: 1.0})
	}
	return r
}

func (d *defaultAlgorithm) Compress(memories []schemas.Memory, _ schemas.AlgorithmContext) []schemas.Memory {
	return nil
}

func (d *defaultAlgorithm) Decay(memories []schemas.Memory, _ string) []schemas.MemoryAlgorithmState {
	return nil
}

func (d *defaultAlgorithm) Summarize(memories []schemas.Memory, _ schemas.AlgorithmContext) []schemas.Memory {
	return nil
}

func (d *defaultAlgorithm) ExportState(_ string) (schemas.MemoryAlgorithmState, bool) {
	return schemas.MemoryAlgorithmState{}, false
}

func (d *defaultAlgorithm) LoadState(_ schemas.MemoryAlgorithmState) {}

// NewDefaultAlgorithm returns a shared no-op algorithm instance.
// It is safe for concurrent use.
func NewDefaultAlgorithm() schemas.MemoryManagementAlgorithm {
	return &defaultAlgorithm{}
}
