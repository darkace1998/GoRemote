package rlogin

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darkace1998/GoRemote/sdk/protocol"
)

func TestBuildHandshake(t *testing.T) {
	got, err := buildHandshake("alice", "bob", "xterm-256color", 38400)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []byte{0x00}
	want = append(want, []byte("alice")...)
	want = append(want, 0x00)
	want = append(want, []byte("bob")...)
	want = append(want, 0x00)
	want = append(want, []byte("xterm-256color/38400")...)
	want = append(want, 0x00)
	if !bytes.Equal(got, want) {
		t.Fatalf("handshake bytes mismatch:\n got: %q\nwant: %q", got, want)
	}
	if got[0] != 0x00 {
		t.Fatalf("handshake must start with 0x00, got 0x%02x", got[0])
	}
	// Exactly four null-terminated segments (the leading 0x00 counts as an
	// empty zero-th segment before client_user).
	if n := bytes.Count(got, []byte{0x00}); n != 4 {
		t.Fatalf("expected 4 NUL bytes in handshake, got %d", n)
	}
}

func TestBuildHandshake_EmptyClientUserAllowed(t *testing.T) {
	got, err := buildHandshake("", "bob", "xterm", 9600)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []byte{0x00, 0x00}
	want = append(want, []byte("bob")...)
	want = append(want, 0x00)
	want = append(want, []byte("xterm/9600")...)
	want = append(want, 0x00)
	if !bytes.Equal(got, want) {
		t.Fatalf("handshake mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestBuildHandshake_RejectsEmbeddedNUL(t *testing.T) {
	if _, err := buildHandshake("a\x00b", "bob", "xterm", 9600); err == nil {
		t.Fatal("expected error for embedded NUL in client_username")
	}
	if _, err := buildHandshake("alice", "", "xterm", 9600); err == nil {
		t.Fatal("expected error for empty server username")
	}
}

func TestBuildWindowSize(t *testing.T) {
	got := buildWindowSize(24, 80, 0, 0)
	if len(got) != 12 {
		t.Fatalf("window-size msg must be 12 bytes, got %d", len(got))
	}
	if got[0] != 0xFF || got[1] != 0xFF || got[2] != 's' || got[3] != 's' {
		t.Fatalf("window-size magic mismatch: %x", got[:4])
	}
	if r := binary.BigEndian.Uint16(got[4:6]); r != 24 {
		t.Fatalf("rows = %d, want 24", r)
	}
	if c := binary.BigEndian.Uint16(got[6:8]); c != 80 {
		t.Fatalf("cols = %d, want 80", c)
	}
	if x := binary.BigEndian.Uint16(got[8:10]); x != 0 {
		t.Fatalf("xpix = %d, want 0", x)
	}
	if y := binary.BigEndian.Uint16(got[10:12]); y != 0 {
		t.Fatalf("ypix = %d, want 0", y)
	}
}

// fakeRloginServer is a test-only in-process rlogin server that records the
// handshake it saw, echoes client input, and notes any in-band window-size
// message the client sends.
type fakeRloginServer struct {
	ln net.Listener

	ackByte byte // byte to write as ACK (defaults to 0x00)

	mu             sync.Mutex
	handshake      []byte
	windowSizeMsgs [][]byte
	echoed         []byte
	acceptErr      error
	done           chan struct{}
}

func newFakeRloginServer(t *testing.T, ackByte byte) *fakeRloginServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &fakeRloginServer{
		ln:      ln,
		ackByte: ackByte,
		done:    make(chan struct{}),
	}
	go s.serveOne()
	return s
}

func (s *fakeRloginServer) addr() string { return s.ln.Addr().String() }

func (s *fakeRloginServer) close() { _ = s.ln.Close(); <-s.done }

func (s *fakeRloginServer) serveOne() {
	defer close(s.done)
	conn, err := s.ln.Accept()
	if err != nil {
		s.mu.Lock()
		s.acceptErr = err
		s.mu.Unlock()
		return
	}
	defer conn.Close()

	// Read handshake: 0x00 prefix + 3 NUL-terminated strings.
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	br := make([]byte, 0, 256)
	buf := make([]byte, 1)
	nulsSeen := 0
	sawPrefix := false
	for nulsSeen < 4 {
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		if n == 0 {
			continue
		}
		b := buf[0]
		br = append(br, b)
		if !sawPrefix {
			if b != 0x00 {
				return
			}
			sawPrefix = true
			nulsSeen = 1
			continue
		}
		if b == 0x00 {
			nulsSeen++
		}
	}
	s.mu.Lock()
	s.handshake = append([]byte(nil), br...)
	s.mu.Unlock()

	// ACK.
	if _, err := conn.Write([]byte{s.ackByte}); err != nil {
		return
	}
	if s.ackByte != 0x00 {
		// Bad ACK: caller should error out; keep the connection around
		// briefly so the client's deadline doesn't race us, then exit.
		time.Sleep(50 * time.Millisecond)
		return
	}

	_ = conn.SetReadDeadline(time.Time{})

	// Echo + capture window-size messages. Scan byte-by-byte for the
	// 0xFF 0xFF 's' 's' magic; when seen, consume the following 8 bytes
	// as payload instead of echoing them.
	readBuf := make([]byte, 1024)
	var pending []byte
	for {
		n, err := conn.Read(readBuf)
		if n > 0 {
			pending = append(pending, readBuf[:n]...)
			for {
				consumed, emitEcho, ws := extractWindowSize(pending)
				if consumed == 0 {
					break
				}
				if ws != nil {
					s.mu.Lock()
					s.windowSizeMsgs = append(s.windowSizeMsgs, ws)
					s.mu.Unlock()
				}
				if len(emitEcho) > 0 {
					s.mu.Lock()
					s.echoed = append(s.echoed, emitEcho...)
					s.mu.Unlock()
					if _, werr := conn.Write(emitEcho); werr != nil {
						return
					}
				}
				pending = pending[consumed:]
			}
		}
		if err != nil {
			// Flush any remaining non-magic bytes as echo.
			if len(pending) > 0 {
				// If pending still starts with an incomplete magic, just
				// treat it as normal bytes at shutdown.
				s.mu.Lock()
				s.echoed = append(s.echoed, pending...)
				s.mu.Unlock()
			}
			return
		}
	}
}

// extractWindowSize scans pending starting at the front. It returns
// (consumed, echoBytes, windowSize):
//   - if pending starts with a complete window-size frame, consumed is 12,
//     echoBytes is nil, windowSize is the 8-byte payload.
//   - if pending starts with bytes that are definitely not a prefix of the
//     magic, it returns consumed = length of that non-magic run, echoBytes
//     equal to those bytes, windowSize nil.
//   - otherwise (incomplete potential magic or incomplete ws payload),
//     returns 0, nil, nil — caller should wait for more data.
func extractWindowSize(pending []byte) (int, []byte, []byte) {
	if len(pending) == 0 {
		return 0, nil, nil
	}
	// Find first index of 0xFF 0xFF 's' 's'; bytes before it are echo.
	idx := -1
	for i := 0; i+3 < len(pending); i++ {
		if pending[i] == 0xFF && pending[i+1] == 0xFF && pending[i+2] == 's' && pending[i+3] == 's' {
			idx = i
			break
		}
	}
	if idx == -1 {
		// No complete magic anywhere. But trailing bytes might start a
		// partial magic (up to 3 bytes of 0xFF/s). Keep those pending.
		keep := 0
		if n := len(pending); n >= 1 && pending[n-1] == 0xFF {
			keep = 1
			if n >= 2 && pending[n-2] == 0xFF {
				keep = 2
				if n >= 3 && pending[n-3] == 0xFF && pending[n-2] == 0xFF && pending[n-1] == 's' {
					keep = 3
				}
			}
		}
		emit := len(pending) - keep
		if emit == 0 {
			return 0, nil, nil
		}
		return emit, append([]byte(nil), pending[:emit]...), nil
	}
	if idx > 0 {
		return idx, append([]byte(nil), pending[:idx]...), nil
	}
	// idx == 0: need 12 bytes total for a full frame.
	if len(pending) < 12 {
		return 0, nil, nil
	}
	ws := append([]byte(nil), pending[4:12]...)
	return 12, nil, ws
}

func TestOpenStartResizeClose_FullFlow(t *testing.T) {
	srv := newFakeRloginServer(t, 0x00)
	defer srv.close()

	host, portStr, err := net.SplitHostPort(srv.addr())
	if err != nil {
		t.Fatalf("split host/port: %v", err)
	}
	port := mustAtoi(t, portStr)

	mod := New()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sess, err := mod.Open(ctx, protocol.OpenRequest{
		Host: host,
		Port: port,
		Settings: map[string]any{
			"username":        "bob",
			"client_username": "alice",
			"terminal_type":   "xterm-256color",
			"terminal_speed":  38400,
		},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Drive Start in a goroutine with a pipe for stdin and a buffer for stdout.
	stdinR, stdinW := io.Pipe()
	var stdoutBuf safeBuffer
	startErrCh := make(chan error, 1)
	go func() {
		startErrCh <- sess.Start(ctx, stdinR, &stdoutBuf)
	}()

	// Send some input, expect echo back.
	payload := []byte("hello rlogin\n")
	if _, err := stdinW.Write(payload); err != nil {
		t.Fatalf("stdinW.Write: %v", err)
	}

	// Wait for echo to arrive in stdout.
	waitForBytes(t, &stdoutBuf, payload, 2*time.Second)

	// Send a resize (rows=40, cols=132).
	if err := sess.Resize(ctx, protocol.Size{Cols: 132, Rows: 40}); err != nil {
		t.Fatalf("Resize: %v", err)
	}

	// Wait up to 2s for the server to observe the window-size message.
	deadline := time.Now().Add(2 * time.Second)
	var wsMsgs [][]byte
	for time.Now().Before(deadline) {
		srv.mu.Lock()
		wsMsgs = append([][]byte(nil), srv.windowSizeMsgs...)
		srv.mu.Unlock()
		if len(wsMsgs) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(wsMsgs) == 0 {
		t.Fatalf("server did not observe any window-size message")
	}
	ws := wsMsgs[0]
	if r := binary.BigEndian.Uint16(ws[0:2]); r != 40 {
		t.Errorf("server saw rows=%d, want 40", r)
	}
	if c := binary.BigEndian.Uint16(ws[2:4]); c != 132 {
		t.Errorf("server saw cols=%d, want 132", c)
	}
	if x := binary.BigEndian.Uint16(ws[4:6]); x != 0 {
		t.Errorf("server saw xpix=%d, want 0", x)
	}
	if y := binary.BigEndian.Uint16(ws[6:8]); y != 0 {
		t.Errorf("server saw ypix=%d, want 0", y)
	}

	// Verify SendInput path too.
	extra := []byte("ping\n")
	if err := sess.SendInput(ctx, extra); err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	want := append([]byte(nil), payload...)
	want = append(want, extra...)
	waitForBytes(t, &stdoutBuf, want, 2*time.Second)

	// Close (twice — must be idempotent).
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := sess.Close(); err != nil && !isBenignCloseErr(err) {
		t.Fatalf("second Close returned error: %v", err)
	}
	_ = stdinW.Close()

	select {
	case err := <-startErrCh:
		if err != nil && !isBenignCloseErr(err) {
			t.Fatalf("Start returned: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return after Close")
	}

	// Verify handshake bytes captured by server.
	srv.mu.Lock()
	hs := append([]byte(nil), srv.handshake...)
	srv.mu.Unlock()
	if len(hs) == 0 || hs[0] != 0x00 {
		t.Fatalf("server did not see 0x00 prefix; got %x", hs)
	}
	if bytes.Count(hs, []byte{0x00}) != 4 {
		t.Fatalf("expected 4 NULs in handshake, got %d (%q)", bytes.Count(hs, []byte{0x00}), hs)
	}
	// Split on NUL and verify fields.
	parts := bytes.Split(hs, []byte{0x00})
	// parts = ["", "alice", "bob", "xterm-256color/38400", ""]
	if len(parts) < 5 {
		t.Fatalf("handshake parts = %q", parts)
	}
	if string(parts[1]) != "alice" {
		t.Errorf("client_user = %q, want alice", parts[1])
	}
	if string(parts[2]) != "bob" {
		t.Errorf("server_user = %q, want bob", parts[2])
	}
	if string(parts[3]) != "xterm-256color/38400" {
		t.Errorf("term/speed = %q, want xterm-256color/38400", parts[3])
	}
}

func TestOpen_BadACK(t *testing.T) {
	srv := newFakeRloginServer(t, 0xFF)
	defer srv.close()

	host, portStr, err := net.SplitHostPort(srv.addr())
	if err != nil {
		t.Fatalf("split host/port: %v", err)
	}
	port := mustAtoi(t, portStr)

	mod := New()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sess, err := mod.Open(ctx, protocol.OpenRequest{
		Host: host,
		Port: port,
		Settings: map[string]any{
			"username":        "bob",
			"client_username": "alice",
			"terminal_type":   "xterm",
			"terminal_speed":  9600,
		},
	})
	if err == nil {
		_ = sess.Close()
		t.Fatal("expected Open to fail on non-zero ACK, got nil error")
	}
	if !strings.Contains(err.Error(), "0xff") && !strings.Contains(err.Error(), "0xFF") {
		t.Errorf("error should mention the offending byte, got: %v", err)
	}
}

// safeBuffer is a goroutine-safe bytes.Buffer.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.buf.Bytes()...)
}

func waitForBytes(t *testing.T, b *safeBuffer, want []byte, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		got := b.Bytes()
		if bytes.Contains(got, want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q in stdout buffer; got %q", want, b.Bytes())
}

func mustAtoi(t *testing.T, s string) int {
	t.Helper()
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			t.Fatalf("bad port %q", s)
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// Guard against accidentally-removed io import.
var _ = io.EOF
var _ = errors.New
