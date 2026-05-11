package plugin

import (
	"strings"
	"testing"
)

func TestManifestValidate(t *testing.T) {
	cases := []struct {
		name string
		m    *Manifest
		ok   bool
	}{
		{"nil", nil, false},
		{"missing id", &Manifest{Name: "n", Kind: KindProtocol, Version: "1", APIVersion: "1"}, false},
		{"missing name", &Manifest{ID: "x", Kind: KindProtocol, Version: "1", APIVersion: "1"}, false},
		{"bad kind", &Manifest{ID: "x", Name: "n", Kind: "bogus", Version: "1", APIVersion: "1"}, false},
		{"missing version", &Manifest{ID: "x", Name: "n", Kind: KindProtocol, APIVersion: "1"}, false},
		{"missing api version", &Manifest{ID: "x", Name: "n", Kind: KindProtocol, Version: "1"}, false},
		{"empty cap", &Manifest{ID: "x", Name: "n", Kind: KindProtocol, Version: "1", APIVersion: "1", Capabilities: []Capability{""}}, false},
		{"valid", &Manifest{ID: "x", Name: "n", Kind: KindProtocol, Version: "1", APIVersion: "1", Capabilities: []Capability{CapNetworkOutbound}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.m.Validate()
			if tc.ok && err != nil {
				t.Fatalf("expected ok, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("expected error, got nil")
			}
		})
	}
}

func TestHasCapability(t *testing.T) {
	m := &Manifest{Capabilities: []Capability{CapNetworkOutbound, CapTerminal}}
	if !m.HasCapability(CapTerminal) {
		t.Fatal("expected terminal cap")
	}
	if m.HasCapability(CapClipboardRead) {
		t.Fatal("did not expect clipboard cap")
	}
}

// TestValidate_ProtocolForbiddenCaps verifies that KindProtocol manifests are
// rejected when they declare CapProcessSpawn or CapExternalLauncher, and that
// KindCredential manifests with the same capabilities are accepted.
func TestValidate_ProtocolForbiddenCaps(t *testing.T) {
	base := func(kind Kind) *Manifest {
		return &Manifest{
			ID:         "x",
			Name:       "n",
			Kind:       kind,
			Version:    "1.0.0",
			APIVersion: "1.0.0",
		}
	}

	forbiddenCaps := []Capability{CapProcessSpawn, CapExternalLauncher}

	for _, cap := range forbiddenCaps {
		cap := cap
		t.Run("protocol_rejects_"+string(cap), func(t *testing.T) {
			m := base(KindProtocol)
			m.Capabilities = []Capability{cap}
			err := m.Validate()
			if err == nil {
				t.Fatalf("expected Validate to reject KindProtocol with cap %q, got nil", cap)
			}
			if !strings.Contains(err.Error(), string(cap)) {
				t.Errorf("error message should mention the forbidden capability; got: %v", err)
			}
		})

		t.Run("credential_allows_"+string(cap), func(t *testing.T) {
			m := base(KindCredential)
			m.Capabilities = []Capability{cap}
			if err := m.Validate(); err != nil {
				t.Fatalf("KindCredential with cap %q should be valid, got: %v", cap, err)
			}
		})
	}
}
