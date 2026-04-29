// Package powershell implements the built-in PowerShell protocol plugin for
// goremote.
//
// The plugin spawns a local PowerShell host (`pwsh` if available, falling
// back to `powershell.exe` on Windows) inside a pseudo-terminal so the user
// gets an interactive shell wired into goremote's terminal renderer. Auth is
// handled by the operating system (the child process inherits the calling
// user's identity); no network is involved.
package powershell

import "github.com/darkace1998/GoRemote/sdk/plugin"

// Manifest is the static manifest published by the built-in PowerShell
// protocol plugin. Hosts validate it via [plugin.Manifest.Validate] before
// registering the module.
//
// Platforms is left empty (meaning "all"), but Open returns an error on
// Windows in this build because creack/pty does not support Windows yet;
// once a ConPTY-backed implementation lands the windows session file will
// drop the error.
var Manifest = plugin.Manifest{
	ID:          "io.goremote.protocol.powershell",
	Name:        "PowerShell",
	Description: "Local PowerShell (pwsh / powershell.exe) terminal sessions hosted in a PTY.",
	Kind:        plugin.KindProtocol,
	Version:     "1.0.0",
	APIVersion:  plugin.CurrentAPIVersion,
	Capabilities: []plugin.Capability{
		plugin.CapTerminal,
		plugin.CapProcessSpawn,
	},
	Status:    plugin.StatusReady,
	Publisher: "goremote",
	License:   "MIT",
}
