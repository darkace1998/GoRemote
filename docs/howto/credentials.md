# Credentials

GoRemote treats credentials as **references** held by a credential
**provider**. A connection definition typically only stores the
provider ID and the entry name within that provider; the secret itself
never appears in the connection JSON, in backups, or in git-sync
commits.

> _Screenshot pending — see [contributing-screenshots.md](./contributing-screenshots.md). Suggested filename: `credentials-unlock.png`._

## Available providers

| Provider | Where it stores secrets | When to use it |
|---|---|---|
| **OS keychain** | macOS Keychain, Windows Credential Manager, libsecret/GNOME Keyring/KWallet on Linux | Default for desktop users; no extra setup. |
| **Encrypted file vault** | A local AES-256-GCM file, key derived with Argon2id from a master password | Headless / portable / containerised; or when you don't trust the OS keyring. |
| **1Password (`op`)** | Your 1Password account via the official `op` CLI | You already use 1Password; want roaming via 1Password's sync. |
| **Bitwarden (`bw`)** | Your Bitwarden vault via the official `bw` CLI | Same idea, for Bitwarden users. |

The 1Password and Bitwarden providers shell out to their official
CLIs; you must have `op` / `bw` installed and authenticated in your
shell environment.

## Open the credentials dialog

Click the toolbar icon whose tooltip reads `"Manage credentials…"`.

Each provider is listed with its current state:

* **locked** — vault exists but has not been unlocked this session.
* **unlocked** — ready to read/write.
* **unavailable** — provider preconditions not met (e.g. `op` not in
  `PATH`, no system keyring).

## Lock / unlock

* Click the button whose tooltip is `"Unlock credential vault"` and
  enter the master password to unlock.
* Click the button whose tooltip is `"Lock credential vault"` to
  re-lock without quitting the app. The lock state is also reset
  automatically on quit.

## Encrypted file vault

The encrypted file vault uses **AES-256-GCM** for confidentiality and
integrity, with the key derived from your master password using
**Argon2id** (memory-hard, side-channel-resistant). The on-disk file
records:

* The Argon2id parameters used (so future versions can read the file
  even if defaults change).
* A random salt per vault.
* AEAD-sealed entries.

The default vault path is under your OS user-config dir
(`~/.config/goremote/credentials.vault` on Linux,
`%APPDATA%\goremote\credentials.vault` on Windows,
`~/Library/Application Support/goremote/credentials.vault` on macOS).
**Do not commit this file** to git-sync; the workspace and the vault
live in separate directories precisely so that the vault stays out of
your repo.

## 1Password / Bitwarden

When using `op` or `bw`, GoRemote shells out to the CLI on demand to
fetch the secret value at session-open time. The secret is held in
process memory only for as long as the protocol layer needs it.

Authenticate the CLI from your shell first:

```bash
op signin     # or:
bw login && bw unlock
```

Then add the provider in the credentials dialog. From then on,
GoRemote uses the existing CLI session.

## Storing references on a connection

When editing a connection:

1. Choose the credential provider in the dropdown.
2. Pick the entry within that provider.

Plaintext secrets typed into the dialog are persisted on the
connection only if you have **not** chosen a provider — this is a
fallback for trivial test cases. Production use should always pick a
provider.

## Related buttons

| Tooltip | What it does |
|---|---|
| `Manage credentials…` | Opens this dialog. |
| `Unlock credential vault` | Prompt for master password and unlock the selected provider. |
| `Lock credential vault` | Re-lock the selected provider without closing the app. |
