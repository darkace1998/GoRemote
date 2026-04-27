# Delivery Stages

## Stage 0 - Discovery and Baseline
**Status: ✅ Delivered**
### Goals
- Establish feature inventory from current mRemoteNG behavior and docs.
- Confirm target personas, protocol priorities, and enterprise requirements.
- Define parity matrix and success metrics.

### Exit Criteria
- Approved requirements.
- Approved target architecture.
- Prioritized backlog and protocol roadmap.

## Stage 1 - Foundation Platform
**Status: ✅ Delivered**
### Goals
- Create Go workspace boundaries, core packages, and initial desktop shell scaffolding.
- Implement domain model for connections, folders, inheritance, templates, and workspaces.
- Implement native config format, migration framework, backup logic, and logging.
- Stand up cross-platform CI for Windows, Linux, and macOS.

### Exit Criteria
- App can create, edit, save, and reload connection trees.
- Import pipeline skeleton exists.
- Automated builds run on all three desktop platforms.

## Stage 2 - Usable Desktop Shell
**Status: ✅ Delivered** — Fyne v2 (pure-Go, OpenGL) replaces the original Wails/React plan.
### Goals
- Build connection tree, search/filtering, property editing, quick connect, tabs, panes, and notifications.
- Implement theme support, high DPI handling, and basic accessibility.
- Implement workspace persistence and recovery.

### Exit Criteria
- Users can manage a medium-to-large connection inventory productively.
- UI state survives restarts.
- Performance is acceptable with large datasets.

## Stage 3 - Terminal Protocol Core
**Status: ✅ Delivered**
### Goals
- Deliver SSH, Telnet, TN5250, Raw Socket, and rlogin modules.
- Implement terminal rendering, scrollback, encoding, clipboard, logging, reconnect, and keyboard mapping.
- Add protocol contract tests.

### Exit Criteria
- Terminal workflows are production-usable across Windows, Linux, and macOS.
- Sessions can run in tabs and split panes.

## Stage 4 - Credential Provider Plugins
**Status: ✅ Delivered**
### Goals
- Deliver plugin host, manifest validation, capability model, and lifecycle management.
- Ship built-in secure local provider and OS-keychain integration.
- Ship at least two external provider implementations.

### Exit Criteria
- Providers can be installed, configured, unlocked, and used at runtime.
- Provider crashes or timeouts do not take down the app.
- Secret handling passes review.

## Stage 5 - Graphical and Launcher Protocols
**Status: ✅ Delivered** — RDP, VNC, HTTP, PowerShell via `internal/extlaunch`; MOSH via external launcher.
### Goals
- Add VNC and RDP support.
- Add HTTP/HTTPS launch flows, PowerShell remoting integration, and external tool modules.
- Support embedded rendering where practical and external-launch fallback where necessary.

### Exit Criteria
- Users have a viable path for major non-terminal workflows.
- Graphical protocols behave consistently with tabs, panes, reconnect, and clipboard expectations where supported.

## Stage 6 - Migration, Enterprise, and Hardening
**Status: ✅ Delivered**
### Goals
- Complete mRemoteNG import compatibility coverage.
- Add policy controls, audit events, plugin trust workflows, and enterprise deployment options.
- Add crash recovery, corruption recovery, and richer diagnostics.

### Exit Criteria
- Migration from mRemoteNG is reliable for real-world datasets.
- Enterprise operators can evaluate the product without custom builds.

## Stage 7 - Parity Closure and Release Readiness
**Status: ✅ Delivered** — PARITY.md documents final matrix; cross-platform dist targets in Makefile.
### Goals
- Close the remaining gap list versus mRemoteNG.
- Improve areas where the new app should be better: startup time, portability, security, plugin ecosystem, and maintainability.
- Finalize installers, signing, documentation, and release process.

### Exit Criteria
- Published parity matrix shows same or better capability for the targeted release scope.
- Cross-platform packages are releasable.
- User documentation and migration guides are complete.
