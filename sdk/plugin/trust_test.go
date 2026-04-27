package plugin

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"
)

func newTestManifest(id string) *Manifest {
	return &Manifest{
		ID:         id,
		Name:       "Test Plugin",
		Kind:       KindProtocol,
		Version:    "1.0.0",
		APIVersion: "1.0.0",
	}
}

func TestVerify_UnsignedPermissive(t *testing.T) {
	v := NewVerifier(nil, PolicyPermissive)
	m := newTestManifest("io.test.unsigned")
	if err := v.Verify(m); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	if m.Trust != TrustCommunity {
		t.Fatalf("expected TrustCommunity, got %q", m.Trust)
	}
}

func TestVerify_UnsignedStrict(t *testing.T) {
	v := NewVerifier(nil, PolicyStrict)
	m := newTestManifest("io.test.unsigned")
	if err := v.Verify(m); err == nil {
		t.Fatal("expected error for unsigned manifest under strict policy")
	}
}

func TestVerify_ValidSignaturePermissive(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	ts := &TrustStore{}
	ts.Add("publisher", pub)
	v := NewVerifier(ts, PolicyPermissive)

	m := newTestManifest("io.test.signed")
	if err := Sign(m, priv); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if err := v.Verify(m); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	if m.Trust != TrustVerified {
		t.Fatalf("expected TrustVerified, got %q", m.Trust)
	}
}

func TestVerify_ValidSignatureStrict(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	ts := &TrustStore{}
	ts.Add("publisher", pub)
	v := NewVerifier(ts, PolicyStrict)

	m := newTestManifest("io.test.signed")
	if err := Sign(m, priv); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if err := v.Verify(m); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	if m.Trust != TrustVerified {
		t.Fatalf("expected TrustVerified, got %q", m.Trust)
	}
}

func TestVerify_InvalidBase64(t *testing.T) {
	for _, policy := range []Policy{PolicyPermissive, PolicyStrict} {
		v := NewVerifier(nil, policy)
		m := newTestManifest("io.test.badb64")
		m.SignatureB64 = "!!!not-valid-base64!!!"
		if err := v.Verify(m); err == nil {
			t.Errorf("policy %q: expected error for invalid base64", policy)
		}
	}
}

func TestVerify_TamperedManifest(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	ts := &TrustStore{}
	ts.Add("publisher", pub)
	v := NewVerifier(ts, PolicyPermissive)

	m := newTestManifest("io.test.tampered")
	if err := Sign(m, priv); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	// Tamper with a field after signing.
	m.Name = "EVIL NAME"
	if err := v.Verify(m); err == nil {
		t.Fatal("expected error for tampered manifest")
	}
}

func TestVerify_UnknownKey(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	// Store a different public key (not the one used to sign).
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	ts := &TrustStore{}
	ts.Add("other", otherPub)
	v := NewVerifier(ts, PolicyPermissive)

	m := newTestManifest("io.test.unknownkey")
	if err := Sign(m, priv); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if err := v.Verify(m); err == nil {
		t.Fatal("expected error: signing key not in trust store")
	}
}

func TestVerify_MultipleKeys(t *testing.T) {
	pub1, _, _ := ed25519.GenerateKey(rand.Reader)
	pub2, priv2, _ := ed25519.GenerateKey(rand.Reader)
	ts := &TrustStore{}
	ts.Add("publisher1", pub1)
	ts.Add("publisher2", pub2)
	v := NewVerifier(ts, PolicyStrict)

	m := newTestManifest("io.test.multikey")
	if err := Sign(m, priv2); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if err := v.Verify(m); err != nil {
		t.Fatalf("expected ok with second key, got %v", err)
	}
	if m.Trust != TrustVerified {
		t.Fatalf("expected TrustVerified, got %q", m.Trust)
	}
}

func TestVerify_NilManifest(t *testing.T) {
	v := NewVerifier(nil, PolicyPermissive)
	if err := v.Verify(nil); err == nil {
		t.Fatal("expected error for nil manifest")
	}
}

func TestSign_RoundTrip(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	ts := &TrustStore{}
	ts.Add("publisher", pub)
	v := NewVerifier(ts, PolicyStrict)

	m := newTestManifest("io.test.roundtrip")
	if err := Sign(m, priv); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if m.SignatureB64 == "" {
		t.Fatal("expected SignatureB64 to be set after Sign")
	}
	if err := v.Verify(m); err != nil {
		t.Fatalf("Verify after Sign: %v", err)
	}
	if m.Trust != TrustVerified {
		t.Fatalf("expected TrustVerified, got %q", m.Trust)
	}
}

func TestManifestBody_SignatureExcluded(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	m := newTestManifest("io.test.bodycheck")
	if err := Sign(m, priv); err != nil {
		t.Fatalf("Sign: %v", err)
	}

	body, err := manifestBody(m)
	if err != nil {
		t.Fatalf("manifestBody: %v", err)
	}

	// The body JSON must not contain the "signature" key.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if _, ok := raw["signature"]; ok {
		t.Fatal("body must not contain the signature field")
	}

	// The original manifest's SignatureB64 must survive the call.
	if m.SignatureB64 == "" {
		t.Fatal("Sign must not have cleared SignatureB64")
	}

	// Double-check raw bytes don't contain the base64 value.
	if strings.Contains(string(body), m.SignatureB64) {
		t.Fatal("body bytes must not contain the signature value")
	}
}
