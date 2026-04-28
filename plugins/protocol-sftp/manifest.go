// Package sftp implements the built-in SFTP protocol plugin for goremote.
//
// The plugin opens an SSH connection (reusing the SSH plugin's auth /
// known-hosts machinery) and runs an interactive SFTP file-browser shell
// inside the host terminal. The command set mirrors OpenSSH's `sftp`
// CLI — ls, cd, pwd, get, put, mkdir, rmdir, rm, mv, chmod, lcd, lls,
// lpwd, help, exit — so users familiar with that tool feel at home.
//
// Rendering as a terminal protocol means the existing fyne-io/terminal
// pane infrastructure handles the UI without the host needing a custom
// graphical file-browser widget.
package sftp

import "github.com/goremote/goremote/sdk/plugin"

// Manifest is the static manifest published by the built-in SFTP plugin.
var Manifest = plugin.Manifest{
	ID:          "io.goremote.protocol.sftp",
	Name:        "SFTP",
	Description: "Interactive SFTP file-browser shell over SSH (ls/cd/get/put/mkdir/rm/...).",
	Kind:        plugin.KindProtocol,
	Version:     "1.0.0",
	APIVersion:  plugin.CurrentAPIVersion,
	Capabilities: []plugin.Capability{
		plugin.CapNetworkOutbound,
		plugin.CapTerminal,
		plugin.CapKeychainRead,
		plugin.CapFilesystemWrite,
	},
	Status:    plugin.StatusReady,
	Publisher: "goremote",
	License:   "MIT",
}
