package http

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"

	"github.com/goremote/goremote/sdk/plugin"
	"github.com/goremote/goremote/sdk/protocol"
)

// Setting keys exposed by the HTTP/HTTPS launcher plugin.
const (
	SettingURL         = "url"
	SettingBrowser     = "browser"
	SettingVerifyTLS   = "verify_tls"
	SettingHealthCheck = "health_check"
)

// launcher abstracts the platform-specific browser-open mechanism so tests
// can substitute a fake. resolve returns the executable path and any leading
// fixed arguments that come *before* the URL (e.g. for windows
// "rundll32 url.dll,FileProtocolHandler"); the URL is appended as the final
// argument by the caller.
type launcher interface {
	resolve(ctx context.Context, browserOverride string) (path string, prefixArgs []string, err error)
	run(ctx context.Context, path string, args []string) error
}

// Module is the built-in HTTP/HTTPS launcher protocol module.
//
// The zero value is ready to use, but tests can construct a Module with a
// custom launcher via [Module.WithLauncher].
type Module struct {
	// launcher resolves and runs the platform browser-open command. nil ==
	// use the default platform launcher.
	launcher launcher
}

// New returns a ready-to-use [Module] backed by the default platform
// launcher.
func New() *Module { return &Module{} }

// WithLauncher returns a copy of m that uses the provided launcher. Intended
// for tests; production code should use [New].
func (m *Module) WithLauncher(l launcher) *Module {
	cp := *m
	cp.launcher = l
	return &cp
}

func (m *Module) effectiveLauncher() launcher {
	if m.launcher != nil {
		return m.launcher
	}
	return defaultLauncher{}
}

// Manifest returns the static manifest for this plugin.
func (m *Module) Manifest() plugin.Manifest { return Manifest }

// Settings returns the protocol-specific setting schema.
func (m *Module) Settings() []protocol.SettingDef {
	return []protocol.SettingDef{
		{
			Key: SettingURL, Label: "URL", Type: protocol.SettingString,
			Required:    true,
			Description: "Full URL to launch. Must start with http:// or https://.",
		},
		{
			Key: SettingBrowser, Label: "Browser binary", Type: protocol.SettingString,
			Description: "Optional path to a browser executable. If empty, the platform default open helper is used.",
		},
		{
			Key: SettingVerifyTLS, Label: "Verify TLS", Type: protocol.SettingBool,
			Default:     true,
			Description: "Whether the optional health-check probe verifies TLS certificates. Has no effect on the launched browser.",
		},
		{
			Key: SettingHealthCheck, Label: "Health-check", Type: protocol.SettingBool,
			Default:     false,
			Description: "If true, performs a single HEAD (falling back to GET) probe at Open and reports the status to the session output.",
		},
	}
}

// Capabilities reports the runtime capabilities of the HTTP launcher module.
// It renders externally only and supports no auth, resize, or send-input.
func (m *Module) Capabilities() protocol.Capabilities {
	return protocol.Capabilities{
		RenderModes:       []protocol.RenderMode{protocol.RenderExternal},
		AuthMethods:       []protocol.AuthMethod{protocol.AuthNone},
		SupportsResize:    false,
		SupportsClipboard: false,
		SupportsLogging:   true,
		SupportsReconnect: false,
	}
}

// settingsView is a typed view over the untyped settings map.
type settingsView struct{ m map[string]any }

func (s settingsView) stringOr(key, def string) string {
	if v, ok := s.m[key]; ok {
		if x, ok := v.(string); ok && x != "" {
			return x
		}
	}
	return def
}

func (s settingsView) boolOr(key string, def bool) bool {
	v, ok := s.m[key]
	if !ok {
		return def
	}
	switch x := v.(type) {
	case bool:
		return x
	case string:
		switch strings.ToLower(x) {
		case "true", "yes", "1":
			return true
		case "false", "no", "0":
			return false
		}
	}
	return def
}

// Open validates settings, resolves the launcher, and returns a [Session]
// that has not yet started its lifecycle. The host must call Session.Start
// to actually launch the URL.
func (m *Module) Open(ctx context.Context, req protocol.OpenRequest) (protocol.Session, error) {
	view := settingsView{m: req.Settings}

	rawURL := view.stringOr(SettingURL, "")
	if rawURL == "" {
		return nil, errors.New("http: url is required")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("http: invalid url %q: %w", rawURL, err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("http: url scheme must be http or https, got %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("http: url %q has no host", rawURL)
	}

	browser := view.stringOr(SettingBrowser, "")
	verifyTLS := view.boolOr(SettingVerifyTLS, true)
	healthCheck := view.boolOr(SettingHealthCheck, false)

	return newSession(sessionConfig{
		url:         parsed.String(),
		browser:     browser,
		verifyTLS:   verifyTLS,
		healthCheck: healthCheck,
		launcher:    m.effectiveLauncher(),
		probe:       defaultProbe,
	}), nil
}

// defaultLauncher resolves and runs platform-native browser-open helpers.
type defaultLauncher struct{}

func (defaultLauncher) resolve(_ context.Context, browserOverride string) (string, []string, error) {
	if browserOverride != "" {
		// If the user gave a bare program name, look it up on PATH; if they
		// gave an absolute or relative path, use it verbatim.
		if strings.ContainsAny(browserOverride, "/\\") {
			return browserOverride, nil, nil
		}
		path, err := exec.LookPath(browserOverride)
		if err != nil {
			return "", nil, fmt.Errorf("http: browser %q not found on PATH: %w", browserOverride, err)
		}
		return path, nil, nil
	}

	switch runtime.GOOS {
	case "darwin":
		path, err := exec.LookPath("open")
		if err != nil {
			return "", nil, fmt.Errorf("http: cannot find 'open' on darwin: %w", err)
		}
		return path, nil, nil
	case "windows":
		path, err := exec.LookPath("rundll32")
		if err != nil {
			return "", nil, fmt.Errorf("http: cannot find 'rundll32' on windows: %w", err)
		}
		return path, []string{"url.dll,FileProtocolHandler"}, nil
	default:
		// linux, *bsd, etc.
		for _, candidate := range []string{"xdg-open", "gio", "sensible-browser"} {
			path, err := exec.LookPath(candidate)
			if err != nil {
				continue
			}
			if candidate == "gio" {
				return path, []string{"open"}, nil
			}
			return path, nil, nil
		}
		return "", nil, errors.New("http: no browser launcher found (looked for xdg-open, gio, sensible-browser)")
	}
}

func (defaultLauncher) run(ctx context.Context, path string, args []string) error {
	cmd := exec.CommandContext(ctx, path, args...)
	// Detach: we don't want the browser process to keep the goremote
	// process attached to its lifetime, and we don't capture its output.
	if err := cmd.Start(); err != nil {
		return err
	}
	// Release the child so we don't leak a zombie if Wait is never called.
	go func() { _ = cmd.Wait() }()
	return nil
}
