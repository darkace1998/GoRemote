# Go Cross-Platform Successor Requirements

## 1. Product Goal
Build a Go-based successor to mRemoteNG that delivers the same or better functionality while running well on Windows, Linux, and macOS, with a strong plugin model for credential providers and a modular protocol system.

## 2. Product Principles
- Match or exceed the current mRemoteNG user experience for day-to-day remote connection management.
- Be truly cross-platform, not Windows-first.
- Keep the core stable while allowing protocols and credential sources to evolve independently.
- Treat security, portability, and performance as first-class requirements.
- Preserve user migration paths from existing mRemoteNG data.

## 3. Scope
### In Scope
- Desktop application for Windows, Linux, and macOS.
- Multi-protocol remote session management.
- Dockable tabs/panels and connection workspaces.
- Tree-based connection organization with inheritance/templates.
- Plugin system for credential providers.
- Modular protocol system for built-in and third-party protocols.
- Import/migration from existing mRemoteNG connection data.
- Secure local storage, sync options, and enterprise-friendly deployment.

### Out of Scope for v1
- Mobile clients.
- Browser-only client.
- Full cloud SaaS backend as a hard requirement.
- Exact 1:1 UI clone of WinForms behavior when a better cross-platform UX is available.

## 4. Functional Requirements
### 4.1 Core Connection Management
- Support folders, nested folders, and tree-based organization.
- Support inheritance of connection properties from parent containers.
- Support default connection templates and quick-connect flows.
- Support search, filtering, bulk edit, duplicate, move, copy, and tagging.
- Support favorites, recents, and environment grouping.
- Support import/export for at least XML and CSV, plus a new native format.
- Support safe backup and recovery of configuration data.

### 4.2 Session Management
- Support tabbed and split-pane session layouts.
- Support reconnect, disconnect, duplicate session, and reopen last workspace.
- Support per-session display metadata such as colors, icons, labels, and environment markers.
- Support session notifications, error reporting, and status indicators.
- Support workspace persistence across app restarts.
- Support opening protocols either embedded or externally, depending on protocol capability.

### 4.3 Protocol Support
The platform must support a modular protocol system with a stable SDK for built-in and external protocol modules.

#### Required first-class protocols
- SSH
- Telnet
- Terminal 5250 / TN5250
- RDP
- VNC
- HTTP / HTTPS
- Raw socket
- rlogin
- PowerShell remoting
- External tool based sessions

#### Protocol requirements
- Each protocol module must declare capabilities, settings schema, auth requirements, rendering mode, and platform support.
- Terminal-style protocols must support keyboard mapping, copy/paste, font settings, encoding selection, scrollback, logging, and theming.
- Graphical protocols must support resize, clipboard integration, scaling, fullscreen, reconnect, and credential injection where permitted.
- Protocol modules must be independently testable and releasable.
- Adding a new protocol must not require invasive changes in the application core.

### 4.4 Credential Provider Plugin System
- Credential providers must be implemented through a plugin API, not hard-coded into the app core.
- Providers must support lookup by connection metadata, user selection, and fallback resolution.
- Providers must be able to return username, password, domain, private keys, OTP, and protocol-specific secrets.
- Providers must support secure unlock, refresh, caching, and revocation semantics.
- Provider execution must be isolated from the main app process when possible.
- The system must support both built-in providers and third-party providers.
- The system must support provider capability discovery and version negotiation.
- Provider failures must degrade gracefully without crashing the main app.

### 4.5 Configuration and Data
- Support a new versioned native config format.
- Support import of existing mRemoteNG connection files, including folders and inheritance behavior.
- Preserve legacy data during migration; imports must be reversible or reproducible.
- Support local encrypted storage for sensitive metadata.
- Support optional external storage backends later, such as SQL or Git-backed sync.
- Support schema migration with rollback safety.

### 4.6 Security
- Secrets must never be stored in plaintext by default.
- Use OS-native secret storage where appropriate:
  - Windows Credential Manager / DPAPI
  - macOS Keychain
  - Linux Secret Service / KWallet with fallback strategy
- Support strong encryption for exported sensitive data.
- Support audit logging for credential provider access and privileged actions.
- Support plugin signing or trust policies.
- Enforce capability-based permissions for plugins.
- Protect against malicious or broken plugins via isolation, timeouts, and validation.

### 4.7 Cross-Platform UX
- Deliver consistent information architecture across all supported desktop platforms.
- Respect platform conventions for keybindings, tray behavior, notifications, and file locations.
- Support light/dark themes and high DPI displays.
- Support accessibility basics from the start: keyboard navigation, readable contrast, scalable text, and screen-reader-friendly metadata where possible.

## 5. Non-Functional Requirements
### 5.1 Performance
- Cold start should feel fast on commodity hardware.
- Tree navigation and search must remain responsive with large datasets.
- The app should handle thousands of saved connections without major UI lag.
- Session rendering and credential resolution must not block the UI path.

### 5.2 Reliability
- The app must recover cleanly from plugin crashes, protocol crashes, and partial config corruption.
- Background tasks must be cancellable.
- Connection data must be autosaved safely and backed up predictably.

### 5.3 Maintainability
- Use a Go monorepo or `go.work` layout with strong package boundaries.
- Keep protocol logic, credential logic, persistence, and UI separate.
- Publish internal SDK contracts for protocols and credential providers.
- Enforce semantic versioning for plugin APIs.
- Do not depend on Go's native `plugin` package for cross-platform extensions.

### 5.4 Observability
- Structured logging must exist for app core, protocol modules, and plugins.
- Diagnostics must be redactable to avoid secret leakage.
- Crash reporting must be opt-in and privacy-aware.

### 5.5 Packaging and Delivery
- Windows, Linux, and macOS builds must be first-class CI artifacts.
- Support signed installers/packages where practical.
- Support portable mode and standard OS-native installation mode.

## 6. Compatibility Requirements
- Existing mRemoteNG users must be able to import their current connection inventory with minimal manual cleanup.
- The new app should preserve familiar concepts: connection tree, inheritance, tabs, quick connect, protocol-specific settings, external tools, backups, and themes.
- Feature gaps vs. mRemoteNG must be documented clearly and tracked to closure.

## 7. Recommended v1 Success Criteria
- Cross-platform desktop app released for Windows, Linux, and macOS.
- Stable core UX for large connection inventories.
- Built-in SSH, Telnet, and TN5250 support.
- Working credential provider plugin system with at least two reference providers.
- Import path for mRemoteNG data.
- At least one graphical protocol path available in v1, with the rest scheduled and designed.
- Security review completed for secret handling and plugin isolation.

## 8. Stretch Goals
- Shared team workspaces and sync.
- Role-based credential/provider policies.
- Recorded sessions and audit trails.
- Gateway/jump-host orchestration.
- Policy packs for enterprise deployment.
- Marketplace or registry for signed plugins.
