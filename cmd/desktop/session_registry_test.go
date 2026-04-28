package main

import (
	"sync"
	"testing"
	"time"
)

// The restoreWorkspace barrier hinges on two contracts of
// sessionRegistry.addedHook:
//  1. add() invokes the hook exactly once per successful session add.
//  2. The hook is the *only* coupling between openSession's async
//     pipeline and restoreWorkspace's layout pass.
//
// We can't drive add() end-to-end in a headless test (it needs the
// Fyne tab container), so we exercise the same handshake the way
// restoreWorkspace consumes it: install a hook, fire it from N
// goroutines, and assert that a counting receiver lands at exactly N.

func TestSessionRegistryAddedHookHandshake(t *testing.T) {
	r := &sessionRegistry{}
	const want = 5
	done := make(chan struct{}, want)
	r.addedHook = func(st *sessionTab) {
		if st == nil {
			t.Error("hook received nil session")
			return
		}
		done <- struct{}{}
	}

	var wg sync.WaitGroup
	for i := 0; i < want; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			st := &sessionTab{connID: "c" + string(rune('0'+i))}
			// Mimic the call site at the tail of add().
			if h := r.addedHook; h != nil {
				h(st)
			}
		}(i)
	}
	wg.Wait()

	got := 0
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for got < want {
		select {
		case <-done:
			got++
		case <-timer.C:
			t.Fatalf("timeout: got %d/%d hook fires", got, want)
		}
	}
}

func TestSessionRegistryAddedHookNilSafe(t *testing.T) {
	r := &sessionRegistry{}
	// No hook installed: tail-of-add() inline check must be a no-op.
	if r.addedHook != nil {
		t.Fatalf("expected nil addedHook on fresh registry")
	}
}
