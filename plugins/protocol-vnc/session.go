package vnc

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/darkace1998/GoRemote/sdk/protocol"
)

// Session is a live VNC session placeholder.
//
// Open validates config and provides a reachability check via TCP dial in the
// module. Start immediately returns ErrUnsupported: full RFB protocol framing,
// auth, and TLS are not yet implemented. Connecting without these would deliver
// garbage data to the caller.
type Session struct {
	addr string

	closeOnce sync.Once
	closeErr  error
}

// Compile-time assertion: *Session implements protocol.Session.
var _ protocol.Session = (*Session)(nil)

func newSession(addr string) *Session {
	return &Session{addr: addr}
}

// RenderMode reports the graphical rendering mode used by VNC sessions.
func (s *Session) RenderMode() protocol.RenderMode { return protocol.RenderGraphical }

// Start returns ErrUnsupported. Full RFB protocol framing is not yet
// implemented; returning ErrUnsupported avoids presenting a "connected"
// session that streams raw unframed TCP bytes.
func (s *Session) Start(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	return protocol.ErrUnsupported
}

// Resize is not yet wired to an RFB resize message.
func (s *Session) Resize(ctx context.Context, size protocol.Size) error {
	return protocol.ErrUnsupported
}

// SendInput returns an error because no connection is established while
// Start returns ErrUnsupported.
func (s *Session) SendInput(ctx context.Context, data []byte) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return fmt.Errorf("vnc: session not started")
}

// Close is idempotent. No connection is held while Start returns ErrUnsupported.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {})
	return s.closeErr
}
