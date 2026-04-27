// Package vnc implements the built-in VNC protocol plugin.
//
// The plugin does not embed an RFB client; instead it discovers a native VNC
// viewer (TigerVNC, TightVNC, RealVNC, Remmina, macOS' open(1), ...) on the
// host system and launches it as an external process. Authentication can
// optionally be relayed to the viewer via stdin or a temporary VNC password
// file (mode 0600), depending on the password_via setting.
package vnc

import "github.com/goremote/goremote/sdk/plugin"

// Manifest is the static manifest published by the built-in VNC protocol
// plugin. Hosts validate it via [plugin.Manifest.Validate] before
// registering the module.
var Manifest = plugin.Manifest{
	ID:          "io.goremote.protocol.vnc",
	Name:        "VNC",
	Description: "Launches the system's native VNC viewer (vncviewer/tigervnc/tvnviewer/Remmina/open) as an external process.",
	Kind:        plugin.KindProtocol,
	Version:     "1.0.0",
	APIVersion:  plugin.CurrentAPIVersion,
	Capabilities: []plugin.Capability{
		plugin.CapOSExec,
	},
	Status:    plugin.StatusReady,
	Publisher: "goremote",
	License:   "MIT",
}
