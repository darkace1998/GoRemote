// Package vnc implements the built-in VNC protocol plugin for goremote.
//
// Sessions are handled entirely in-process: the plugin dials the remote host
// over TCP using Go's standard net package. No external binary is required.
// Full RFB handling is still experimental.
package vnc

import "github.com/darkace1998/GoRemote/sdk/plugin"

// Manifest is the static manifest published by the built-in VNC protocol
// plugin. Hosts validate it via [plugin.Manifest.Validate] before
// registering the module.
var Manifest = plugin.Manifest{
	ID:          "io.goremote.protocol.vnc",
	Name:        "VNC",
	Description: "Virtual Network Computing — experimental Go-native in-process TCP/RFB session.",
	Kind:        plugin.KindProtocol,
	Version:     "2.0.0",
	APIVersion:  plugin.CurrentAPIVersion,
	Capabilities: []plugin.Capability{
		plugin.CapNetworkOutbound,
		plugin.CapGraphical,
	},
	Status:    plugin.StatusExperimental,
	Publisher: "goremote",
	License:   "MIT",
}
