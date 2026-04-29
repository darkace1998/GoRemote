package onepassword

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/darkace1998/GoRemote/sdk/credential"
)

// fakeCall records one invocation made by the provider against the
// Runner abstraction. It captures everything a test might want to assert
// on, including the env passed through.
type fakeCall struct {
	Name  string
	Args  []string
	Stdin []byte
	Env   []string
}

// fakeResponse scripts a single response from the fake runner.
type fakeResponse struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	Err      error
}

// fakeRunner is a Runner implementation that returns scripted responses
// based on the first argument (the `op` subcommand). Tests configure it
// by populating Responses; each call also appends to Calls so tests can
// inspect what the provider asked for.
type fakeRunner struct {
	mu        sync.Mutex
	Responses map[string]fakeResponse
	Calls     []fakeCall
	// Default is returned when no entry matches the subcommand.
	Default fakeResponse
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{Responses: map[string]fakeResponse{}}
}

func (f *fakeRunner) Run(ctx context.Context, name string, args []string, stdin []byte, env []string) ([]byte, []byte, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, fakeCall{
		Name:  name,
		Args:  append([]string(nil), args...),
		Stdin: append([]byte(nil), stdin...),
		Env:   append([]string(nil), env...),
	})
	key := ""
	if len(args) > 0 {
		key = args[0]
		if len(args) > 1 {
			// Allow keying off "item get" or "item list".
			composite := args[0] + " " + args[1]
			if r, ok := f.Responses[composite]; ok {
				return r.Stdout, r.Stderr, r.ExitCode, r.Err
			}
		}
	}
	if r, ok := f.Responses[key]; ok {
		return r.Stdout, r.Stderr, r.ExitCode, r.Err
	}
	return f.Default.Stdout, f.Default.Stderr, f.Default.ExitCode, f.Default.Err
}

func TestStateUnavailableWhenBinaryMissing(t *testing.T) {
	p := &Provider{binary: "", runner: newFakeRunner()}
	if got := p.State(context.Background()); got != credential.StateUnavailable {
		t.Fatalf("State: got %v want %v", got, credential.StateUnavailable)
	}
}

func TestStateLockedWhenWhoamiReportsAuthError(t *testing.T) {
	r := newFakeRunner()
	r.Responses["whoami"] = fakeResponse{
		Stderr:   []byte("[ERROR] You are not currently signed in. Please run `op signin`."),
		ExitCode: 1,
	}
	p := New(Options{OpBinary: "/usr/local/bin/op", Runner: r})
	if got := p.State(context.Background()); got != credential.StateLocked {
		t.Fatalf("State: got %v want %v", got, credential.StateLocked)
	}
	if len(r.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(r.Calls))
	}
	if r.Calls[0].Args[0] != "whoami" {
		t.Fatalf("expected whoami, got %v", r.Calls[0].Args)
	}
}

func TestStateLockedWhenSessionExpired(t *testing.T) {
	r := newFakeRunner()
	r.Responses["whoami"] = fakeResponse{
		Stderr:   []byte("session expired"),
		ExitCode: opSessionExpiredExitCode,
	}
	p := New(Options{OpBinary: "/op", Runner: r})
	if got := p.State(context.Background()); got != credential.StateLocked {
		t.Fatalf("State: got %v want %v", got, credential.StateLocked)
	}
}

func TestStateUnlocked(t *testing.T) {
	r := newFakeRunner()
	r.Responses["whoami"] = fakeResponse{
		Stdout:   []byte(`{"url":"https://my.1password.com","email":"a@b","user_uuid":"U"}`),
		ExitCode: 0,
	}
	p := New(Options{OpBinary: "/op", Runner: r})
	if got := p.State(context.Background()); got != credential.StateUnlocked {
		t.Fatalf("State: got %v want %v", got, credential.StateUnlocked)
	}
}

func TestUnlockEmptyPassphraseRejected(t *testing.T) {
	p := New(Options{OpBinary: "/op", Runner: newFakeRunner()})
	if err := p.Unlock(context.Background(), ""); !errors.Is(err, credential.ErrInvalidPassphrase) {
		t.Fatalf("expected ErrInvalidPassphrase, got %v", err)
	}
}

func TestUnlockCapturesTokenAndPropagatesEnv(t *testing.T) {
	const masterPwd = "hunter2"
	const token = "SESSION-TOKEN-XYZ"
	r := newFakeRunner()
	r.Responses["signin"] = fakeResponse{
		Stdout:   []byte(token + "\n"),
		ExitCode: 0,
	}
	r.Responses["whoami"] = fakeResponse{
		Stdout:   []byte(`{"email":"x"}`),
		ExitCode: 0,
	}

	p := New(Options{OpBinary: "/op", Account: "acme", Runner: r})
	if err := p.Unlock(context.Background(), masterPwd); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	// Verify stdin captured.
	signinCall := r.Calls[0]
	if string(signinCall.Stdin) != masterPwd {
		t.Fatalf("stdin: got %q want %q", signinCall.Stdin, masterPwd)
	}
	// Verify --account propagated.
	if !contains(signinCall.Args, "--account") || !contains(signinCall.Args, "acme") {
		t.Fatalf("expected --account acme in signin args, got %v", signinCall.Args)
	}
	// A subsequent call should carry OP_SESSION_acme=token.
	if got := p.State(context.Background()); got != credential.StateUnlocked {
		t.Fatalf("State after unlock: %v", got)
	}
	whoamiCall := r.Calls[1]
	wantEnv := "OP_SESSION_acme=" + token
	if !contains(whoamiCall.Env, wantEnv) {
		t.Fatalf("expected env %q, got %v", wantEnv, whoamiCall.Env)
	}
}

func TestUnlockWrongPasswordReturnsErrInvalidPassphrase(t *testing.T) {
	r := newFakeRunner()
	r.Responses["signin"] = fakeResponse{
		Stderr:   []byte("[ERROR] incorrect master password"),
		ExitCode: 1,
	}
	p := New(Options{OpBinary: "/op", Runner: r})
	err := p.Unlock(context.Background(), "wrong")
	if !errors.Is(err, credential.ErrInvalidPassphrase) {
		t.Fatalf("expected ErrInvalidPassphrase, got %v", err)
	}
}

func TestLockInvokesSignoutAndClearsToken(t *testing.T) {
	r := newFakeRunner()
	r.Responses["signin"] = fakeResponse{Stdout: []byte("TOK\n"), ExitCode: 0}
	r.Responses["signout"] = fakeResponse{ExitCode: 0}
	r.Responses["whoami"] = fakeResponse{
		Stderr: []byte("not currently signed in"), ExitCode: 1,
	}

	p := New(Options{OpBinary: "/op", Account: "acme", Runner: r})
	if err := p.Unlock(context.Background(), "pw"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if err := p.Lock(context.Background()); err != nil {
		t.Fatalf("Lock: %v", err)
	}
	// Find the signout call.
	var foundSignout bool
	for _, c := range r.Calls {
		if len(c.Args) > 0 && c.Args[0] == "signout" {
			foundSignout = true
		}
	}
	if !foundSignout {
		t.Fatalf("expected signout call, got calls: %+v", r.Calls)
	}
	// After Lock, the in-memory token must be cleared: a follow-up
	// invocation should have an empty (non-OP_SESSION) env.
	r.Responses["whoami"] = fakeResponse{ExitCode: 0, Stdout: []byte(`{}`)}
	_ = p.State(context.Background())
	last := r.Calls[len(r.Calls)-1]
	for _, e := range last.Env {
		if strings.HasPrefix(e, "OP_SESSION_") {
			t.Fatalf("expected no OP_SESSION_* after Lock, got %q", e)
		}
	}
}

// sampleItemJSON is a representative `op item get --format=json` payload.
func sampleItemJSON(t *testing.T) []byte {
	t.Helper()
	item := opItem{
		ID:    "abc123",
		Title: "Production DB",
	}
	item.Vault.ID = "v1"
	item.Vault.Name = "Engineering"
	item.Fields = []opItemField{
		{ID: "username", Label: "username", Value: "alice", Purpose: "USERNAME"},
		{ID: "password", Label: "password", Value: "s3cret!", Purpose: "PASSWORD", Type: "CONCEALED"},
		{ID: "host", Label: "host", Value: "db.internal"},
		{ID: "one-time password", Label: "one-time password", Value: "123456", Type: "OTP"},
	}
	b, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal sample: %v", err)
	}
	return b
}

func TestResolveParsesItem(t *testing.T) {
	r := newFakeRunner()
	r.Responses["item get"] = fakeResponse{Stdout: sampleItemJSON(t), ExitCode: 0}

	p := New(Options{OpBinary: "/op", Runner: r})
	mat, err := p.Resolve(context.Background(), credential.Reference{
		EntryID: "abc123",
		Hints:   map[string]string{"vault": "Engineering"},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if mat.Username != "alice" {
		t.Fatalf("Username: got %q want %q", mat.Username, "alice")
	}
	if mat.Password != "s3cret!" {
		t.Fatalf("Password: got %q want %q", mat.Password, "s3cret!")
	}
	if mat.OTP != "123456" {
		t.Fatalf("OTP: got %q want %q", mat.OTP, "123456")
	}
	if mat.Fields["host"] != "db.internal" {
		t.Fatalf("Fields[host]: got %q", mat.Fields["host"])
	}
	if mat.Reference.ProviderID != ManifestID {
		t.Fatalf("ProviderID: got %q", mat.Reference.ProviderID)
	}
	// Verify the runner saw --vault=Engineering on the args.
	args := r.Calls[0].Args
	if !contains(args, "--vault=Engineering") {
		t.Fatalf("expected --vault=Engineering, got %v", args)
	}
}

func TestResolveEmptyEntryIDReturnsNotFound(t *testing.T) {
	p := New(Options{OpBinary: "/op", Runner: newFakeRunner()})
	_, err := p.Resolve(context.Background(), credential.Reference{})
	if !errors.Is(err, credential.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestResolveSessionExpiredReturnsErrLocked(t *testing.T) {
	r := newFakeRunner()
	r.Responses["item get"] = fakeResponse{
		Stderr:   []byte("session has expired"),
		ExitCode: opSessionExpiredExitCode,
	}
	p := New(Options{OpBinary: "/op", Runner: r})
	_, err := p.Resolve(context.Background(), credential.Reference{EntryID: "x"})
	if !errors.Is(err, credential.ErrLocked) {
		t.Fatalf("expected ErrLocked, got %v", err)
	}
}

func TestResolveItemNotFound(t *testing.T) {
	r := newFakeRunner()
	r.Responses["item get"] = fakeResponse{
		Stderr:   []byte(`"missing" isn't an item.`),
		ExitCode: 1,
	}
	p := New(Options{OpBinary: "/op", Runner: r})
	_, err := p.Resolve(context.Background(), credential.Reference{EntryID: "missing"})
	if !errors.Is(err, credential.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListParsesItems(t *testing.T) {
	r := newFakeRunner()
	items := []opListItem{
		{ID: "id1", Title: "Server A"},
		{ID: "id2", Title: "Server B"},
	}
	items[0].Vault.Name = "Personal"
	items[1].Vault.Name = "Engineering"
	items[1].URLs = []struct {
		Href string `json:"href"`
	}{{Href: "https://b.example"}}
	body, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	r.Responses["item list"] = fakeResponse{Stdout: body, ExitCode: 0}

	p := New(Options{OpBinary: "/op", Runner: r})
	refs, err := p.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("expected 2 refs, got %d", len(refs))
	}
	want := map[string]string{"id1": "Server A", "id2": "Server B"}
	for _, ref := range refs {
		if ref.ProviderID != ManifestID {
			t.Fatalf("ProviderID: %q", ref.ProviderID)
		}
		if ref.Hints["title"] != want[ref.EntryID] {
			t.Fatalf("title for %s: %q", ref.EntryID, ref.Hints["title"])
		}
	}
	// Second item should have URL hint.
	for _, ref := range refs {
		if ref.EntryID == "id2" && ref.Hints["url"] != "https://b.example" {
			t.Fatalf("url hint missing on id2: %+v", ref.Hints)
		}
	}
}

func TestPutAndDeleteAreReadOnly(t *testing.T) {
	p := New(Options{OpBinary: "/op", Runner: newFakeRunner()})
	if _, err := p.Put(context.Background(), credential.Material{}); !errors.Is(err, credential.ErrReadOnly) {
		t.Fatalf("Put: expected ErrReadOnly, got %v", err)
	}
	if err := p.Delete(context.Background(), credential.Reference{}); !errors.Is(err, credential.ErrReadOnly) {
		t.Fatalf("Delete: expected ErrReadOnly, got %v", err)
	}
}

func TestManifestAndCapabilities(t *testing.T) {
	p := New(Options{OpBinary: "/op", Runner: newFakeRunner()})
	m := p.Manifest()
	if m.ID != ManifestID {
		t.Fatalf("manifest id: %q", m.ID)
	}
	if m.Version != "1.0.0" {
		t.Fatalf("manifest version: %q", m.Version)
	}
	if m.Status != "ready" {
		t.Fatalf("manifest status: %q", m.Status)
	}
	if !m.HasCapability("os.exec") {
		t.Fatalf("missing os.exec capability: %+v", m.Capabilities)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("manifest invalid: %v", err)
	}
	caps := p.Capabilities()
	if !caps.Lookup || !caps.Unlock || caps.Write {
		t.Fatalf("unexpected caps: %+v", caps)
	}
}

// Sanity-check sanitizeEnvKey for shell-injection-style inputs.
func TestSanitizeEnvKey(t *testing.T) {
	cases := map[string]string{
		"acme":         "acme",
		"ACME-2":       "ACME_2",
		"a b;rm -rf /": "a_b_rm__rf__",
	}
	for in, want := range cases {
		if got := sanitizeEnvKey(in); got != want {
			t.Errorf("sanitizeEnvKey(%q) = %q, want %q", in, got, want)
		}
	}
}

// contains reports whether s appears in xs (slice membership helper).
func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// debugDump is a no-op import shim ensuring fmt is referenced even if a
// future test edit removes its only usage.
var _ = fmt.Sprintf
