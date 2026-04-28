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

- [ ] **Regression test for the `attachSessionInto` ↔ `st.run()` race** that
  caused blank SSH terminals (commit `a2e83bd`). Lift `content()` priming into
  a unit test that asserts `st.term != nil` before `run()` inspects it. Without
  a guard, a future refactor can re-introduce the race.
- [ ] **Replace the 150 ms / 500 ms sleeps in `restoreWorkspace`** with a
  completion-counted barrier (channel or `WaitGroup` per pending session). The
  current timing assumes session opens finish in <500 ms; on slow hosts the
  pane regroup runs before the tab exists and the layout is silently dropped.
  Add a focused test that drives `restorePaneLayouts` with deterministic
  timing.
- [ ] **Snapshot integrity test for `PaneLayouts`**: round-trip through
  `persistWorkspace` + `restorePaneLayouts` against a stub `sessionRegistry`
  to confirm a 3+ pane tree survives shutdown and that single-leaf groups are
  omitted (avoids the `len(connIDs) < 2` silent skip).
- [ ] **Audit other `fyne.Do(...)` + immediate goroutine-read patterns** in
  `cmd/desktop/gui_fyne.go` for the same race shape (e.g. `reattachToMain`
  followed by a goroutine reading `st.tabItem`). One was the SSH bug; there
  may be siblings.
- [ ] **Add a `-tags=racegui` headless smoke that drives a fake session through
  open → split → detach → reattach → close** so the pane-tree refactor has a
  living reference test outside the GUI loop.

## 2. Documented parity gaps (P1)

From `PARITY.md`. Each row is a 🟡/🔶 marker that the matrix already promises
to close.

### 2.1 Connection management (`PARITY.md §4.1`)
- [ ] **Multi-select + bulk edit** in the connection tree: shift/ctrl-click
  selection, "Edit selected" form that applies a diff to the chosen subset,
  bulk move/duplicate/delete.
- [ ] **Favorites surface**: the data model already persists a favorite flag;
  expose a "Favorites" virtual folder and a star toggle in the row chrome.
- [ ] **Recents view**: render `internal/app` recents in a side panel /
  command-palette section (currently tracked, not visible).
- [ ] **Environment grouping UI**: tags exist; add an "Environment" facet
  (prod/staging/dev) with quick filters in the toolbar.

### 2.2 Session UX (`§4.2`)
- [ ] **Per-session icon picker** in the connection editor (color + label are
  already wired). Bundle a small icon set; allow custom-image upload that
  is stored next to the workspace.
- [ ] **Reconnect-with-prompt flow** for sessions that disconnected on auth
  failure — today the user has to delete and re-open the tab.
- [ ] **Drag-to-reorder tabs** (Fyne `DocTabs` supports re-arrange via mouse;
  verify and wire keyboard shortcuts for it).

### 2.3 Cross-platform polish (`§4.7`)
- [ ] **Tray-icon integration**: minimize-to-tray, recent connections in the
  tray menu, "quit" / "show window". Use `fyne.io/systray` (already a
  transitive dep in many Fyne stacks; otherwise a thin platform-specific
  wrapper in `internal/platform`).
- [ ] **Screen-reader audit**: walk every dialog with the platform AT
  inspector, verify accessible names on icon-only buttons (right-click
  context menu, pane close button, drag handles).
- [ ] **Keyboard accelerators for split panes**: `Ctrl+\` / `Ctrl+-` to split
  the active pane; `Ctrl+W` already closes — make it close the active leaf,
  not the tab, when the tab is multi-pane.

### 2.4 Configuration & data (`§4.5`)
- [ ] **External storage backends — SQL / Git sync** (🔶 Planned). Sketch:
  `app/storage` interface; built-in JSON store remains default; add `sqlite`
  driver and a `git` driver that commits the workspace JSON to a configured
  remote on save.
- [ ] **Per-workspace overlay** so multiple "profiles" can share a connection
  inventory but have their own open-tab state.

### 2.5 Security & distribution (`§4.6`, `§5.5`)
- [ ] **Signed installers** (🔶 Planned).
  - Windows: Authenticode signing in `release.yml`; produce an MSI via
    `wixtoolset` (or msix). Today we ship a zip + `.bat` launcher.
  - macOS: codesign + notarization; produce a `.dmg` with proper
    `Info.plist` and `LSUIElement` settings for the dock icon.
  - Linux: `.deb` and `.rpm` (or AppImage) with detached `.sig` signatures.
- [ ] **Auto-update** (opt-in) — verify the installer signature before
  installing; respect enterprise "no auto-update" policy.
- [ ] **Plugin signing UX**: the verifier is in `sdk/plugin.Verifier`; add a
  Settings page that lists trusted keys, plus an "import key" / "trust this
  plugin once" flow when an unsigned plugin is loaded under permissive policy.

## 3. Protocol breadth & quality (P1/P2)

- [ ] **PowerShell on Windows** currently uses stdin/stdout pipes (no PTY,
  no resize). Investigate `ConPTY` (`golang.org/x/sys/windows`
  `CreatePseudoConsole`) so Windows hosts get the same UX as Unix.
- [ ] **Pure-Go RFB client** (🔶 Experimental). Either complete it for the
  "no external `vncviewer`" portable case, or formally drop the experimental
  scaffolding and shrink the binary.
- [ ] **HTTP/HTTPS — embedded WebView**: today it shells out to a system
  browser (acceptable per `PARITY.md` notes). Consider an opt-in embedded
  view via `webview/webview` for tabs-inside-tabs, gated behind a build tag
  so the default ships without a CGO browser dep.
- [ ] **SFTP browser tab** when an SSH session is open (file-tree pane that
  reuses the session's transport). Common request from mRemoteNG users.
- [ ] **Serial / COM port protocol** (mRemoteNG ships it via PuTTY). Low
  priority but recurring user request.

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
