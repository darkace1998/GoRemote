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
   - Use the **search box** to filter the tree by connection name, host, or tag (shortcut: `Ctrl/Cmd + F`).
   - Use the **environment selector** to filter the tree to only show connections matching a specific environment.
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
* Click the tree-action toolbar icon whose tooltip is `"Connect selected connection"`, or
* Click the main toolbar icon whose tooltip is `"Connect (open selected)"`.

A new tab opens in the right pane with a live terminal for ready terminal
protocols (SSH / Telnet / Rlogin / Raw / Serial / SFTP). RDP, TN5250,
and MOSH currently surface experimental/planned Go-native protocol work.
HTTP and VNC rely on external launchers (system browser and `vncviewer`).

## 6. Tabs, splits, and detached windows

* **Close** the active tab with the per-tab `"Close tab"` button or
  with `Ctrl/Cmd + W`.
* **Reorder** the active tab by pressing `Ctrl+Shift+PageUp` (move left) or `Ctrl+Shift+PageDown` (move right).
* **Split** the active tab horizontally or vertically. Press `Ctrl+Shift+\` to split the pane right, or `Ctrl+Shift+-` to split it down.
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

## 8. About GoRemote

Click the toolbar icon whose tooltip is `"About GoRemote"` to open
the About dialog which shows the version and license information.

## Related buttons

| Tooltip | What it does |
|---|---|
| `New folder…` | Create a tree folder |
| `New connection…` | Create a connection in the selected folder |
| `Connect selected connection` | Open a session for the highlighted connection (tree toolbar) |
| `Connect (open selected)` | Open a session for the highlighted connection (main toolbar) |
| `Close tab` | Close the active tab |
| `Disconnect current session` | Cleanly close the active session |
| `Detach current tab to its own window` | Float the active tab as a window |
| `Move this tab back into the main window` | Reattach a detached tab |
| `Recent connections` | Reopen something you used recently |
| `Open a favorite…` | Quick-pick a favorited connection |
| `Workspace Profiles…` | Switch between different workspace profiles |
| `About GoRemote` | Open the About dialog |
