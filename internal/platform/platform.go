// Package platform provides OS-specific abstractions for the goremote
// application (filesystem paths, secret storage, clipboard access and
// user notifications). All platform-dependent behavior is hidden behind
// the interfaces declared here so that higher layers stay portable.
package platform

import "errors"

// AppName is the application-specific directory name used when composing
// OS-standard configuration, data, cache and log locations.
const AppName = "goremote"

// Sentinel errors returned by platform implementations. Consumers should
// use errors.Is to test for these conditions.
var (
	// ErrKeychainUnavailable indicates the underlying OS keychain service
	// is not available on this system or cannot be reached.
	ErrKeychainUnavailable = errors.New("platform: keychain unavailable")

	// ErrKeychainNotFound indicates no secret exists for the given
	// (service, account) tuple.
	ErrKeychainNotFound = errors.New("platform: keychain entry not found")

	// ErrClipboardUnavailable indicates no clipboard backend is available
	// (e.g. no xclip/xsel/wl-copy on Linux).
	ErrClipboardUnavailable = errors.New("platform: clipboard unavailable")

	// ErrNotifierUnavailable indicates no user-notification backend is
	// available; callers may still log the message themselves.
	ErrNotifierUnavailable = errors.New("platform: notifier unavailable")
)

// Paths exposes OS-appropriate, application-specific filesystem
// locations. Implementations do not create the directories; callers are
// expected to MkdirAll on first use.
type Paths interface {
	// ConfigDir returns the directory in which user configuration is
	// stored (e.g. $XDG_CONFIG_HOME/goremote on Linux).
	ConfigDir() (string, error)
	// DataDir returns the directory for persistent application data
	// (connections, history, plugin state).
	DataDir() (string, error)
	// CacheDir returns the directory for disposable cached data.
	CacheDir() (string, error)
	// LogDir returns the directory in which log files should be written.
	LogDir() (string, error)
}

// Keychain abstracts the OS secret store. Implementations translate
// backend-specific errors into the sentinel errors declared above.
type Keychain interface {
	// Get returns the secret associated with (service, account).
	// It returns ErrKeychainNotFound if no such entry exists, or
	// ErrKeychainUnavailable if the backend cannot be reached.
	Get(service, account string) (string, error)
	// Set stores (or replaces) the secret associated with (service,
	// account).
	Set(service, account, secret string) error
	// Delete removes the entry for (service, account). Deleting a
	// non-existent entry returns ErrKeychainNotFound.
	Delete(service, account string) error
}

// Clipboard abstracts the system clipboard.
type Clipboard interface {
	// ReadText returns the current clipboard text. Returns
	// ErrClipboardUnavailable if no backend is available.
	ReadText() (string, error)
	// WriteText replaces the clipboard contents with s. Returns
	// ErrClipboardUnavailable if no backend is available.
	WriteText(s string) error
}

// Notifier abstracts desktop notification delivery.
type Notifier interface {
	// Notify displays a user-facing notification with the given title
	// and body. Implementations should degrade gracefully (e.g. fall
	// back to writing to stderr) when no backend is available.
	Notify(title, body string) error
}

// Provider bundles the four platform services. Consumers should depend
// on the narrower interfaces where possible.
type Provider interface {
	Paths
	Keychain
	Clipboard
	Notifier
}

// New returns the Provider appropriate for the current operating
// system. The returned value is safe for concurrent use.
func New() Provider {
	return &provider{
		Paths:     NewPaths(),
		Keychain:  NewKeychain(),
		Clipboard: NewClipboard(),
		Notifier:  NewNotifier(),
	}
}

type provider struct {
	Paths
	Keychain
	Clipboard
	Notifier
}
