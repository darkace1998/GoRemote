package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/goremote/goremote/internal/domain"
	"github.com/goremote/goremote/sdk/credential"
	sdkplugin "github.com/goremote/goremote/sdk/plugin"
	"github.com/goremote/goremote/sdk/protocol"
)

// ---- fake protocol module ------------------------------------------------

type fakeSession struct {
	mu     sync.Mutex
	stdout io.Writer
	stdin  io.Reader

	done   chan struct{}
	closed atomic.Bool

	seenReq protocol.OpenRequest
}

func (s *fakeSession) RenderMode() protocol.RenderMode { return protocol.RenderTerminal }

func (s *fakeSession) Start(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	s.mu.Lock()
	s.stdout = stdout
	s.stdin = stdin
	s.mu.Unlock()
	// Write greeting so subscribers always see at least one chunk.
	_, _ = stdout.Write([]byte("hello"))
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return nil
	}
}

func (s *fakeSession) Resize(ctx context.Context, sz protocol.Size) error { return nil }

func (s *fakeSession) SendInput(ctx context.Context, data []byte) error {
	s.mu.Lock()
	w := s.stdout
	s.mu.Unlock()
	if w == nil {
		return nil
	}
	_, err := w.Write(data)
	return err
}

func (s *fakeSession) Close() error {
	if s.closed.CompareAndSwap(false, true) {
		close(s.done)
	}
	return nil
}

type fakeModule struct {
	id       string
	lastReq  protocol.OpenRequest
	lastMu   sync.Mutex
	sessions []*fakeSession
}

func (m *fakeModule) Manifest() sdkplugin.Manifest {
	return sdkplugin.Manifest{
		ID:         m.id,
		Name:       m.id,
		Kind:       sdkplugin.KindProtocol,
		Version:    "1.0.0",
		APIVersion: sdkplugin.CurrentAPIVersion,
	}
}
func (m *fakeModule) Settings() []protocol.SettingDef     { return nil }
func (m *fakeModule) Capabilities() protocol.Capabilities { return protocol.Capabilities{} }
func (m *fakeModule) Open(ctx context.Context, req protocol.OpenRequest) (protocol.Session, error) {
	m.lastMu.Lock()
	m.lastReq = req
	m.lastMu.Unlock()
	s := &fakeSession{done: make(chan struct{})}
	m.lastMu.Lock()
	m.sessions = append(m.sessions, s)
	m.lastMu.Unlock()
	s.seenReq = req
	return s, nil
}

// ---- fake credential provider -------------------------------------------

type fakeProvider struct {
	id       string
	password string
	state    credential.State
}

func (p *fakeProvider) Manifest() sdkplugin.Manifest {
	return sdkplugin.Manifest{
		ID:         p.id,
		Name:       p.id,
		Kind:       sdkplugin.KindCredential,
		Version:    "1.0.0",
		APIVersion: sdkplugin.CurrentAPIVersion,
	}
}
func (p *fakeProvider) Capabilities() credential.Capabilities      { return credential.Capabilities{} }
func (p *fakeProvider) State(ctx context.Context) credential.State { return p.state }
func (p *fakeProvider) Unlock(ctx context.Context, _ string) error {
	p.state = credential.StateUnlocked
	return nil
}
func (p *fakeProvider) Lock(ctx context.Context) error { p.state = credential.StateLocked; return nil }
func (p *fakeProvider) Resolve(ctx context.Context, ref credential.Reference) (*credential.Material, error) {
	return &credential.Material{
		Reference: ref,
		Username:  "alice",
		Password:  p.password,
	}, nil
}
func (p *fakeProvider) List(ctx context.Context) ([]credential.Reference, error) { return nil, nil }

// ---- helpers ------------------------------------------------------------

func newTestApp(t *testing.T) (*App, string) {
	t.Helper()
	dir := t.TempDir()
	a, err := New(Config{Dir: dir, PersistInterval: 200 * time.Millisecond})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	return a, dir
}

// ---- tests ---------------------------------------------------------------

func TestCRUDHappyPath(t *testing.T) {
	a, _ := newTestApp(t)
	defer a.Shutdown(context.Background())
	ctx := context.Background()
	if err := a.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	fid, err := a.CreateFolder(ctx, domain.NilID, "prod", FolderOpts{Tags: []string{"env:prod"}})
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	cid, err := a.CreateConnection(ctx, fid, ConnectionOpts{
		Name: "web1", ProtocolID: "io.goremote.protocol.ssh", Host: "web1.example.com", Port: 22, Username: "alice",
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	// Update connection.
	port := 2222
	if err := a.UpdateConnection(ctx, cid, ConnectionPatch{Port: &port}); err != nil {
		t.Fatalf("update: %v", err)
	}
	cv, err := a.GetConnection(ctx, cid)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if cv.Port != 2222 {
		t.Fatalf("port not updated, got %d", cv.Port)
	}

	// Move to root.
	if err := a.MoveNode(ctx, cid, domain.NilID); err != nil {
		t.Fatalf("move: %v", err)
	}
	cv, _ = a.GetConnection(ctx, cid)
	if cv.ParentID != "" {
		t.Fatalf("expected root parent, got %q", cv.ParentID)
	}

	// Search.
	results := a.Search(ctx, SearchQuery{Name: "web"})
	if len(results) != 1 || results[0].ID != cid.String() {
		t.Fatalf("search: %+v", results)
	}

	// Delete.
	if err := a.DeleteNode(ctx, cid); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := a.GetConnection(ctx, cid); err == nil {
		t.Fatalf("expected not-found after delete")
	}
}

func TestPersistRoundTrip(t *testing.T) {
	dir := t.TempDir()
	a1, err := New(Config{Dir: dir, PersistInterval: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if err := a1.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	fid, _ := a1.CreateFolder(ctx, domain.NilID, "prod", FolderOpts{})
	cid, _ := a1.CreateConnection(ctx, fid, ConnectionOpts{
		Name: "db1", ProtocolID: "io.goremote.protocol.ssh", Host: "db1", Port: 22,
	})
	if err := a1.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	a2, err := New(Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer a2.Shutdown(ctx)
	if _, err := a2.GetConnection(ctx, cid); err != nil {
		t.Fatalf("expected %s to survive reload: %v", cid, err)
	}
	tree := a2.ListTree(ctx)
	if len(tree.Root.Children) == 0 {
		t.Fatalf("tree empty after reload")
	}
}

func TestSessionOpenSendClose(t *testing.T) {
	a, _ := newTestApp(t)
	ctx := context.Background()
	_ = a.Start(ctx)
	defer a.Shutdown(ctx)

	mod := &fakeModule{id: "fake.proto"}
	if err := a.RegisterProtocol(ctx, mod, sdkplugin.TrustCore); err != nil {
		t.Fatalf("register: %v", err)
	}
	cid, _ := a.CreateConnection(ctx, domain.NilID, ConnectionOpts{
		Name: "x", ProtocolID: "fake.proto", Host: "h", Port: 1,
	})
	h, err := a.OpenSession(ctx, cid)
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	sub, err := a.SubscribeOutput(ctx, h, 16)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// Collect bytes until we've seen our payload.
	var gotBuf bytes.Buffer
	done := make(chan struct{})
	go func() {
		defer close(done)
		timeout := time.After(2 * time.Second)
		for {
			select {
			case b, ok := <-sub:
				if !ok {
					return
				}
				gotBuf.Write(b)
				if strings.Contains(gotBuf.String(), "ping") {
					return
				}
			case <-timeout:
				return
			}
		}
	}()
	// Let the fake's Start run and write greeting.
	time.Sleep(50 * time.Millisecond)
	if err := a.SendInput(ctx, h, []byte("ping")); err != nil {
		t.Fatalf("send input: %v", err)
	}
	<-done
	if !strings.Contains(gotBuf.String(), "ping") {
		t.Fatalf("expected ping in output, got %q", gotBuf.String())
	}

	infos := a.ListSessions()
	if len(infos) != 1 {
		t.Fatalf("expected 1 session, got %d", len(infos))
	}

	if err := a.CloseSession(ctx, h); err != nil {
		t.Fatalf("close: %v", err)
	}
	// Idempotent.
	_ = a.CloseSession(ctx, h)
	if len(a.ListSessions()) != 0 {
		t.Fatalf("expected no sessions post-close")
	}
}

func TestOpenSession_NormalizesShortProtocolID(t *testing.T) {
	a, _ := newTestApp(t)
	ctx := context.Background()
	_ = a.Start(ctx)
	defer a.Shutdown(ctx)

	mod := &fakeModule{id: "io.goremote.protocol.ssh"}
	if err := a.RegisterProtocol(ctx, mod, sdkplugin.TrustCore); err != nil {
		t.Fatalf("register: %v", err)
	}
	cid, _ := a.CreateConnection(ctx, domain.NilID, ConnectionOpts{
		Name: "short", ProtocolID: "ssh", Host: "h", Port: 22,
	})

	h, err := a.OpenSession(ctx, cid)
	if err != nil {
		t.Fatalf("open session with short protocol id: %v", err)
	}
	defer a.CloseSession(ctx, h)
}

func TestCredentialResolution(t *testing.T) {
	a, _ := newTestApp(t)
	ctx := context.Background()
	_ = a.Start(ctx)
	defer a.Shutdown(ctx)

	prov := &fakeProvider{id: "fake.creds", password: "s3cret", state: credential.StateUnlocked}
	if err := a.RegisterCredential(ctx, prov, sdkplugin.TrustCore); err != nil {
		t.Fatalf("register cred: %v", err)
	}
	mod := &fakeModule{id: "fake.proto"}
	if err := a.RegisterProtocol(ctx, mod, sdkplugin.TrustCore); err != nil {
		t.Fatalf("register proto: %v", err)
	}

	cid, _ := a.CreateConnection(ctx, domain.NilID, ConnectionOpts{
		Name: "x", ProtocolID: "fake.proto", Host: "h",
		CredentialRef: credential.Reference{ProviderID: "fake.creds", EntryID: "e1"},
	})
	h, err := a.OpenSession(ctx, cid)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer a.CloseSession(ctx, h)

	mod.lastMu.Lock()
	got := mod.lastReq.Secret.Password
	mod.lastMu.Unlock()
	if got != "s3cret" {
		t.Fatalf("expected password 's3cret' passed to module, got %q", got)
	}
}

func TestExportAndRestore(t *testing.T) {
	a, _ := newTestApp(t)
	ctx := context.Background()
	_ = a.Start(ctx)
	cid, _ := a.CreateConnection(ctx, domain.NilID, ConnectionOpts{
		Name: "keepme", ProtocolID: "ssh", Host: "h",
	})
	bi, err := a.ExportSnapshot(ctx)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if _, err := os.Stat(bi.Path); err != nil {
		t.Fatalf("backup file missing: %v", err)
	}

	// Mutate then restore.
	_ = a.DeleteNode(ctx, cid)
	if err := a.RestoreSnapshot(ctx, bi.Path); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if _, err := a.GetConnection(ctx, cid); err != nil {
		t.Fatalf("expected restored connection to exist: %v", err)
	}
	_ = a.Shutdown(ctx)
}

func TestEventBus(t *testing.T) {
	a, _ := newTestApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = a.Start(ctx)
	defer a.Shutdown(ctx)

	ch := a.Events().Subscribe(ctx, 16)
	_, err := a.CreateConnection(ctx, domain.NilID, ConnectionOpts{
		Name: "evt", ProtocolID: "ssh", Host: "h",
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-ch:
		if ev.Kind != EventNodeCreated {
			t.Fatalf("expected NodeCreated, got %v", ev.Kind)
		}
	case <-time.After(time.Second):
		t.Fatal("no event received")
	}
}

func TestDebouncedPersister(t *testing.T) {
	dir := t.TempDir()
	a, err := New(Config{Dir: dir, PersistInterval: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	_, err = a.CreateConnection(ctx, domain.NilID, ConnectionOpts{Name: "persist", ProtocolID: "ssh", Host: "h"})
	if err != nil {
		t.Fatal(err)
	}

	// Before debounce interval elapses + a bit, inventory should still update.
	deadline := time.Now().Add(1 * time.Second)
	inv := filepath.Join(dir, "inventory.json")
	var data []byte
	for time.Now().Before(deadline) {
		data, _ = os.ReadFile(inv)
		if bytes.Contains(data, []byte("persist")) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !bytes.Contains(data, []byte("persist")) {
		t.Fatalf("expected inventory to contain 'persist' after debounce; got %s", string(data))
	}

	// Shutdown force-flushes: add another conn then shutdown immediately.
	_, _ = a.CreateConnection(ctx, domain.NilID, ConnectionOpts{Name: "shutdownflush", ProtocolID: "ssh", Host: "h"})
	if err := a.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(inv)
	if !bytes.Contains(data, []byte("shutdownflush")) {
		t.Fatalf("shutdown did not flush; inventory=%s", string(data))
	}
}

func TestConcurrentCreateConnections(t *testing.T) {
	a, _ := newTestApp(t)
	ctx := context.Background()
	_ = a.Start(ctx)
	defer a.Shutdown(ctx)

	const N = 50
	var wg sync.WaitGroup
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := a.CreateConnection(ctx, domain.NilID, ConnectionOpts{
				Name: fmt.Sprintf("c%d", i), ProtocolID: "ssh", Host: "h",
			})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatalf("concurrent create error: %v", e)
		}
	}
	// Count by walking the tree view.
	tv := a.ListTree(ctx)
	count := 0
	var walk func(n *NodeView)
	walk = func(n *NodeView) {
		if n.Kind == string(domain.NodeKindConnection) {
			count++
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(tv.Root)
	if count != N {
		t.Fatalf("expected %d connections, got %d", N, count)
	}
}

func TestImportMRemoteNGCSV(t *testing.T) {
	a, _ := newTestApp(t)
	ctx := context.Background()
	_ = a.Start(ctx)
	defer a.Shutdown(ctx)
	csv := "Name;Protocol;Hostname;Port;Username\n" +
		"alpha;SSH2;host1;22;bob\n" +
		"beta;SSH2;host2;22;carol\n"
	res, err := a.ImportMRemoteNG(ctx, "csv", strings.NewReader(csv))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Imported < 2 {
		t.Fatalf("expected >=2 imported, got %d", res.Imported)
	}
}
