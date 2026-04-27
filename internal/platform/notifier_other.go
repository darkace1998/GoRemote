//go:build !linux && !darwin && !windows

package platform

func osNotify(title, body string) error {
	return ErrNotifierUnavailable
}
