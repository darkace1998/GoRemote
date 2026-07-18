# Serial

**Status:** Ready

Local serial / COM-port terminal sessions (PuTTY-style serial console).

## Overview

Package serial implements the built-in Serial / COM-port protocol plugin
for goremote.

The plugin opens a local serial port (RS-232 / USB-CDC / virtual COM)
at a user-configured baud rate and frame format, and pumps the byte
stream straight to the host terminal — equivalent to PuTTY's "Serial"
session type or the Windows-era mRemoteNG serial protocol.
