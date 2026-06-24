# Protocols

GoRemote protocol modules are Go-native packages linked into the binary. The
protocol system does not spawn vendor viewers or route protocol sessions over
IPC. External tool launching is a separate planned feature outside the protocol
plugin system.

> _Screenshot pending — see [contributing-screenshots.md](./contributing-screenshots.md). Suggested filename: `protocol-ssh-session.png`._

## In-process protocols

| Protocol | Notes |
|---|---|
| **SSH** | Native Go SSH client. Supports agent / key / password / keyboard-interactive. |
| **SFTP** | Same auth surface as SSH; renders a file browser tab. |
| **Telnet** | Linemode + character mode with optional TLS-STARTTLS. |
| **Rlogin** | Mostly here for legacy parity with mRemoteNG. |
| **Raw socket** | Plain TCP byte stream — useful for serial-over-IP devices. |
| **HTTP** | Experimental Go-native in-process HTTP client; fetches URLs without spawning a browser. |
| **Serial / COM** | Local serial ports (`/dev/ttyUSB0`, `COM3`, …) at configurable baud rate / parity. |
| **RDP** | Experimental Go-native TCP scaffold; full graphical protocol pipeline is planned. |
| **VNC** | Experimental Go-native TCP scaffold; full RFB protocol framing is planned. |
| **TN5250** | Experimental Go-native TCP scaffold; full 5250 negotiation/screen model is planned. |
| **MOSH** | Planned/experimental Go-native package; session start is unsupported until MOSH UDP transport lands. |
| **PowerShell remoting** | Planned; not registered until a Go-native remoting transport exists. |

Open any of these by selecting the connection in the tree and
clicking the toolbar icon whose tooltip reads `"Connect (open
selected)"`.

## Choosing per-connection settings

Open a connection's edit dialog and look at the **Protocol** section.
Common knobs:

* Connection-specific port override.
* Protocol-specific options such as SSH host-key policy, terminal encoding, or
  serial baud rate.

## Related buttons

| Tooltip | What it does |
|---|---|
| `New connection…` | Create a connection (you choose the protocol here). |
| `Connect (open selected)` | Open a session with the protocol configured on the selected connection. |
| `Disconnect current session` | Cleanly terminate the active session. |
| `Plugins…` | Where third-party protocol modules surface (see [plugins.md](./plugins.md)). |
