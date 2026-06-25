# Parity with mRemoteNG

This matrix tracks feature parity between **goremote** and mRemoteNG.

- ✅ **Ready** — implemented with passing tests (`go test -race`).
- 🟡 **Partial** — usable today; one or more sub-features still planned.
- 🔶 **Planned / Experimental** — manifest is registered but the implementation is a stub or research scaffold.
- ❌ — not supported by mRemoteNG (not a parity gap).

## Protocols

| Protocol     | mRemoteNG | goremote          | Notes |
|--------------|-----------|-------------------|-------|
| SSH          | ✅        | ✅ **Ready**       | Password / public-key / agent / interactive auth; `known_hosts` (strict / accept-new / off); keepalive; PTY + window-size updates. |
| Telnet       | ✅        | ✅ **Ready**       | RFC 854 negotiation; NAWS; TTYPE; configurable encoding. |
| Rlogin       | ✅        | ✅ **Ready**       | RFC 1282 handshake; in-band window-size updates. |
| Raw socket   | ✅        | ✅ **Ready**       | Configurable EOL (lf / crlf / none), keepalive, configurable encoding. |
| PowerShell   | ✅        | 🔶 **Planned**     | Local PowerShell process launching was removed from the protocol system; Go-native PSRP/WinRM remoting is planned before registration. |
| HTTP / HTTPS | ✅        | ✅ **Ready**        | Go-native in-process HTTP client (no external browser). |
| RDP          | ✅        | 🔶 **Experimental** | Go-native TCP scaffold; full MS-RDPBCGR graphics/security pipeline still planned. |
| VNC          | ✅        | 🔶 **Experimental** | Go-native TCP scaffold; full RFB protocol framing still planned. |
| IBM TN5250   | ✅        | 🔶 **Experimental** | Go-native TCP scaffold; full TN5250 negotiation/screen model still planned. |
| External app | ✅        | 🔶 **Planned**     | External tool launching is outside the protocol plugin system. |
| MOSH         | (3rd-party) | 🔶 **Planned / Experimental** | Go-native package exists, but Start returns unsupported until MOSH UDP transport is implemented. |
| SFTP         | (3rd-party) | ✅ **Ready**       | Interactive SFTP file-browser shell (ls/cd/get/put/mkdir/rm/mv/chmod/...) over an SSH connection. Reuses the SSH plugin's auth + known-hosts machinery. |
| Serial / COM | ✅ (PuTTY)  | ✅ **Ready**       | Local serial-console terminal sessions. Configurable baud/data-bits/parity/stop-bits/EOL. Cross-platform (Linux/macOS `/dev/tty*`, Windows `COMn`). |

Plugins shipped today (`plugins/`):

```
protocol-ssh        protocol-sftp       protocol-telnet     protocol-rlogin
protocol-rawsocket  protocol-rdp        protocol-tn5250     protocol-mosh
protocol-serial     protocol-http       protocol-vnc
protocol-powershell (planned, not registered)
```

## Credential providers

| Provider             | mRemoteNG    | goremote        | Notes |
|----------------------|--------------|-----------------|-------|
| OS keychain          | ⚠️ (Windows) | ✅ **Ready**    | Cross-platform via `zalando/go-keyring` (Linux Secret Service / macOS Keychain / Windows Credential Manager). |
| Encrypted file vault | ✅           | ✅ **Ready**    | AES-256-GCM + Argon2id (t=1, m=64 MiB, p=4); header bound as AAD for tamper detection. |
| 1Password            | ❌           | ✅ **Ready**    | Uses the official `op` CLI; capability-gated; `Material.Zeroize` on every resolve. |
| Bitwarden            | ❌           | ✅ **Ready**    | Uses the official `bw` CLI with session-token unlock model. |

## Compared to mRemoteNG (full feature matrix)

Source of truth for the feature list: `requirements.md`, `architecture.md`, `stages.md`. Items below are scored against goremote's current implementation.

### 4.1 Core connection management

| Feature                                          | goremote        | Where |
|--------------------------------------------------|-----------------|-------|
| Folders / nested folders / tree organization     | ✅ **Ready**    | `internal/domain` |
| Inheritance of connection properties             | ✅ **Ready**    | `InheritanceProfile.Resolve` (with provenance) |
| Connection templates / quick-connect             | ✅ **Ready**    | App command + UI modal |
| Search / filter / tagging                        | ✅ **Ready**    | `internal/domain/search.go` |
| Bulk edit / duplicate / move / copy              | 🟡 **Partial**  | Bulk move and bulk delete shipped via toolbar (`"Add selection to multi-select"`, `"Move selected to folder…"`, `"Bulk delete selected"`); multi-row diff-style bulk *edit* still deferred |
| Favorites / recents / environment grouping       | ✅ **Ready**    | Favorites flag, Recents ring (max 20), Environment filter Select |
| Import / export XML and CSV                      | ✅ **Ready**    | `internal/import/mremoteng` |
| Native versioned config format                   | ✅ **Ready**    | `internal/persistence` (atomic JSON + migrator) |
| Backup / recovery                                | ✅ **Ready**    | Zip snapshots, validated restore |

### 4.2 Session management

| Feature                                          | goremote        | Where |
|--------------------------------------------------|-----------------|-------|
| Tabbed workspace                                 | ✅ **Ready**    | `container.AppTabs` in `cmd/desktop/gui_fyne.go` |
| Split-pane layouts                               | ✅ **Ready**    | Recursive H/V splits per tab via right-click; per-pane title bar with active indicator + close; layouts persist across restarts |
| Reconnect / disconnect / duplicate session       | ✅ **Ready**    | `internal/app` commands |
| Reopen last workspace                            | ✅ **Ready**    | `app/workspace` |
| Per-session colors / icons / labels              | 🟡 **Partial**  | Color/label persisted; icon picker deferred |
| Notifications / status indicators                | ✅ **Ready**    | Event bus + UI banner |
| Workspace persistence across restarts            | ✅ **Ready**    | `app/workspace` |
| Embedded Go-native protocol modules             | 🟡 **Partial**  | Ready terminal protocols plus experimental RDP / TN5250 / MOSH scaffolding |

### 4.3 Protocols

See the protocols table above. Ready terminal protocols are available today; graphical protocols like RDP, TN5250, MOSH, and PowerShell remoting are tracked as experimental or planned until their Go-native engines are complete.

### 4.4 Credential providers

| Feature                                                | goremote        | Where |
|--------------------------------------------------------|-----------------|-------|
| Plugin API (no hard-coded providers)                   | ✅ **Ready**    | `sdk/credential` |
| Lookup by metadata / user selection / fallback         | ✅ **Ready**    | `host/credential` resolver chain |
| Username / password / domain / private key / OTP / per-protocol secrets | ✅ **Ready** | `credential.Material` shape |
| Unlock / refresh / cache / revocation                  | ✅ **Ready**    | Provider lifecycle + `Material.Zeroize` |
| Out-of-process isolation                               | ✅ **Ready**    | IPC contract defined and reference-implemented (`host/plugin/ipc/`); 1Password / Bitwarden today shell out to vendor CLIs which already isolate process-side; the IPC reference transport (length-prefixed JSON over Unix domain sockets) works on Linux, macOS, and Windows 10 1809+. |
| Capability discovery / version negotiation             | ✅ **Ready**    | `plugin.Manifest` + `plugin.CurrentAPIVersion` |
| Graceful failure on provider crash                     | ✅ **Ready**    | Host quarantine after repeated failures |

### 4.5 Configuration and data

| Feature                                          | goremote        |
|--------------------------------------------------|-----------------|
| Versioned native config format                   | ✅ **Ready**    |
| Import of mRemoteNG XML/CSV with inheritance     | ✅ **Ready**    |
| Migration warnings (no silent drops)             | ✅ **Ready**    |
| Local encrypted storage for sensitive metadata   | ✅ **Ready**    |
| External backends (SQL / Git sync)               | 🟡 **Partial**  | Git-sync via `app/sync` shipped (commit-and-push on save, opt-in via Settings); SQL backend still planned |
| Schema migration with rollback safety            | ✅ **Ready**    |

### 4.6 Security

| Feature                                          | goremote        |
|--------------------------------------------------|-----------------|
| No plaintext secrets at rest                     | ✅ **Ready**    |
| OS-native secret storage                         | ✅ **Ready**    |
| Strong encryption of exports                     | ✅ **Ready**    |
| Audit logging for credential access              | ✅ **Ready** (`internal/logging`) |
| Plugin signing / trust policies                  | ✅ **Ready**    | Ed25519 signature verification implemented in `sdk/plugin.Verifier`; `TrustStore`, `Policy` (permissive/strict), and `Sign` helper all available. |
| Capability-based plugin permissions              | ✅ **Ready**    |
| Plugin isolation / timeouts / validation         | ✅ **Ready**    |

### 4.7 Cross-platform UX

| Feature                                          | goremote        |
|--------------------------------------------------|-----------------|
| Consistent IA across Windows / Linux / macOS     | ✅ **Ready**    |
| Light / dark themes, high-DPI                    | ✅ **Ready**    |
| Keyboard navigation, contrast, scalable text     | ✅ **Ready**    |
| Screen-reader friendly metadata                  | 🟡 **Partial**  | ARIA roles in place; full audit pending |
| Tray / OS notifications / native file locations  | ✅ **Ready**    | Notifications + tray Show/Quit/Recents submenu via `desktop.App` |
| Drag-to-reorder tabs                             | 🟡 **Partial**  | `Ctrl+Shift+PageUp/PageDown` shortcut shipped; pointer-drag awaits Fyne `OnReordered` |

### 5. Non-functional

| Area                                             | goremote        |
|--------------------------------------------------|-----------------|
| Cold-start performance                           | ✅ **Ready**    |
| Large-inventory responsiveness (search/tree)     | ✅ **Ready**    |
| Plugin / protocol crash recovery                 | ✅ **Ready**    |
| Cancellable background tasks (`context.Context`) | ✅ **Ready**    |
| Autosave + safe backup                           | ✅ **Ready**    |
| Strong package boundaries (`go.work`)            | ✅ **Ready**    |
| Versioned plugin API; no native `plugin` pkg     | ✅ **Ready**    |
| Structured logs with redaction                   | ✅ **Ready**    |
| Cross-platform CI artifacts (linux/macos/windows) | ✅ **Ready**   |
| Signed installers                                | 🟡 **Partial**  | `release.yml` Authenticode-signs `.exe` + WiX MSI when secrets configured (`installers/windows/`); macOS notary + Linux .deb/.rpm still planned |
| Auto-update (Ed25519-signed manifest)            | ✅ **Ready**    | `app/update` verifies per-target signature before download; in-place SwapInPlace; `cmd/sign-manifest` release helper |
| Vulnerability scanning (`govulncheck`) in CI     | ✅ **Ready**    |

## Notes

### Go-native protocol model

Protocol modules are Go packages compiled into the application binary. They do
not spawn local vendor viewers, local shells, or protocol helper processes, and
they do not use plugin IPC for session transport. Modules whose protocol engines
are not complete are marked experimental or planned in their manifests and in
the table above.

### IPC reference implementation

External credential providers are expected to communicate over the IPC contract
at `host/plugin/ipc/` (length-prefixed JSON frames over Unix domain sockets —
supported on Linux, macOS, and Windows 10 1809+), not Go's native `plugin`
package. Protocol sessions remain Go-native and in-process.
