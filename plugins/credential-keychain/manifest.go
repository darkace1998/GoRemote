// Package credentialkeychain implements a credential provider backed by
// the host operating system's native secret store (macOS Keychain, Windows
// Credential Manager, Linux Secret Service). Secrets are stored at rest in
// the OS keychain; a non-sensitive index file records which References
// exist so that List() does not require enumerating the keychain (an
// operation most backends do not support portably).
package credentialkeychain

import (
	"github.com/goremote/goremote/sdk/credential"
	"github.com/goremote/goremote/sdk/plugin"
)

// ManifestID is the reverse-DNS identifier of this provider.
const ManifestID = "io.goremote.credential.keychain"

// PluginVersion is the semver of this provider implementation.
const PluginVersion = "0.1.0"

// KeychainService is the service name used for all entries written to the
// OS keychain by this provider. The paired account is the Reference
// EntryID.
const KeychainService = "goremote"

// Manifest returns the static plugin manifest describing this provider.
func Manifest() plugin.Manifest {
	return plugin.Manifest{
		ID:          ManifestID,
		Name:        "OS Keychain",
		Description: "Stores credentials in the host operating system's native keychain (macOS Keychain, Windows Credential Manager, Linux Secret Service).",
		Kind:        plugin.KindCredential,
		Version:     PluginVersion,
		APIVersion:  credential.CurrentAPIVersion,
		Capabilities: []plugin.Capability{
			plugin.CapKeychainRead,
			plugin.CapKeychainWrite,
			plugin.CapFilesystemRead,
			plugin.CapFilesystemWrite,
		},
		Status:    plugin.StatusReady,
		Publisher: "goremote",
		License:   "Apache-2.0",
	}
}

// ProviderCapabilities returns the credential.Capabilities advertised by
// this provider.
//
// Unlock is false because the OS keychain backends apply their own
// authentication (login-keychain prompts, Windows DPAPI, kwallet/gnome
// prompts) transparently at Get/Set time.
func ProviderCapabilities() credential.Capabilities {
	return credential.Capabilities{
		Lookup:  true,
		Refresh: false,
		Write:   true,
		Unlock:  false,
		SupportedKinds: []credential.Kind{
			credential.KindPassword,
			credential.KindPrivateKey,
			credential.KindOTP,
			credential.KindAPIKey,
		},
	}
}
