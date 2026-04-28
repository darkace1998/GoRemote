# GoRemote — TODO

A living backlog derived from a deep audit of `requirements.md`, `architecture.md`,
`stages.md`, `PARITY.md`, the source tree, and recent commit history. Items are
grouped by intent and ordered roughly by user-visible impact within each group.

> Legend: **P0** = ship-blocker / regression risk · **P1** = closes a documented
> partial / planned parity gap · **P2** = polish · **P3** = stretch goals from
> `requirements.md §8`.

---

## 1. Stability & regression hardening (P0)

These exist because of bugs surfaced in recent sessions and now have no
automated coverage to prevent recurrence.

- [x] **Regression test for the `attachSessionInto` ↔ `st.run()` race** that
  caused blank SSH terminals (commit `a2e83bd`). Covered indirectly via
  `cmd/desktop/session_registry_test.go` (addedHook handshake) and the
  pre-`fyne.Do` `content()` priming in `attachSessionInto`. A direct
  `content()` idempotency test would require a Fyne app harness; deferred.
- [x] **Replace the 150 ms / 500 ms sleeps in `restoreWorkspace`** with a
  completion-counted barrier driven by `sessionRegistry.addedHook`. Layouts
  now wait for every reopened session's `add()` (with a 15 s safety
  timeout) before applying.
- [x] **Snapshot integrity test for `PaneLayouts`** via
  `cmd/desktop/pane_tree_test.go::TestSnapshotPaneTreeMatchesShape`
  (combined with the existing `app/workspace.TestPaneLayoutRoundTrip`).
- [x] **Audit other `fyne.Do(...)` + immediate goroutine-read patterns** in
  `cmd/desktop/gui_fyne.go`. All other call sites are fire-and-forget
  (dialogs, label/text updates, tab removes, watcher-driven cleanups);
  the SSH race had no siblings.
- [ ] **Add a `-tags=racegui` headless smoke that drives a fake session through
  open → split → detach → reattach → close** so the pane-tree refactor has a
  living reference test outside the GUI loop.

## 2. Documented parity gaps (P1)

From `PARITY.md`. Each row is a 🟡/🔶 marker that the matrix already promises
to close.

### 2.1 Connection management (`PARITY.md §4.1`)
- [x] **Multi-select + bulk delete/move** in the connection tree: a "✓"
  toolbar button adds the highlighted node to a bulk-edit set (rendered
  with a check glyph in the row); "Bulk move" reparents every member
  under a folder picked from a `collectFolderChoices` enumeration;
  "Bulk delete" force-closes any active sessions for the targets and
  deletes them after a single confirmation. A status label (`✓ N`)
  next to the tree toolbar shows the current count. Bulk *edit* (a
  diff-style multi-row form) intentionally remains out of scope — most
  users want bulk move/delete and that ships now.
- [x] **Favorites surface** — `domain.ConnectionNode` gained a `Favorite`
  field, `App.ToggleFavorite`/`ListFavorites` commands publish updates,
  treeRow paints a yellow ★ next to favorited connections, the right-click
  menu has Add/Remove favorites, and the toolbar has a Favorites picker.
- [x] **Recents view** — `workspace.Recents` (bounded ring, MaxRecents=20)
  is touched on every `OpenSession`/`OpenSessionWithPassword`; a toolbar
  button and tray submenu both expose the list.
- [x] **Environment grouping UI** — toolbar Select above the search field
  enumerates distinct `Environment` values from the tree and filters
  visible nodes to that environment (folders survive while any descendant
  passes).

### 2.2 Session UX (`§4.2`)
- [x] **Per-session icon picker** — connection editor exposes Icon
  (preset list: server/database/terminal/cloud/router/firewall/docker/
  kubernetes/laptop/desktop) and Color, both threaded through
  `ConnectionPatch`.
- [x] **Reconnect-with-prompt flow** — `openSession` detects auth-style
  failures (permission denied, publickey, keyboard-interactive, etc.)
  and offers an inline retry that re-opens the password prompt.
- [x] **Tab reorder** — `Ctrl+Shift+PageUp`/`PageDown` swap the active
  tab with its neighbour. `persistWorkspace` snapshots `DocTabs.Items`
  visual order under the registry lock and `sort.SliceStable`s the
  persisted `OpenTabs` slice so the user's arrangement survives
  restart. Native pointer-drag reorder still gated on Fyne adding an
  `OnReordered` callback to `DocTabs`.

### 2.3 Cross-platform polish (`§4.7`)
- [x] **Tray-icon integration** — `installSystemTray` wires Show/Quit
  plus a "Recent connections" submenu via `desktop.App` when the
  runtime supports it (Windows + most desktop Linux). Falls through
  silently on platforms without tray support.
- [ ] **Screen-reader audit**
  *Deferred — Fyne 2.7 has no public tooltip / accessible-label API on
  toolbar actions and tree rows, so a useful AT-inspector pass cannot
  produce changes here. Re-open when Fyne adds the surface.*
- [x] **Keyboard accelerators for split panes** — `Ctrl+Shift+\` splits
  the selected tree connection right; `Ctrl+Shift+-` splits below.
  Existing `Ctrl+W` continues to close the active session.

### 2.4 Configuration & data (`§4.5`)
- [x] **Git-sync storage backend** — `app/sync/git.go` shells out to the
  system `git` binary against the workspace dir. When
  `Settings.GitSyncEnabled` is true, every successful `SaveWorkspace`
  fires a best-effort commit-and-push in a background goroutine; the
  toolbar exposes a "Sync now" action for explicit pushes. First push
  auto-runs `--set-upstream`. Errors are logged at warn level only —
  sync failure must never block the primary save path.
- [ ] **SQL storage backend** (🔶 Planned). Still requires a SQLite driver
  dependency (no stdlib option); deferring until the team settles on
  modernc.org/sqlite vs mattn/go-sqlite3 and a migration story.
- [ ] **Per-workspace overlay** so multiple "profiles" can share a connection
  inventory but have their own open-tab state.

### 2.5 Security & distribution (`§4.6`, `§5.5`)
- [~] **Signed installers** — release infrastructure is in place; running
  it requires CI secrets that must be configured at the repo level (see
  `installers/windows/README.md`).
  - Windows: `release.yml` Authenticode-signs the `.exe`, builds an MSI
    via WiX 4 (`installers/windows/goremote.wxs`), and signs the MSI.
    Triggered on `WINDOWS_CERT_PFX_BASE64` + `WINDOWS_CERT_PASSWORD`
    secrets; falls through silently if absent so PR builds still pass.
  - macOS: codesign + notarization hooks documented but not yet wired
    (no Developer ID available in this environment).
  - Linux: `.deb`/`.rpm` packaging still TBD.
- [x] **Auto-update** (opt-in) — `app/update` verifies an Ed25519
  signature over the canonical payload `version|os|arch|sha256|url`
  *before* downloading; `SwapInPlace` handles Windows' running-binary
  lock by renaming the live exe to `.old` and cleaning it up at the
  next launch. `cmd/sign-manifest` is the release-side helper that
  fills in per-target signatures using `GOREMOTE_RELEASE_KEY`. UI:
  toolbar action plus Settings page (URL + base64 public key).
- [ ] **Plugin signing UX**: the verifier is in `sdk/plugin.Verifier`; add a
  Settings page that lists trusted keys, plus an "import key" / "trust this
  plugin once" flow when an unsigned plugin is loaded under permissive policy.

## 3. Protocol breadth & quality (P1/P2)

- [x] **PowerShell on Windows** now runs over **ConPTY**
  (`github.com/ActiveState/termtest/conpty`, already a transitive dep), so
  Windows hosts get the same full-VT terminal UX as Unix — including
  working `Resize` via `ResizePseudoConsole`. The legacy stdin/stdout-pipe
  fallback has been removed; Windows 10 1809+ is required (every
  supported Windows release).
- [x] **Pure-Go RFB client** — formally **dropped**. The "🔶 Experimental"
  row has been removed from `PARITY.md`; the supported VNC path remains
  the external `vncviewer` launcher. There was never any RFB code in
  `plugins/protocol-vnc/` to remove — only the aspiration.
- [ ] **SFTP browser tab** when an SSH session is open (file-tree pane that
  reuses the session's transport). Common request from mRemoteNG users.
  **Deferred-by-policy** — needs `github.com/pkg/sftp`, which is the
  pragmatic choice but a new external dep. Revisit once the no-external-deps
  rule is relaxed for SFTP specifically.
- [ ] **HTTP/HTTPS — embedded WebView**: today it shells out to a system
  browser (acceptable per `PARITY.md` notes). **Deferred-by-policy** —
  every realistic option (`webview/webview`, `wails`, CEF) brings a CGO
  browser-engine dep. The system-browser path stays the default.
- [ ] **Serial / COM port protocol** (mRemoteNG ships it via PuTTY). Low
  priority. **Deferred-by-policy** — needs `go.bug.st/serial` (no stdlib
  serial-port API exists).

## 4. Plugin / extensibility hardening (P2)

- [ ] **External plugin loader UI**: add Settings → "Plugins" with install /
  enable / disable / quarantine controls. The host APIs exist; this is just
  the missing surface.
- [ ] **Plugin marketplace bootstrap** (`requirements.md §8`). Even just a
  static JSON manifest hosted alongside releases, listing trusted-key
  fingerprints and download URLs, would unblock community plugins.
- [ ] **gRPC / Connect IPC stress test**: today `host/plugin/ipc` is exercised
  by `plugins/external-example` only. Add a chaos test that crashes the
  child mid-call and asserts the host quarantines the plugin without
  hanging the dispatcher.

## 5. Observability & diagnostics (P2)

- [ ] **In-app log viewer** (tail of `internal/logging` output, with the
  redaction rules applied). Useful for users who can't get to `%APPDATA%`
  on Windows.
- [ ] **Diagnostic bundle** command: zip up `workspace.json` (with secrets
  redacted), `settings.json`, last N MB of logs, plugin manifests, OS info.
  Drop in user-chosen location for support.
- [ ] **Crash-report opt-in** (`requirements.md §5.4`). Currently we don't
  ship one. Either wire `sentry-go` behind an explicit opt-in toggle, or
  just dump panic traces to a stable file path.

## 6. Stretch goals (P3 — `requirements.md §8`)

- [ ] **Shared team workspaces / sync** (overlaps with §2.4 SQL/Git backend).
- [ ] **Role-based credential / provider policies** (admin can pin which
  providers are usable for which folders).
- [ ] **Recorded sessions / audit trails**: store keystrokes + timestamps for
  terminal sessions; play back as `asciinema`. The `internal/logging` audit
  hooks can feed this.
- [ ] **Gateway / jump-host orchestration**: connection chains à la
  `~/.ssh/config ProxyJump`, surfaced in the editor as a multi-hop builder.
- [ ] **Enterprise policy packs** (lockdown bundle: disable export, enforce
  vault provider, mandatory tags).

## 7. Housekeeping

- [ ] Populate `docs/screenshots/` (placeholder README only). Helps the
  README and any future plugin marketplace listings.
- [ ] `Makefile`: add `make run` (currently you have to read the build line
  out of `dist-linux`).
- [ ] Bump `Version` injection — there's drift between `1.11.0`, `1.11.1`
  and the tagless main commit. Tag a release after the next P0/P1 batch.
- [ ] Trim `webui/` (archived React shell) into its own historical branch so
  the main tree stops carrying the dead JS/TS toolchain.

---

## How to triage

1. **Start with §1** — every item there protects shipping work that's already
   in users' hands.
2. **Then §2** in order — each row already has a documented promise in
   `PARITY.md` and a clear acceptance bar.
3. **§3 + §4** are independent and can run in parallel by separate workstreams.
4. **§5** is small enough to fold into §1/§2 PRs as opportunity allows.
5. **§6** is the post-1.0 roadmap; resist starting it until §1–§3 are clean.
