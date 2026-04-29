package integration

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/darkace1998/GoRemote/app/settings"
	"github.com/darkace1998/GoRemote/app/workspace"
	"github.com/darkace1998/GoRemote/internal/app"
	"github.com/darkace1998/GoRemote/internal/domain"
	"github.com/darkace1998/GoRemote/sdk/credential"
	sdkplugin "github.com/darkace1998/GoRemote/sdk/plugin"
	"github.com/darkace1998/GoRemote/sdk/protocol"

	"github.com/darkace1998/GoRemote/test/integration/fakes/fakecred"
	"github.com/darkace1998/GoRemote/test/integration/fakes/fakeprotocol"
)

// ----------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------

// createFakeConnection adds a connection backed by the fake protocol with a
// reference to the fake credential provider, returning its id.
func createFakeConnection(t *testing.T, h *Harness, name string) domain.ID {
	t.Helper()
	id, err := h.App.CreateConnection(testCtx(t), domain.NilID, app.ConnectionOpts{
		Name:       name,
		ProtocolID: fakeprotocol.ManifestID,
		Host:       "test.invalid",
		Port:       2222,
		Username:   "user-from-conn",
		AuthMethod: protocol.AuthPassword,
		CredentialRef: credential.Reference{
			ProviderID: fakecred.ManifestID,
			EntryID:    "primary",
		},
	})
	if err != nil {
		t.Fatalf("CreateConnection(%s): %v", name, err)
	}
	return id
}

// testCtx returns a context bound to the test's deadline (or a 10s default).
func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// waitFor polls cond until it returns true or timeout elapses.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("waitFor timeout: %s", msg)
}

// drainEcho subscribes to a session's output and returns the first chunk
// containing the given substring (or fails on timeout).
func drainEcho(t *testing.T, h *Harness, sess app.SessionHandle, substr string, timeout time.Duration) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ch, err := h.App.SubscribeOutput(ctx, sess, 16)
	if err != nil {
		t.Fatalf("SubscribeOutput: %v", err)
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	var got []byte
	for {
		select {
		case b, ok := <-ch:
			if !ok {
				t.Fatalf("output channel closed before seeing %q (got=%q)", substr, got)
			}
			got = append(got, b...)
			if containsSubstr(got, substr) {
				return string(got)
			}
		case <-deadline.C:
			t.Fatalf("timed out waiting for %q (got=%q)", substr, got)
		}
	}
}

func containsSubstr(haystack []byte, needle string) bool {
	if needle == "" {
		return true
	}
	if len(haystack) < len(needle) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if string(haystack[i:i+len(needle)]) == needle {
			return true
		}
	}
	return false
}

// ----------------------------------------------------------------------
// Tests
// ----------------------------------------------------------------------

// TestAppBootShutdown verifies that NewHarness boots the application core,
// installs the fake plugins in the registries, exposes default
// settings/workspace, and tears down cleanly.
func TestAppBootShutdown(t *testing.T) {
	t.Parallel()
	h := NewHarness(t)

	// Settings default to settings.Default().
	gotSettings, err := h.Settings.Get(testCtx(t))
	if err != nil {
		t.Fatalf("Settings.Get: %v", err)
	}
	wantSettings := settings.Default()
	if gotSettings.Theme != wantSettings.Theme || gotSettings.FontSizePx != wantSettings.FontSizePx {
		t.Errorf("settings = %+v, want defaults %+v", gotSettings, wantSettings)
	}

	// Workspace defaults to workspace.Default().
	gotWS, err := h.Workspace.Load(testCtx(t))
	if err != nil {
		t.Fatalf("Workspace.Load: %v", err)
	}
	if gotWS.Version != workspace.CurrentVersion || len(gotWS.OpenTabs) != 0 {
		t.Errorf("workspace = %+v, want default version=%d/empty tabs", gotWS, workspace.CurrentVersion)
	}

	// Registries contain exactly the fake plugins.
	protos := h.Protocols.List()
	if len(protos) != 1 || protos[0].ID != fakeprotocol.ManifestID {
		t.Errorf("protocols = %+v, want one fake-protocol", protos)
	}
	creds := h.Credentials.List()
	if len(creds) != 1 || creds[0].ID != fakecred.ManifestID {
		t.Errorf("credentials = %+v, want one fake-cred", creds)
	}

	// Explicit shutdown succeeds (and the t.Cleanup re-shutdown stays no-op).
	h.Shutdown(2 * time.Second)
}

// TestOpenConnection_PasswordAuth opens a session via the app command path,
// asserts that the resolved password reached the protocol, and verifies the
// SendInput → echo → Close lifecycle.
func TestOpenConnection_PasswordAuth(t *testing.T) {
	t.Parallel()
	h := NewHarness(t)

	connID := createFakeConnection(t, h, "fake-conn")
	sess, err := h.App.OpenSession(testCtx(t), connID)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}

	// The credential provider's Resolve was called exactly once with the
	// expected reference.
	resolves := h.Recorder.Cred.Resolves()
	if len(resolves) != 1 {
		t.Fatalf("cred Resolves = %d, want 1", len(resolves))
	}
	if got, want := resolves[0].ProviderID, fakecred.ManifestID; got != want {
		t.Errorf("Resolve.ProviderID = %q, want %q", got, want)
	}
	if got, want := resolves[0].EntryID, "primary"; got != want {
		t.Errorf("Resolve.EntryID = %q, want %q", got, want)
	}

	// The protocol module saw the resolved credential material.
	opens := h.Recorder.Protocol.Opens()
	if len(opens) != 1 {
		t.Fatalf("protocol Opens = %d, want 1", len(opens))
	}
	if got, want := opens[0].Secret.Password, fakecred.CannedPassword; got != want {
		t.Errorf("OpenRequest.Secret.Password = %q, want %q", got, want)
	}
	if got, want := opens[0].Secret.Username, fakecred.CannedUsername; got != want {
		t.Errorf("OpenRequest.Secret.Username = %q, want %q", got, want)
	}
	if got, want := opens[0].Host, "test.invalid"; got != want {
		t.Errorf("OpenRequest.Host = %q, want %q", got, want)
	}

	// SendInput → echo round-trip.
	if err := h.App.SendInput(testCtx(t), sess, []byte("hello")); err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	echo := drainEcho(t, h, sess, "> hello\n", 2*time.Second)
	if !containsSubstr([]byte(echo), "> hello\n") {
		t.Errorf("echo = %q, want it to contain %q", echo, "> hello\n")
	}

	// Close session and assert Close was observed exactly once.
	if err := h.App.CloseSession(testCtx(t), sess); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}
	waitFor(t, 2*time.Second, func() bool {
		return h.Recorder.Protocol.Closes() == 1
	}, "session Close to be invoked once")
}

// TestSettingsRoundTrip checks that UpdateSettings persists to disk and a
// fresh harness on the same dir reads the new value back.
func TestSettingsRoundTrip(t *testing.T) {
	t.Parallel()
	h := NewHarness(t)

	updated := settings.Default()
	updated.Theme = settings.ThemeDark
	updated.FontSizePx = 17
	if _, err := h.Settings.Update(testCtx(t), updated); err != nil {
		t.Fatalf("Update: %v", err)
	}
	dir := h.Dir
	h.Shutdown(2 * time.Second)

	// Reload — fresh harness on the same dir.
	h2 := NewHarnessInDir(t, dir)
	got, err := h2.Settings.Get(testCtx(t))
	if err != nil {
		t.Fatalf("Get after reload: %v", err)
	}
	if got.Theme != settings.ThemeDark {
		t.Errorf("Theme = %q, want %q", got.Theme, settings.ThemeDark)
	}
	if got.FontSizePx != 17 {
		t.Errorf("FontSizePx = %d, want 17", got.FontSizePx)
	}
}

// TestWorkspaceRestore writes a 3-tab workspace, reloads, and verifies the
// document hydrates identically. Skipped if app/workspace is unavailable.
func TestWorkspaceRestore(t *testing.T) {
	t.Parallel()
	// Pre-positioned skip in case sibling work removes the package.
	var _ = workspace.CurrentVersion // compile-time presence check
	if workspace.CurrentVersion < 1 {
		t.Skip("workspace package not available")
	}
	h := NewHarness(t)

	wantTabs := []workspace.TabState{
		{ID: "tab-1", ConnectionID: "conn-1", Title: "one", LastUsedAt: time.Unix(1, 0).UTC()},
		{ID: "tab-2", ConnectionID: "conn-2", Title: "two", LastUsedAt: time.Unix(2, 0).UTC()},
		{ID: "tab-3", ConnectionID: "conn-3", Title: "three", LastUsedAt: time.Unix(3, 0).UTC()},
	}
	doc := workspace.Workspace{
		Version:   workspace.CurrentVersion,
		OpenTabs:  wantTabs,
		ActiveTab: "tab-2",
	}
	if err := h.Workspace.Save(testCtx(t), doc); err != nil {
		t.Fatalf("Workspace.Save: %v", err)
	}
	dir := h.Dir
	h.Shutdown(2 * time.Second)

	h2 := NewHarnessInDir(t, dir)
	got, err := h2.Workspace.Load(testCtx(t))
	if err != nil {
		t.Fatalf("Workspace.Load: %v", err)
	}
	if len(got.OpenTabs) != len(wantTabs) {
		t.Fatalf("OpenTabs len = %d, want %d (got=%+v)", len(got.OpenTabs), len(wantTabs), got.OpenTabs)
	}
	for i := range wantTabs {
		if got.OpenTabs[i].ID != wantTabs[i].ID || got.OpenTabs[i].Title != wantTabs[i].Title {
			t.Errorf("OpenTabs[%d] = %+v, want %+v", i, got.OpenTabs[i], wantTabs[i])
		}
	}
	if got.ActiveTab != "tab-2" {
		t.Errorf("ActiveTab = %q, want %q", got.ActiveTab, "tab-2")
	}
}

// TestProtocolErrorPropagation verifies that a typed error from the
// protocol's Open call is surfaced by the app and that no session is
// installed in the session manager (i.e. no "tab" is opened).
func TestProtocolErrorPropagation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a, err := app.New(app.Config{Dir: dir})
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(func() {
		_ = a.Shutdown(context.Background())
	})
	bootCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.Start(bootCtx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	openErr := errors.New("fakeprotocol: synthetic open failure")
	proto := fakeprotocol.New(fakeprotocol.WithOpenError(openErr))
	cred := fakecred.New()
	if err := a.RegisterProtocol(bootCtx, proto, sdkplugin.TrustCore); err != nil {
		t.Fatalf("RegisterProtocol: %v", err)
	}
	if err := a.RegisterCredential(bootCtx, cred, sdkplugin.TrustCore); err != nil {
		t.Fatalf("RegisterCredential: %v", err)
	}

	connID, err := a.CreateConnection(testCtx(t), domain.NilID, app.ConnectionOpts{
		Name:       "broken",
		ProtocolID: fakeprotocol.ManifestID,
		Host:       "127.0.0.1",
		Port:       1,
		AuthMethod: protocol.AuthPassword,
		CredentialRef: credential.Reference{
			ProviderID: fakecred.ManifestID,
			EntryID:    "primary",
		},
	})
	if err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}

	if _, err := a.OpenSession(testCtx(t), connID); err == nil {
		t.Fatalf("OpenSession returned nil error, want %v", openErr)
	} else if !errors.Is(err, openErr) {
		// host wraps the error in a fmt.Errorf with %w; verify via Is.
		t.Errorf("OpenSession err = %v, want wraps %v", err, openErr)
	}

	if got := a.ListSessions(); len(got) != 0 {
		t.Errorf("ListSessions = %d, want 0 (no tab should be added)", len(got))
	}
}

// TestConcurrentSessions opens 5 sessions in parallel, sends a unique input
// to each, and verifies every session sees the correct echo with no
// cross-talk.
func TestConcurrentSessions(t *testing.T) {
	t.Parallel()
	h := NewHarness(t)

	const N = 5
	connIDs := make([]domain.ID, N)
	for i := 0; i < N; i++ {
		connIDs[i] = createFakeConnection(t, h, fmt.Sprintf("conn-%d", i))
	}

	var wg sync.WaitGroup
	errCh := make(chan error, N)
	wg.Add(N)
	for i := 0; i < N; i++ {

		go func() {
			defer wg.Done()
			sess, err := h.App.OpenSession(testCtx(t), connIDs[i])
			if err != nil {
				errCh <- fmt.Errorf("session %d Open: %w", i, err)
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			ch, err := h.App.SubscribeOutput(ctx, sess, 16)
			if err != nil {
				errCh <- fmt.Errorf("session %d Subscribe: %w", i, err)
				return
			}
			payload := fmt.Sprintf("msg-%d", i)
			want := "> " + payload + "\n"
			if err := h.App.SendInput(testCtx(t), sess, []byte(payload)); err != nil {
				errCh <- fmt.Errorf("session %d SendInput: %w", i, err)
				return
			}
			deadline := time.NewTimer(3 * time.Second)
			defer deadline.Stop()
			var got []byte
			for {
				select {
				case b, ok := <-ch:
					if !ok {
						errCh <- fmt.Errorf("session %d output closed early (got=%q)", i, got)
						return
					}
					got = append(got, b...)
					if containsSubstr(got, want) {
						// Make sure no other session's payload leaked in.
						for j := 0; j < N; j++ {
							if j == i {
								continue
							}
							other := fmt.Sprintf("> msg-%d", j)
							if containsSubstr(got, other) {
								errCh <- fmt.Errorf("session %d cross-talk: saw %q in %q", i, other, got)
								return
							}
						}
						_ = h.App.CloseSession(testCtx(t), sess)
						return
					}
				case <-deadline.C:
					errCh <- fmt.Errorf("session %d timed out (got=%q want=%q)", i, got, want)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
}

// TestGracefulShutdownAbortsSessions opens 2 sessions and asserts that
// app.Shutdown closes both within 5 seconds.
func TestGracefulShutdownAbortsSessions(t *testing.T) {
	t.Parallel()
	h := NewHarness(t)

	const N = 2
	for i := 0; i < N; i++ {
		connID := createFakeConnection(t, h, fmt.Sprintf("g-conn-%d", i))
		if _, err := h.App.OpenSession(testCtx(t), connID); err != nil {
			t.Fatalf("OpenSession #%d: %v", i, err)
		}
	}
	if got := len(h.App.ListSessions()); got != N {
		t.Fatalf("pre-shutdown sessions = %d, want %d", got, N)
	}

	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		done <- h.App.Shutdown(ctx)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("Shutdown did not return within 5s")
	}

	if got := h.Recorder.Protocol.Closes(); got != N {
		t.Errorf("Closes = %d, want %d", got, N)
	}
	if got := len(h.App.ListSessions()); got != 0 {
		t.Errorf("post-shutdown sessions = %d, want 0", got)
	}
}
