package eventbackbone

import "sync/atomic"

// WatermarkPublisher maintains the advancing time-tick / watermark boundary.
// Workers subscribe to the "timetick" channel to learn the latest safe
// logical timestamp at which data is fully visible (bounded-staleness model
// from spec section 13).
type WatermarkPublisher struct {
	clock     *HybridClock
	bus       Bus
	watermark int64
}

func NewWatermarkPublisher(clock *HybridClock, bus Bus) *WatermarkPublisher {
	return &WatermarkPublisher{clock: clock, bus: bus}
}

// Advance allocates the next logical timestamp, stores it as the current
// watermark, and broadcasts it on the "timetick" channel.
func (w *WatermarkPublisher) Advance() TimeTick {
	ts := w.clock.Next()
	atomic.StoreInt64(&w.watermark, ts)
	tick := TimeTick{LogicalTS: ts}
	w.bus.Publish(Message{Channel: "timetick", Body: tick})
	return tick
}

// Current returns the latest published watermark without advancing it.
func (w *WatermarkPublisher) Current() int64 {
	return atomic.LoadInt64(&w.watermark)
}
