package http

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/darkace1998/GoRemote/sdk/plugin"
	"github.com/darkace1998/GoRemote/sdk/protocol"
)

// Setting keys exposed by the HTTP/HTTPS plugin.
const (
	SettingURL         = "url"
	SettingVerifyTLS   = "verify_tls"
	SettingHealthCheck = "health_check"
)

// Module is the built-in HTTP/HTTPS protocol module.
type Module struct{}

// New returns a ready-to-use [Module].
func New() *Module { return &Module{} }

// Manifest returns the static manifest for this plugin.
func (m *Module) Manifest() plugin.Manifest { return Manifest }

// Settings returns the protocol-specific setting schema.
func (m *Module) Settings() []protocol.SettingDef {
	return []protocol.SettingDef{
		{
			Key: SettingURL, Label: "URL", Type: protocol.SettingString,
			Required:    true,
			Description: "Full URL to fetch. Must start with http:// or https://.",
		},
		{
			Key: SettingVerifyTLS, Label: "Verify TLS", Type: protocol.SettingBool,
			Default:     true,
			Description: "Whether HTTPS requests verify TLS certificates.",
		},
		{
			Key: SettingHealthCheck, Label: "Health-check", Type: protocol.SettingBool,
			Default:     false,
			Description: "If true, performs a single HEAD (falling back to GET) probe at Open and reports the status to the session output.",
		},
	}
}

// Capabilities reports the runtime capabilities of the HTTP module.
func (m *Module) Capabilities() protocol.Capabilities {
	return protocol.Capabilities{
		RenderModes:       []protocol.RenderMode{protocol.RenderTerminal},
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

// Open validates settings and returns a [Session] that has not yet started
// its lifecycle. The host must call Session.Start to fetch the URL.
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

	verifyTLS := view.boolOr(SettingVerifyTLS, true)
	healthCheck := view.boolOr(SettingHealthCheck, false)

	return newSession(sessionConfig{
		url:         parsed.String(),
		verifyTLS:   verifyTLS,
		healthCheck: healthCheck,
		probe:       defaultProbe,
		fetch:       defaultFetch,
	}), nil
}
