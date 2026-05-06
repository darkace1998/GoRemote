package rdp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/darkace1998/GoRemote/sdk/protocol"
)

// startEchoServer starts a TCP echo server and returns its address and a
// closer. The server echoes every byte it receives back to the sender.
func startEchoServer(t *testing.T) (addr string, close func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { io.Copy(conn, conn); conn.Close() }() //nolint:errcheck
		}
	}()
	return ln.Addr().String(), func() { _ = ln.Close() }
}

// startFixedReplyServer starts a TCP server that sends a fixed reply to each
// accepted connection and then closes it.
func startFixedReplyServer(t *testing.T, reply []byte) (addr string, close func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, _ = conn.Write(reply)
				_ = conn.Close()
			}()
		}
	}()
	return ln.Addr().String(), func() { _ = ln.Close() }
}

func TestRenderMode(t *testing.T) {
	s := newSession("127.0.0.1:3389")
	if s.RenderMode() != protocol.RenderGraphical {
		t.Fatalf("RenderMode = %s, want graphical", s.RenderMode())
	}
}

func TestStart_ReceivesDataFromServer(t *testing.T) {
	want := []byte("rdp-server-hello")
	addr, closeServer := startFixedReplyServer(t, want)
	defer closeServer()

	sess := newSession(addr)
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := sess.Start(ctx, nil, &out); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !bytes.Equal(out.Bytes(), want) {
		t.Fatalf("output = %q, want %q", out.Bytes(), want)
	}
}

func TestStart_SendsDataToServer(t *testing.T) {
	addr, closeServer := startEchoServer(t)
	defer closeServer()

	sess := newSession(addr)

	pr, pw := io.Pipe()
	var out bytes.Buffer

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- sess.Start(ctx, pr, &out) }()

	// Write then close stdin so the I/O loop terminates.
	msg := []byte("hello-rdp")
	_, _ = pw.Write(msg)
	_ = pw.Close()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return")
	}

	if !bytes.Equal(out.Bytes(), msg) {
		t.Fatalf("echoed = %q, want %q", out.Bytes(), msg)
	}
}

func TestStart_ContextCancellation(t *testing.T) {
	addr, closeServer := startEchoServer(t)
	defer closeServer()

	sess := newSession(addr)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- sess.Start(ctx, nil, io.Discard) }()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after context cancel")
	}
}

func TestStart_DialFailure(t *testing.T) {
	sess := newSession("127.0.0.1:1") // port 1 should not be listening
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := sess.Start(ctx, nil, io.Discard)
	if err == nil {
		t.Fatal("expected dial error, got nil")
	}
}

func TestClose_Idempotent(t *testing.T) {
	addr, closeServer := startEchoServer(t)
	defer closeServer()

	sess := newSession(addr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = sess.Start(ctx, nil, io.Discard) }()
	time.Sleep(30 * time.Millisecond)

	for i := 0; i < 8; i++ {
		if err := sess.Close(); err != nil {
			t.Errorf("Close #%d: %v", i, err)
		}
	}
}

func TestClose_BeforeStart(t *testing.T) {
	sess := newSession("127.0.0.1:3389")
	if err := sess.Close(); err != nil {
		t.Fatalf("Close before Start: %v", err)
	}
}

func TestSendInput_BeforeStart(t *testing.T) {
	sess := newSession("127.0.0.1:3389")
	err := sess.SendInput(context.Background(), []byte("x"))
	if err == nil {
		t.Fatal("expected error when session not started")
	}
}

func TestResize_Unsupported(t *testing.T) {
	sess := newSession("127.0.0.1:3389")
	err := sess.Resize(context.Background(), protocol.Size{Cols: 80, Rows: 24})
	if !errors.Is(err, protocol.ErrUnsupported) {
		t.Fatalf("Resize err = %v, want ErrUnsupported", err)
	}
}
