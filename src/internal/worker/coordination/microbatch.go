package coordination

import (
	"sync"

	"andb/src/internal/worker/nodes"
)

// InMemoryMicroBatchScheduler buffers retrieval task payloads and flushes
// them as a batch when Flush() is called or the buffer size is reached.
type InMemoryMicroBatchScheduler struct {
	id        string
	mu        sync.Mutex
	queue     []any
	batchSize int
}

func CreateInMemoryMicroBatchScheduler(id string, batchSize int) *InMemoryMicroBatchScheduler {
	if batchSize <= 0 {
		batchSize = 32
	}
	return &InMemoryMicroBatchScheduler{id: id, batchSize: batchSize, queue: make([]any, 0, batchSize)}
}

func (w *InMemoryMicroBatchScheduler) Info() nodes.NodeInfo {
	return nodes.NodeInfo{
		ID:           w.id,
		Type:         nodes.NodeTypeMicroBatch,
		State:        nodes.NodeStateReady,
		Capabilities: []string{"query_batching", "cross_agent_merge", "backpressure"},
	}
}

func (w *InMemoryMicroBatchScheduler) Enqueue(_ string, payload any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.queue = append(w.queue, payload)
}

func (w *InMemoryMicroBatchScheduler) Flush() []any {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]any, len(w.queue))
	copy(out, w.queue)
	w.queue = w.queue[:0]
	return out
}
