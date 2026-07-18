# VNC

**Status:** Experimental

Virtual Network Computing — experimental Go-native in-process TCP/RFB session.

## Overview

Package vnc implements the built-in VNC protocol plugin for goremote.

Sessions are handled entirely in-process: the plugin dials the remote host
over TCP using Go's standard net package. No external binary is required.
Full RFB handling is still experimental.
