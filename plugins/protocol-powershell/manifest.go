// Package powershell declares the planned built-in PowerShell remoting
// protocol plugin for goremote.
//
// The previous local-process PTY launcher has been removed because protocols
// must be Go-native in-process implementations. This package remains as a
// planned remoting module until a Go-native PSRP/WinRM transport lands.
package powershell

import "github.com/darkace1998/GoRemote/sdk/plugin"

// Manifest is the static manifest published by the built-in PowerShell
// protocol plugin. Hosts validate it via [plugin.Manifest.Validate] before
// registering the module.
var Manifest = plugin.Manifest{
	ID:          "io.goremote.protocol.powershell",
	Name:        "PowerShell remoting",
	Description: "Planned Go-native PowerShell remoting protocol; local process launching is intentionally unsupported.",
	Kind:        plugin.KindProtocol,
	Version:     "2.0.0",
	APIVersion:  plugin.CurrentAPIVersion,
	Capabilities: []plugin.Capability{
		plugin.CapNetworkOutbound,
		plugin.CapTerminal,
	},
	Status:    plugin.StatusPlanned,
	Publisher: "goremote",
	License:   "MIT",
}
