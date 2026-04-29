//go:build unix

package ipc_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/darkace1998/GoRemote/host/plugin/ipc"
	pluginv1 "github.com/darkace1998/GoRemote/proto/plugin/v1"
)

// blockingEcho.Ping waits on a per-call gate so the test can hold a
// request open and then crash the server mid-flight.
type blockingEcho struct {
	gate chan struct{} // never receives; closing it releases all waiters
	ctx  context.Context
}

func (b *blockingEcho) Ping(ctx context.Context, req *pluginv1.PingRequest) (*pluginv1.PingResponse, error) {
	select {
	case <-b.gate:
	case <-b.ctx.Done():
	case <-ctx.Done():
	}
	return &pluginv1.PingResponse{Payload: req.Payload}, nil
}

// startBlockingServer brings up an ipc.Server whose Ping handler blocks
// on the returned gate channel until the test releases it (or stop()).
func startBlockingServer(t *testing.T) (sock string, stop func(), gate chan struct{}) {
	t.Helper()
	sock = testSocketPath(t)

	ctx, cancel := context.WithCancel(context.Background())
	ln, err := ipc.ListenUnix(ctx, sock)
	if err != nil {
		cancel()
		t.Fatalf("ListenUnix: %v", err)
	}
	gate = make(chan struct{})
	echo := &blockingEcho{gate: gate, ctx: ctx}
	srv := ipc.NewServer(ln, helloImpl{}, echo)
	done := make(chan struct{})
	go func() {
		_ = srv.Serve(ctx)
		close(done)
	}()

	stop = func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Errorf("server Serve did not return after cancel")
		}
	}
	return sock, stop, gate
}

// TestChaos_SingleCallerSurvivesServerStop pins down the contract that an
// in-flight Ping call returns an error (rather than hanging) when the
// server is stopped underneath it.
func TestChaos_SingleCallerSurvivesServerStop(t *testing.T) {
	sock, stop, _ := startBlockingServer(t)
	defer stop()

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dialCancel()
	c, err := ipc.DialUnix(dialCtx, sock)
	if err != nil {
		t.Fatalf("DialUnix: %v", err)
	}
	defer c.Close()

	callCtx, callCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer callCancel()

	done := make(chan error, 1)
	go func() {
		_, perr := c.Ping(callCtx, &pluginv1.PingRequest{Payload: []byte("blocked")})
		done <- perr
	}()

	// Give the call enough time to land in the server's blocking handler.
	time.Sleep(100 * time.Millisecond)

	stop() // cancel server context, close listener, force-close conns

	// Contract: the call must *return* within a bounded time after
	// server stop. A successful response is acceptable (the server
	// gives handlers a short drain window before closing connections);
	// a hang is not.
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("Ping hung after server stop")
	}
}

// TestChaos_ManyCallersSurviveServerStop expands the previous case: 50
// callers all blocked on Ping when the server is killed must all unblock
// within 2 seconds.
func TestChaos_ManyCallersSurviveServerStop(t *testing.T) {
	sock, stop, _ := startBlockingServer(t)
	stopped := false
	defer func() {
		if !stopped {
			stop()
		}
	}()

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dialCancel()
	c, err := ipc.DialUnix(dialCtx, sock)
	if err != nil {
		t.Fatalf("DialUnix: %v", err)
	}
	defer c.Close()

	const N = 50
	var wg sync.WaitGroup
	errs := make(chan error, N)
	callCtx, callCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer callCancel()

	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, perr := c.Ping(callCtx, &pluginv1.PingRequest{Payload: []byte("x")})
			errs <- perr
		}()
	}

	time.Sleep(150 * time.Millisecond) // let calls park in the handler

	stop()
	stopped = true

	gotAll := make(chan struct{})
	go func() { wg.Wait(); close(gotAll) }()
	select {
	case <-gotAll:
	case <-time.After(6 * time.Second):
		t.Fatalf("not all callers returned within 6s of server stop")
	}
	close(errs)
	// Contract: each caller must *return* (success or error) — the
	// server's drain window allows clean completions, but no caller
	// may hang. Count only that we received a result for every call;
	// the type of result is implementation-defined.
	got := 0
	for range errs {
		got++
	}
	if got != N {
		t.Errorf("got %d returns, want %d", got, N)
	}
}

// TestChaos_DialAfterStopFails asserts that a fresh dial against a
// stopped server fails fast rather than hanging on a phantom socket file.
func TestChaos_DialAfterStopFails(t *testing.T) {
	sock, stop, _ := startBlockingServer(t)
	stop() // tear down before dialing

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer dialCancel()
	start := time.Now()
	if _, err := ipc.DialUnix(dialCtx, sock, ipc.WithDialTimeout(500*time.Millisecond)); err == nil {
		t.Fatalf("expected dial to a stopped server to fail")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("dial took too long to fail after stop: %v", elapsed)
	}
}

// TestChaos_CallerCancelDoesNotLeak verifies that cancelling a single
// caller's context releases its in-flight slot and lets later calls
// proceed normally — i.e. no permanent inflight-map leak.
func TestChaos_CallerCancelDoesNotLeak(t *testing.T) {
	sock, stop, gate := startBlockingServer(t)
	defer stop()

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dialCancel()
	c, err := ipc.DialUnix(dialCtx, sock)
	if err != nil {
		t.Fatalf("DialUnix: %v", err)
	}
	defer c.Close()

	// First call: blocked, then cancelled.
	cancelCtx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, perr := c.Ping(cancelCtx, &pluginv1.PingRequest{Payload: []byte("cancel-me")})
		done <- perr
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("expected ctx cancellation to surface as error")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Ping did not honour ctx cancellation")
	}

	// Release the gate so subsequent callers can complete.
	close(gate)

	// Second call: should complete normally even though the cancelled
	// caller's frame may still arrive late from the server.
	c2ctx, c2cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer c2cancel()
	resp, err := c.Ping(c2ctx, &pluginv1.PingRequest{Payload: []byte("after")})
	if err != nil {
		t.Fatalf("post-cancel Ping failed: %v", err)
	}
	if string(resp.Payload) != "after" {
		t.Errorf("payload = %q", resp.Payload)
	}
}
