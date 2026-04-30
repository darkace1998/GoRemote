# Logs & diagnostics

GoRemote writes structured logs (Go's `log/slog`) to a per-OS file
sink with rotation, and surfaces them through an in-app **Log
viewer** plus a **Diagnostics** dialog that builds a self-contained
support bundle.

> _Screenshot pending — see [contributing-screenshots.md](./contributing-screenshots.md). Suggested filename: `logs-viewer.png`._

## Log file location

| OS | Default location |
|---|---|
| Linux | `~/.cache/goremote/logs/goremote.log` (rotated as `.log.1`) |
| macOS | `~/Library/Caches/goremote/logs/goremote.log` |
| Windows | `%LOCALAPPDATA%\goremote\logs\goremote.log` |

The exact path is computed via `os.UserCacheDir()` (see
[`internal/platform/paths.go`](../../internal/platform/paths.go)). If
your environment does not provide a cache dir, GoRemote logs to
stderr only.

## Redaction

The logging package applies **secret redaction** to every record
before it is written. Specifically:

* Known secret-bearing fields (`password`, `token`, `secret`, `api_key`,
  `passphrase`, etc.) are replaced with `***`.
* Auth-bearing URL fragments (`https://user:pass@host/…`) are
  rewritten to drop the user-info portion.
* Structured `slog.Attr` values declared as secret are dropped before
  serialisation.

The redaction logic lives in
[`internal/logging`](../../internal/logging/); review it if you need
to add a new sensitive field type.

## In-app log viewer

Click the toolbar icon whose tooltip reads `"View logs…"`. The viewer
shows the tail of the active log file (256 KiB) with three actions:

| Tooltip | What it does |
|---|---|
| `Reload the tail of the log file` | Re-read the file from disk. |
| `Copy the visible log to the clipboard` | Copies the visible content. |
| `Open the log folder in your file manager` | Opens the log directory in the OS file manager. |

If no log file is configured (stderr-only run), the viewer says so.

## Diagnostics bundle

Click the toolbar icon whose tooltip reads `"Diagnostics"`. GoRemote
collects a zip archive containing:

* A version + build-info header.
* The settings JSON (with secret-bearing fields redacted).
* The workspace JSON.
* The log file (and the previous rotation, if any), capped at 2 MiB
  tail per file.
* The plugin root listing (manifests only — no plugin binaries).
* Notes about anything the bundler had to skip.

Pick a destination zip path; the bundle is written atomically. Attach
it to bug reports.

## Crash dumps

When GoRemote panics, the crash handler writes a dump to
`<state>/crashes/` (per-OS path computed via `os.UserConfigDir()`).
Crash dumps are local-only — nothing is uploaded. Disable the
behaviour entirely by toggling **Crash reports disabled** in Settings.

## Related buttons

| Tooltip | What it does |
|---|---|
| `View logs…` | Open the in-app log viewer. |
| `Reload the tail of the log file` | Re-read the log file. |
| `Copy the visible log to the clipboard` | Copy log to clipboard. |
| `Open the log folder in your file manager` | Reveal the log directory. |
| `Diagnostics` | Build a support bundle zip. |
