# Settings

GoRemote provides a settings dialog to configure application behaviour and appearance.

> _Screenshot pending — see [contributing-screenshots.md](./contributing-screenshots.md). Suggested filename: `settings-dialog.png`._

## Open settings

Click the toolbar icon whose tooltip reads `"Settings…"`.

## Appearance

* **Theme**: Choose between `system` (matches OS preference), `light`, or `dark`.
* **Font Family**: Set a custom font for the terminal sessions.
* **Font Size**: Change the size of the terminal font (must be between 8 and 72 pixels).

## Behaviour

* **Confirm on close**: When enabled, GoRemote will prompt for confirmation if you attempt to close the application while there are still active sessions.
* **Auto-reconnect**: When enabled, connections that drop unexpectedly will automatically attempt to reconnect. You can configure the **Max Retries** (up to 50) and the **Delay** between retries (up to 60,000 milliseconds).

## Diagnostics and Telemetry

* **Telemetry Enabled**: Opt-in to basic, anonymous usage telemetry. (Default is disabled).
* **Crash Reports Disabled**: By default, GoRemote writes crash dumps to a local `<state>/crashes` folder on panic. Enable this setting to opt out of writing these dumps. The dumps are local only; nothing is uploaded.

## Other settings

The settings dialog also includes tabs for:
* **Git sync** — see [git-sync.md](./git-sync.md).
* **Updates** — see [updates.md](./updates.md).
* **Credentials** — via the dedicated `"Manage credentials…"` toolbar button (see [credentials.md](./credentials.md)).

## Settings storage

Settings are saved as a JSON document located in the OS-specific state directory (see [logs-and-diagnostics.md](./logs-and-diagnostics.md) for details on where this directory is).

## Related buttons

| Tooltip | What it does |
|---|---|
| `Settings…` | Open the settings dialog. |
