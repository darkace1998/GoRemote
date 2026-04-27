// Package bitwarden implements a credential.Provider backed by the
// official Bitwarden command-line client (`bw`). The provider shells out
// to the binary for every operation; no Bitwarden secrets are stored on
// disk by goremote — at most a session token is held in memory.
package bitwarden

import (
	"github.com/goremote/goremote/sdk/credential"
	"github.com/goremote/goremote/sdk/plugin"
)

// ManifestID is the reverse-DNS identifier of this provider.
const ManifestID = "io.goremote.credential.bitwarden"

// PluginVersion is the semver of this provider implementation.
const PluginVersion = "1.0.0"

// Manifest returns the static plugin manifest describing this provider.
func Manifest() plugin.Manifest {
	return plugin.Manifest{
		ID:           ManifestID,
		Name:         "Bitwarden",
		Description:  "Resolves credentials from a Bitwarden vault via the Bitwarden CLI (`bw`).",
		Kind:         plugin.KindCredential,
		Version:      PluginVersion,
		APIVersion:   credential.CurrentAPIVersion,
		Capabilities: []plugin.Capability{plugin.CapProcessSpawn},
		Status:       plugin.StatusReady,
		Publisher:    "goremote",
		License:      "Apache-2.0",
	}
}

// ProviderCapabilities returns the credential.Capabilities advertised by
// this provider. Bitwarden is treated as a read-only source of truth.
func ProviderCapabilities() credential.Capabilities {
	return credential.Capabilities{
		Lookup:  true,
		Refresh: true,
		Write:   false,
		Unlock:  true,
		SupportedKinds: []credential.Kind{
			credential.KindPassword,
		},
	}
}
