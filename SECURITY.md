# Security policy

This document summarizes the goremote security posture. See `requirements.md` §4.6 for the full requirement set.

## Reporting a vulnerability

Please **do not** open a public issue for security reports. Email the maintainers (see `MAINTAINERS` — to be populated before release) or use GitHub's private vulnerability reporting when the repository is public. We aim to acknowledge reports within three business days.

Include:

- A description of the issue and its impact.
- A minimal reproducer (configuration, commands, sample input).
- The affected version / commit.

## Threat model (summary)

Attackers of concern:

1. **Local unprivileged users** reading configuration / cache files on a shared machine.
2. **Malicious plugins** (installed by a user who did not fully audit them).
3. **Network adversaries** between the user and remote endpoints.
4. **Compromised host processes** attempting to read credential material from memory.

Out of scope:

- A root/administrator adversary on the user's machine.
- Side-channel attacks against the underlying crypto libraries (we defer to the Go standard library and `golang.org/x/crypto`).

## Credential handling

- Connections persist only **credential references** (provider ID + opaque key). Raw secrets are never serialized to disk by the app core.
- The encrypted-file provider uses **Argon2id** (t=1, m=64 MiB, p=4) for key derivation and **AES-256-GCM** for encryption. The file header (version + KDF parameters + salt + nonce) is bound as additional authenticated data so tampering is detected.
- The OS-keychain provider delegates to the platform's protected storage via `zalando/go-keyring` (Secret Service on Linux, Keychain on macOS, Credential Manager on Windows).
- Credential **material** is represented by `credential.Material`, which exposes `Zeroize()`. The session manager zeroizes material as soon as a session has consumed it.
- The logging wrapper in `internal/logging` redacts a known set of field names (`password`, `passphrase`, `token`, `secret`, `api_key`, etc.) before emission.

## Plugin isolation

- Every plugin advertises a **manifest** with capabilities (`network.outbound`, `ui.terminal`, `fs.read`, `fs.write`, `clipboard`, `os.exec`, …) and a **trust** label. The host enforces capability grants at the call site.
- Built-in plugins run in-process. External plugins are expected to run **out-of-process** over IPC (see `host/plugin/ipc.go`); Go's native `plugin` package is not used because it is incompatible with cross-platform and reproducible builds.
- The host wraps plugin calls in panic-recovering shims and publishes `EventCrashed` on failure.
- The credential host tracks failures over a rolling window and **auto-quarantines** providers that exceed a configurable threshold. Quarantine auto-expires; operators may reinstate providers explicitly.
- API-version compatibility is enforced on registration; a plugin declaring a different major version of the SDK is rejected.

## Transport security

- SSH defaults to strict host-key checking. `accept-new` semantics (file-locked on unix) and `off` are opt-in and clearly flagged.
- Telnet, Rlogin, and raw TCP are unencrypted by nature and are surfaced as such in the UI. No credentials should be entered into these protocols; the credential host does not block them, but the protocol plugins never transmit material that was not explicitly provided.

## Dependency policy

- Direct dependencies are pinned in `go.mod` and checked into `go.sum`.
- CI verifies `go mod verify` and runs `golangci-lint`. Supply-chain scanning (govulncheck) will be added before the first tagged release.

## Coordinated disclosure

We will coordinate fixes and publish a GitHub Security Advisory once a patch is available. Credit is offered to the reporter upon request.

## Tooling

The repository ships a security-tooling baseline that runs both locally and in CI.

- **CI workflows** (`.github/workflows/`):
  - `ci.yml` — Go build/test/vet on Linux, macOS and Windows (Go 1.26.2).
  - `webui-ci.yml` — typecheck, test, build for the React/Vite frontend.
  - `lint.yml` — `golangci-lint` (config in `.golangci.yml`).
  - `security.yml` — `govulncheck`, `gosec` (SARIF), `gitleaks`, `trivy fs`, and `npm audit`. Scheduled weekly in addition to push/PR.
  - `codeql.yml` — CodeQL analysis for `go` and GitHub Actions.
- **Make targets**: `build`, `test`, `lint`, `vuln`, `sec`, `audit` (= lint + vuln + sec), `webui-build`, `webui-test`, `all`.
- **Dependabot** (`.github/dependabot.yml`) covers `gomod`, `github-actions`, and the `webui` npm tree on a weekly cadence.

Findings reported by these tools must not be silenced by default; suppressions require an explicit `//nolint:<rule>` (Go) or `# trivy:ignore` comment with rationale.

## Internal Security Audit (automated review)

2026-04-25

The following review pass was conducted using ripgrep over the Go tree plus `govulncheck` and `gosec`. Findings are listed for triage; remediation is intentionally **out of scope** for the tooling task and tracked separately. Severity tags follow gosec/govulncheck conventions.

### Findings

- [HIGH] toolchain — `govulncheck` reports the project is built against an out-of-date Go standard library and is reachable on standard-library vulnerabilities. _Remediation:_ keep the repository toolchain and CI pinned to the latest patched Go release in active use.
- [HIGH] `plugins/protocol-http/session.go:130` (G402) — `tls.Config{InsecureSkipVerify: !verifyTLS}` is reachable when the user disables TLS verification on an HTTP probe. _Already annotated `//nolint:gosec`._ _Remediation:_ keep the existing UI warning, ensure verifyTLS defaults to `true`, and surface the relaxed mode in connection telemetry.
- [HIGH] `plugins/protocol-ssh/module.go:444` (G106) — `ssh.InsecureIgnoreHostKey()` returned when `strict == StrictOff`. _Already annotated `//nolint:gosec` and gated by an explicit user setting (see `SECURITY.md` "Transport security")._ _Remediation:_ verify the UI never lets `StrictOff` be the default and add an event-bus warning every time it is selected.
- [HIGH] `plugins/protocol-ssh/module.go:407` (G704) — SSH client config consumes user-tainted `KnownHostsPath` / identity paths. _Remediation:_ restrict to paths under the user config dir or explicitly chosen via the file picker; reject `..` traversal.
- [MEDIUM] `internal/persistence/backup.go:266` (G110) — `io.Copy(out, rc)` from a zip entry with no size cap (decompression bomb). _Remediation:_ wrap the source reader in `io.LimitReader` using `f.UncompressedSize64` plus a sanity ceiling, or stream into a `CountingWriter` and abort over a configured limit.
- [MEDIUM] `internal/persistence/backup.go:122` (G122) — `filepath.Walk` callback opens files via the raw walked path; race-prone if the tree mutates during walk. _Remediation:_ open the file from a `*os.Root` rooted at the backup target, or use `securejoin` to validate paths before opening.
- [MEDIUM] `internal/persistence/backup.go:262` (G302) — files written with mode `0644`. _Remediation:_ tighten to `0600` for credential-adjacent artifacts; `0644` is acceptable only for non-secret payloads (document which).
- [MEDIUM] `internal/persistence/store.go:144,199`, `internal/persistence/atomic.go:27`, `internal/persistence/backup.go:44,252,254` (G301) — directories created with `0o755`. _Remediation:_ use `0o700` (or `0o750` at minimum) for any directory that holds workspace, credential, or backup data.
- [MEDIUM] `internal/persistence/atomic.go:103` — `io.ReadAll(f)` on a config file with no upper bound. _Remediation:_ enforce a max file size (e.g. 16 MiB) via `io.LimitReader` to bound memory if the file is corrupted or hostile.
- [MEDIUM] `plugins/credential-keychain/provider.go:258` (G117) — JSON-marshaled struct exposes a field named `Password` to disk. The provider already wraps the secret in `Material.Zeroize()`, but the on-disk JSON envelope retains the field name. _Remediation:_ rename the JSON key to a neutral term (`material`, `value`) and document that the keychain backend stores opaque blobs.
- [MEDIUM] `plugins/protocol-powershell/session_unix.go:44`, `plugins/protocol-http/module.go:219`, `plugins/credential-bitwarden/provider.go:29`, `plugins/credential-1password/runner.go:31`, `internal/extlaunch/extlaunch.go:183`, `internal/platform/notifier_*.go` (G204) — `exec.Command(<variable>)` with user-influenced arguments. _Remediation:_ document the trust boundary in each module (binary path resolved by `discover.go` / explicit user-launcher setting), and pin the executable via an absolute path looked up once at startup with `exec.LookPath` validation.
- [MEDIUM] `plugins/credential-file/provider.go:302,325`, `plugins/credential-keychain/index.go:38,68,90`, `plugins/protocol-ssh/module.go:460,506`, `internal/persistence/{atomic,backup}.go`, `app/{settings,workspace}/store.go`, `cmd/desktop/main.go:223` (G304) — `os.Open(<variable>)` / `os.OpenFile(<variable>)` with paths that originate from user config. _Remediation:_ centralize path joining via a helper that calls `filepath.Clean` and refuses paths escaping the user's config root via `..`.

### Non-findings

- **Credential leakage in logs** — searched `(log|Print|Errorf|Infof|Debugf|Warnf).*\b(password|passphrase|secret|token|api_?key|credential)\b`. All hits are `%w`-wrapped error strings (e.g. `fmt.Errorf("vnc: write password file: %w", err)`); no secret material is interpolated. The redacting wrapper in `internal/logging` is in force for structured fields.
- **`fmt.Print*` debug calls near credential code** — none found outside `_test.go`.
- **`math/rand` usage for security-sensitive code** — `math/rand` and `math/rand/v2` are not imported anywhere in the tree. `crypto/rand` is used in `plugins/credential-file/{format,provider}.go` for KDF salt and AES-GCM nonces.
- **Hardcoded secrets / API keys / sample creds** — `gosec` G101 hits at `internal/domain/inheritance.go:18` (a `Field` constant string `"username"`) and `internal/app/events.go:23` (an `EventKind` constant) are **false positives** triggered by the literal substrings, not real credentials. No string matching `(api[_-]?key|secret|password|token)\s*=\s*"<value>"` was found in source.
- **Unauthenticated unix-socket binds** — no `net.Listen("unix", …)` calls present in the tree (the IPC plumbing in `host/plugin/ipc.go` uses transports added by sibling agents). Existing files written with `0600` mode (settings, workspace, VNC password file) chmod correctly.
- **`crypto/rand` vs `math/rand`** — exclusively `crypto/rand` for security paths; no `math/rand` usage to migrate.
- **Goroutines lacking context cancellation** — every `go func(){…}` reviewed in `internal/extlaunch`, `internal/app/sessions.go`, `plugins/protocol-*` is paired with an explicit `<-ctx.Done()` arm, a `cmd.Wait()` channel, or a tied `Close()` lifecycle. No leaks identified during this pass.

### Summary

- **Total gosec issues:** 52 (15 HIGH, 31 MEDIUM, 6 LOW; 10 of the HIGH are G115 integer-conversion warnings concentrated in generated `*.pb.go` and `syscall.SIGWINCH` plumbing — accepted as low risk).
- **Total govulncheck issues:** reachable stdlib advisories are driven by the Go toolchain version; additional unreachable findings may exist in imported packages.
- **Critical immediate action:** keep the Go toolchain pinned to a patched release; the rest are tracked above for the protocol/persistence owners.
