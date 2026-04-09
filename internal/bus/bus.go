package bus

import (
	"sync"
	"sync/atomic"
)

// DefaultBufferSize is the per-subscriber channel capacity used when no
// explicit buffer is supplied to [Bus.Subscribe].
const DefaultBufferSize = 256

// Handler is the callback signature subscribers register. Each handler
// runs serially on its own dispatch goroutine, so handlers should not
// assume anything about concurrency between subscribers.
type Handler func(Event)

// Bus is the in-process pub/sub bus. The zero value is not usable; call
// [New] to construct one.
type Bus struct {
	mu          sync.RWMutex
	subscribers map[uint64]*subscription
	nextID      uint64
	closed      atomic.Bool
}

// Stats reports counters useful for debugging slow subscribers and
// hot loops.
type Stats struct {
	Subscribers int
	Published   uint64
	Dropped     uint64
}

type subscription struct {
	id      uint64
	handler Handler
	ch      chan Event
	dropped uint64
	done    chan struct{}
}

// New constructs an empty Bus.
func New() *Bus {
	return &Bus{
		subscribers: make(map[uint64]*subscription),
	}
}

// Subscribe registers handler. The returned function unsubscribes the
// handler and tears down its dispatch goroutine. bufferSize <= 0 falls
// back to [DefaultBufferSize].
func (b *Bus) Subscribe(handler Handler) func() {
	return b.SubscribeWithBuffer(handler, DefaultBufferSize)
}

// SubscribeWithBuffer is the explicit-buffer variant of [Bus.Subscribe].
func (b *Bus) SubscribeWithBuffer(handler Handler, bufferSize int) func() {
	if bufferSize <= 0 {
		bufferSize = DefaultBufferSize
	}
	if handler == nil {
		return func() {}
	}

	sub := &subscription{
		handler: handler,
		ch:      make(chan Event, bufferSize),
		done:    make(chan struct{}),
	}

	b.mu.Lock()
	b.nextID++
	sub.id = b.nextID
	b.subscribers[sub.id] = sub
	b.mu.Unlock()

	go sub.run()

	return func() { b.unsubscribe(sub) }
}

// Publish fans out an event to every active subscriber. It is
// non-blocking with respect to slow consumers: if a subscriber's
// channel is full the oldest event is dropped to make room and the
// drop counter increments.
func (b *Bus) Publish(e Event) {
	if b.closed.Load() {
		return
	}
	b.mu.RLock()
	subs := make([]*subscription, 0, len(b.subscribers))
	for _, s := range b.subscribers {
		subs = append(subs, s)
	}
	b.mu.RUnlock()

	for _, s := range subs {
		s.deliver(e)
	}
}

// Close stops every subscriber. Subsequent Publish calls become
// no-ops. It is safe to call Close more than once.
func (b *Bus) Close() {
	if !b.closed.CompareAndSwap(false, true) {
		return
	}
	b.mu.Lock()
	subs := make([]*subscription, 0, len(b.subscribers))
	for _, s := range b.subscribers {
		subs = append(subs, s)
	}
	b.subscribers = nil
	b.mu.Unlock()

	for _, s := range subs {
		close(s.ch)
		<-s.done
	}
}

// Stats reports the current bus state.
func (b *Bus) Stats() Stats {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var dropped uint64
	for _, s := range b.subscribers {
		dropped += atomic.LoadUint64(&s.dropped)
	}
	return Stats{
		Subscribers: len(b.subscribers),
		Dropped:     dropped,
	}
}

func (b *Bus) unsubscribe(sub *subscription) {
	b.mu.Lock()
	if _, ok := b.subscribers[sub.id]; !ok {
		b.mu.Unlock()
		return
	}
	delete(b.subscribers, sub.id)
	b.mu.Unlock()

	close(sub.ch)
	<-sub.done
}

func (s *subscription) deliver(e Event) {
	for {
		select {
		case s.ch <- e:
			return
		default:
			// Channel is full: drop the oldest event to make space
			// and try again. The drop counter is incremented for
			// each evicted event.
			select {
			case <-s.ch:
				atomic.AddUint64(&s.dropped, 1)
			default:
				// Another goroutine drained between the failed
				// send and the drop attempt; loop and retry.
			}
		}
	}
}

func (s *subscription) run() {
	defer close(s.done)
	for e := range s.ch {
		s.handler(e)
	}
}
