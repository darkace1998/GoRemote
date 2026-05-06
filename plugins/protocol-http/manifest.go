// Package http implements the built-in HTTP / HTTPS protocol plugin for
// goremote.
//
// HTTP sessions are handled entirely in-process with Go's net/http package.
// No browser or OS launcher is spawned.
package http

import "github.com/darkace1998/GoRemote/sdk/plugin"

// Manifest is the static manifest published by the built-in HTTP/HTTPS plugin.
// Hosts validate it via [plugin.Manifest.Validate] before registering the
// module.
var Manifest = plugin.Manifest{
	ID:          "io.goremote.protocol.http",
	Name:        "HTTP/HTTPS",
	Description: "Fetches HTTP or HTTPS URLs with Go's in-process HTTP client.",
	Kind:        plugin.KindProtocol,
	Version:     "2.0.0",
	APIVersion:  plugin.CurrentAPIVersion,
	Status:      plugin.StatusExperimental,
	Publisher:   "goremote",
	License:     "MIT",
	Capabilities: []plugin.Capability{
		plugin.CapNetworkOutbound,
		plugin.CapTerminal,
	},
}
