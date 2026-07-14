# TN5250

**Status:** Experimental

IBM 5250 terminal emulation for AS/400 and IBM i hosts — experimental Go-native in-process TCP session.

## Overview

Package tn5250 implements the built-in TN5250 protocol plugin for goremote.

Sessions are handled entirely in-process: the plugin dials the remote IBM i /
AS/400 host over TCP using Go's standard net package. No external binary is
required. Full TN5250 negotiation is still experimental.
