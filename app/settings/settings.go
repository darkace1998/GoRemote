// Package settings defines the user-facing application settings model and
// a simple file-backed Store.
//
// Settings are intentionally a small, stable surface used by the UI. They
// are persisted as a single JSON document. Validation lives on the model
// itself so callers can validate without a live store.
package settings

import (
	"errors"
	"fmt"
	"time"
)

// Theme values.
const (
	ThemeSystem = "system"
	ThemeLight  = "light"
	ThemeDark   = "dark"
)

// Log level values.
const (
	LogLevelTrace = "trace"
	LogLevelDebug = "debug"
	LogLevelInfo  = "info"
	LogLevelWarn  = "warn"
	LogLevelError = "error"
)

// Limits (inclusive).
const (
	MinFontSizePx       = 8
	MaxFontSizePx       = 72
	MinReconnectMaxN    = 0
	MaxReconnectMaxN    = 50
	MinReconnectDelayMs = 0
	MaxReconnectDelayMs = 60_000
)

// Settings is the full user-configurable settings document. All fields are
// JSON-serialisable so the value can cross the UI bridge unchanged.
type Settings struct {
	Theme            string    `json:"theme"`
	FontFamily       string    `json:"fontFamily"`
	FontSizePx       int       `json:"fontSizePx"`
	ConfirmOnClose   bool      `json:"confirmOnClose"`
	AutoReconnect    bool      `json:"autoReconnect"`
	ReconnectMaxN    int       `json:"reconnectMaxN"`
	ReconnectDelayMs int       `json:"reconnectDelayMs"`
	TelemetryEnabled bool      `json:"telemetryEnabled"`
	LogLevel         string    `json:"logLevel"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

// Default returns the baseline settings for a fresh install.
func Default() Settings {
	return Settings{
		Theme:            ThemeSystem,
		FontFamily:       "",
		FontSizePx:       13,
		ConfirmOnClose:   true,
		AutoReconnect:    false,
		ReconnectMaxN:    3,
		ReconnectDelayMs: 2000,
		TelemetryEnabled: false,
		LogLevel:         LogLevelInfo,
	}
}

// Validate returns nil if the settings are well-formed, or a non-nil error
// describing the first invalid field. Errors are joined when multiple
// fields are invalid so the UI can surface them all at once.
func (s *Settings) Validate() error {
	var errs []error
	switch s.Theme {
	case ThemeSystem, ThemeLight, ThemeDark:
	default:
		errs = append(errs, fmt.Errorf("invalid theme %q: want one of %s|%s|%s",
			s.Theme, ThemeSystem, ThemeLight, ThemeDark))
	}
	if s.FontSizePx < MinFontSizePx || s.FontSizePx > MaxFontSizePx {
		errs = append(errs, fmt.Errorf("fontSizePx %d out of range [%d,%d]",
			s.FontSizePx, MinFontSizePx, MaxFontSizePx))
	}
	if s.ReconnectMaxN < MinReconnectMaxN || s.ReconnectMaxN > MaxReconnectMaxN {
		errs = append(errs, fmt.Errorf("reconnectMaxN %d out of range [%d,%d]",
			s.ReconnectMaxN, MinReconnectMaxN, MaxReconnectMaxN))
	}
	if s.ReconnectDelayMs < MinReconnectDelayMs || s.ReconnectDelayMs > MaxReconnectDelayMs {
		errs = append(errs, fmt.Errorf("reconnectDelayMs %d out of range [%d,%d]",
			s.ReconnectDelayMs, MinReconnectDelayMs, MaxReconnectDelayMs))
	}
	switch s.LogLevel {
	case LogLevelTrace, LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError:
	default:
		errs = append(errs, fmt.Errorf("invalid logLevel %q: want one of %s|%s|%s|%s|%s",
			s.LogLevel, LogLevelTrace, LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError))
	}
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}
