// Package credentialfile implements a built-in credential provider that
// stores secrets in a single encrypted file on disk.
//
// The file format is versioned; v1 uses Argon2id + AES-256-GCM with a
// user-supplied passphrase. See format.go for the on-disk layout.
package credentialfile

import (
	"github.com/goremote/goremote/sdk/credential"
	"github.com/goremote/goremote/sdk/plugin"
)

// ManifestID is the reverse-DNS identifier of the built-in encrypted-file
// credential provider.
const ManifestID = "io.goremote.credential.file"

// PluginVersion is the semver of this provider implementation.
const PluginVersion = "0.1.0"

// Manifest returns the static plugin manifest describing this provider.
func Manifest() plugin.Manifest {
	return plugin.Manifest{
		ID:          ManifestID,
		Name:        "Encrypted File",
		Description: "Local credential store encrypted with Argon2id + AES-256-GCM.",
		Kind:        plugin.KindCredential,
		Version:     PluginVersion,
		APIVersion:  credential.CurrentAPIVersion,
		Capabilities: []plugin.Capability{
			plugin.CapFilesystemRead,
			plugin.CapFilesystemWrite,
		},
		Status:    plugin.StatusReady,
		Publisher: "goremote",
		License:   "Apache-2.0",
	}
}

// ProviderCapabilities returns the credential.Capabilities advertised by the
// encrypted-file provider.
func ProviderCapabilities() credential.Capabilities {
	return credential.Capabilities{
		Write:  true,
		Lookup: true,
		Unlock: true,
		SupportedKinds: []credential.Kind{
			credential.KindPassword,
			credential.KindPrivateKey,
		},
	}
}
