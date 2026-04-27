package eventbus

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMultipleSubscribersReceive(t *testing.T) {
	t.Parallel()
	b := New[int]()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c1 := b.Subscribe(ctx, 4)
	c2 := b.Subscribe(ctx, 4)

	b.Publish(ctx, 42)

	for i, ch := range []<-chan int{c1, c2} {
		select {
		case got := <-ch:
			if got != 42 {
				t.Fatalf("sub %d: got %d want 42", i, got)
			}
		case <-time.After(time.Second):
			t.Fatalf("sub %d: timeout", i)
		}
	}

	st := b.Stats()
	if st.Subscribers != 2 {
		t.Fatalf("subscribers = %d want 2", st.Subscribers)
	}
	if st.Published != 1 {
		t.Fatalf("published = %d want 1", st.Published)
	}
	if st.Dropped != 0 {
		t.Fatalf("dropped = %d want 0", st.Dropped)
	}
}

func TestUnsubscribeOnContextCancel(t *testing.T) {
	t.Parallel()
	b := New[int]()
	rootCtx := context.Background()

	subCtx, cancel := context.WithCancel(rootCtx)
	ch := b.Subscribe(subCtx, 1)

	cancel()

	// Wait for the subscription goroutine to remove us.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if b.Stats().Subscribers == 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if n := b.Stats().Subscribers; n != 0 {
		t.Fatalf("subscribers = %d want 0 after cancel", n)
	}

	// Channel must close so downstream range/select unblocks.
	select {
	case _, ok := <-ch:
		if ok {
			// ok=true would mean a value leaked; only close is acceptable.
			t.Fatalf("expected channel close, got value")
		}
	case <-time.After(time.Second):
		t.Fatalf("channel not closed after ctx cancel")
	}

	// Further publishes must not block or panic.
	done := make(chan struct{})
	go func() {
		b.Publish(rootCtx, 1)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("Publish blocked after unsubscribe")
	}
}

func TestSlowSubscriberDoesNotBlockFast(t *testing.T) {
	t.Parallel()
	b := New[int]()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	slow := b.Subscribe(ctx, 1)
	fast := b.Subscribe(ctx, 128)

	// Drain fast subscriber eagerly.
	var got atomic.Int64
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range fast {
			got.Add(1)
		}
	}()

	// Deliberately do not read from slow until later.
	start := time.Now()
	for i := 0; i < 50; i++ {
		b.Publish(ctx, i)
	}
	elapsed := time.Since(start)
	if elapsed > 500*time.Millisecond {
		t.Fatalf("publish took too long (%v) — slow sub blocked", elapsed)
	}

	// Now drain slow (up to 1 buffered item expected).
	go func() {
		time.Sleep(20 * time.Millisecond)
		for range slow {
		}
	}()

	cancel()
	wg.Wait()

	if got.Load() < 50 {
		t.Fatalf("fast subscriber received %d want 50", got.Load())
	}

	st := b.Stats()
	if st.Published != 50 {
		t.Fatalf("published = %d want 50", st.Published)
	}
	if st.Dropped == 0 {
		t.Fatalf("expected drops on slow subscriber, got 0")
	}
}

func TestStatsCountsDropped(t *testing.T) {
	t.Parallel()
	b := New[int]()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_ = b.Subscribe(ctx, 1) // never drained

	for i := 0; i < 10; i++ {
		b.Publish(ctx, i)
	}
	st := b.Stats()
	if st.Published != 10 {
		t.Fatalf("published = %d want 10", st.Published)
	}
	// First publish fills buffer; remaining 9 drop.
	if st.Dropped != 9 {
		t.Fatalf("dropped = %d want 9", st.Dropped)
	}
}

func TestCloseRejectsPublish(t *testing.T) {
	t.Parallel()
	b := New[int]()
	ctx := context.Background()
	ch := b.Subscribe(ctx, 1)

	b.Close()

	// Channel should be closed.
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatalf("channel should be closed after Close()")
		}
	case <-time.After(time.Second):
		t.Fatalf("channel not closed after Close()")
	}

	// Publish must not panic; must be a no-op.
	b.Publish(ctx, 1)

	// Subscribing after Close returns a closed channel.
	ch2 := b.Subscribe(ctx, 1)
	if _, ok := <-ch2; ok {
		t.Fatalf("expected closed channel from Subscribe after Close")
	}

	// Close is idempotent.
	b.Close()

	if st := b.Stats(); st.Subscribers != 0 {
		t.Fatalf("subscribers = %d want 0 after Close", st.Subscribers)
	}
}

func TestConcurrentSubscribePublishClose(t *testing.T) {
	t.Parallel()
	b := New[int]()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			subCtx, subCancel := context.WithCancel(ctx)
			defer subCancel()
			ch := b.Subscribe(subCtx, 4)
			for j := 0; j < 100; j++ {
				select {
				case <-ch:
				default:
				}
			}
		}()
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				b.Publish(ctx, j)
			}
		}()
	}
	wg.Wait()
	b.Close()
}

func TestRegistryLazyAndShared(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	b1 := RegistryGet[int](r, "ints")
	b2 := RegistryGet[int](r, "ints")
	if b1 != b2 {
		t.Fatalf("expected shared bus instance")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := b1.Subscribe(ctx, 1)
	b2.Publish(ctx, 7)
	select {
	case v := <-ch:
		if v != 7 {
			t.Fatalf("got %d want 7", v)
		}
	case <-time.After(time.Second):
		t.Fatalf("timeout receiving from shared bus")
	}

	bs := RegistryGet[string](r, "strings")
	if bs == nil {
		t.Fatalf("expected non-nil string bus")
	}

	r.Close()
}

func TestRegistryTypeMismatchPanics(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	_ = RegistryGet[int](r, "x")
	defer func() {
		if recover() == nil {
			t.Fatalf("expected panic on type mismatch")
		}
	}()
	_ = RegistryGet[string](r, "x")
}
