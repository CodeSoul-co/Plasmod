package eventbackbone

import "sync/atomic"

type HybridClock struct {
	seq int64
}

func NewHybridClock() *HybridClock {
	return &HybridClock{}
}

func (c *HybridClock) Next() int64 {
	return atomic.AddInt64(&c.seq, 1)
}
