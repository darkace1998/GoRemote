# goremote

A Go-based, cross-platform successor to **mRemoteNG**: a modern multi-protocol remote-connection manager with a modular protocol/credential plugin model and a clean separation between UI, application core, and protocol/credential implementations.

> **Status:** Pre-1.0; foundational protocols + credentials operational. See [PARITY.md](PARITY.md) for the full feature matrix.

## Highlights

- 🖥️ **Cross-platform desktop** — Windows, Linux, macOS via **Fyne v2** (pure-Go, OpenGL-rendered native UI; no browser runtime required).
- 🔌 **Modular protocols** — SSH, Telnet, Rlogin, Raw socket, PowerShell, HTTP, plus RDP / VNC / TN5250 via the external-launcher model. See `plugins/`.
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
  appcore/              Higher-level orchestration helpers
  domain/               Connection / folder tree, inheritance resolver, search
  persistence/          Atomic store, migrator, integrity validator, zip backup/restore
  eventbus/             Generic context-scoped Bus[T]
  platform/             OS-specific paths, keychain, clipboard, notifier
  logging/              slog wrapper with secret redaction
  import/mremoteng/     XML + CSV importer with warnings
  extlaunch/            External-launcher session helper (RDP / VNC / TN5250)
  session/              Session lifecycle primitives
app/                    App-level features (settings, workspace persistence)
plugins/
  protocol-{ssh,telnet,rlogin,rawsocket}/         Real terminal protocols
  protocol-{powershell,http}/                     Real launcher / browser protocols
  protocol-{rdp,vnc,tn5250}/                      External-launcher protocols
  credential-{file,keychain,1password,bitwarden}/ Credential providers
  external-example/                               Reference out-of-process plugin
proto/plugin/v1/        gRPC / Connect IDL for the IPC plugin transport
cmd/desktop/            Fyne v2 entry point (gui_fyne.go + fyne_session.go)
webui/                  Archived React/TypeScript UI (not active; kept for reference)
test/integration/       Cross-cutting harness
docs/screenshots/       Reserved for screenshots once the UI stabilizes
.github/workflows/      CI matrix (linux/macos/windows) + supply-chain checks
```

## Prerequisites

- **Go 1.25+** (enforced by the `go.mod` toolchain directive).
- For desktop builds:
  - Linux: `libgl1-mesa-dev xorg-dev` (OpenGL + X11 headers for Fyne).
  - macOS: Xcode command-line tools.
  - Windows (cross-compiling from Linux): `gcc-mingw-w64-x86-64`.
  - Windows (native build): any C compiler; no extra SDK required.
- Optional, only when used: `xfreerdp` / `mstsc`, `vncviewer`, `tn5250`, `mosh`, `op`, `bw`.

## Install / build / run

The repository ships a `Makefile` that wraps the canonical commands.

```bash
# Build everything
make build           # go build ./...

# Run the full test suite
make test            # go test -race ./...

# Static checks and supply-chain audit
make vet             # go vet ./...
make lint            # golangci-lint run ./...
make audit           # lint + govulncheck + gosec

# Combined gauntlet (mirrors CI)
make all             # build + test + audit

# Maintenance
make tidy            # go mod tidy
make clean           # remove bin / dist / build
```

### Quick demo

```bash
make build
go run ./cmd/desktop
```

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
