# RDP

**Status:** Experimental

Microsoft Remote Desktop Protocol — experimental Go-native in-process TCP session.

## Overview

Package rdp implements the built-in RDP protocol plugin for goremote.

Sessions are handled entirely in-process: the plugin dials the remote host
over TCP using Go's standard net package. No external binary is required.
Full MS-RDPBCGR framing is still experimental.
