# goremote

A Go-based, cross-platform successor to **mRemoteNG**: a modern multi-protocol remote-connection manager with a modular protocol/credential plugin model and a clean separation between UI, application core, and protocol/credential implementations.

> **Status:** Pre-1.0; foundational protocols + credentials operational. See [PARITY.md](PARITY.md) for the full feature matrix.

## Highlights

- 🖥️ **Cross-platform desktop** — Windows, Linux, macOS via **Fyne v2** (pure-Go, OpenGL-rendered native UI; no browser runtime required).
- 🔌 **Modular protocols** — SSH, SFTP, Telnet, Rlogin, Raw socket, PowerShell, HTTP, Serial / COM, plus RDP / VNC / TN5250 / MOSH via the external-launcher model. See `plugins/`.
- 🔐 **Pluggable credentials** — OS keychain, encrypted file vault (AES-256-GCM + Argon2id), 1Password (`op`), Bitwarden (`bw`).
- 📥 **Trustworthy migration** — streaming XML + CSV importer for mRemoteNG data with explicit warnings instead of silent drops.
- 🧱 **Stable plugin contract** — versioned SDK in `sdk/`; out-of-process plugin reference implementation in `host/plugin/ipc/`.
- 🔒 **Security-aware by default** — capability-declared manifests, structured logging with secret redaction, no plaintext secrets at rest, plugin quarantine on repeated failures.

## Repository layout

```
sdk/                    Stable public contracts for plugins (plugin, protocol, credential)
host/                   Plugin / protocol / credential hosts (lifecycle, capability enforcement, IPC)
internal/
  app/                  Application core: command dispatcher, session manager, debounced persister
  domain/               Connection / folder tree, inheritance resolver, search
  persistence/          Atomic store, migrator, integrity validator, zip backup/restore
  eventbus/             Generic context-scoped Bus[T]
  platform/             OS-specific paths, keychain, clipboard, notifier
  logging/              slog wrapper with secret redaction (file sink + rotation)
  import/mremoteng/     XML + CSV importer with warnings
  extlaunch/            External-launcher session helper (RDP / VNC / TN5250 / MOSH)
app/                    App-level features: settings, workspace persistence, update,
                        diagnostics, marketplace, extplugin loader, git-sync
plugins/
  protocol-{ssh,sftp,telnet,rlogin,rawsocket,serial}/   Real terminal protocols
  protocol-{powershell,http}/                           Real launcher / browser protocols
  protocol-{rdp,vnc,tn5250,mosh}/                       External-launcher protocols
  credential-{file,keychain,1password,bitwarden}/       Credential providers
  external-example/                                     Reference out-of-process plugin
proto/plugin/v1/        Length-prefixed JSON IPC frame + message types for the
                        out-of-process plugin transport
cmd/desktop/            Fyne v2 entry point (gui_fyne.go + fyne_session.go)
cmd/sign-manifest/      Release helper that signs auto-update manifests (Ed25519)
installers/             Windows WiX MSI sources (and platform packaging stubs)
test/integration/       Cross-cutting harness
docs/screenshots/       Reserved for screenshots once the UI stabilizes
.github/workflows/      CI matrix (linux/macos/windows) + supply-chain checks
```

## Prerequisites

- **Go 1.26.2+** (pinned via the `go.mod` toolchain directive).
- For desktop builds:
  - Linux: `libgl1-mesa-dev xorg-dev` (OpenGL + X11 headers for Fyne).
  - macOS: Xcode command-line tools.
  - Windows (cross-compiling from Linux): `gcc-mingw-w64-x86-64`.
  - Windows (native build): any C compiler; no extra SDK required.
- Optional, only when used: `xfreerdp` / `mstsc`, `vncviewer`, `tn5250`, `mosh`, `op`, `bw`.

## Install / build / run

The repository ships a `Makefile` that wraps the canonical commands.

```bash
# Build everything except the desktop GUI when Linux OpenGL/X11 headers are missing
make build

# Build the desktop GUI explicitly (requires native desktop headers)
make build-desktop

# Run the test suite except cmd/desktop when Linux OpenGL/X11 headers are missing
make test

# Run the desktop package tests explicitly (requires native desktop headers)
make test-desktop

# Static checks and supply-chain audit
make vet             # go vet ./...
make lint            # golangci-lint run ./...
make vuln            # govulncheck (skips cmd/desktop on Linux when GUI headers are missing)
make sec             # gosec (same desktop-package skip behavior)
make audit           # lint + govulncheck + gosec

# Combined gauntlet (mirrors CI)
make all             # build + test + audit

# Maintenance
make tidy            # go mod tidy
make clean           # remove bin / dist / build
```

### Quick demo

```bash
make build-desktop
go run ./cmd/desktop
```

On Linux, `make build`, `make test`, `make vuln`, and `make sec` automatically skip `cmd/desktop` when `pkg-config` cannot find the Fyne native prerequisites (`libgl1-mesa-dev` and `xorg-dev`). Install those packages and use `make build-desktop` / `make test-desktop` to validate the GUI package directly.

The Fyne window opens immediately — no separate frontend build step required.

### Distribution builds

```bash
# Linux (native)
make dist-linux

# macOS
make dist-darwin          # amd64
make dist-darwin-arm64    # Apple Silicon

# Windows — cross-compile from Linux
apt install gcc-mingw-w64-x86-64
make dist-windows         # uses CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc internally
```

## Documentation

- **[PARITY.md](PARITY.md)** — feature parity matrix vs mRemoteNG, including the external-launcher model for RDP / VNC / TN5250.
- **[CONTRIBUTING.md](CONTRIBUTING.md)** — dev workflow, plugin contracts, code review checklist.
- **[SECURITY.md](SECURITY.md)** — threat model, credential handling guarantees, vulnerability reporting.
- **[requirements.md](requirements.md)** — product and non-functional requirements.
- **[architecture.md](architecture.md)** — target system architecture and boundaries.
- **[stages.md](stages.md)** — delivery sequencing.

## License

TBD.
