# Plugins

GoRemote can load extra protocol and credential providers as **plugins**
without rebuilding the app. Plugins live as folders under a
plugins directory; each folder ships a manifest declaring its
capabilities, version, signing key, and binary.

> _Screenshot pending — see [contributing-screenshots.md](./contributing-screenshots.md). Suggested filename: `plugins-dialog.png`._

## Open the plugins dialog

Click the toolbar icon whose tooltip reads `"Plugins…"`. The dialog
has three sections:

1. **Discovered plugins** — every plugin folder GoRemote found,
   together with status (enabled / disabled / quarantined / broken)
   and a row of action buttons.
2. **Trust policy + keys** — global trust mode (`permissive` /
   `strict`) plus the list of ed25519 publisher keys you have decided
   to trust.
3. **Marketplace** — optional; if a marketplace URL is configured in
   settings, this section lets you browse listings and install them.

## Discovering / refreshing plugins

* The button whose tooltip is `"Re-scan the plugin folder for changes"`
  rebuilds the discovered list from disk. Use it after dropping a
  plugin folder in by hand.
* The button whose tooltip is `"Open the plugin folder in your file
  manager"` reveals the plugin root in the OS file manager so you can
  copy folders in.

## Per-plugin actions

Each row exposes:

| Tooltip | What it does |
|---|---|
| `Enable this plugin` | Mark the plugin as enabled and start using it. |
| `Disable this plugin` | Stop using the plugin without removing it. |
| `Quarantine this plugin (block it pending review)` | Block the plugin even if its trust policy would normally allow it. Use after a plugin misbehaves. |
| `Forget this plugin (remove from registry; folder kept)` | Drop the plugin from GoRemote's registry but leave the folder on disk. |

The status column reflects the chosen state; broken plugins (manifest
parse failure, missing binary, signature mismatch) include the error
text on the right of the row.

## Trust policy

The trust policy controls what happens when a plugin is loaded:

* **`permissive`** — load any plugin you have explicitly enabled,
  regardless of whether its publisher key is in the trusted list.
* **`strict`** — only load plugins signed with a publisher key listed
  in **Trusted keys**.

Switching between modes takes effect on the next refresh.

### Adding a trusted key

Click the button whose tooltip is `"Add a trusted publisher's ed25519
public key"`, paste the publisher's base64-encoded ed25519 public key
(32 bytes), give it a label, and save. Remove a key with the matching
`"Remove this trusted publisher key"` button.

## Marketplace

When **Plugin marketplace URL** is set in Settings, the **Marketplace**
section pulls a JSON document from that URL. Each listing has an
**Install** button (tooltip: `"Download and install this plugin"`)
that downloads the package into the plugin root and triggers a
refresh. Newly-installed plugins start in the **disabled** state — you
must explicitly enable them, after reviewing their manifest.

The buttons:

| Tooltip | What it does |
|---|---|
| `Save the marketplace URL to settings` | Persist the URL you typed. |
| `Fetch listings from the marketplace URL` | Pull the listings JSON. |
| `Download and install this plugin` | Stage the listing into the plugins folder. |

## Out-of-process plugins

GoRemote prefers **out-of-process** plugins that talk to the host over
a small JSON-framed IPC (see
[`proto/plugin/v1/`](../../proto/plugin/v1/)). This isolates plugin
crashes from the host and avoids ABI surprises that would come from
Go's native `plugin` package. The reference implementation lives in
[`plugins/external-example/`](../../plugins/external-example/).

## Related buttons

| Tooltip | What it does |
|---|---|
| `Plugins…` | Opens this dialog. |
| `Re-scan the plugin folder for changes` | Pick up plugins added by hand. |
| `Open the plugin folder in your file manager` | Reveal the plugin root. |
| `Enable this plugin` / `Disable this plugin` / `Quarantine this plugin (block it pending review)` / `Forget this plugin (remove from registry; folder kept)` | Per-plugin lifecycle. |
| `Add a trusted publisher's ed25519 public key` / `Remove this trusted publisher key` | Manage trust. |
| `Save the marketplace URL to settings` / `Fetch listings from the marketplace URL` / `Download and install this plugin` | Marketplace flow. |
