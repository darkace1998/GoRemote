package platform

import (
	"errors"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestKeychainMock(t *testing.T) {
	keyring.MockInit()
	kc := NewKeychain()

	if _, err := kc.Get("svc", "missing"); !errors.Is(err, ErrKeychainNotFound) {
		t.Fatalf("Get missing: expected ErrKeychainNotFound, got %v", err)
	}

	if err := kc.Set("svc", "alice", "s3cret"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := kc.Get("svc", "alice")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "s3cret" {
		t.Fatalf("Get returned %q want %q", got, "s3cret")
	}

	if err := kc.Set("svc", "alice", "new"); err != nil {
		t.Fatalf("Set overwrite: %v", err)
	}
	got, _ = kc.Get("svc", "alice")
	if got != "new" {
		t.Fatalf("overwrite: got %q want %q", got, "new")
	}

	if err := kc.Delete("svc", "alice"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := kc.Get("svc", "alice"); !errors.Is(err, ErrKeychainNotFound) {
		t.Fatalf("after Delete: expected ErrKeychainNotFound, got %v", err)
	}
}
