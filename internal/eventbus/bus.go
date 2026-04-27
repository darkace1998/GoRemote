// Package eventbus provides a typed, in-process publish/subscribe event bus
// used by the goremote application core. It is safe for concurrent use and
// isolates slow subscribers so they cannot block publishers or other
// subscribers.
package eventbus

import (
	"context"
	"sync"
	"sync/atomic"
)

// BusStats reports runtime counters for a Bus.
type BusStats struct {
	Subscribers int
	Published   uint64
	Dropped     uint64
}

type subscription[T any] struct {
	ch      chan T
	dropped uint64
}

// Bus is a typed in-process pub/sub event bus.
//
// Publish fans out to all current subscribers. A subscriber whose channel is
// full has the event dropped (counted in Stats().Dropped) rather than blocking
// the publisher or other subscribers.
type Bus[T any] struct {
	mu        sync.RWMutex
	subs      map[*subscription[T]]struct{}
	closed    bool
	published atomic.Uint64
	dropped   atomic.Uint64
}

// New constructs an empty Bus.
func New[T any]() *Bus[T] {
	return &Bus[T]{
		subs: make(map[*subscription[T]]struct{}),
	}
}

// Subscribe registers a new subscriber and returns its receive channel.
//
// buffer is the channel buffer size; buffer=0 means an unbuffered channel,
// for which events are dropped if the subscriber is not actively receiving.
//
// The subscription is automatically removed and the channel closed when ctx
// is cancelled or the Bus is closed.
func (b *Bus[T]) Subscribe(ctx context.Context, buffer int) <-chan T {
	if buffer < 0 {
		buffer = 0
	}
	s := &subscription[T]{ch: make(chan T, buffer)}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		close(s.ch)
		return s.ch
	}
	b.subs[s] = struct{}{}
	b.mu.Unlock()

	go func() {
		<-ctx.Done()
		b.removeSub(s)
	}()

	return s.ch
}

func (b *Bus[T]) removeSub(s *subscription[T]) {
	b.mu.Lock()
	if _, ok := b.subs[s]; !ok {
		b.mu.Unlock()
		return
	}
	delete(b.subs, s)
	closed := b.closed
	b.mu.Unlock()
	if !closed {
		close(s.ch)
	}
}

// Publish delivers ev to every current subscriber. If a subscriber's channel
// is full, the event is dropped for that subscriber. Publish never blocks on
// slow subscribers. If ctx is already cancelled, Publish returns without
// delivering.
func (b *Bus[T]) Publish(ctx context.Context, ev T) {
	if err := ctx.Err(); err != nil {
		return
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return
	}
	b.published.Add(1)
	for s := range b.subs {
		select {
		case s.ch <- ev:
		default:
			atomic.AddUint64(&s.dropped, 1)
			b.dropped.Add(1)
		}
	}
}

// Close closes all subscriber channels and causes future Publish calls to
// become no-ops. Close is idempotent.
func (b *Bus[T]) Close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	subs := b.subs
	b.subs = make(map[*subscription[T]]struct{})
	b.mu.Unlock()

	for s := range subs {
		close(s.ch)
	}
}

// Stats returns a snapshot of the Bus counters.
func (b *Bus[T]) Stats() BusStats {
	b.mu.RLock()
	n := len(b.subs)
	b.mu.RUnlock()
	return BusStats{
		Subscribers: n,
		Published:   b.published.Load(),
		Dropped:     b.dropped.Load(),
	}
}
