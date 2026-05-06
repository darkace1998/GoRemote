package sftp

import (
	"context"
	"io"
	"testing"
	"time"
)

// TestStart_StdinBlockedCtxCancel verifies that Start returns promptly when ctx
// is cancelled even if the stdin reader is permanently blocked (never delivers
// a line). Without the select-based goroutine pattern this would block forever.
func TestStart_StdinBlockedCtxCancel(t *testing.T) {
	// Build a bare Session with a closed channel so Close() is a no-op.
	sess := &Session{closed: make(chan struct{})}

	// io.Pipe reader blocks until the write end is written to or closed.
	stdinR, _ := io.Pipe()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- sess.Start(ctx, stdinR, io.Discard) }()

	// Give Start a moment to reach the select, then cancel.
	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Start returned — goroutine leak is fixed.
	case <-time.After(time.Second):
		t.Fatal("Start did not return within 1s after ctx cancel with blocked stdin")
	}
}
