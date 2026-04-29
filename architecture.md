# Proposed Architecture for a Go-Based Cross-Platform Successor

## 1. Architecture Goals
- Cross-platform first.
- Go-first application core.
- Strong isolation for risky extensions.
- Modular protocols and credential providers.
- Ability to reach mRemoteNG feature parity without rebuilding the app every time a protocol or vault changes.

## 2. Recommended High-Level Shape
Use a **Go monorepo with a shared `go.work`** and a thin desktop shell backed by a set of clearly separated internal packages and versioned SDKs.

### Major layers
1. **UI Shell**
   - Cross-platform native desktop shell built with **Fyne v2** (pure Go, OpenGL-rendered).
   - Responsible for windows, tabs, panes, settings forms, search, and workspace state.
   - `cmd/desktop/gui_fyne.go` — main window, connection tree, toolbar, dialogs.
   - `cmd/desktop/fyne_session.go` — session tab lifecycle, terminal bridge to session I/O.
2. **Application Core**
   - Coordinates commands, state, events, permissions, and lifecycle.
   - No protocol-specific implementation details.
3. **Domain Model**
   - Connections, folders, inheritance, tags, workspaces, credentials, protocol descriptors.
4. **Persistence Layer**
   - Native config format, import/export, backups, schema migration.
5. **Protocol Host**
   - Loads built-in protocol modules and external protocol plugins.
6. **Credential Host**
   - Loads built-in credential providers and external credential plugins.
7. **Platform Services**
   - Keychain access, notifications, file dialogs, tray, system integration.

## 3. Suggested Repository Layout
- `cmd/desktop` — Fyne desktop entry point (main, bindings, GUI, session bridge)
- `cmd/sign-manifest` — release helper that signs auto-update manifests (Ed25519)
- `internal/app` — application core (commands, events, sessions, views)
- `internal/domain` — connection / folder / template / inheritance types
- `internal/persistence` — versioned config, secret blob, migrations, backups
- `internal/import/mremoteng` — XML/CSV importer with per-row warnings
- `internal/extlaunch` — generic external-process launcher (RDP/VNC/TN5250/MOSH/PowerShell)
- `internal/eventbus` — typed pub/sub used by hosts and app core
- `internal/logging` — structured-logger wrapper with secret redaction (file sink + rotation)
- `internal/platform` — paths, keychain abstraction, clipboard, notifications
- `app/settings`, `app/workspace` — persisted UI documents (settings, open tabs)
- `app/update`, `app/diagnostics`, `app/marketplace`, `app/extplugin`, `app/sync` — app-level features (auto-update, diagnostic bundle, plugin marketplace, external-plugin loader, git-sync)
- `sdk/plugin`, `sdk/protocol`, `sdk/credential` — versioned plugin contracts
- `host/plugin`, `host/protocol`, `host/credential` — in-process plugin hosts
- `host/plugin/ipc/` + `proto/plugin/v1/` — out-of-process IPC contract (length-prefixed JSON over Unix domain sockets)
- `plugins/protocol-{ssh,sftp,telnet,rlogin,rawsocket,serial,tn5250,rdp,vnc,http,powershell,mosh}` — built-in protocols
- `plugins/credential-{file,keychain,1password,bitwarden}` — built-in providers
- `plugins/external-example` — reference out-of-process plugin
- `installers/` — Windows WiX MSI sources (and platform packaging stubs)
- `test/integration` — fake-plugin integration harness

## 4. UI Architecture
### Recommended approach
- Keep the UI declarative and state-driven.
- Route all mutations through application commands exposed by the Go backend.
- Maintain a session/workspace store separate from rendered widgets.
- Use a dock model for tabs/panes instead of tightly coupling layout to protocol implementations.

### UI responsibilities
- Connection tree and search.
- Property editors and inheritance display.
- Quick connect.
- Session tabs, panes, and workspaces.
- Notifications and diagnostics.
- Plugin management UI.

### UI should not own
- Secret resolution.
- Protocol transport logic.
- Config migrations.
- Plugin lifecycle decisions.

## 5. Domain Model
The domain model should represent concepts rather than UI widgets.

### Core entities
- `ConnectionNode`
- `FolderNode`
- `ConnectionTemplate`
- `InheritanceProfile`
- `ProtocolDescriptor`
- `SessionDescriptor`
- `CredentialReference`
- `CredentialMaterial`
- `WorkspaceLayout`
- `PluginManifest`

### Key rule
Connection definitions store **references** to credentials and providers where possible, not raw secrets.

## 6. Data and Persistence
### Native format
Use a versioned, human-inspectable format:
- `TOML` or `JSON` for structure
- encrypted blobs for sensitive values
- separate workspace/session cache from durable connection inventory

### Persistence components
- serializer/deserializer
- schema migrator
- backup manager
- import/export adapters
- integrity validator

### Compatibility path
- Dedicated importer for current mRemoteNG XML and CSV data.
- Preserve folder structure, inheritance flags, per-protocol settings, and display metadata where possible.
- Store import warnings instead of silently dropping unsupported values.

## 7. Protocol System
### Design choice
Do **not** rely on Go's native `plugin` package for third-party extensions. It is not a good fit for a cross-platform desktop product and is especially problematic on Windows.

Use an **out-of-process plugin model** with a stable IPC contract.

### Recommended plugin transport
- gRPC or Connect RPC over local IPC
- named pipes on Windows
- Unix domain sockets on Linux/macOS
- optional sandboxing later for highly constrained plugins

### Protocol plugin contract
Each protocol module should expose:
- manifest metadata
- settings schema
- supported auth types
- capabilities
- platform support matrix
- session creation hooks
- reconnect/resize/clipboard hooks
- diagnostics hooks

### Built-in vs external protocols
- Built-in protocols can live in the same repository and use direct Go interfaces internally.
- External protocols should go through the stable IPC boundary.
- The host should present both through one unified registry.

### Session rendering modes
- **Terminal mode** for SSH, Telnet, TN5250, Raw Socket, rlogin.
- **Graphical framebuffer mode** for VNC and future embedded RDP.
- **External launcher mode** for protocols that still depend on OS-native or third-party clients.

## 8. Credential Provider System
### Required model
Credential providers should also run as isolated plugins or trusted built-in modules.

### Provider contract
A provider should support:
- discovery
- unlock/init
- resolve credential request
- refresh
- revoke/clear cache
- health/status reporting
- structured errors

### Security model
- The app requests only the capabilities a provider declares.
- Providers never receive more connection context than needed.
- Secrets remain in memory only for the shortest practical time.
- The host can quarantine unhealthy providers.

### Recommended provider categories
- local encrypted file provider
- OS keychain provider
- 1Password
- Bitwarden
- enterprise vault providers

## 9. Execution Model
- UI process handles rendering and user interaction.
- Goroutines handle I/O, plugin IPC, and background work.
- Long-running protocol and credential tasks run off the UI path.
- An event bus distributes state changes to the UI, logs, and background services.
- `context.Context` is used consistently for cancellation, timeouts, and request scoping.

## 10. Security Boundaries
### Trust zones
1. Trusted app core
2. Semi-trusted built-in modules
3. Less-trusted third-party plugins
4. Untrusted remote endpoints

### Enforcements
- signed manifests or explicit trust prompts
- per-plugin capability declarations
- IPC timeouts and resource limits where possible
- secret redaction in logs
- encrypted persistence for sensitive fields

## 11. Cross-Platform Platform Services Layer
Abstract platform-specific behavior behind interfaces for:
- secure secret storage
- notifications
- filesystem paths
- tray/system menu behavior
- shell integration
- window state persistence
- clipboard integration

This avoids scattering OS checks across the UI and protocol code.

## 12. Testing Strategy by Layer
- Domain: pure unit tests.
- Persistence: golden files and migration tests.
- Importers: compatibility fixtures from real mRemoteNG exports.
- Protocol SDK: contract tests.
- Plugin host: process isolation and failure recovery tests.
- UI: integration tests for critical flows.
- Security: secret handling, redaction, and permission tests.

## 13. Architecture Decisions That Reduce Risk
- Greenfield rewrite, not line-by-line port.
- **Fyne v2 (pure Go + OpenGL)** chosen for the UI shell over the original Wails/React plan — enables true cross-platform builds from a single Go toolchain without a browser runtime or Node.js dependency.
- Stable IPC boundary for third-party extensions.
- Core/domain separation before protocol implementation.
- Import compatibility treated as a product feature, not a migration script.
- External-launch fallback allowed for some protocols while embedded implementations mature.
