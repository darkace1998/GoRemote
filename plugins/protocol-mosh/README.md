# MOSH

**Status:** Experimental

Mobile Shell: experimental SSH-bootstrapped Go-native session; MOSH UDP transport is not complete yet.

## Overview

Package mosh implements the MOSH (Mobile Shell) protocol plugin for goremote.

Sessions are handled in-process: the plugin uses golang.org/x/crypto/ssh to
dial the SSH port, runs "mosh-server new" on the remote, and parses the
"MOSH CONNECT <port> <key>" output. The UDP transport is still experimental.
No local external binary is required. Rendering uses [protocol.RenderTerminal].
