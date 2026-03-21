package coordination

import (
	"fmt"
	"sync"

	"andb/src/internal/schemas"
	"andb/src/internal/worker/nodes"
)

// InMemoryMicroBatchScheduler buffers retrieval task payloads and flushes
// them as a batch when Flush() is called or the buffer size reaches threshold.
type InMemoryMicroBatchScheduler struct {
	id        string
	mu        sync.Mutex
	queue     []any
	batchSize int
	threshold int
}

func CreateInMemoryMicroBatchScheduler(id string, batchSize int) *InMemoryMicroBatchScheduler {
	if batchSize <= 0 {
		batchSize = 32
	}
	return &InMemoryMicroBatchScheduler{
		id:        id,
		batchSize: batchSize,
		threshold: batchSize, // default threshold equals batchSize
		queue:     make([]any, 0, batchSize),
	}
}

func (w *InMemoryMicroBatchScheduler) Run(input schemas.WorkerInput) (schemas.WorkerOutput, error) {
	in, ok := input.(schemas.MicroBatchEnqueueInput)
	if !ok {
		return schemas.MicroBatchFlushOutput{}, fmt.Errorf("micro_batch: unexpected input type %T (use Flush() directly to drain)", input)
	}
	flushed := w.Enqueue(in.QueryID, in.Payload)
	w.mu.Lock()
	pending := len(w.queue)
	w.mu.Unlock()
	return schemas.MicroBatchFlushOutput{Count: pending, Flushed: len(flushed)}, nil
}

func (w *InMemoryMicroBatchScheduler) Info() nodes.NodeInfo {
	return nodes.NodeInfo{
		ID:           w.id,
		Type:         nodes.NodeTypeMicroBatch,
		State:        nodes.NodeStateReady,
		Capabilities: []string{"query_batching", "cross_agent_merge", "backpressure"},
	}
}

func (w *InMemoryMicroBatchScheduler) Enqueue(_ string, payload any) []any {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.queue = append(w.queue, payload)
	// Auto-flush when threshold is reached
	if w.threshold > 0 && len(w.queue) >= w.threshold {
		out := make([]any, len(w.queue))
		copy(out, w.queue)
		w.queue = w.queue[:0]
		return out
	}
	return nil
}

func (w *InMemoryMicroBatchScheduler) SetThreshold(size int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if size <= 0 {
		size = 32
	}
	w.threshold = size
}

func (w *InMemoryMicroBatchScheduler) Flush() []any {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]any, len(w.queue))
	copy(out, w.queue)
	w.queue = w.queue[:0]
	return out
}
