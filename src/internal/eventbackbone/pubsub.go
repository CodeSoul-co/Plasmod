package eventbackbone

import (
	"log"
	"sync"
	"time"
)

type Message struct {
	Channel string
	Body    any
}

type InMemoryBus struct {
	mu   sync.RWMutex
	subs map[string][]chan Message
}

func NewInMemoryBus() *InMemoryBus {
	return &InMemoryBus{subs: map[string][]chan Message{}}
}

func (b *InMemoryBus) Subscribe(channel string) <-chan Message {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan Message, 32)
	b.subs[channel] = append(b.subs[channel], ch)
	return ch
}

// Publish sends a message to all subscribers, blocking up to 5s per subscriber.
// Silently drops only after the timeout expires (backpressure applied upstream
// by the gateway write semaphore makes this path rare in practice).
func (b *InMemoryBus) Publish(msg Message) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subs[msg.Channel] {
		select {
		case ch <- msg:
		case <-time.After(5 * time.Second):
			log.Printf("[pubsub] publish timeout after 5s for channel=%q; message dropped", msg.Channel)
		}
	}
}
