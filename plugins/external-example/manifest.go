// Package externalexample is the in-host descriptor for the demonstration
// out-of-process plugin shipped under plugins/external-example.
//
// The descriptor is registered with the plugin registry so the host can
// advertise the plugin in UI listings without having to launch the binary.
// The actual plugin process implements the IPC contract defined in
// proto/plugin/v1 and is built from cmd/external-example.
package externalexample

import "github.com/goremote/goremote/sdk/plugin"

// PluginID is the canonical id for the demonstration external plugin.
const PluginID = "io.goremote.example.external"

// Manifest returns the static manifest published by the external-example
// plugin. The plugin is marked Experimental because it exists purely as a
// reference for the IPC transport contract.
func Manifest() plugin.Manifest {
	return plugin.Manifest{
		ID:           PluginID,
		Name:         "External Example",
		Description:  "Reference out-of-process plugin demonstrating the goremote IPC contract (PluginHandshake + Echo).",
		Kind:         plugin.KindProtocol,
		Version:      "0.1.0",
		APIVersion:   plugin.CurrentAPIVersion,
		Capabilities: []plugin.Capability{},
		Status:       plugin.StatusExperimental,
		Publisher:    "goremote",
		License:      "MIT",
	}
}
