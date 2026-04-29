# Protocols

GoRemote splits protocols into two families:

* **In-process** protocols are linked into the binary; they render
  directly into a tab in the main window.
* **External-launcher** protocols spawn a vendor viewer (RDP / VNC /
  TN5250 / MOSH client). GoRemote tracks the lifetime of the spawned
  process but the actual remote rendering happens in the launched
  window.

> _Screenshot pending — see [contributing-screenshots.md](./contributing-screenshots.md). Suggested filename: `protocol-ssh-session.png`._

## In-process protocols

| Protocol | Notes |
|---|---|
| **SSH** | Native Go SSH client. Supports agent / key / password / keyboard-interactive. |
| **SFTP** | Same auth surface as SSH; renders a file browser tab. |
| **Telnet** | Linemode + character mode with optional TLS-STARTTLS. |
| **Rlogin** | Mostly here for legacy parity with mRemoteNG. |
| **Raw socket** | Plain TCP byte stream — useful for serial-over-IP devices. |
| **PowerShell** | Spawns a local PowerShell process and pipes it into a tab. |
| **HTTP** | Embedded browser tab for `http://` / `https://` connection definitions. |
| **Serial / COM** | Local serial ports (`/dev/ttyUSB0`, `COM3`, …) at configurable baud rate / parity. |

Open any of these by selecting the connection in the tree and
clicking the toolbar icon whose tooltip reads `"Connect (open
selected)"`.

## External-launcher protocols

| Protocol | Required executable on `PATH` |
|---|---|
| **RDP** | `xfreerdp` (Linux/macOS) or `mstsc.exe` (Windows). |
| **VNC** | `vncviewer` (TigerVNC / RealVNC / TightVNC). |
| **TN5250** | `tn5250` (the GNU `tn5250` CLI). |
| **MOSH** | `mosh-client` plus a reachable `mosh-server` on the remote. |

When you connect to one of these, GoRemote:

1. Resolves the credential reference and writes any required temporary
   credentials to a tightly-scoped location (e.g. an `.rdp` file with
   `0600` permissions, deleted on session close).
2. Spawns the launcher with the appropriate command-line flags.
3. Tracks the spawned process so that closing the GoRemote tab also
   terminates the launcher.

If the launcher executable is missing, GoRemote refuses to open the
session and surfaces a `"<binary> not found in PATH"` message; install
the launcher and try again.

## Choosing per-connection settings

Open a connection's edit dialog and look at the **Protocol** section.
Common knobs:

* Connection-specific port override.
* Protocol-specific options (SSH proxy command, RDP screen geometry,
  VNC view-only mode, serial baud rate, etc.).
* Custom launcher flags for external-launcher protocols, when you
  need to push a flag the dialog does not surface natively.

## Related buttons

| Tooltip | What it does |
|---|---|
| `New connection…` | Create a connection (you choose the protocol here). |
| `Connect (open selected)` | Open a session with the protocol configured on the selected connection. |
| `Disconnect current session` | Cleanly terminate the active session, including external launchers. |
| `Plugins…` | Where third-party protocol modules surface (see [plugins.md](./plugins.md)). |
