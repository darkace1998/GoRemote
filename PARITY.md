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
| PowerShell   | ✅        | ✅ **Ready**       | Local `pwsh`/`powershell` launch; on Unix uses PTY (full terminal), on Windows uses ConPTY (`CreatePseudoConsole`) with full ANSI/VT and working resize. Argument quoting safe-by-default. |
| HTTP / HTTPS | ✅        | ✅ **Ready**       | In-app browser session via `webview`/system browser launcher; cookie isolation per session; basic / bearer auth from credential providers. |
| RDP          | ✅        | ✅ **Ready** (external) | Launches `mstsc.exe` on Windows or `xfreerdp` on Linux/macOS; credentials handed off via the platform launcher with cleanup of any spool files. See **Notes**. |
| VNC          | ✅        | ✅ **Ready** (external) | Launches the system `vncviewer` (TigerVNC / RealVNC / TightVNC) with credentials piped through the launcher. See **Notes**. |
| IBM TN5250   | ✅        | ✅ **Ready** (external) | Launches `tn5250` / `xtn5250`. See **Notes**. |
| External app | ✅        | ✅ **Ready**       | `internal/extlaunch` runs arbitrary launcher commands with templated args. |
| MOSH         | (3rd-party) | ✅ **Ready** (external) | Launched via the system `mosh` binary (extlaunch, RenderExternal); not available on Windows (no native mosh.exe). |

Plugins shipped today (`plugins/`):

```
protocol-ssh        protocol-telnet     protocol-rlogin     protocol-rawsocket
protocol-powershell protocol-http       protocol-rdp        protocol-vnc
protocol-tn5250     protocol-mosh
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
| Bulk edit / duplicate / move / copy              | 🟡 **Partial**  | Bulk move and bulk delete shipped via toolbar (`✓` add, `Bulk move`, `Bulk delete`); multi-row diff-style bulk *edit* still deferred |
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
| Embedded vs external launch per protocol         | ✅ **Ready**    | `internal/extlaunch` + protocol manifest capability flags |

### 4.3 Protocols

See the protocols table above. All required first-class protocols are available; the graphical protocols (RDP / VNC / TN5250) ship via the external-launcher model documented in **Notes**.

### 4.4 Credential providers

| Feature                                                | goremote        | Where |
|--------------------------------------------------------|-----------------|-------|
| Plugin API (no hard-coded providers)                   | ✅ **Ready**    | `sdk/credential` |
| Lookup by metadata / user selection / fallback         | ✅ **Ready**    | `host/credential` resolver chain |
| Username / password / domain / private key / OTP / per-protocol secrets | ✅ **Ready** | `credential.Material` shape |
| Unlock / refresh / cache / revocation                  | ✅ **Ready**    | Provider lifecycle + `Material.Zeroize` |
| Out-of-process isolation                               | ✅ **Ready**    | IPC contract defined and reference-implemented (`host/plugin/ipc/`); 1Password / Bitwarden today shell out to vendor CLIs which already isolate process-side; IPC now fully functional on Windows (Unix domain sockets, Win10 RS1+). |
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

### External-launcher model (RDP, VNC, TN5250)

Graphical and 5250 protocols ship through `internal/extlaunch` rather than a re-implemented in-process client. When a session of these types is opened, goremote:

1. Resolves credentials from the configured provider chain.
2. Templates a launcher command from the protocol manifest (e.g. `xfreerdp /v:{host} /u:{user} /p:{password-file}`).
3. Spawns the platform-appropriate binary (`mstsc` / `xfreerdp` / `vncviewer` / `tn5250`) under a supervised child process.
4. Streams the launcher's stdout/stderr into the session log and exposes lifecycle events (`opened`, `exited`, `failed`) on the event bus.
5. Cleans up any temporary credential files / `.rdp` / `.vnc` files on session close.

**Rationale.** A fully in-process RDP or RFB stack is months of work and a meaningful security surface. Every supported desktop OS already ships (or makes available) a hardened, well-maintained client for these protocols. The external-launcher model:

- Reaches feature parity with mRemoteNG on day one for these protocols.
- Inherits OS-native rendering, GPU acceleration, smart-card redirection, and clipboard integration "for free."
- Keeps the goremote process small and free of large protocol decoders.
- Leaves the door open for an in-process implementation later (the SDK already supports `Session` types that own their own renderer).

Trade-off: embedded-tab UX is replaced by a separate OS window. This is documented in the connection's settings panel.

### IPC reference implementation

External plugins are expected to communicate over the IPC contract at `host/plugin/ipc/` (gRPC/Connect over named pipes or Unix sockets), not Go's native `plugin` package. The reference implementation is exercised by `plugins/external-example` and tested in `host/plugin/ipc`.
