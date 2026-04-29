// Package ssh implements the built-in SSH protocol plugin for goremote.
//
// The plugin exposes a [protocol.Module] that opens interactive shell sessions
// over SSH using golang.org/x/crypto/ssh. It supports password, public-key,
// SSH agent, and keyboard-interactive authentication, honors strict host-key
// checking policies via a known_hosts file, and forwards terminal resize
// events to the remote session.
//
// The plugin is designed as a "core" (in-process) plugin. The contract with
// the host (manifest, settings schema, capabilities) is identical to what an
// out-of-process SSH plugin would publish, so it can be split out in the
// future without any API churn.
package ssh

import "github.com/darkace1998/GoRemote/sdk/plugin"

// Manifest is the static manifest published by the built-in SSH protocol
// plugin. Hosts validate and register it via [plugin.Manifest.Validate].
var Manifest = plugin.Manifest{
	ID:          "io.goremote.protocol.ssh",
	Name:        "SSH",
	Description: "Interactive SSH terminal sessions with password, public-key, agent, and keyboard-interactive authentication.",
	Kind:        plugin.KindProtocol,
	Version:     "1.0.0",
	APIVersion:  plugin.CurrentAPIVersion,
	Capabilities: []plugin.Capability{
		plugin.CapNetworkOutbound,
		plugin.CapTerminal,
		plugin.CapKeychainRead,
	},
	Status:    plugin.StatusReady,
	Publisher: "goremote",
	License:   "MIT",
}
