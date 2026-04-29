package telnet

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darkace1998/GoRemote/sdk/protocol"
)

// fakeServer is a minimal in-process Telnet server used for integration
// testing. It:
//   - sends IAC DO TTYPE and IAC DO NAWS on accept
//   - sends a "Welcome\r\n" banner
//   - echoes every data byte it receives back to the client
//   - records the last SB NAWS and SB TTYPE IS payloads it observed
type fakeServer struct {
	ln net.Listener

	mu         sync.Mutex
	lastCols   int
	lastRows   int
	lastTType  string
	nawsEvents int
	nawsCh     chan struct{}
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	return &fakeServer{ln: ln, nawsCh: make(chan struct{}, 8)}
}

func (f *fakeServer) addr() string { return f.ln.Addr().String() }

func (f *fakeServer) stop() { _ = f.ln.Close() }

func (f *fakeServer) serveOnce(t *testing.T) {
	t.Helper()
	go func() {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Initial negotiation: DO TTYPE, DO NAWS, then request TTYPE value.
		if _, err := conn.Write([]byte{
			IAC, DO, OptTTYPE,
			IAC, DO, OptNAWS,
			IAC, SB, OptTTYPE, ttypeSEND, IAC, SE,
		}); err != nil {
			return
		}
		// Banner.
		if _, err := conn.Write([]byte("Welcome\r\n")); err != nil {
			return
		}

		// Parse inbound with the same state machine logic, echoing data.
		buf := make([]byte, 512)
		state := 0
		var sbOpt byte
		var sbBuf []byte
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				for _, b := range buf[:n] {
					switch state {
					case 0:
						if b == IAC {
							state = 1
						} else {
							_, _ = conn.Write([]byte{b})
						}
					case 1:
						switch b {
						case IAC:
							_, _ = conn.Write([]byte{IAC, IAC})
							state = 0
						case WILL, WONT, DO, DONT:
							state = 10 + int(b-WILL) // consume option
						case SB:
							state = 20
						default:
							state = 0
						}
					case 10, 11, 12, 13:
						// WILL/WONT/DO/DONT <opt> -- just consume
						state = 0
					case 20:
						sbOpt = b
						sbBuf = sbBuf[:0]
						state = 21
					case 21:
						if b == IAC {
							state = 22
						} else {
							sbBuf = append(sbBuf, b)
						}
					case 22:
						switch b {
						case IAC:
							sbBuf = append(sbBuf, 0xFF)
							state = 21
						case SE:
							f.recordSB(sbOpt, sbBuf)
							state = 0
						default:
							state = 0
						}
					}
				}
			}
			if err != nil {
				return
			}
		}
	}()
}

func (f *fakeServer) recordSB(opt byte, data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch opt {
	case OptNAWS:
		if c, r, ok := parseNAWS(data); ok {
			f.lastCols = c
			f.lastRows = r
			f.nawsEvents++
			select {
			case f.nawsCh <- struct{}{}:
			default:
			}
		}
	case OptTTYPE:
		if len(data) >= 1 && data[0] == ttypeIS {
			f.lastTType = string(data[1:])
		}
	}
}

func (f *fakeServer) snapshot() (cols, rows, events int, term string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastCols, f.lastRows, f.nawsEvents, f.lastTType
}

// syncBuffer is a thread-safe io.Writer used as the test stdout sink.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", timeout)
}

func TestSession_EchoRoundTrip(t *testing.T) {
	srv := newFakeServer(t)
	defer srv.stop()
	srv.serveOnce(t)

	host, portStr, err := net.SplitHostPort(srv.addr())
	if err != nil {
		t.Fatal(err)
	}
	var port int
	if _, err := fmtScanPort(portStr, &port); err != nil {
		t.Fatalf("parse port: %v", err)
	}

	mod := New()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sess, err := mod.Open(ctx, protocol.OpenRequest{
		Host: host,
		Port: port,
		Settings: map[string]any{
			"host":                    host,
			"port":                    port,
			"terminal_type":           "xterm-256color",
			"connect_timeout_seconds": 5,
		},
		AuthMethod:  protocol.AuthNone,
		InitialSize: protocol.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer sess.Close()

	stdin, stdinW := io.Pipe()
	out := &syncBuffer{}
	done := make(chan error, 1)
	go func() { done <- sess.Start(ctx, stdin, out) }()

	// Wait for the initial "Welcome" banner to arrive.
	waitFor(t, 2*time.Second, func() bool {
		return strings.Contains(out.String(), "Welcome")
	})

	// Send input; server echoes.
	if err := sess.SendInput(ctx, []byte("ping\n")); err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	// The server's echo comes back via stdout.
	waitFor(t, 2*time.Second, func() bool {
		return strings.Contains(out.String(), "ping")
	})

	// Trigger a resize.
	if err := sess.Resize(ctx, protocol.Size{Cols: 132, Rows: 43}); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	// Wait for the server to observe the new NAWS.
	waitFor(t, 2*time.Second, func() bool {
		cols, rows, _, _ := srv.snapshot()
		return cols == 132 && rows == 43
	})

	// Also exercise a second SendInput path to ensure IAC escaping works.
	if err := sess.SendInput(ctx, []byte{'a', 0xFF, 'b', '\n'}); err != nil {
		t.Fatalf("SendInput iac: %v", err)
	}
	waitFor(t, 2*time.Second, func() bool {
		// Server un-escapes IAC IAC -> 0xFF and echoes 0xFF unchanged;
		// our negotiator strips the echoed IAC IAC back to 0xFF in stdout.
		return bytes.Contains([]byte(out.String()), []byte{'a', 0xFF, 'b'})
	})

	// Close shuts everything down.
	_ = stdinW.Close()
	if err := sess.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		t.Fatalf("Close: %v", err)
	}

	select {
	case err := <-done:
		if err != nil && !isBenignCloseErr(err) {
			t.Fatalf("Start returned: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return after Close")
	}

	// Verify terminal type negotiation happened.
	_, _, _, term := srv.snapshot()
	if term != "xterm-256color" {
		t.Fatalf("lastTType=%q want xterm-256color", term)
	}
}

func TestSession_CloseIsIdempotent(t *testing.T) {
	srv := newFakeServer(t)
	defer srv.stop()
	srv.serveOnce(t)

	host, portStr, _ := net.SplitHostPort(srv.addr())
	var port int
	if _, err := fmtScanPort(portStr, &port); err != nil {
		t.Fatalf("parse port: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	sess, err := New().Open(ctx, protocol.OpenRequest{
		Host: host, Port: port,
		Settings:    map[string]any{"host": host, "port": port},
		AuthMethod:  protocol.AuthNone,
		InitialSize: protocol.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := sess.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		t.Fatalf("first Close: %v", err)
	}
	if err := sess.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		t.Fatalf("second Close: %v", err)
	}
}

func TestSession_RenderMode(t *testing.T) {
	s := &Session{neg: &Negotiator{}}
	if got := s.RenderMode(); got != protocol.RenderTerminal {
		t.Fatalf("RenderMode=%q want terminal", got)
	}
}

func TestModule_Metadata(t *testing.T) {
	m := New()
	man := m.Manifest()
	if err := man.Validate(); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	caps := m.Capabilities()
	if !caps.SupportsResize {
		t.Fatal("SupportsResize should be true")
	}
	if !caps.SupportsLogging {
		t.Fatal("SupportsLogging should be true")
	}
	foundPwd := false
	for _, a := range caps.AuthMethods {
		if a == protocol.AuthPassword {
			foundPwd = true
		}
	}
	if !foundPwd {
		t.Fatal("AuthPassword should be advertised")
	}
	foundHost := false
	for _, sd := range m.Settings() {
		if sd.Key == "host" && sd.Required {
			foundHost = true
		}
	}
	if !foundHost {
		t.Fatal("host setting should exist and be required")
	}
}

// fmtScanPort avoids a direct fmt.Sscanf dependency elsewhere; it parses a
// TCP port string.
func fmtScanPort(s string, out *int) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errors.New("invalid port")
		}
		n = n*10 + int(c-'0')
	}
	*out = n
	return 1, nil
}
