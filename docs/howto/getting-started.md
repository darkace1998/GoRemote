# Getting started

This guide walks you through launching GoRemote for the first time,
creating your first folder + connection, and opening a session.

> _Screenshot pending — see [contributing-screenshots.md](./contributing-screenshots.md). Suggested filename: `getting-started-main-window.png`._

## 1. Launch

```bash
go run ./cmd/desktop
# or, from a release build:
./goremote
```

On Linux the app expects the OpenGL/X11 prerequisites listed in the
top-level [README](../../README.md#prerequisites). On macOS and Windows
the binary is self-contained.

A single window opens with three regions:

1. **Left pane** — the connection tree, an environment selector, a
   search box, and a tree-action toolbar.
2. **Top toolbar** — global actions (new folder, new connection,
   connect, disconnect, settings, plugins, sync, …).
3. **Right pane** — a tabbed area that hosts session terminals.

Hover over any toolbar icon for a tooltip describing the action.

## 2. Pick a credential provider (optional but recommended)

Open **Settings → Credentials** by clicking the toolbar icon whose
tooltip reads `"Manage credentials…"`.

GoRemote ships with several providers (see
[credentials.md](./credentials.md)). For most users the easiest path is:

* **Linux / Windows / macOS** with a desktop keyring → choose **OS
  keychain**.
* **No keyring available, or you want a portable vault** → choose
  **encrypted file vault**, set a master password, and click **Unlock
  credential vault** (the toolbar icon with that exact tooltip).

You can skip this step and store passwords directly on the connection,
but credential references are recommended — they are not written into
backups or git-sync commits in plaintext.

## 3. Create a folder

Click the toolbar icon whose tooltip is `"New folder…"`. Folders are
purely organisational; they propagate inheritable fields (username,
domain, port, protocol defaults) to their children.

## 4. Create a connection

Click the toolbar icon whose tooltip is `"New connection…"`, fill in
the dialog, and save. The minimum required fields are **Name**,
**Protocol**, and **Host**. Anything inheritable that you leave blank
is resolved from the parent folder chain.

## 5. Open a session

Select a connection in the tree, then either:

* Press **Enter**, or
* Click the toolbar icon whose tooltip is `"Connect (open selected)"`.

A new tab opens in the right pane with a live terminal (for SSH /
Telnet / Rlogin / Raw / Serial / SFTP / PowerShell), an HTTP browser
for `http://…`, or a launched external viewer for RDP / VNC / TN5250 /
MOSH.

## 6. Tabs, splits, and detached windows

* **Close** the active tab with the per-tab `"Close tab"` button or
  with `Ctrl/Cmd + W`.
* **Detach** the active tab into its own window using the toolbar icon
  whose tooltip is `"Detach current tab to its own window"`. From the
  detached window, the **Reattach to main** button (tooltip: `"Move
  this tab back into the main window"`) drops it back into the tab
  strip without disturbing the underlying session.
* **Disconnect** the active session with the toolbar icon whose
  tooltip is `"Disconnect current session"`.

## 7. Recents and favorites

* The toolbar icon with tooltip `"Recent connections"` shows a popup
  list of recently-opened connections.
* The toolbar icon with tooltip `"Open a favorite…"` opens a quick
  picker over connections marked as favorites.

## Related buttons

| Tooltip | What it does |
|---|---|
| `New folder…` | Create a tree folder |
| `New connection…` | Create a connection in the selected folder |
| `Connect (open selected)` | Open a session for the highlighted connection |
| `Disconnect current session` | Cleanly close the active session |
| `Detach current tab to its own window` | Float the active tab as a window |
| `Recent connections` | Reopen something you used recently |
| `Open a favorite…` | Quick-pick a favorited connection |
