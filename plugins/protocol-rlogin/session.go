package rlogin

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"sync"

	"github.com/darkace1998/GoRemote/sdk/protocol"
)

// Session is a live rlogin session. All methods are safe for concurrent use.
type Session struct {
	conn net.Conn

	// writeMu serializes writes to the connection so a Resize issued
	// concurrently with the stdin->remote copy loop cannot interleave bytes
	// mid-window-size-message.
	writeMu sync.Mutex

	closeOnce sync.Once
	closeErr  error
}

// Compile-time assertion: *Session implements protocol.Session.
var _ protocol.Session = (*Session)(nil)

func newSession(conn net.Conn) *Session {
	return &Session{conn: conn}
}

// RenderMode always returns RenderTerminal for rlogin sessions.
func (s *Session) RenderMode() protocol.RenderMode { return protocol.RenderTerminal }

// Start runs the rlogin I/O loop until the remote closes, ctx is cancelled,
// or Close is called. It copies remote->stdout and stdin->remote.
func (s *Session) Start(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	if stdout == nil {
		stdout = io.Discard
	}

	errCh := make(chan error, 2)
	// stdin -> remote
	go func() {
		if stdin == nil {
			errCh <- nil
			return
		}
		buf := make([]byte, 4096)
		for {
			n, err := stdin.Read(buf)
			if n > 0 {
				if werr := s.writeLocked(buf[:n]); werr != nil {
					errCh <- werr
					return
				}
			}
			if err != nil {
				if errors.Is(err, io.EOF) {
					errCh <- nil
					return
				}
				errCh <- err
				return
			}
		}
	}()
	// remote -> stdout
	go func() {
		_, err := io.Copy(stdout, s.conn)
		errCh <- err
	}()

	ctxDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = s.Close()
		case <-ctxDone:
		}
	}()

	first := <-errCh
	_ = s.Close()
	second := <-errCh
	close(ctxDone)

	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	for _, e := range [2]error{first, second} {
		if e != nil && !isBenignCloseErr(e) {
			return e
		}
	}
	return nil
}

// writeLocked writes p to the connection while holding writeMu.
func (s *Session) writeLocked(p []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err := s.conn.Write(p)
	return err
}

// buildWindowSize builds the in-band window-size notification message:
//
//	0xFF 0xFF 's' 's' <rows> <cols> <xpix> <ypix>
//
// where each of rows/cols/xpix/ypix is a big-endian uint16.
func buildWindowSize(rows, cols, xpix, ypix uint16) []byte {
	b := make([]byte, 4+8)
	b[0] = 0xFF
	b[1] = 0xFF
	b[2] = 's'
	b[3] = 's'
	binary.BigEndian.PutUint16(b[4:6], rows)
	binary.BigEndian.PutUint16(b[6:8], cols)
	binary.BigEndian.PutUint16(b[8:10], xpix)
	binary.BigEndian.PutUint16(b[10:12], ypix)
	return b
}

// Resize returns ErrUnsupported. RFC 1282 requires the window-size message
// to be delivered via TCP urgent (out-of-band) data, which portable user-space
// code cannot reliably send. The in-band 0xFF 0xFF 's' 's' framing is not
// honoured by compliant rlogind implementations. Use SSH for resize support.
//
// buildWindowSize is retained for potential future use when OOB delivery is
// implemented.
func (s *Session) Resize(ctx context.Context, size protocol.Size) error {
	return protocol.ErrUnsupported
}

// SendInput writes data to the remote.
func (s *Session) SendInput(ctx context.Context, data []byte) error {
	return s.writeLocked(data)
}

// Close tears down the underlying connection. Safe to call repeatedly.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		s.closeErr = s.conn.Close()
	})
	return s.closeErr
}

// isBenignCloseErr returns true for errors that indicate normal shutdown.
func isBenignCloseErr(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, io.EOF) {
		return true
	}
	if errors.Is(err, net.ErrClosed) {
		return true
	}
	return strings.Contains(err.Error(), "use of closed network connection")
}
