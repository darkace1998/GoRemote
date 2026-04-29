// Package telnet implements the built-in Telnet (RFC 854) protocol plugin.
//
// The plugin speaks plain RFC 854 Telnet plus the option negotiations required
// by most modern servers: TTYPE (RFC 1091), NAWS (RFC 1073), Suppress-Go-Ahead
// (SGA, option 3), and ECHO (option 1). LINEMODE (RFC 1184) is acknowledged
// but not fully negotiated: we default to character-at-a-time mode by asking
// the server to WILL SGA and refusing to ECHO ourselves, which is what every
// mainstream Telnet server expects from an interactive terminal client.
//
// Security: Telnet is a cleartext protocol. Operators should use SSH where
// possible. The AuthPassword method here transmits the username and password
// as plaintext after a simple "login:" / "password:" expect dance, exactly as
// a user typing at the keyboard would. There is no cryptographic protection
// of credentials or session data. The UI layer is responsible for surfacing
// this fact to the user before the session starts.
package telnet

import "github.com/darkace1998/GoRemote/sdk/plugin"

// Manifest is the static plugin manifest for the Telnet protocol plugin.
var Manifest = plugin.Manifest{
	ID:          "io.goremote.protocol.telnet",
	Name:        "Telnet",
	Description: "RFC 854 Telnet client with TTYPE (RFC 1091) and NAWS (RFC 1073) negotiation. Cleartext: credentials and session data are transmitted in the clear.",
	Kind:        plugin.KindProtocol,
	Version:     "1.0.0",
	APIVersion:  plugin.CurrentAPIVersion,
	Status:      plugin.StatusReady,
	Publisher:   "goremote",
	License:     "MIT",
	Capabilities: []plugin.Capability{
		plugin.CapNetworkOutbound,
		plugin.CapTerminal,
	},
}
