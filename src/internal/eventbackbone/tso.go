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

// AdvanceTo moves the clock forward without iterating through intermediate values.
func (c *HybridClock) AdvanceTo(target int64) int64 {
	for {
		current := atomic.LoadInt64(&c.seq)
		if current >= target {
			return current
		}
		if atomic.CompareAndSwapInt64(&c.seq, current, target) {
			return target
		}
	}
}
