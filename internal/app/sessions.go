package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	protohost "github.com/goremote/goremote/host/protocol"
	"github.com/goremote/goremote/internal/domain"
	"github.com/goremote/goremote/sdk/credential"
	"github.com/goremote/goremote/sdk/protocol"
)

// SessionHandle uniquely identifies an open session. It is opaque to callers.
type SessionHandle = domain.ID

// CredentialResolveTimeout is the per-call timeout applied when Resolve'ing
// credentials during OpenSession.
const CredentialResolveTimeout = 5 * time.Second

// outputBufferBytes is the default buffer size for each output fan-out
// subscriber channel; items are dropped for slow subscribers.
const outputBufferBytes = 64

// sessionEntry is the internal record tracked per open session.
type sessionEntry struct {
	handle       SessionHandle
	connectionID domain.ID
	protocolID   string
	host         string
	openedAt     time.Time

	sess   protocol.Session
	cancel context.CancelFunc

	inW  *io.PipeWriter // host writes into this; session reads via stdin
	outR *io.PipeReader // fan-out reads from this; session writes via stdout
	outW *io.PipeWriter // handed to session as stdout

	subsMu sync.Mutex
	subs   []*subscriber

	closed atomic.Bool

	// material is the resolved credential; zeroized on close.
	material *credential.Material

	done chan struct{}
	err  error
}

type subscriber struct {
	ch        chan []byte
	ctx       context.Context
	done      chan struct{}
	closeOnce sync.Once

	// mu serialises sends against close so the channel is never written
	// after it has been closed (which would panic). fanOut takes RLock
	// during a send; close() takes the write lock.
	mu     sync.RWMutex
	closed bool
}

// trySend delivers chunk to s.ch on a best-effort basis without blocking.
// It is safe to call concurrently with close().
func (s *subscriber) trySend(chunk []byte) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return
	}
	select {
	case <-s.ctx.Done():
	case s.ch <- chunk:
	default:
		// Slow subscriber: drop the chunk.
	}
}

// sessionManager owns the live-session map.
type sessionManager struct {
	app *App

	mu       sync.RWMutex
	sessions map[SessionHandle]*sessionEntry
}

func newSessionManager(a *App) *sessionManager {
	return &sessionManager{
		app:      a,
		sessions: make(map[SessionHandle]*sessionEntry),
	}
}

// OpenSession resolves inheritance + credentials and opens a protocol session.
func (a *App) OpenSession(ctx context.Context, connID domain.ID) (SessionHandle, error) {
	return a.openSessionInternal(ctx, connID, nil)
}

// OpenSessionWithSecret behaves like OpenSession but uses the supplied
// credential material verbatim instead of resolving the connection's
// configured credential reference. This enables one-shot prompts (e.g. an
// interactive password dialog) without persisting the secret to a vault.
//
// The supplied material is taken over by the session and zeroized on close.
func (a *App) OpenSessionWithSecret(ctx context.Context, connID domain.ID, secret *credential.Material) (SessionHandle, error) {
	return a.openSessionInternal(ctx, connID, secret)
}

func (a *App) openSessionInternal(ctx context.Context, connID domain.ID, inline *credential.Material) (SessionHandle, error) {
	if err := ctx.Err(); err != nil {
		return domain.NilID, err
	}

	// 1. Resolve the connection with inheritance applied.
	a.treeMu.RLock()
	c, err := a.tree.Connection(connID)
	if err != nil {
		a.treeMu.RUnlock()
		return domain.NilID, err
	}
	ancestors, err := a.tree.Ancestors(connID)
	if err != nil {
		a.treeMu.RUnlock()
		return domain.NilID, err
	}
	resolved := c.Inheritance.Resolve(c, ancestors)
	a.treeMu.RUnlock()

	if resolved.ProtocolID == "" {
		return domain.NilID, errors.New("app: connection has no protocol")
	}

	// 2. Resolve credential (if any). Inline material takes precedence.
	var material *credential.Material
	var mat protocol.CredentialMaterial
	switch {
	case inline != nil:
		material = inline
		mat = adaptMaterial(inline)
	case !isRefZero(resolved.CredentialRef):
		rctx, cancel := context.WithTimeout(ctx, CredentialResolveTimeout)
		m, rerr := a.credH.Resolve(rctx, resolved.CredentialRef, CredentialResolveTimeout)
		cancel()
		if rerr != nil {
			a.publish(Event{Kind: EventError, Where: "credential.resolve", Err: rerr})
			return domain.NilID, fmt.Errorf("app: resolve credential: %w", rerr)
		}
		material = m
		mat = adaptMaterial(m)
		a.publish(Event{
			Kind:       EventCredentialUnlocked,
			ProviderID: resolved.CredentialRef.ProviderID,
		})
	}

	// 3. Ask the protocol host to open the session.
	authMethod := resolved.AuthMethod
	if authMethod == "" {
		authMethod = defaultAuthMethod(resolved.ProtocolID, mat)
	}
	req := protocol.OpenRequest{
		Host:       resolved.Host,
		Port:       resolved.Port,
		Username:   resolved.Username,
		AuthMethod: authMethod,
		Secret:     mat,
		Settings:   cloneSettings(resolved.Settings),
	}
	protocolID := resolved.ProtocolID
	sess, err := a.protoH.Open(ctx, protocolID, req)
	if err != nil && errors.Is(err, protohost.ErrProtocolNotFound) {
		if canonical, ok := canonicalBuiltInProtocolID(protocolID); ok {
			sess, err = a.protoH.Open(ctx, canonical, req)
			if err == nil {
				protocolID = canonical
			}
		}
	}
	if err != nil {
		if material != nil {
			material.Zeroize()
		}
		a.publish(Event{Kind: EventError, Where: "protocol.open", Err: err})
		return domain.NilID, err
	}

	// 4. Wire pipes + start the session's IO loop.
	handle := domain.NewID()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	sessCtx, sessCancel := context.WithCancel(context.Background())

	entry := &sessionEntry{
		handle:       handle,
		connectionID: connID,
		protocolID:   protocolID,
		host:         resolved.Host,
		openedAt:     a.now(),
		sess:         sess,
		cancel:       sessCancel,
		inW:          inW,
		outR:         outR,
		outW:         outW,
		material:     material,
		done:         make(chan struct{}),
	}

	a.sess.mu.Lock()
	a.sess.sessions[handle] = entry
	a.sess.mu.Unlock()

	// Fan-out: read from outR and dispatch bytes to all current subscribers.
	go a.sess.fanOut(entry)

	// Run the session's I/O loop.
	go func() {
		defer close(entry.done)
		defer func() {
			_ = outW.Close() // unblocks fanOut
			_ = inR.Close()
		}()
		err := sess.Start(sessCtx, inR, outW)
		entry.err = err
		if err != nil && !errors.Is(err, context.Canceled) {
			a.logger.Warn("session ended with error",
				slog.String("session", handle.String()),
				slog.String("protocol", protocolID),
				slog.String("err", err.Error()))
		}
		// Ensure Close is called so plugin resources are freed even if
		// Start returned on its own.
		_ = sess.Close()
		a.sess.removeAndPublish(entry)
	}()

	a.publish(Event{
		Kind:      EventSessionOpened,
		SessionID: handle,
		NodeID:    connID,
	})
	return handle, nil
}

func canonicalBuiltInProtocolID(id string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "ssh", "telnet", "rlogin", "rawsocket", "rdp", "vnc", "tn5250", "powershell", "http", "mosh":
		return "io.goremote.protocol." + strings.ToLower(strings.TrimSpace(id)), true
	default:
		return "", false
	}
}

// SendInput sends data to the session. Per spec, this calls
// protocol.Session.SendInput directly rather than writing to the stdin pipe;
// this matches the shape used by the ssh/telnet/rlogin/rawsocket modules.
func (a *App) SendInput(ctx context.Context, h SessionHandle, data []byte) error {
	e, err := a.sess.get(h)
	if err != nil {
		return err
	}
	return e.sess.SendInput(ctx, data)
}

// Resize forwards a resize request.
func (a *App) Resize(ctx context.Context, h SessionHandle, cols, rows uint16) error {
	e, err := a.sess.get(h)
	if err != nil {
		return err
	}
	return e.sess.Resize(ctx, protocol.Size{Cols: int(cols), Rows: int(rows)})
}

// CloseSession cancels and removes the session. Close is idempotent.
func (a *App) CloseSession(ctx context.Context, h SessionHandle) error {
	e, err := a.sess.get(h)
	if err != nil {
		return err
	}
	if !e.closed.CompareAndSwap(false, true) {
		return nil
	}
	e.cancel()
	_ = e.sess.Close()
	_ = e.inW.Close()
	// Wait for the session goroutine to exit before returning so callers can
	// observe subscribers being closed.
	select {
	case <-e.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

// SubscribeOutput returns a channel that receives every byte slice emitted by
// the session. Each subscriber gets an independent buffered channel; slow
// subscribers have messages dropped rather than blocking the session.
//
// The subscription is removed and the channel closed when ctx is cancelled
// or the session ends.
func (a *App) SubscribeOutput(ctx context.Context, h SessionHandle, buffer int) (<-chan []byte, error) {
	e, err := a.sess.get(h)
	if err != nil {
		return nil, err
	}
	if buffer <= 0 {
		buffer = 16
	}
	sub := &subscriber{
		ch:   make(chan []byte, buffer),
		ctx:  ctx,
		done: make(chan struct{}),
	}
	e.subsMu.Lock()
	e.subs = append(e.subs, sub)
	e.subsMu.Unlock()

	go func() {
		select {
		case <-ctx.Done():
		case <-sub.done:
		case <-e.done:
		}
		e.removeSub(sub)
	}()
	return sub.ch, nil
}

// ListSessions returns a snapshot of every active session.
func (a *App) ListSessions() []SessionInfo {
	a.sess.mu.RLock()
	defer a.sess.mu.RUnlock()
	out := make([]SessionInfo, 0, len(a.sess.sessions))
	for _, e := range a.sess.sessions {
		out = append(out, SessionInfo{
			ID:           e.handle.String(),
			ConnectionID: e.connectionID.String(),
			Protocol:     e.protocolID,
			Host:         e.host,
			OpenedAt:     e.openedAt.Format(time.RFC3339Nano),
		})
	}
	return out
}

// --- sessionManager internals --------------------------------------------

func (m *sessionManager) get(h SessionHandle) (*sessionEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.sessions[h]
	if !ok {
		return nil, fmt.Errorf("app: session %s not found", h)
	}
	return e, nil
}

func (m *sessionManager) closeAll(ctx context.Context) {
	m.mu.Lock()
	entries := make([]*sessionEntry, 0, len(m.sessions))
	for _, e := range m.sessions {
		entries = append(entries, e)
	}
	m.mu.Unlock()
	for _, e := range entries {
		_ = m.app.CloseSession(ctx, e.handle)
	}
}

// fanOut reads bytes from the session's stdout pipe and delivers a copy to
// every active subscriber on a best-effort, non-blocking basis.
func (m *sessionManager) fanOut(e *sessionEntry) {
	buf := make([]byte, 4096)
	for {
		n, err := e.outR.Read(buf)
		if n > 0 {
			// Copy to isolate bytes from buf re-use.
			chunk := append([]byte(nil), buf[:n]...)
			e.subsMu.Lock()
			subs := make([]*subscriber, len(e.subs))
			copy(subs, e.subs)
			e.subsMu.Unlock()
			for _, s := range subs {
				s.trySend(chunk)
			}
		}
		if err != nil {
			break
		}
	}
	// Close every subscriber channel on end-of-stream.
	e.subsMu.Lock()
	subs := e.subs
	e.subs = nil
	e.subsMu.Unlock()
	for _, s := range subs {
		s.close()
	}
}

// removeAndPublish removes the session from the map, zeroizes credential
// material, and publishes SessionClosed.
func (m *sessionManager) removeAndPublish(e *sessionEntry) {
	m.mu.Lock()
	delete(m.sessions, e.handle)
	m.mu.Unlock()
	if e.material != nil {
		e.material.Zeroize()
		e.material = nil
	}
	m.app.publish(Event{
		Kind:      EventSessionClosed,
		SessionID: e.handle,
		NodeID:    e.connectionID,
		Err:       e.err,
	})
}

func (e *sessionEntry) removeSub(sub *subscriber) {
	e.subsMu.Lock()
	for i, s := range e.subs {
		if s == sub {
			e.subs = append(e.subs[:i], e.subs[i+1:]...)
			break
		}
	}
	e.subsMu.Unlock()
	sub.close()
}

func (s *subscriber) close() {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		close(s.done)
		close(s.ch)
		s.mu.Unlock()
	})
}

func isRefZero(r credential.Reference) bool {
	return r.ProviderID == "" && r.EntryID == "" && len(r.Hints) == 0
}

// defaultAuthMethod returns a reasonable default auth method for a protocol
// when the user/connection has not picked one. This avoids the
// "auth method not specified" error for the common SSH "just connect" case.
//
// The defaults are intentionally conservative:
//   - SSH: prefer "password" when a password is available, else "publickey"
//     when a private key is present, else "agent" (the SSH agent will be
//     consulted when SSH_AUTH_SOCK is set; the protocol layer surfaces an
//     actionable error otherwise).
//   - Other protocols: "password" when a password exists, else "none".
func defaultAuthMethod(protocolID string, mat protocol.CredentialMaterial) protocol.AuthMethod {
	short := protocolID
	if i := strings.LastIndex(short, "."); i >= 0 {
		short = short[i+1:]
	}
	if short == "ssh" {
		switch {
		case mat.Password != "":
			return protocol.AuthPassword
		case len(mat.PrivateKey) > 0:
			return protocol.AuthPublicKey
		default:
			return protocol.AuthAgent
		}
	}
	if mat.Password != "" {
		return protocol.AuthPassword
	}
	return protocol.AuthNone
}

func adaptMaterial(m *credential.Material) protocol.CredentialMaterial {
	if m == nil {
		return protocol.CredentialMaterial{}
	}
	var extra map[string]string
	if len(m.Extra) > 0 {
		extra = make(map[string]string, len(m.Extra))
		for k, v := range m.Extra {
			extra[k] = v
		}
	}
	pk := append([]byte(nil), m.PrivateKey...)
	return protocol.CredentialMaterial{
		Username:   m.Username,
		Password:   m.Password,
		Domain:     m.Domain,
		PrivateKey: pk,
		Passphrase: m.Passphrase,
		Extra:      extra,
	}
}
