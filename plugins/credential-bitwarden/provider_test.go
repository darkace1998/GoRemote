package bitwarden

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/darkace1998/GoRemote/sdk/credential"
)

// fakeCall captures one observed invocation against the Runner.
type fakeCall struct {
	name  string
	args  []string
	stdin []byte
	env   []string
}

// fakeResp is a programmed response keyed by the joined args string.
type fakeResp struct {
	stdout []byte
	stderr []byte
	code   int
	err    error
}

// fakeRunner is a programmable Runner: each invocation matches the
// first response whose argMatch is a prefix of args; matched responses
// are popped so a sequence of calls can return different payloads.
type fakeRunner struct {
	responses []fakeRespEntry
	calls     []fakeCall
}

type fakeRespEntry struct {
	argPrefix []string
	resp      fakeResp
}

func (f *fakeRunner) push(prefix []string, resp fakeResp) {
	f.responses = append(f.responses, fakeRespEntry{argPrefix: prefix, resp: resp})
}

func (f *fakeRunner) Run(ctx context.Context, name string, args []string, stdin []byte, env []string) ([]byte, []byte, int, error) {
	stdinCopy := append([]byte(nil), stdin...)
	envCopy := append([]string(nil), env...)
	f.calls = append(f.calls, fakeCall{name: name, args: append([]string(nil), args...), stdin: stdinCopy, env: envCopy})
	for i, e := range f.responses {
		if hasPrefix(args, e.argPrefix) {
			f.responses = append(f.responses[:i], f.responses[i+1:]...)
			return e.resp.stdout, e.resp.stderr, e.resp.code, e.resp.err
		}
	}
	return nil, []byte("no fake response programmed for " + strings.Join(args, " ")), 1, errors.New("no fake response")
}

func hasPrefix(args, prefix []string) bool {
	if len(prefix) > len(args) {
		return false
	}
	for i, p := range prefix {
		if args[i] != p {
			return false
		}
	}
	return true
}

// newProvider builds a Provider wired to f with a fake binary path so
// the LookPath fallback in New is bypassed.
func newProvider(f *fakeRunner) *Provider {
	return New(Options{BWBinary: "/usr/bin/bw", Runner: f})
}

func TestStateMapping(t *testing.T) {
	cases := []struct {
		name string
		body string
		want credential.State
	}{
		{"unauthenticated", `{"status":"unauthenticated"}`, credential.StateUnavailable},
		{"locked", `{"status":"locked"}`, credential.StateLocked},
		{"unlocked", `{"status":"unlocked"}`, credential.StateUnlocked},
		{"unknown", `{"status":"weird"}`, credential.StateUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeRunner{}
			f.push([]string{"status", "--raw"}, fakeResp{stdout: []byte(tc.body)})
			p := newProvider(f)
			if got := p.State(context.Background()); got != tc.want {
				t.Fatalf("State()=%q want %q", got, tc.want)
			}
		})
	}
}

func TestStateMissingBinary(t *testing.T) {
	// No BWBinary, and exec.LookPath("bw") may or may not find one on
	// the host. To keep the test deterministic we construct a Provider
	// directly and force bin to "".
	p := &Provider{runner: &fakeRunner{}}
	if got := p.State(context.Background()); got != credential.StateUnavailable {
		t.Fatalf("State()=%q want StateUnavailable", got)
	}
}

func TestUnlockCapturesSession(t *testing.T) {
	f := &fakeRunner{}
	f.push([]string{"unlock", "--raw"}, fakeResp{stdout: []byte("session-token-123\n")})
	// A subsequent status call should observe BW_SESSION in env.
	f.push([]string{"status", "--raw"}, fakeResp{stdout: []byte(`{"status":"unlocked"}`)})

	p := newProvider(f)
	if err := p.Unlock(context.Background(), "hunter2"); err != nil {
		t.Fatalf("Unlock returned error: %v", err)
	}
	if p.session != "session-token-123" {
		t.Fatalf("session=%q want session-token-123", p.session)
	}
	if len(f.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(f.calls))
	}
	if string(f.calls[0].stdin) != "hunter2" {
		t.Fatalf("stdin=%q want hunter2", string(f.calls[0].stdin))
	}

	// Subsequent State() should pass BW_SESSION env to the child.
	if got := p.State(context.Background()); got != credential.StateUnlocked {
		t.Fatalf("State()=%q want StateUnlocked", got)
	}
	if !containsEnv(f.calls[1].env, "BW_SESSION=session-token-123") {
		t.Fatalf("env=%v missing BW_SESSION", f.calls[1].env)
	}
}

func TestUnlockInvalidPassphrase(t *testing.T) {
	f := &fakeRunner{}
	f.push([]string{"unlock", "--raw"}, fakeResp{
		stderr: []byte("Invalid master password.\n"),
		code:   1,
		err:    errors.New("exit 1"),
	})
	p := newProvider(f)
	err := p.Unlock(context.Background(), "wrong")
	if !errors.Is(err, credential.ErrInvalidPassphrase) {
		t.Fatalf("err=%v want ErrInvalidPassphrase", err)
	}
}

func TestLockClearsSession(t *testing.T) {
	f := &fakeRunner{}
	f.push([]string{"lock"}, fakeResp{stdout: []byte("Your vault is locked.")})
	p := newProvider(f)
	p.session = "abc"
	if err := p.Lock(context.Background()); err != nil {
		t.Fatalf("Lock returned error: %v", err)
	}
	if p.session != "" {
		t.Fatalf("session=%q want empty", p.session)
	}
	if len(f.calls) != 1 || f.calls[0].args[0] != "lock" {
		t.Fatalf("expected lock invocation, got %#v", f.calls)
	}
	// Lock must forward BW_SESSION while the session is still cached.
	if !containsEnv(f.calls[0].env, "BW_SESSION=abc") {
		t.Fatalf("env=%v missing BW_SESSION", f.calls[0].env)
	}
}

func TestResolveLockedReturnsErrLocked(t *testing.T) {
	p := newProvider(&fakeRunner{})
	_, err := p.Resolve(context.Background(), credential.Reference{EntryID: "x"})
	if !errors.Is(err, credential.ErrLocked) {
		t.Fatalf("err=%v want ErrLocked", err)
	}
}

func TestResolveHappyPath(t *testing.T) {
	itemJSON := `{
        "id": "11111111-2222-3333-4444-555555555555",
        "name": "Production DB",
        "notes": "rotate quarterly",
        "login": {"username": "dba", "password": "s3cret"},
        "fields": [
            {"name": "totp_seed", "value": "JBSWY3DPEHPK3PXP", "type": 0},
            {"name": "host", "value": "db.example.com", "type": 0}
        ]
    }`
	f := &fakeRunner{}
	f.push([]string{"get", "item", "Production DB"}, fakeResp{stdout: []byte(itemJSON)})

	p := newProvider(f)
	p.session = "tok"
	mat, err := p.Resolve(context.Background(), credential.Reference{EntryID: "Production DB"})
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if mat.Username != "dba" || mat.Password != "s3cret" {
		t.Fatalf("username/password mismatch: %+v", mat)
	}
	if mat.Reference.EntryID != "11111111-2222-3333-4444-555555555555" {
		t.Fatalf("EntryID=%q", mat.Reference.EntryID)
	}
	if mat.Reference.Hints["name"] != "Production DB" {
		t.Fatalf("Hints=%v", mat.Reference.Hints)
	}
	wantExtra := map[string]string{
		"notes":     "rotate quarterly",
		"totp_seed": "JBSWY3DPEHPK3PXP",
		"host":      "db.example.com",
	}
	if !reflect.DeepEqual(mat.Extra, wantExtra) {
		t.Fatalf("Extra=%v want %v", mat.Extra, wantExtra)
	}
	if !containsEnv(f.calls[0].env, "BW_SESSION=tok") {
		t.Fatalf("env=%v missing BW_SESSION", f.calls[0].env)
	}
}

func TestResolveNotFound(t *testing.T) {
	f := &fakeRunner{}
	f.push([]string{"get", "item", "missing"}, fakeResp{
		stderr: []byte("Not found."),
		code:   1,
		err:    errors.New("exit 1"),
	})
	p := newProvider(f)
	p.session = "tok"
	_, err := p.Resolve(context.Background(), credential.Reference{EntryID: "missing"})
	if !errors.Is(err, credential.ErrNotFound) {
		t.Fatalf("err=%v want ErrNotFound", err)
	}
}

func TestListParsesItems(t *testing.T) {
	f := &fakeRunner{}
	f.push([]string{"list", "items"}, fakeResp{stdout: []byte(`[
        {"id": "a", "name": "Alpha", "login": {"username":"u"}},
        {"id": "b", "name": "Beta",  "login": {"username":"v"}}
    ]`)})
	p := newProvider(f)
	p.session = "tok"
	refs, err := p.List(context.Background())
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("len(refs)=%d want 2", len(refs))
	}
	if refs[0].EntryID != "a" || refs[0].Hints["name"] != "Alpha" {
		t.Fatalf("refs[0]=%+v", refs[0])
	}
	if refs[1].EntryID != "b" || refs[1].Hints["name"] != "Beta" {
		t.Fatalf("refs[1]=%+v", refs[1])
	}
	// Verify the args include --search "" exactly as documented.
	got := f.calls[0].args
	want := []string{"list", "items", "--search", ""}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args=%v want %v", got, want)
	}
}

func TestPutDeleteReturnReadOnly(t *testing.T) {
	p := newProvider(&fakeRunner{})
	if _, err := p.Put(context.Background(), credential.Material{}); !errors.Is(err, credential.ErrReadOnly) {
		t.Fatalf("Put err=%v want ErrReadOnly", err)
	}
	if err := p.Delete(context.Background(), credential.Reference{EntryID: "x"}); !errors.Is(err, credential.ErrReadOnly) {
		t.Fatalf("Delete err=%v want ErrReadOnly", err)
	}
}

func TestSync(t *testing.T) {
	f := &fakeRunner{}
	f.push([]string{"sync"}, fakeResp{stdout: []byte("Syncing complete.")})
	p := newProvider(f)
	p.session = "tok"
	if err := p.Sync(context.Background()); err != nil {
		t.Fatalf("Sync err=%v", err)
	}
	if !containsEnv(f.calls[0].env, "BW_SESSION=tok") {
		t.Fatalf("env=%v missing BW_SESSION", f.calls[0].env)
	}
}

func TestNewServerURLConfig(t *testing.T) {
	f := &fakeRunner{}
	f.push([]string{"config", "server", "https://vault.example.com"}, fakeResp{stdout: []byte("Saved.")})
	_ = New(Options{BWBinary: "/usr/bin/bw", ServerURL: "https://vault.example.com", Runner: f})
	if len(f.calls) != 1 {
		t.Fatalf("expected 1 config call, got %d (%v)", len(f.calls), f.calls)
	}
	want := []string{"config", "server", "https://vault.example.com"}
	if !reflect.DeepEqual(f.calls[0].args, want) {
		t.Fatalf("args=%v want %v", f.calls[0].args, want)
	}
}

func containsEnv(env []string, kv string) bool {
	for _, e := range env {
		if e == kv {
			return true
		}
	}
	return false
}
