package bitwarden

import (
	"testing"

	"github.com/darkace1998/GoRemote/sdk/credential"
)

var _ credential.Provider = (*Provider)(nil)

func TestManifestValidate(t *testing.T) {
	m := Manifest()
	if err := m.Validate(); err != nil {
		t.Fatalf("Manifest.Validate() returned error: %v", err)
	}
}
