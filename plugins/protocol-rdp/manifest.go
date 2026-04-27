// Package rdp implements the built-in RDP protocol plugin for goremote.
//
// Rather than ship a Go-native RDP stack, this plugin launches the operating
// system's native RDP client (xfreerdp / xfreerdp3 / remmina on Linux and
// macOS, mstsc on Windows) as an external process. The plugin renders in
// [protocol.RenderExternal] mode: the host displays a placeholder while the
// native client owns its own window.
//
// Binary discovery and process supervision are delegated to the shared
// helper at [github.com/goremote/goremote/internal/extlaunch].
package rdp

import "github.com/goremote/goremote/sdk/plugin"

// Manifest is the static manifest published by the built-in RDP protocol
// plugin. Hosts validate it via [plugin.Manifest.Validate] before
// registering the module.
var Manifest = plugin.Manifest{
	ID:          "io.goremote.protocol.rdp",
	Name:        "RDP",
	Description: "Microsoft Remote Desktop Protocol via the system's native RDP client (xfreerdp / mstsc).",
	Kind:        plugin.KindProtocol,
	Version:     "1.0.0",
	APIVersion:  plugin.CurrentAPIVersion,
	Capabilities: []plugin.Capability{
		plugin.CapOSExec,
	},
	// Platforms is intentionally empty: the plugin works on any GOOS where
	// at least one of its candidate binaries is installed; discovery
	// happens at Open time.
	Status:    plugin.StatusReady,
	Publisher: "goremote",
	License:   "MIT",
}
