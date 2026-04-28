package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	terminal "github.com/fyne-io/terminal"

	iapp "github.com/goremote/goremote/internal/app"
	"github.com/goremote/goremote/internal/domain"
)

// sessionTab represents a single open session displayed in the AppTabs
// (when tabItem is non-nil) or in a standalone window (when window is
// non-nil). Exactly one of tabItem / window is set per session at a time;
// detach/reattach atomically swap the parent container without tearing
// down the session.
type sessionTab struct {
	b          *Bindings
	cv         iapp.ConnectionView
	handle     string
	hid        domain.ID
	connID     string // connection ID that spawned this session
	tabItem    *container.TabItem
	window     fyne.Window
	term       *terminal.Terminal
	contentObj fyne.CanvasObject // cached so detach/reattach can move it
	// transferring is set while a detach or reattach operation is in
	// progress so the tab's OnClosed handler and the window's
	// CloseIntercept know to skip the session-termination path.
	transferring bool
	// pendingSplit is consumed by sessionRegistry.add when this
	// session is being attached as an additional pane in an existing
	// tab. Empty means the session opens as a fresh tab. Values:
	// "h" (split right) or "v" (split below).
	pendingSplit string
	ctx          context.Context
	cancel       context.CancelFunc
}

// terminalProtocols lists the protocol IDs rendered with an in-process
// terminal widget. All others launch an external process.
var terminalProtocols = map[string]bool{
	"ssh":        true,
	"telnet":     true,
	"rlogin":     true,
	"rawsocket":  true,
	"powershell": true,
	"sftp":       true,
	"serial":     true,
}

// content builds the Fyne canvas object for this session tab on first call
// and caches it; subsequent calls return the same object so detach/reattach
// can move the running terminal between containers without recreating it.
// For terminal-capable protocols it creates a terminal.Terminal widget;
// for external protocols it returns a descriptive label.
func (st *sessionTab) content() fyne.CanvasObject {
	if st.contentObj != nil {
		return st.contentObj
	}
	proto := st.cv.EffectiveProtocol
	if proto == "" {
		proto = st.cv.Protocol
	}
	if terminalProtocols[proto] {
		st.term = terminal.New()
		st.contentObj = st.term
		return st.contentObj
	}
	msg := fmt.Sprintf("External session launched\nProtocol: %s\nHost: %s",
		proto, st.cv.EffectiveHost)
	st.contentObj = container.NewCenter(widget.NewLabel(msg))
	return st.contentObj
}

// run drives the session lifecycle in a goroutine. onClose is called deferred
// so it always fires when the session exits, even on error.
func (st *sessionTab) run(onClose func()) {
	defer st.cancel()
	defer onClose()

	if st.term == nil {
		// External session: wait until the context is cancelled (e.g. by the
		// user clicking Disconnect, or by window close).
		<-st.ctx.Done()
		return
	}

	br, err := newSessionBridge(st.ctx, st.b, st.handle)
	if err != nil {
		_, _ = st.term.Write([]byte("\r\n[Error: " + err.Error() + "]\r\n"))
		return
	}
	defer br.Close()

	// Forward terminal resize events to the remote session.
	cfgCh := make(chan terminal.Config, 4)
	st.term.AddListener(cfgCh)
	defer st.term.RemoveListener(cfgCh)
	go st.forwardResize(cfgCh)

	if err := st.term.RunWithConnection(br, br); err != nil {
		slog.Warn("terminal session ended with error", "handle", st.handle, "err", err)
	}
	_, _ = st.term.Write([]byte("\r\n[Session closed]\r\n"))
}

// forwardResize plumbs config changes from the terminal widget through to the
// remote session as PTY resize events.
func (st *sessionTab) forwardResize(ch <-chan terminal.Config) {
	var lastCols, lastRows uint16
	for {
		select {
		case <-st.ctx.Done():
			return
		case cfg, ok := <-ch:
			if !ok {
				return
			}
			cols := uint16(cfg.Columns)
			rows := uint16(cfg.Rows)
			if cols == 0 || rows == 0 {
				continue
			}
			if cols == lastCols && rows == lastRows {
				continue
			}
			lastCols, lastRows = cols, rows
			rctx, cancel := context.WithTimeout(st.ctx, 2*time.Second)
			if err := st.b.Resize(rctx, st.handle, cols, rows); err != nil {
				slog.Debug("resize forward", "handle", st.handle, "err", err)
			}
			cancel()
		}
	}
}

// --- sessionBridge ---------------------------------------------------------

// sessionBridge plumbs a fyne-io/terminal widget to a goremote session.
//
//   - Read: delivers server output to the terminal (reads from the pipe fed by
//     SubscribeOutput).
//   - Write: forwards terminal keystrokes to the session via SendInput.
//   - Close: tears down the subscription context and closes the read pipe.
type sessionBridge struct {
	pr     *io.PipeReader
	pw     *io.PipeWriter
	b      *Bindings
	handle string
	ctx    context.Context
	cancel context.CancelFunc
}

// newSessionBridge subscribes to a session's output stream and returns a
// bridge that the terminal widget can use as its I/O connection.
func newSessionBridge(ctx context.Context, b *Bindings, handle string) (*sessionBridge, error) {
	hid, err := domain.ParseID(handle)
	if err != nil {
		return nil, fmt.Errorf("session bridge: parse handle: %w", err)
	}

	pr, pw := io.Pipe()
	bctx, cancel := context.WithCancel(ctx)

	ch, err := b.app.SubscribeOutput(bctx, hid, 256)
	if err != nil {
		cancel()
		_ = pr.Close()
		_ = pw.Close()
		return nil, fmt.Errorf("session bridge: subscribe output: %w", err)
	}

	go func() {
		for chunk := range ch {
			if _, werr := pw.Write(chunk); werr != nil {
				break
			}
		}
		_ = pw.Close()
	}()

	return &sessionBridge{
		pr:     pr,
		pw:     pw,
		b:      b,
		handle: handle,
		ctx:    bctx,
		cancel: cancel,
	}, nil
}

// Read implements io.Reader — the terminal reads server output from here.
func (br *sessionBridge) Read(p []byte) (int, error) {
	return br.pr.Read(p)
}

// Write implements io.WriteCloser — the terminal writes keystrokes here to
// forward them to the remote session.
func (br *sessionBridge) Write(p []byte) (int, error) {
	if err := br.b.SendInput(br.ctx, br.handle, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Close cancels the subscription context and closes the read pipe.
func (br *sessionBridge) Close() error {
	br.cancel()
	_ = br.pr.Close()
	return nil
}
