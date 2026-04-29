// Package onepassword implements a credential provider that resolves
// secrets from the user's 1Password vault by shelling out to the
// official 1Password CLI binary (`op`).
//
// Design notes:
//   - All `op` invocations are routed through a Runner interface so the
//     provider is fully testable without a real binary.
//   - The provider is read-mostly: Resolve / List are implemented, Put
//     and Delete return credential.ErrReadOnly. Write support could be
//     added later via `op item create / edit` but is intentionally out
//     of scope here to keep the trust surface small.
//   - Unlock pipes the user's master password to `op signin --raw` and
//     captures the returned session token. Subsequent commands are
//     invoked with `OP_SESSION_<account>=<token>` in their environment.
package onepassword

import (
	"github.com/darkace1998/GoRemote/sdk/credential"
	"github.com/darkace1998/GoRemote/sdk/plugin"
)

// ManifestID is the reverse-DNS identifier of this provider.
const ManifestID = "io.goremote.credential.1password"

// PluginVersion is the semver of this provider implementation.
const PluginVersion = "1.0.0"

// Manifest returns the static plugin manifest describing this provider.
func Manifest() plugin.Manifest {
	return plugin.Manifest{
		ID:          ManifestID,
		Name:        "1Password",
		Description: "Resolves credentials from the user's 1Password vault via the 1Password CLI (`op`).",
		Kind:        plugin.KindCredential,
		Version:     PluginVersion,
		APIVersion:  credential.CurrentAPIVersion,
		Capabilities: []plugin.Capability{
			plugin.CapOSExec,
		},
		Status:    plugin.StatusReady,
		Publisher: "goremote",
		License:   "Apache-2.0",
	}
}

// ProviderCapabilities returns the credential.Capabilities advertised by
// this provider.
func ProviderCapabilities() credential.Capabilities {
	return credential.Capabilities{
		Lookup:  true,
		Refresh: true,
		Write:   false,
		Unlock:  true,
		SupportedKinds: []credential.Kind{
			credential.KindPassword,
			credential.KindAPIKey,
			credential.KindOTP,
		},
	}
}
