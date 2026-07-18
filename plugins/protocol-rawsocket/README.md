# Raw Socket

**Status:** Ready

Raw TCP connection useful for banner-grabbing, telnet-without-negotiation, and custom line protocols.

## Overview

Package rawsocket implements the built-in Raw Socket protocol plugin for
goremote.

The plugin exposes a [protocol.Module] that opens a plain TCP connection
to a remote host and wires the byte stream directly to the host-supplied
stdin/stdout pipes. No protocol negotiation or framing is performed, so it
is useful for banner-grabbing, speaking line protocols manually, or
interacting with services that would otherwise require a protocol-specific
client (e.g. "telnet" without Telnet option negotiation).
