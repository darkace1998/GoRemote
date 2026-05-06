// Package mosh implements the MOSH (Mobile Shell) protocol plugin for goremote.
//
// Sessions are handled in-process: the plugin uses golang.org/x/crypto/ssh to
// dial the SSH port, runs "mosh-server new" on the remote, and parses the
// "MOSH CONNECT <port> <key>" output. The UDP transport is still experimental.
// No local external binary is required. Rendering uses [protocol.RenderTerminal].
package mosh

import "github.com/darkace1998/GoRemote/sdk/plugin"

// Manifest is the static manifest published by the built-in MOSH protocol
// plugin. Hosts validate it via [plugin.Manifest.Validate] before
// registering the module.
var Manifest = plugin.Manifest{
	ID:          "io.goremote.protocol.mosh",
	Name:        "MOSH",
	Description: "Mobile Shell: experimental SSH-bootstrapped Go-native session; MOSH UDP transport is not complete yet.",
	Kind:        plugin.KindProtocol,
	Version:     "2.0.0",
	APIVersion:  plugin.CurrentAPIVersion,
	Capabilities: []plugin.Capability{
		plugin.CapNetworkOutbound,
		plugin.CapTerminal,
	},
	Status:    plugin.StatusExperimental,
	Publisher: "goremote",
	License:   "Apache-2.0",
}
