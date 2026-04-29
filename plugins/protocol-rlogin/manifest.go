// Package rlogin implements the built-in rlogin (RFC 1282) protocol plugin.
//
// The plugin speaks the rlogin client side of RFC 1282: upon TCP connection
// (default port 513) it sends a 4-field null-terminated handshake
// (0x00, client-user, server-user, terminal-type/speed) and expects a single
// 0x00 ACK byte from the server before entering a bidirectional byte-stream.
//
// Deviation from RFC 1282: real rlogin conveys certain control signals
// (0x02 flush, 0x10 / 0x20 flow-control, 0x80 window-size request) using
// TCP urgent / out-of-band data. This implementation instead sends the
// window-size notification (0xFF 0xFF 's' 's' <rows> <cols> <xpix> <ypix>
// big-endian uint16) as an in-band byte sequence on the main data channel.
// Most modern rlogind implementations tolerate this because the window-size
// payload is framed by the distinctive 0xFF 0xFF 's' 's' magic prefix and
// is normally pulled out of the stream by the kernel's SIGURG handler;
// clients that cannot use TCP urgent (portable user-space, sandboxed
// environments, embedded terminals) commonly take this same shortcut.
//
// Security: rlogin is a cleartext protocol with host-based trust. It MUST
// NOT be used over untrusted networks. Prefer SSH.
package rlogin

import "github.com/darkace1998/GoRemote/sdk/plugin"

// Manifest is the static plugin manifest for the rlogin protocol plugin.
var Manifest = plugin.Manifest{
	ID:          "io.goremote.protocol.rlogin",
	Name:        "rlogin",
	Description: "RFC 1282 rlogin client. Deviation: window-size control messages are sent in-band (0xFF 0xFF 's' 's' <rows cols xpix ypix BE uint16>) rather than via TCP urgent/out-of-band data. Cleartext protocol; use SSH where possible.",
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
