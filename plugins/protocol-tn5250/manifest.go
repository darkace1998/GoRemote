// Package tn5250 implements the built-in TN5250 protocol plugin for goremote.
//
// Sessions are handled entirely in-process: the plugin dials the remote IBM i /
// AS/400 host over TCP using Go's standard net package and relays the 5250
// data stream. No external binary is required. Rendering uses
// [protocol.RenderTerminal].
package tn5250

import "github.com/darkace1998/GoRemote/sdk/plugin"

// Manifest is the static manifest published by the built-in TN5250 protocol
// plugin. Hosts validate it via [plugin.Manifest.Validate] before
// registering the module.
var Manifest = plugin.Manifest{
	ID:          "io.goremote.protocol.tn5250",
	Name:        "TN5250",
	Description: "IBM 5250 terminal emulation for AS/400 and IBM i hosts — Go-native in-process TCP session.",
	Kind:        plugin.KindProtocol,
	Version:     "2.0.0",
	APIVersion:  plugin.CurrentAPIVersion,
	Capabilities: []plugin.Capability{
		plugin.CapNetworkOutbound,
		plugin.CapTerminal,
	},
	Status:    plugin.StatusReady,
	Publisher: "goremote",
	License:   "MIT",
}

