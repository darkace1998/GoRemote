package plugin

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
)

// Policy governs how unsigned or unverified plugins are treated.
type Policy string

const (
	// PolicyPermissive allows unsigned plugins and plugins whose key is
	// not in the trusted set. This is the default for development builds.
	PolicyPermissive Policy = "permissive"

	// PolicyStrict rejects any plugin that does not carry a valid signature
	// from a key present in the TrustStore.
	PolicyStrict Policy = "strict"
)

// TrustStore holds a set of trusted Ed25519 public keys, keyed by an
// arbitrary label (e.g. publisher name). The zero value is a valid, empty
// store (no trusted keys).
type TrustStore struct {
	keys map[string]ed25519.PublicKey
}

// Add registers a public key under the given label. The label is purely
// informational; uniqueness is not enforced (a duplicate label overwrites
// the previous entry).
func (ts *TrustStore) Add(label string, pub ed25519.PublicKey) {
	if ts.keys == nil {
		ts.keys = make(map[string]ed25519.PublicKey)
	}
	ts.keys[label] = pub
}

// Verifier checks plugin manifests against a TrustStore and a Policy.
type Verifier struct {
	store  *TrustStore
	policy Policy
}

// NewVerifier returns a Verifier with the given store and policy. If store
// is nil an empty store is used.
func NewVerifier(store *TrustStore, policy Policy) *Verifier {
	if store == nil {
		store = &TrustStore{}
	}
	return &Verifier{store: store, policy: policy}
}

// Verify checks m's signature (if present) against the trust store and
// returns an error when the policy rejects the manifest.
//
// With PolicyPermissive:
//   - A manifest with no signature is accepted (TrustCommunity assigned).
//   - A manifest with a valid signature from a trusted key is accepted
//     (TrustVerified assigned).
//   - A manifest with an invalid or unrecognised signature is rejected.
//
// With PolicyStrict:
//   - A manifest with no signature is rejected.
//   - A manifest with a valid signature from a trusted key is accepted
//     (TrustVerified assigned).
//   - A manifest with an invalid or unrecognised signature is rejected.
//
// Verify sets m.Trust before returning.
func (v *Verifier) Verify(m *Manifest) error {
	if m == nil {
		return errors.New("plugin: Verify called with nil manifest")
	}

	if m.SignatureB64 == "" {
		if v.policy == PolicyStrict {
			return fmt.Errorf("plugin: manifest %q has no signature and policy is strict", m.ID)
		}
		m.Trust = TrustCommunity
		return nil
	}

	sig, err := base64.StdEncoding.DecodeString(m.SignatureB64)
	if err != nil {
		return fmt.Errorf("plugin: manifest %q: invalid signature encoding: %w", m.ID, err)
	}

	body, err := manifestBody(m)
	if err != nil {
		return fmt.Errorf("plugin: manifest %q: serialise for verification: %w", m.ID, err)
	}

	for _, pub := range v.store.keys {
		if ed25519.Verify(pub, body, sig) {
			m.Trust = TrustVerified
			return nil
		}
	}

	return fmt.Errorf("plugin: manifest %q: signature not verified by any trusted key", m.ID)
}

// manifestBody serialises m to canonical JSON with SignatureB64 and Trust cleared.
// This is the byte slice that must be signed.
func manifestBody(m *Manifest) ([]byte, error) {
	// Shallow copy so we don't mutate the caller's manifest.
	copy := *m
	copy.SignatureB64 = ""
	copy.Trust = ""
	return json.Marshal(copy)
}

// Sign creates an Ed25519 signature over m's canonical body using the
// supplied private key and encodes it in SignatureB64. Sign mutates m.
// Intended for use in signing tools, not in the host runtime.
func Sign(m *Manifest, priv ed25519.PrivateKey) error {
	if m == nil {
		return errors.New("plugin: Sign called with nil manifest")
	}
	body, err := manifestBody(m)
	if err != nil {
		return fmt.Errorf("plugin: Sign: serialise manifest: %w", err)
	}
	sig := ed25519.Sign(priv, body)
	m.SignatureB64 = base64.StdEncoding.EncodeToString(sig)
	return nil
}
