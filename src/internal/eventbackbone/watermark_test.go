package eventbackbone

import (
	"testing"
	"time"
)

func TestWatermarkPublisherAdvanceToIsMonotonicAndPublishesLSN(t *testing.T) {
	t.Parallel()

	bus := NewInMemoryBus()
	pub := NewWatermarkPublisher(NewHybridClock(), bus)
	ticks := bus.Subscribe("timetick")

	first := pub.AdvanceTo(17)
	if first.LogicalTS != 17 || pub.Current() != 17 {
		t.Fatalf("first advance = %+v current=%d", first, pub.Current())
	}
	select {
	case msg := <-ticks:
		tick, ok := msg.Body.(TimeTick)
		if !ok || tick.LogicalTS != 17 {
			t.Fatalf("published body = %#v", msg.Body)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for timetick")
	}

	second := pub.AdvanceTo(9)
	if second.LogicalTS != 17 || pub.Current() != 17 {
		t.Fatalf("regressing advance changed watermark: tick=%+v current=%d", second, pub.Current())
	}
	select {
	case msg := <-ticks:
		t.Fatalf("regressing advance published unexpected message: %#v", msg)
	case <-time.After(20 * time.Millisecond):
	}
}
