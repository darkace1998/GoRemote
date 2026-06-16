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
			name:       "ssh with password and private key prefers password",
			protocolID: "ssh",
			material:   protocol.CredentialMaterial{Password: "secret", PrivateKey: []byte("keydata")},
			want:       protocol.AuthPassword,
		},
		{
			name:       "protocol id starting with dot resolves to ssh",
			protocolID: ".ssh",
			material:   protocol.CredentialMaterial{},
			want:       protocol.AuthAgent,
		},
		{
			name:       "protocol id with multiple dots resolves to mosh",
			protocolID: "a.b.c.mosh",
			material:   protocol.CredentialMaterial{},
			want:       protocol.AuthAgent,
		},
		{
			name:       "plugin.v1.ssh empty material extracts ssh and defaults to agent",
			protocolID: "plugin.v1.ssh",
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
			name:       "rdp with password and private key prefers password",
			protocolID: "rdp",
			material:   protocol.CredentialMaterial{Password: "secret", PrivateKey: []byte("keydata")},
			want:       protocol.AuthPassword,
		},
		{
			name:       "sftp with private key defaults to none",
			protocolID: "sftp",
			material:   protocol.CredentialMaterial{PrivateKey: []byte("keydata")},
			want:       protocol.AuthNone,
		},
		{
			name:       "protocol id ending in dot extracts empty string and defaults to none",
			protocolID: "ssh.",
			material:   protocol.CredentialMaterial{},
			want:       protocol.AuthNone,
		},
		{
			name:       "empty protocol id",
			protocolID: "",
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
