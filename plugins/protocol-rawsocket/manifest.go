// Package rawsocket implements the built-in Raw Socket protocol plugin for
// goremote.
//
// The plugin exposes a [protocol.Module] that opens a plain TCP connection
// to a remote host and wires the byte stream directly to the host-supplied
// stdin/stdout pipes. No protocol negotiation or framing is performed, so it
// is useful for banner-grabbing, speaking line protocols manually, or
// interacting with services that would otherwise require a protocol-specific
// client (e.g. "telnet" without Telnet option negotiation).
package rawsocket

import "github.com/darkace1998/GoRemote/sdk/plugin"

// Manifest is the static manifest published by the built-in Raw Socket
// protocol plugin. Hosts validate it via [plugin.Manifest.Validate] before
// registering the module.
var Manifest = plugin.Manifest{
	ID:          "io.goremote.protocol.rawsocket",
	Name:        "Raw Socket",
	Description: "Raw TCP connection useful for banner-grabbing, telnet-without-negotiation, and custom line protocols.",
	Kind:        plugin.KindProtocol,
	Version:     "1.0.0",
	APIVersion:  plugin.CurrentAPIVersion,
	Capabilities: []plugin.Capability{
		plugin.CapNetworkOutbound,
		plugin.CapTerminal,
	},
	Status:    plugin.StatusReady,
	Publisher: "goremote",
	License:   "MIT",
}
