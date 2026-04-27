package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func newTestLogger(t *testing.T, level slog.Leveler) (*slog.Logger, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	lg := New(Options{Writer: &buf, Level: level})
	return lg, &buf
}

// decodeLast parses the last non-empty line of buf as JSON.
func decodeLast(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) == 0 || lines[len(lines)-1] == "" {
		t.Fatalf("no log output; got %q", buf.String())
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &out); err != nil {
		t.Fatalf("invalid JSON output %q: %v", lines[len(lines)-1], err)
	}
	return out
}

func TestRedactSensitiveKeyTopLevel(t *testing.T) {
	lg, buf := newTestLogger(t, slog.LevelDebug)
	lg.Info("login", slog.String("user", "alice"), slog.String("password", "hunter2"))

	m := decodeLast(t, buf)
	if m["password"] != Redacted {
		t.Errorf("expected password to be redacted, got %v", m["password"])
	}
	if m["user"] != "alice" {
		t.Errorf("expected user preserved, got %v", m["user"])
	}
}

func TestRedactSensitiveKeyCaseInsensitive(t *testing.T) {
	lg, buf := newTestLogger(t, slog.LevelDebug)
	lg.Info("x", slog.String("APIKey", "abc"), slog.String("Authorization", "Basic zzz"))

	m := decodeLast(t, buf)
	if m["APIKey"] != Redacted {
		t.Errorf("APIKey not redacted: %v", m["APIKey"])
	}
	if m["Authorization"] != Redacted {
		t.Errorf("Authorization not redacted: %v", m["Authorization"])
	}
}

func TestRedactSensitiveKeyNested(t *testing.T) {
	lg, buf := newTestLogger(t, slog.LevelDebug)
	lg.Info("conn",
		slog.Group("creds",
			slog.String("username", "bob"),
			slog.Group("inner",
				slog.String("token", "t0p"),
				slog.String("otp", "123456"),
			),
		),
	)

	m := decodeLast(t, buf)
	creds, ok := m["creds"].(map[string]any)
	if !ok {
		t.Fatalf("expected creds group: %v", m["creds"])
	}
	if creds["username"] != "bob" {
		t.Errorf("username should be preserved, got %v", creds["username"])
	}
	inner, ok := creds["inner"].(map[string]any)
	if !ok {
		t.Fatalf("expected inner group: %v", creds["inner"])
	}
	if inner["token"] != Redacted {
		t.Errorf("nested token not redacted: %v", inner["token"])
	}
	if inner["otp"] != Redacted {
		t.Errorf("nested otp not redacted: %v", inner["otp"])
	}
}

func TestRedactPEMBlockInNestedGroup(t *testing.T) {
	pem := "-----BEGIN RSA PRIVATE KEY-----\nMIIBOgIBAAJB\n-----END RSA PRIVATE KEY-----"
	lg, buf := newTestLogger(t, slog.LevelDebug)
	lg.Info("cert",
		slog.Group("tls",
			slog.Group("material",
				slog.String("blob", "prefix "+pem+" suffix"),
			),
		),
	)

	m := decodeLast(t, buf)
	tls := m["tls"].(map[string]any)
	material := tls["material"].(map[string]any)
	if material["blob"] != Redacted {
		t.Errorf("PEM not redacted in nested group, got %v", material["blob"])
	}
}

func TestRedactBearerAndAWSPatterns(t *testing.T) {
	lg, buf := newTestLogger(t, slog.LevelDebug)
	lg.Info("req",
		slog.String("header", "Bearer abc.def-123_XYZ"),
		slog.String("aws", "AKIAABCDEFGHIJKLMNOP"),
		slog.String("plain", "hello world"),
	)

	m := decodeLast(t, buf)
	if m["header"] != Redacted {
		t.Errorf("bearer not redacted: %v", m["header"])
	}
	if m["aws"] != Redacted {
		t.Errorf("aws key not redacted: %v", m["aws"])
	}
	if m["plain"] != "hello world" {
		t.Errorf("non-secret mutated: %v", m["plain"])
	}
}

func TestJSONOutputIsValid(t *testing.T) {
	lg, buf := newTestLogger(t, slog.LevelDebug)
	lg.Info("hi",
		slog.Int("n", 3),
		slog.Group("g", slog.String("password", "x")),
	)
	// Every line must parse as JSON.
	for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		var v any
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			t.Fatalf("invalid JSON line %q: %v", line, err)
		}
	}
}

func TestLevelFiltering(t *testing.T) {
	lg, buf := newTestLogger(t, slog.LevelWarn)
	lg.Debug("debug-msg")
	lg.Info("info-msg")
	lg.Warn("warn-msg")
	lg.Error("error-msg")

	out := buf.String()
	if strings.Contains(out, "debug-msg") || strings.Contains(out, "info-msg") {
		t.Errorf("sub-threshold messages leaked: %s", out)
	}
	if !strings.Contains(out, "warn-msg") || !strings.Contains(out, "error-msg") {
		t.Errorf("expected warn and error messages, got: %s", out)
	}
}

func TestWithComponent(t *testing.T) {
	lg, buf := newTestLogger(t, slog.LevelDebug)
	child := WithComponent(lg, "session")
	child.Info("started")

	m := decodeLast(t, buf)
	if m["component"] != "session" {
		t.Errorf("expected component=session, got %v", m["component"])
	}
}

func TestWithAttrsRedactsPreboundSecrets(t *testing.T) {
	lg, buf := newTestLogger(t, slog.LevelDebug)
	child := lg.With(slog.String("token", "s3cret"), slog.String("user", "eve"))
	child.Info("event")

	m := decodeLast(t, buf)
	if m["token"] != Redacted {
		t.Errorf("prebound token not redacted: %v", m["token"])
	}
	if m["user"] != "eve" {
		t.Errorf("prebound user changed: %v", m["user"])
	}
}

func TestNonSecretKeysPreserved(t *testing.T) {
	lg, buf := newTestLogger(t, slog.LevelDebug)
	lg.Info("m",
		slog.String("host", "example.com"),
		slog.Int("port", 22),
		slog.Bool("tls", true),
	)
	m := decodeLast(t, buf)
	if m["host"] != "example.com" || m["port"].(float64) != 22 || m["tls"] != true {
		t.Errorf("non-secret attrs mutated: %#v", m)
	}
}

func TestTraceEmitsBelowDebug(t *testing.T) {
var buf bytes.Buffer
lv := new(slog.LevelVar)
lv.Set(LevelTrace)
logger := New(Options{Writer: &buf, Level: lv})

Trace(logger, "hello", slog.String("k", "v"))
if !strings.Contains(buf.String(), `"msg":"hello"`) {
t.Fatalf("trace not emitted at LevelTrace: %q", buf.String())
}

buf.Reset()
lv.Set(slog.LevelDebug)
Trace(logger, "hello", slog.String("k", "v"))
if buf.Len() != 0 {
t.Fatalf("trace should be suppressed at debug: %q", buf.String())
}

// Nil logger must not panic and must use default.
Trace(nil, "noop")
}
