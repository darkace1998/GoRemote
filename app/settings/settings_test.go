package settings

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	t.Parallel()
	d := Default()
	if d.Theme != ThemeSystem {
		t.Errorf("Theme = %q, want %q", d.Theme, ThemeSystem)
	}
	if d.FontSizePx != 13 {
		t.Errorf("FontSizePx = %d, want 13", d.FontSizePx)
	}
	if !d.ConfirmOnClose {
		t.Errorf("ConfirmOnClose = false, want true")
	}
	if d.AutoReconnect {
		t.Errorf("AutoReconnect = true, want false")
	}
	if d.ReconnectMaxN != 3 {
		t.Errorf("ReconnectMaxN = %d, want 3", d.ReconnectMaxN)
	}
	if d.ReconnectDelayMs != 2000 {
		t.Errorf("ReconnectDelayMs = %d, want 2000", d.ReconnectDelayMs)
	}
	if d.TelemetryEnabled {
		t.Errorf("TelemetryEnabled = true, want false")
	}
	if err := d.Validate(); err != nil {
		t.Errorf("Default().Validate() = %v", err)
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		mutate  func(*Settings)
		wantSub string
	}{
		{"bad theme", func(s *Settings) { s.Theme = "neon" }, "invalid theme"},
		{"empty theme", func(s *Settings) { s.Theme = "" }, "invalid theme"},
		{"font too small", func(s *Settings) { s.FontSizePx = 7 }, "fontSizePx"},
		{"font too large", func(s *Settings) { s.FontSizePx = 73 }, "fontSizePx"},
		{"reconnect max neg", func(s *Settings) { s.ReconnectMaxN = -1 }, "reconnectMaxN"},
		{"reconnect max big", func(s *Settings) { s.ReconnectMaxN = 51 }, "reconnectMaxN"},
		{"delay neg", func(s *Settings) { s.ReconnectDelayMs = -1 }, "reconnectDelayMs"},
		{"delay big", func(s *Settings) { s.ReconnectDelayMs = 60_001 }, "reconnectDelayMs"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := Default()
			tc.mutate(&s)
			err := s.Validate()
			if err == nil {
				t.Fatalf("Validate() = nil, want error containing %q", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("Validate() = %v, want substring %q", err, tc.wantSub)
			}
		})
	}
}

func TestValidateMultipleErrors(t *testing.T) {
	t.Parallel()
	s := Settings{Theme: "neon", FontSizePx: 4, ReconnectMaxN: 999, ReconnectDelayMs: -1}
	err := s.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want error")
	}
	for _, want := range []string{"theme", "fontSizePx", "reconnectMaxN", "reconnectDelayMs"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err.Error(), want)
		}
	}
}

func TestJSONRoundTrip(t *testing.T) {
	t.Parallel()
	in := Settings{
		Theme:            ThemeDark,
		FontFamily:       "Iosevka",
		FontSizePx:       16,
		ConfirmOnClose:   false,
		AutoReconnect:    true,
		ReconnectMaxN:    7,
		ReconnectDelayMs: 1500,
		TelemetryEnabled: true,
		UpdatedAt:        time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var out Settings
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out != in {
		t.Errorf("round trip mismatch:\n got = %+v\nwant = %+v", out, in)
	}
	// JSON keys are camelCase.
	for _, key := range []string{
		"\"theme\"", "\"fontFamily\"", "\"fontSizePx\"", "\"confirmOnClose\"",
		"\"autoReconnect\"", "\"reconnectMaxN\"", "\"reconnectDelayMs\"",
		"\"telemetryEnabled\"", "\"updatedAt\"",
	} {
		if !strings.Contains(string(data), key) {
			t.Errorf("encoded JSON missing key %s: %s", key, string(data))
		}
	}
}
