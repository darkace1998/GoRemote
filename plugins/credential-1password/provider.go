package onepassword

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"regexp"
	"strings"
	"sync"

	"github.com/darkace1998/GoRemote/sdk/credential"
	"github.com/darkace1998/GoRemote/sdk/plugin"
)

// opSessionExpiredExitCode is the exit code `op` returns when a previously
// captured session token has expired. Mapped to credential.ErrLocked so
// callers can re-prompt for the master password.
const opSessionExpiredExitCode = 6

// authErrorPattern matches stderr fragments emitted by `op` when the
// command failed because the user is not signed in or the session has
// expired. Used by State() to distinguish Locked from Error.
var authErrorPattern = regexp.MustCompile(`(?i)(not currently signed in|session.*expired|signin|sign in|authentication required)`)

// Options configures construction of a Provider.
type Options struct {
	// OpBinary is the absolute path or PATH-resolvable name of the `op`
	// CLI binary. If empty, the constructor probes $PATH for "op"; if
	// that also fails, the provider's State() reports StateUnavailable.
	OpBinary string

	// Account is the optional 1Password account shorthand (as configured
	// in `op account list`). When set, signin uses `--account <Account>`
	// and the resulting session token is exported as
	// OP_SESSION_<Account>. Leave empty for best-effort default-account
	// behaviour.
	Account string

	// Runner overrides the process invoker (for tests). When nil, an
	// os/exec-backed runner is used.
	Runner Runner
}

// Provider is a credential.Provider that delegates to the 1Password CLI.
type Provider struct {
	binary  string
	account string
	runner  Runner

	mu           sync.Mutex
	sessionToken string
	logger       *slog.Logger
}

// New constructs a Provider from the given Options. The constructor never
// returns an error; missing-binary conditions are surfaced via State().
func New(opts Options) *Provider {
	binary := opts.OpBinary
	if binary == "" {
		if found, err := exec.LookPath("op"); err == nil {
			binary = found
		}
	}
	r := opts.Runner
	if r == nil {
		r = execRunner{}
	}
	return &Provider{
		binary:  binary,
		account: opts.Account,
		runner:  r,
		logger:  slog.Default().With(slog.String("plugin", ManifestID)),
	}
}

// Manifest implements credential.Provider.
func (p *Provider) Manifest() plugin.Manifest { return Manifest() }

// Capabilities implements credential.Provider.
func (p *Provider) Capabilities() credential.Capabilities { return ProviderCapabilities() }

// sessionEnv returns the environment variables that must be passed to
// every `op` invocation. The session token (if any) is exported as
// OP_SESSION_<account> so `op` does not need to re-authenticate. Caller
// must hold p.mu.
func (p *Provider) sessionEnvLocked() []string {
	if p.sessionToken == "" {
		return nil
	}
	key := p.account
	if key == "" {
		key = "default"
	}
	return []string{fmt.Sprintf("OP_SESSION_%s=%s", sanitizeEnvKey(key), p.sessionToken)}
}

// sanitizeEnvKey strips characters that would be illegal in a POSIX
// environment variable name. 1Password account shorthands are typically
// already safe, but defensive sanitisation prevents shell injection if a
// caller passes in something exotic.
func sanitizeEnvKey(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// runOp invokes `op <args...>` using the configured Runner with the
// current session env applied. Caller must NOT hold p.mu (this method
// briefly acquires it to read the session token).
func (p *Provider) runOp(ctx context.Context, args []string, stdin []byte) (stdout, stderr []byte, exitCode int, err error) {
	if p.binary == "" {
		return nil, nil, -1, fmt.Errorf("op binary not found in PATH")
	}
	p.mu.Lock()
	env := p.sessionEnvLocked()
	p.mu.Unlock()
	return p.runner.Run(ctx, p.binary, args, stdin, env)
}

// classifyOpError converts a non-zero `op` exit into a credential SDK
// error. exit=6 indicates session expiry; messages mentioning sign-in
// also map to ErrLocked. Anything else is wrapped verbatim.
func classifyOpError(exitCode int, stderr []byte) error {
	msg := strings.TrimSpace(string(stderr))
	if exitCode == opSessionExpiredExitCode || authErrorPattern.MatchString(msg) {
		if msg != "" {
			return fmt.Errorf("%w: %s", credential.ErrLocked, msg)
		}
		return credential.ErrLocked
	}
	if msg == "" {
		return fmt.Errorf("op exited with code %d", exitCode)
	}
	return fmt.Errorf("op exited with code %d: %s", exitCode, msg)
}

// State implements credential.Provider. It probes `op whoami` to decide:
//   - StateUnavailable: binary not found.
//   - StateLocked: binary present but `op whoami` reports "not signed in".
//   - StateUnlocked: `op whoami` succeeds.
//   - StateError: other transport-level failures.
func (p *Provider) State(ctx context.Context) credential.State {
	if p.binary == "" {
		return credential.StateUnavailable
	}
	args := []string{"whoami", "--format=json"}
	if p.account != "" {
		args = append(args, "--account", p.account)
	}
	_, stderr, exitCode, err := p.runOp(ctx, args, nil)
	if err != nil {
		return credential.StateUnavailable
	}
	if exitCode == 0 {
		return credential.StateUnlocked
	}
	if exitCode == opSessionExpiredExitCode || authErrorPattern.MatchString(string(stderr)) {
		return credential.StateLocked
	}
	return credential.StateError
}

// Unlock pipes the master password to `op signin --raw` and captures the
// resulting session token. An empty passphrase is rejected up front.
func (p *Provider) Unlock(ctx context.Context, passphrase string) error {
	if passphrase == "" {
		return credential.ErrInvalidPassphrase
	}
	if p.binary == "" {
		return fmt.Errorf("op binary not found in PATH")
	}
	args := []string{"signin", "--raw"}
	if p.account != "" {
		args = []string{"signin", "--account", p.account, "--raw"}
	}
	stdout, stderr, exitCode, err := p.runner.Run(ctx, p.binary, args, []byte(passphrase), nil)
	if err != nil {
		return fmt.Errorf("invoke op signin: %w", err)
	}
	if exitCode != 0 {
		// Wrong master password: op exits non-zero with a hint. Map to
		// ErrInvalidPassphrase so the caller knows to re-prompt.
		if bytes.Contains(bytes.ToLower(stderr), []byte("password")) ||
			bytes.Contains(bytes.ToLower(stderr), []byte("incorrect")) {
			return credential.ErrInvalidPassphrase
		}
		return classifyOpError(exitCode, stderr)
	}
	token := strings.TrimSpace(string(stdout))
	if token == "" {
		return fmt.Errorf("op signin returned empty session token")
	}
	p.mu.Lock()
	p.sessionToken = token
	p.mu.Unlock()
	return nil
}

// Lock invokes `op signout` and forgets the cached session token. A
// missing binary or CLI error is logged but does not prevent the
// in-memory token from being cleared.
func (p *Provider) Lock(ctx context.Context) error {
	if p.binary == "" {
		p.mu.Lock()
		p.sessionToken = ""
		p.mu.Unlock()
		return nil
	}
	args := []string{"signout"}
	if p.account != "" {
		args = []string{"signout", "--account", p.account}
	}
	_, stderr, exitCode, err := p.runOp(ctx, args, nil)
	p.mu.Lock()
	p.sessionToken = ""
	p.mu.Unlock()
	if err != nil {
		return fmt.Errorf("invoke op signout: %w", err)
	}
	if exitCode != 0 {
		return classifyOpError(exitCode, stderr)
	}
	return nil
}

// opItemField is one entry in the "fields" array of `op item get` JSON.
type opItemField struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Value   string `json:"value"`
	Purpose string `json:"purpose"` // USERNAME / PASSWORD / NOTES
	Type    string `json:"type"`
}

// opItem is a partial decoding of `op item get --format json`. Only
// fields the provider actually consumes are captured.
type opItem struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Vault struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"vault"`
	Fields []opItemField `json:"fields"`
}

// opListItem is a partial decoding of one element of `op item list
// --format json`.
type opListItem struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Vault struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"vault"`
	URLs []struct {
		Href string `json:"href"`
	} `json:"urls,omitempty"`
}

// Resolve implements credential.Provider. The reference's EntryID is
// passed verbatim to `op item get`; ref.Hints["vault"] (when set) scopes
// the lookup to a specific vault.
func (p *Provider) Resolve(ctx context.Context, ref credential.Reference) (*credential.Material, error) {
	if ref.EntryID == "" {
		return nil, credential.ErrNotFound
	}
	args := []string{"item", "get", ref.EntryID, "--format=json"}
	if v := ref.Hints["vault"]; v != "" {
		args = append(args, "--vault="+v)
	}
	stdout, stderr, exitCode, err := p.runOp(ctx, args, nil)
	if err != nil {
		return nil, fmt.Errorf("invoke op item get: %w", err)
	}
	if exitCode != 0 {
		// op uses different exit codes across versions; treat
		// "not found" stderr as ErrNotFound regardless.
		if bytes.Contains(bytes.ToLower(stderr), []byte("isn't an item")) ||
			bytes.Contains(bytes.ToLower(stderr), []byte("no item found")) ||
			bytes.Contains(bytes.ToLower(stderr), []byte("not found")) {
			return nil, credential.ErrNotFound
		}
		return nil, classifyOpError(exitCode, stderr)
	}
	var item opItem
	if err := json.Unmarshal(stdout, &item); err != nil {
		return nil, fmt.Errorf("decode op item: %w", err)
	}
	mat := &credential.Material{
		Reference: credential.Reference{
			ProviderID: ManifestID,
			EntryID:    ref.EntryID,
			Hints:      copyStringMap(ref.Hints),
		},
		Fields: map[string]string{},
	}
	if mat.Reference.Hints == nil && (item.Vault.Name != "" || item.Title != "") {
		mat.Reference.Hints = map[string]string{}
	}
	if item.Vault.Name != "" {
		mat.Reference.Hints["vault"] = item.Vault.Name
	}
	if item.Title != "" {
		mat.Reference.Hints["title"] = item.Title
	}
	for _, f := range item.Fields {
		key := f.ID
		if key == "" {
			key = f.Label
		}
		switch {
		case f.Purpose == "USERNAME" || strings.EqualFold(key, "username"):
			mat.Username = f.Value
		case f.Purpose == "PASSWORD" || strings.EqualFold(key, "password"):
			mat.Password = f.Value
		case strings.EqualFold(key, "otp") || strings.EqualFold(f.Type, "OTP"):
			mat.OTP = f.Value
		}
		if key != "" && f.Value != "" {
			mat.Fields[key] = f.Value
		}
	}
	return mat, nil
}

// List implements credential.Provider. Hints["vault"] (when set on the
// returned references) is populated from the vault each item belongs to;
// no Hints input is consulted because op's `item list` is account-wide.
func (p *Provider) List(ctx context.Context) ([]credential.Reference, error) {
	args := []string{"item", "list", "--format=json"}
	stdout, stderr, exitCode, err := p.runOp(ctx, args, nil)
	if err != nil {
		return nil, fmt.Errorf("invoke op item list: %w", err)
	}
	if exitCode != 0 {
		return nil, classifyOpError(exitCode, stderr)
	}
	var items []opListItem
	if err := json.Unmarshal(stdout, &items); err != nil {
		return nil, fmt.Errorf("decode op item list: %w", err)
	}
	refs := make([]credential.Reference, 0, len(items))
	for _, it := range items {
		hints := map[string]string{}
		if it.Title != "" {
			hints["title"] = it.Title
		}
		if it.Vault.Name != "" {
			hints["vault"] = it.Vault.Name
		}
		if len(it.URLs) > 0 && it.URLs[0].Href != "" {
			hints["url"] = it.URLs[0].Href
		}
		if len(hints) == 0 {
			hints = nil
		}
		refs = append(refs, credential.Reference{
			ProviderID: ManifestID,
			EntryID:    it.ID,
			Hints:      hints,
		})
	}
	return refs, nil
}

// Put implements credential.Writer but always returns ErrReadOnly: the
// 1Password provider is intentionally read-only.
func (p *Provider) Put(ctx context.Context, mat credential.Material) (credential.Reference, error) {
	return credential.Reference{}, credential.ErrReadOnly
}

// Delete implements credential.Writer but always returns ErrReadOnly.
func (p *Provider) Delete(ctx context.Context, ref credential.Reference) error {
	return credential.ErrReadOnly
}

// copyStringMap returns a shallow copy of m (or nil for nil/empty m).
func copyStringMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// Compile-time interface checks.
var (
	_ credential.Provider = (*Provider)(nil)
	_ credential.Writer   = (*Provider)(nil)
)

// ensure errors is referenced even when unused in some build tags.
var _ = errors.New
