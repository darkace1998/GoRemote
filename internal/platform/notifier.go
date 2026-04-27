package platform

import (
	"fmt"
	"os"
)

// NewNotifier returns the default Notifier. If no OS-level notifier
// backend is available it falls back to writing a formatted line to
// stderr so that messages are never silently dropped.
func NewNotifier() Notifier {
	return notifierImpl{notify: osNotify, stderr: os.Stderr}
}

type notifierImpl struct {
	// notify delivers the notification via an OS-specific backend. It
	// returns ErrNotifierUnavailable if no backend is available.
	notify func(title, body string) error
	stderr interface {
		Write(p []byte) (int, error)
	}
}

// Notify implements Notifier.
func (n notifierImpl) Notify(title, body string) error {
	if n.notify != nil {
		if err := n.notify(title, body); err == nil {
			return nil
		}
	}
	// Fallback: write to stderr so the message is at least logged.
	_, _ = fmt.Fprintf(n.stderr, "[notify] %s: %s\n", title, body)
	return nil
}
