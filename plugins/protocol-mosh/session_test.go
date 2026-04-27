package mosh

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/goremote/goremote/sdk/protocol"
)

func TestSessionCloseIdempotent(t *testing.T) {
	sess := newSession("/bin/sh", []string{"-c", "exec sleep 30"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- sess.Start(ctx, nil, io.Discard) }()

	time.Sleep(50 * time.Millisecond)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := sess.Close(); err != nil {
				t.Errorf("Close: %v", err)
			}
		}()
	}
	wg.Wait()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("Start did not return after Close")
	}

	if err := sess.Close(); err != nil {
		t.Fatalf("Close after stop: %v", err)
	}
}

func TestSessionStartAlreadyStarted(t *testing.T) {
	sess := newSession("/bin/sh", []string{"-c", "exec sleep 30"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done1 := make(chan error, 1)
	go func() { done1 <- sess.Start(ctx, nil, io.Discard) }()

	time.Sleep(50 * time.Millisecond)

	err := sess.Start(ctx, nil, io.Discard)
	if err == nil {
		t.Fatalf("expected error on second Start")
	}
	if !strings.Contains(err.Error(), "already started") {
		t.Fatalf("expected 'already started' error, got: %v", err)
	}

	cancel()
	<-done1
}

func TestSession_ResizeAndSendInputUnsupported(t *testing.T) {
	sess := newSession("/bin/sh", []string{"-c", "true"})
	if err := sess.SendInput(context.Background(), []byte("x")); !errors.Is(err, protocol.ErrUnsupported) {
		t.Fatalf("SendInput err = %v, want ErrUnsupported", err)
	}
	if err := sess.Resize(context.Background(), protocol.Size{Cols: 80, Rows: 24}); !errors.Is(err, protocol.ErrUnsupported) {
		t.Fatalf("Resize err = %v, want ErrUnsupported", err)
	}
}
