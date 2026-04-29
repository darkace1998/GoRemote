// Package serial implements the built-in Serial / COM-port protocol plugin
// for goremote.
//
// The plugin opens a local serial port (RS-232 / USB-CDC / virtual COM)
// at a user-configured baud rate and frame format, and pumps the byte
// stream straight to the host terminal — equivalent to PuTTY's "Serial"
// session type or the Windows-era mRemoteNG serial protocol.
package serial

import "github.com/darkace1998/GoRemote/sdk/plugin"

// Manifest is the static manifest published by the built-in Serial plugin.
// Hosts validate it via [plugin.Manifest.Validate] before registering the
// module.
var Manifest = plugin.Manifest{
	ID:          "io.goremote.protocol.serial",
	Name:        "Serial",
	Description: "Local serial / COM-port terminal sessions (PuTTY-style serial console).",
	Kind:        plugin.KindProtocol,
	Version:     "1.0.0",
	APIVersion:  plugin.CurrentAPIVersion,
	Capabilities: []plugin.Capability{
		plugin.CapTerminal,
	},
	Status:    plugin.StatusReady,
	Publisher: "goremote",
	License:   "MIT",
}
