// Package mosh implements the MOSH (Mobile Shell) protocol plugin for goremote.
// MOSH uses SSH for initial channel negotiation then switches to an encrypted
// UDP datagram stream for the terminal session.  goremote launches the system
// `mosh` binary as an external process (RenderExternal), consistent with how
// graphical protocols like RDP and VNC are handled.
package mosh

import "github.com/darkace1998/GoRemote/sdk/plugin"

// Manifest is the static manifest published by the built-in MOSH protocol
// plugin. Hosts validate it via [plugin.Manifest.Validate] before
// registering the module.
var Manifest = plugin.Manifest{
	ID:          "io.goremote.protocol.mosh",
	Name:        "MOSH",
	Description: "Mobile Shell: SSH-negotiated, UDP-based terminal with local echo and intelligent roaming.",
	Kind:        plugin.KindProtocol,
	Version:     "0.1.0",
	APIVersion:  plugin.CurrentAPIVersion,
	Capabilities: []plugin.Capability{
		plugin.CapNetworkOutbound,
		plugin.CapProcessSpawn,
		plugin.CapOSExec,
		plugin.CapExternalLauncher,
	},
	Platforms: []string{"linux", "darwin"},
	Status:    plugin.StatusReady,
	Publisher: "goremote",
	License:   "Apache-2.0",
}
