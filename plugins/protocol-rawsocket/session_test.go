package rawsocket

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/goremote/goremote/sdk/protocol"
)

// startEchoServer starts an in-process TCP echo server on 127.0.0.1:0. The
// returned host and port can be passed to Module.Open. The server shuts down
// when the test finishes.
func startEchoServer(t *testing.T) (host string, port int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			c, err := ln.Accept()
			if err != nil {
				select {
				case <-stop:
					return
				default:
					return
				}
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer c.Close()
				_, _ = io.Copy(c, c)
			}()
		}
	}()

	t.Cleanup(func() {
		close(stop)
		_ = ln.Close()
		wg.Wait()
	})

	addr := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", addr.Port
}

func openTestSession(t *testing.T, host string, port int, settings map[string]any) protocol.Session {
	t.Helper()
	if settings == nil {
		settings = map[string]any{}
	}
	settings[SettingHost] = host
	settings[SettingPort] = port
	if _, ok := settings[SettingConnectTimeoutSeconds]; !ok {
		settings[SettingConnectTimeoutSeconds] = 2
	}
	mod := New()
	sess, err := mod.Open(context.Background(), protocol.OpenRequest{
		Host:       host,
		Port:       port,
		AuthMethod: protocol.AuthNone,
		Settings:   settings,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return sess
}

func TestOpenStartEcho(t *testing.T) {
	host, port := startEchoServer(t)
	sess := openTestSession(t, host, port, nil)
	defer sess.Close()

	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- sess.Start(ctx, stdinR, stdoutW) }()

	if _, err := stdinW.Write([]byte("hello\n")); err != nil {
		t.Fatalf("stdin write: %v", err)
	}

	br := bufio.NewReader(stdoutR)
	line, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if line != "hello\n" {
		t.Fatalf("echo = %q, want %q", line, "hello\n")
	}

	_ = stdinW.Close()
	_ = sess.Close()
	_ = stdoutW.Close()
	_ = stdoutR.Close()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("Start did not return after close")
	}
}

func TestSendInputAppendsLF(t *testing.T) {
	host, port := startEchoServer(t)
	sess := openTestSession(t, host, port, map[string]any{
		SettingEOLMode: EOLModeLF,
	})
	defer sess.Close()

	stdoutR, stdoutW := io.Pipe()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- sess.Start(ctx, nil, stdoutW) }()

	if err := sess.SendInput(ctx, []byte("hello")); err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	br := bufio.NewReader(stdoutR)
	got, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != "hello\n" {
		t.Fatalf("got %q want %q", got, "hello\n")
	}

	// No double append when trailing LF already present.
	if err := sess.SendInput(ctx, []byte("world\n")); err != nil {
		t.Fatalf("SendInput 2: %v", err)
	}
	got, err = br.ReadString('\n')
	if err != nil {
		t.Fatalf("read 2: %v", err)
	}
	if got != "world\n" {
		t.Fatalf("got %q want %q", got, "world\n")
	}
	// Verify no stray extra byte arrived by attempting a short read with a
	// timeout. We can't set a deadline on an io.Pipe, so race a reader
	// goroutine against a timer.
	extra := make(chan byte, 1)
	go func() {
		b, err := br.ReadByte()
		if err == nil {
			extra <- b
		}
	}()
	select {
	case b := <-extra:
		t.Fatalf("unexpected extra byte after 'world\\n': %q", b)
	case <-time.After(100 * time.Millisecond):
	}

	_ = sess.Close()
	_ = stdoutW.Close()
	_ = stdoutR.Close()
	<-done
}

func TestSendInputEOLNone(t *testing.T) {
	host, port := startEchoServer(t)
	sess := openTestSession(t, host, port, map[string]any{
		SettingEOLMode: EOLModeNone,
	})
	defer sess.Close()

	stdoutR, stdoutW := io.Pipe()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- sess.Start(ctx, nil, stdoutW) }()

	if err := sess.SendInput(ctx, []byte("hi")); err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	buf := make([]byte, 2)
	if _, err := io.ReadFull(stdoutR, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != "hi" {
		t.Fatalf("got %q want %q", string(buf), "hi")
	}

	_ = sess.Close()
	_ = stdoutW.Close()
	_ = stdoutR.Close()
	<-done
}

func TestCloseIdempotent(t *testing.T) {
	host, port := startEchoServer(t)
	sess := openTestSession(t, host, port, nil)

	if err := sess.Close(); err != nil {
		t.Fatalf("Close #1: %v", err)
	}
	// Subsequent Close calls (including concurrent ones) must not panic or
	// return a different error.
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = sess.Close()
		}()
	}
	wg.Wait()
}

func TestOpenUnreachablePortFailsFast(t *testing.T) {
	// Grab a port, then close the listener, so we know nothing is listening.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	mod := New()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	start := time.Now()
	_, err = mod.Open(ctx, protocol.OpenRequest{
		Host:       "127.0.0.1",
		Port:       port,
		AuthMethod: protocol.AuthNone,
		Settings: map[string]any{
			SettingHost:                  "127.0.0.1",
			SettingPort:                  port,
			SettingConnectTimeoutSeconds: 2,
		},
	})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("expected dial error for 127.0.0.1:%d", port)
	}
	if !strings.Contains(err.Error(), strconv.Itoa(port)) {
		t.Logf("error does not mention port (informational): %v", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("dial took too long: %v", elapsed)
	}
}

func TestStartCancellationReturnsPromptly(t *testing.T) {
	host, port := startEchoServer(t)
	sess := openTestSession(t, host, port, nil)
	defer sess.Close()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- sess.Start(ctx, nil, io.Discard) }()

	// Give Start a moment to enter its I/O loop.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			// Start treats cancellation as clean and typically returns nil;
			// accept either nil or a ctx error.
			t.Fatalf("Start returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Start did not return promptly after ctx cancel")
	}
}

func TestInvalidEOLMode(t *testing.T) {
	host, port := startEchoServer(t)
	mod := New()
	_, err := mod.Open(context.Background(), protocol.OpenRequest{
		Host:       host,
		Port:       port,
		AuthMethod: protocol.AuthNone,
		Settings: map[string]any{
			SettingHost:    host,
			SettingPort:    port,
			SettingEOLMode: "bogus",
		},
	})
	if err == nil {
		t.Fatalf("expected error for invalid eol_mode")
	}
}
