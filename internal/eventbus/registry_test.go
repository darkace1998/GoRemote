package eventbus

import (
	"context"
	"testing"
	"time"
)

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
		p := recover()
		if p == nil {
			t.Fatalf("expected panic on type mismatch")
		} else if got, want := p.(string), "eventbus: registry type mismatch for bus x"; got != want {
			t.Fatalf("panic = %q, want %q", got, want)
		}
	}()
	_ = RegistryGet[string](r, "x")
}
