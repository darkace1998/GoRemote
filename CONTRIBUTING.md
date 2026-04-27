# Contributing to goremote

Thanks for your interest. goremote is a clean-slate Go successor to mRemoteNG focused on cross-platform parity, a disciplined plugin contract, and safe credential handling.

## Ground rules

- Changes must preserve the layered boundaries described in `architecture.md`. Protocol-specific logic never leaks into `internal/app`, `internal/domain`, or the UI.
- The SDK (`sdk/`) is a public contract. Any breaking change requires a major-version bump of `plugin.CurrentAPIVersion` and coordinated changes to hosts and built-in plugins.
- Never persist raw secrets. Connections store credential **references**; material is resolved on demand and zeroed after use (`credential.Material.Zeroize`).
- Prefer explicit warnings over silent data loss. The mRemoteNG importer demonstrates the pattern: unknown attributes and unsupported inheritance flags are recorded as `ImportWarning`s, not dropped.
- Use `context.Context` for cancellation, timeouts, and request scoping in all long-running or external operations.

## Getting started

1. Install Go 1.25+.
2. Linux desktop builds additionally require `libgl1-mesa-dev xorg-dev` (Fyne/OpenGL).
3. Clone the repo.
4. The `Makefile` wraps the canonical dev workflow:
   - `make build` — `go build ./...`
   - `make test` — `go test -race ./...`
   - `make vet` — `go vet ./...`
   - `make lint` — `golangci-lint run ./...`
   - `make audit` — lint + `govulncheck` + `gosec`
   - `make all` — full gauntlet (build, test, audit)
   - `make tidy` — `go mod tidy`
5. All of `make build` and `make test` must pass before opening a PR.

## Coding standards

- Format with `gofmt -s` (CI enforces). `golangci-lint run` must pass using the project config in `.golangci.yml`.
- Tests use the standard `testing` package. Prefer table-driven tests; mark slow tests with `testing.Short`.
- Exported identifiers require doc comments. Internal packages should still document non-obvious invariants.
- No `TODO` without a tracking issue reference.

## Plugin contracts

The stable plugin contracts live in `sdk/`:

- `sdk/plugin` — manifest schema, capabilities, status enum, API version.
- `sdk/protocol` — `Module` and `Session` interfaces for protocol plugins.
- `sdk/credential` — `Provider` / `Writer` interfaces and the `Material` type with `Zeroize`.

External (out-of-process) plugins must use the IPC reference implementation at `host/plugin/ipc/` (gRPC/Connect over named pipes or Unix sockets). Do **not** depend on Go's native `plugin` package — see the rationale in `architecture.md`. A worked example is in `plugins/external-example/`.

## Adding a new protocol plugin

1. Create `plugins/protocol-<name>/` with the uniform layout: `manifest.go`, `module.go`, `session.go`, `*_test.go`, and a `TODO.md` if any surface is deferred.
2. Implement `sdk/protocol.Module` and `sdk/protocol.Session`.
3. Advertise required capabilities in the manifest (`network.outbound`, `ui.terminal`, `fs.read`, etc.). The host will enforce them.
4. Tests must cover: successful open, context cancellation, idempotent close, malformed input, and any protocol-specific negotiation.
5. Update `PARITY.md`.

## Adding a new credential provider

1. Create `plugins/credential-<name>/`.
2. Implement `sdk/credential.Provider` (and `Writer` if mutable).
3. Resolve methods must return a `*Material` whose `Zeroize()` wipes underlying buffers.
4. Cover lock / unlock semantics and concurrent callers in tests.
5. Update `PARITY.md`.

## Commit messages

Use conventional commits (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`). Reference the stage from `stages.md` in the body when relevant.

## Code review checklist

- [ ] Tests pass with `-race`.
- [ ] `go vet` and `golangci-lint` clean.
- [ ] No new direct dependency on a protocol-specific package from the UI or app core.
- [ ] No credentials logged (the `internal/logging` wrapper redacts known keys — verify any new field names).
- [ ] Public API changes are documented.
