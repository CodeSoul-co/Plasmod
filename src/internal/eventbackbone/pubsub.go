package eventbackbone

import "sync"

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

func (b *InMemoryBus) Publish(msg Message) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subs[msg.Channel] {
		select {
		case ch <- msg:
		default:
		}
	}
}
