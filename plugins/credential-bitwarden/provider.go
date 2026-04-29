package bitwarden

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/darkace1998/GoRemote/sdk/credential"
	"github.com/darkace1998/GoRemote/sdk/plugin"
)

// Runner abstracts process execution so tests can inject a fake `bw`.
type Runner interface {
	Run(ctx context.Context, name string, args []string, stdin []byte, env []string) (stdout, stderr []byte, exitCode int, err error)
}

// execRunner is the default Runner implementation, backed by os/exec.
type execRunner struct{}

// Run executes name with the supplied args, writing stdin to the
// child's standard input and returning its captured stdout/stderr,
// exit code and any process-level error.
func (execRunner) Run(ctx context.Context, name string, args []string, stdin []byte, env []string) ([]byte, []byte, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if len(stdin) > 0 {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	if env != nil {
		cmd.Env = env
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return stdout.Bytes(), stderr.Bytes(), exitCode, err
}

// Options configures a Provider.
type Options struct {
	// BWBinary is the path to the `bw` executable. When empty, the
	// provider resolves it via exec.LookPath at construction time.
	BWBinary string
	// ServerURL, if non-empty, causes a one-shot `bw config server <url>`
	// to be issued at construction time so the CLI talks to a self-hosted
	// Bitwarden instance.
	ServerURL string
	// Runner overrides the default os/exec-based runner. Useful for
	// tests or sandboxed hosts.
	Runner Runner
}

// Provider is a credential.Provider backed by the Bitwarden CLI.
//
// The zero value is not usable; construct instances with New.
type Provider struct {
	bin    string
	runner Runner

	mu      sync.Mutex
	session string // BW_SESSION token captured from `bw unlock --raw`
}

// New constructs a Bitwarden Provider.
//
// If opts.BWBinary is empty the binary is located via exec.LookPath; a
// missing binary is not fatal — the provider remembers the lookup result
// and reports StateUnavailable until the binary is installed. If
// opts.ServerURL is non-empty, the provider issues a one-shot
// `bw config server <url>` so the CLI is configured for self-hosted
// instances. Failures of that one-shot call are non-fatal.
func New(opts Options) *Provider {
	r := opts.Runner
	if r == nil {
		r = execRunner{}
	}
	bin := opts.BWBinary
	if bin == "" {
		if path, err := exec.LookPath("bw"); err == nil {
			bin = path
		}
	}
	p := &Provider{bin: bin, runner: r}
	if bin != "" && opts.ServerURL != "" {
		// One-shot config; ignore failures so that a misconfigured
		// server URL does not prevent the provider from being created.
		_, _, _, _ = r.Run(context.Background(), bin, []string{"config", "server", opts.ServerURL}, nil, nil)
	}
	return p
}

// Manifest implements credential.Provider.
func (p *Provider) Manifest() plugin.Manifest { return Manifest() }

// Capabilities implements credential.Provider.
func (p *Provider) Capabilities() credential.Capabilities { return ProviderCapabilities() }

// bwStatus mirrors the JSON document returned by `bw status --raw`.
type bwStatus struct {
	Status string `json:"status"`
}

// envWithSession returns a copy of os.Environ-style env containing
// BW_SESSION when a session token has been captured. Returns nil when
// no session is set, instructing exec.Cmd to inherit the parent env.
func (p *Provider) envWithSession() []string {
	if p.session == "" {
		return nil
	}
	return []string{"BW_SESSION=" + p.session}
}

// State implements credential.Provider.
//
// State derives from `bw status --raw` (a JSON document with a "status"
// field). The mapping is:
//
//	"unauthenticated" → StateUnavailable
//	"locked"          → StateLocked
//	"unlocked"        → StateUnlocked
//
// any other value, or an absent binary, also maps to StateUnavailable.
func (p *Provider) State(ctx context.Context) credential.State {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.bin == "" {
		return credential.StateUnavailable
	}
	stdout, _, _, err := p.runner.Run(ctx, p.bin, []string{"status", "--raw"}, nil, p.envWithSession())
	if err != nil {
		return credential.StateUnavailable
	}
	var s bwStatus
	if jerr := json.Unmarshal(bytes.TrimSpace(stdout), &s); jerr != nil {
		return credential.StateUnavailable
	}
	switch s.Status {
	case "unauthenticated":
		return credential.StateUnavailable
	case "locked":
		return credential.StateLocked
	case "unlocked":
		return credential.StateUnlocked
	default:
		return credential.StateUnavailable
	}
}

// Unlock implements credential.Provider. It pipes passphrase to
// `bw unlock --raw` and captures the returned session token, which is
// then forwarded as BW_SESSION on every subsequent invocation.
func (p *Provider) Unlock(ctx context.Context, passphrase string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.bin == "" {
		return fmt.Errorf("bitwarden: bw binary not found")
	}
	stdout, stderr, code, err := p.runner.Run(ctx, p.bin, []string{"unlock", "--raw"}, []byte(passphrase), nil)
	if err != nil || code != 0 {
		// Distinguish bad passphrase from other errors when bw says so.
		if bytes.Contains(stderr, []byte("Invalid master password")) {
			return credential.ErrInvalidPassphrase
		}
		return fmt.Errorf("bitwarden: bw unlock failed (exit %d): %s", code, strings.TrimSpace(string(stderr)))
	}
	token := strings.TrimSpace(string(stdout))
	if token == "" {
		return fmt.Errorf("bitwarden: bw unlock returned empty session token")
	}
	p.session = token
	return nil
}

// Lock implements credential.Provider. It runs `bw lock` and forgets
// any cached session token regardless of the CLI's exit status — once
// the host has decided to lock, leaking the token serves no purpose.
func (p *Provider) Lock(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	defer func() { p.session = "" }()
	if p.bin == "" {
		return nil
	}
	_, stderr, code, err := p.runner.Run(ctx, p.bin, []string{"lock"}, nil, p.envWithSession())
	if err != nil || code != 0 {
		return fmt.Errorf("bitwarden: bw lock failed (exit %d): %s", code, strings.TrimSpace(string(stderr)))
	}
	return nil
}

// bwField mirrors the custom-field shape returned by `bw get item`.
type bwField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Type  int    `json:"type"`
}

// bwLogin mirrors the login section of a Bitwarden item.
type bwLogin struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// bwItem mirrors the subset of a Bitwarden item the provider consumes.
type bwItem struct {
	ID     string    `json:"id"`
	Name   string    `json:"name"`
	Notes  string    `json:"notes"`
	Login  bwLogin   `json:"login"`
	Fields []bwField `json:"fields"`
}

// Resolve implements credential.Provider.
//
// ref.EntryID is forwarded verbatim to `bw get item`, which accepts
// both UUIDs and search strings. The provider must be unlocked.
func (p *Provider) Resolve(ctx context.Context, ref credential.Reference) (*credential.Material, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.bin == "" {
		return nil, fmt.Errorf("bitwarden: bw binary not found")
	}
	if p.session == "" {
		return nil, credential.ErrLocked
	}
	if ref.EntryID == "" {
		return nil, credential.ErrNotFound
	}
	stdout, stderr, code, err := p.runner.Run(ctx, p.bin, []string{"get", "item", ref.EntryID}, nil, p.envWithSession())
	if err != nil || code != 0 {
		// `bw get item` exits non-zero on miss with "Not found." on stderr.
		if bytes.Contains(stderr, []byte("Not found")) {
			return nil, credential.ErrNotFound
		}
		return nil, fmt.Errorf("bitwarden: bw get item failed (exit %d): %s", code, strings.TrimSpace(string(stderr)))
	}
	var item bwItem
	if jerr := json.Unmarshal(bytes.TrimSpace(stdout), &item); jerr != nil {
		return nil, fmt.Errorf("bitwarden: decode item: %w", jerr)
	}
	mat := &credential.Material{
		Reference: credential.Reference{
			ProviderID: ManifestID,
			EntryID:    item.ID,
			Hints:      map[string]string{"name": item.Name},
		},
		Username: item.Login.Username,
		Password: item.Login.Password,
	}
	if len(item.Fields) > 0 || item.Notes != "" {
		mat.Extra = make(map[string]string, len(item.Fields)+1)
	}
	if item.Notes != "" {
		mat.Extra["notes"] = item.Notes
	}
	for _, f := range item.Fields {
		if f.Name == "" {
			continue
		}
		mat.Extra[f.Name] = f.Value
	}
	return mat, nil
}

// List implements credential.Provider.
func (p *Provider) List(ctx context.Context) ([]credential.Reference, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.bin == "" {
		return nil, fmt.Errorf("bitwarden: bw binary not found")
	}
	if p.session == "" {
		return nil, credential.ErrLocked
	}
	stdout, stderr, code, err := p.runner.Run(ctx, p.bin, []string{"list", "items", "--search", ""}, nil, p.envWithSession())
	if err != nil || code != 0 {
		return nil, fmt.Errorf("bitwarden: bw list items failed (exit %d): %s", code, strings.TrimSpace(string(stderr)))
	}
	var items []bwItem
	if jerr := json.Unmarshal(bytes.TrimSpace(stdout), &items); jerr != nil {
		return nil, fmt.Errorf("bitwarden: decode items: %w", jerr)
	}
	refs := make([]credential.Reference, 0, len(items))
	for _, it := range items {
		refs = append(refs, credential.Reference{
			ProviderID: ManifestID,
			EntryID:    it.ID,
			Hints:      map[string]string{"name": it.Name},
		})
	}
	return refs, nil
}

// Sync runs `bw sync` to refresh the local vault cache. It is exposed
// as a Provider-specific method (not part of credential.Provider) so
// hosts that know they're talking to Bitwarden can trigger a refresh.
func (p *Provider) Sync(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.bin == "" {
		return fmt.Errorf("bitwarden: bw binary not found")
	}
	_, stderr, code, err := p.runner.Run(ctx, p.bin, []string{"sync"}, nil, p.envWithSession())
	if err != nil || code != 0 {
		return fmt.Errorf("bitwarden: bw sync failed (exit %d): %s", code, strings.TrimSpace(string(stderr)))
	}
	return nil
}

// Put implements credential.Writer. Bitwarden is treated as the source
// of truth, so the provider deliberately rejects writes.
func (p *Provider) Put(ctx context.Context, mat credential.Material) (credential.Reference, error) {
	return credential.Reference{}, credential.ErrReadOnly
}

// Delete implements credential.Writer.
func (p *Provider) Delete(ctx context.Context, ref credential.Reference) error {
	return credential.ErrReadOnly
}

// Compile-time interface checks.
var (
	_ credential.Provider = (*Provider)(nil)
	_ credential.Writer   = (*Provider)(nil)
)
