// Package http implements the built-in HTTP / HTTPS launcher protocol plugin
// for goremote.
//
// HTTP "sessions" are not interactive byte streams; they are URL launches.
// The plugin opens the configured URL in the user's default browser (or a
// caller-provided binary) and tracks a logical session whose lifecycle is
// driven by the host calling Close. Optionally, a single health-check
// HEAD/GET probe can be performed at Open time and reported on the session's
// stdout sink.
package http

import "github.com/darkace1998/GoRemote/sdk/plugin"

// Manifest is the static manifest published by the built-in HTTP/HTTPS
// launcher plugin. Hosts validate it via [plugin.Manifest.Validate] before
// registering the module.
var Manifest = plugin.Manifest{
	ID:          "io.goremote.protocol.http",
	Name:        "HTTP/HTTPS launcher",
	Description: "Launches HTTP or HTTPS URLs in the user's preferred external browser, with an optional health-check probe.",
	Kind:        plugin.KindProtocol,
	Version:     "1.0.0",
	APIVersion:  plugin.CurrentAPIVersion,
	Status:      plugin.StatusReady,
	Publisher:   "goremote",
	License:     "MIT",
	Capabilities: []plugin.Capability{
		plugin.CapProcessSpawn,
		plugin.CapExternalLauncher,
	},
}
