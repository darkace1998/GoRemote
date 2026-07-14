# PowerShell remoting

**Status:** Planned

Planned Go-native PowerShell remoting protocol; local process launching is intentionally unsupported.

## Overview

Package powershell declares the planned built-in PowerShell remoting
protocol plugin for goremote.

The previous local-process PTY launcher has been removed because protocols
must be Go-native in-process implementations. This package remains as a
planned remoting module until a Go-native PSRP/WinRM transport lands.
