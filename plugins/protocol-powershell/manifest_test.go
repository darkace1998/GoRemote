package powershell

import (
	"testing"

	"github.com/darkace1998/GoRemote/sdk/protocol"
)

var _ protocol.Module = (*Module)(nil)

func TestManifestValidate(t *testing.T) {
	if err := Manifest.Validate(); err != nil {
		t.Fatalf("Manifest.Validate() returned error: %v", err)
	}
}
