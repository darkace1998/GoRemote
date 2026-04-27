package plugin

import "testing"

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
