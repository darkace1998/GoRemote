// Package tn5250 implements the built-in TN5250 protocol plugin for
// goremote.
//
// Rather than ship a Go-native 5250 data-stream stack, this plugin launches
// the operating system's native TN5250 client (tn5250 / xt5250 on Unix,
// tn5250j on Windows) as an external process. The plugin renders in
// [protocol.RenderExternal] mode: the host displays a placeholder while the
// native client owns its own window/terminal.
//
// Binary discovery and process supervision are delegated to the shared
// helper at [github.com/goremote/goremote/internal/extlaunch].
package tn5250

import "github.com/goremote/goremote/sdk/plugin"

// Manifest is the static manifest published by the built-in TN5250 protocol
// plugin. Hosts validate it via [plugin.Manifest.Validate] before
// registering the module.
var Manifest = plugin.Manifest{
	ID:          "io.goremote.protocol.tn5250",
	Name:        "TN5250",
	Description: "IBM 5250 terminal emulation via the system's native TN5250 client (tn5250 / xt5250 / tn5250j) for AS/400 and IBM i hosts.",
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
