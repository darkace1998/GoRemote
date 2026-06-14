package app

import (
	"testing"

	"github.com/darkace1998/GoRemote/sdk/protocol"
)

func TestDefaultAuthMethod(t *testing.T) {
	tests := []struct {
		name       string
		protocolID string
		material   protocol.CredentialMaterial
		want       protocol.AuthMethod
	}{
		{
			name:       "ssh with password prefers password",
			protocolID: "ssh",
			material:   protocol.CredentialMaterial{Password: "secret"},
			want:       protocol.AuthPassword,
		},
		{
			name:       "ssh with private key prefers publickey",
			protocolID: "ssh",
			material:   protocol.CredentialMaterial{PrivateKey: []byte("keydata")},
			want:       protocol.AuthPublicKey,
		},
		{
			name:       "ssh empty material defaults to agent",
			protocolID: "ssh",
			material:   protocol.CredentialMaterial{},
			want:       protocol.AuthAgent,
		},
		{
			name:       "mosh with full protocol id and password",
			protocolID: "io.goremote.protocol.mosh",
			material:   protocol.CredentialMaterial{Password: "secret"},
			want:       protocol.AuthPassword,
		},
		{
			name:       "mosh with full protocol id and private key",
			protocolID: "io.goremote.protocol.mosh",
			material:   protocol.CredentialMaterial{PrivateKey: []byte("keydata")},
			want:       protocol.AuthPublicKey,
		},
		{
			name:       "mosh with full protocol id empty material",
			protocolID: "io.goremote.protocol.mosh",
			material:   protocol.CredentialMaterial{},
			want:       protocol.AuthAgent,
		},
		{
			name:       "rdp with password",
			protocolID: "rdp",
			material:   protocol.CredentialMaterial{Password: "secret"},
			want:       protocol.AuthPassword,
		},
		{
			name:       "vnc with empty material",
			protocolID: "vnc",
			material:   protocol.CredentialMaterial{},
			want:       protocol.AuthNone,
		},
		{
			name:       "unknown with password",
			protocolID: "unknown",
			material:   protocol.CredentialMaterial{Password: "pw"},
			want:       protocol.AuthPassword,
		},
		{
			name:       "unknown empty",
			protocolID: "unknown",
			material:   protocol.CredentialMaterial{},
			want:       protocol.AuthNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := defaultAuthMethod(tt.protocolID, tt.material); got != tt.want {
				t.Errorf("defaultAuthMethod() = %v, want %v", got, tt.want)
			}
		})
	}
}
