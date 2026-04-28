package platform

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

// NewKeychain returns the default Keychain backed by
// github.com/zalando/go-keyring, which internally selects the OS
// keyring (Secret Service on Linux, Keychain on macOS, Credential
// Manager on Windows).
func NewKeychain() Keychain {
	return keychainImpl{}
}

type keychainImpl struct{}

// Get implements Keychain.
func (keychainImpl) Get(service, account string) (string, error) {
	secret, err := keyring.Get(service, account)
	if err != nil {
		return "", translateKeyringErr(err)
	}
	return secret, nil
}

// Set implements Keychain.
func (keychainImpl) Set(service, account, secret string) error {
	if err := keyring.Set(service, account, secret); err != nil {
		return translateKeyringErr(err)
	}
	return nil
}

// Delete implements Keychain.
func (keychainImpl) Delete(service, account string) error {
	if err := keyring.Delete(service, account); err != nil {
		return translateKeyringErr(err)
	}
	return nil
}

func translateKeyringErr(err error) error {
	switch {
	case errors.Is(err, keyring.ErrNotFound):
		return ErrKeychainNotFound
	case errors.Is(err, keyring.ErrSetDataTooBig):
		return fmt.Errorf("platform: keychain secret too large: %w", err)
	}
	// go-keyring surfaces backend-unavailable conditions (dbus not
	// running, no Secret Service provider, etc.) as generic errors;
	// treat anything else as a transport/availability failure.
	return fmt.Errorf("%w: %w", ErrKeychainUnavailable, err)
}
