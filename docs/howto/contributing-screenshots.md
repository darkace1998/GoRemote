# Contributing screenshots

The how-to pages under `docs/howto/` reference screenshots by filename.
Most slots are currently empty because the screenshots must be taken
on a real GoRemote build (they cannot be generated headlessly without
misrepresenting the UI). This page is for contributors who can run
the desktop build and want to fill in the gaps.

## Filename slots reserved by the docs

The how-to pages reserve these PNG filenames in
[`docs/screenshots/`](../screenshots/). Capture the matching dialog and
commit the image with the listed name.

| File | Captured from |
|---|---|
| `getting-started-main-window.png` | Main window: tree + tab area + toolbar. |
| `settings-git-sync.png` | Settings dialog scrolled to the Git sync section. |
| `import-mremoteng-dialog.png` | Import dialog mid-import (with at least one warning visible). |
| `credentials-unlock.png` | Credentials dialog with the unlock prompt visible. |
| `protocol-ssh-session.png` | An SSH session running in a tab. |
| `plugins-dialog.png` | Plugins dialog showing all three sections. |
| `backup-restore.png` | Either the backup save dialog or the restore confirmation. |
| `update-check.png` | Update-check result dialog (up to date or update available). |
| `logs-viewer.png` | Log viewer dialog with sample content visible. |
| `toolbar.png` | Wide capture of the main toolbar with hover tooltips visible. |

## Capture conventions

* **Window size**: resize to **1280×800** before capturing. This is
  large enough for most dialogs and keeps file sizes small.
* **Theme**: capture in **light theme** by default. If a dialog
  looks materially different in dark theme, add a sibling file with
  `-dark.png` suffix.
* **Format**: PNG, lossless, no chrome (do not include the OS window
  decorations unless the screenshot is specifically about them).
* **Pixel ratio**: 1× preferred; if you must use a HiDPI capture,
  scale to roughly 1× equivalent before committing so diffs stay
  readable.

## Scrubbing before commit

Before saving any screenshot:

1. Replace real hostnames with placeholders like `host.example.org`.
2. Replace usernames / domains with `alice`, `EXAMPLE`, etc.
3. Wipe any tooltips or status bars that show real paths under your
   home directory — substitute `~/…`.
4. Disable git-sync remotes that point to private repos for the
   capture, or fake them with `git@example.com:alice/connections.git`.

Captures are committed straight into `docs/screenshots/`. Do not store
them in LFS — they are intentionally small.

## Adding a new screenshot slot

If a new how-to page needs an image we have not captured yet:

1. Reserve the filename in the page (markdown image link).
2. Add the row to the table above.
3. Add a `> _Screenshot pending — see contributing-screenshots.md._`
   caption next to the link until someone captures the image.

## Optional: render a dialog with `fyne/test`

The repo's existing tests already use `fyne.io/fyne/v2/test` to drive
widgets headlessly. In principle you could capture a PNG of a dialog
using `test.NewWindow(…)` plus a software renderer, but those are
test renderings — not "real" application screenshots. We currently do
not commit such captures because they hide platform-specific
rendering quirks (font fallback, native controls, theme variants)
which are exactly what users want to see in the docs.
