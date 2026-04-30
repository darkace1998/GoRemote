# Updates

GoRemote can check for updates against a signed manifest URL. Updates
are **opt-in**: nothing is downloaded unless you explicitly turn the
feature on.

> _Screenshot pending — see [contributing-screenshots.md](./contributing-screenshots.md). Suggested filename: `update-check.png`._

## How it works

1. You configure `AutoUpdateURL` (an HTTPS URL to a JSON manifest)
   and `AutoUpdatePublicKey` (the publisher's base64-encoded ed25519
   public key) in **Settings**.
2. When the auto-update timer fires — or when you click the toolbar
   icon whose tooltip reads `"Check for updates"` — GoRemote fetches
   the manifest, verifies its **Ed25519** signature against the
   configured public key, and compares the manifest's version against
   the running binary's.
3. If a newer version is offered, the dialog shows the version number,
   release notes, and a download URL. You decide whether to apply.

If the manifest signature is invalid, the download URL points off the
manifest's announced host, or the version comparison is malformed,
the check aborts and the failure is logged.

## Generating a signed manifest

The repository ships [`cmd/sign-manifest`](../../cmd/sign-manifest/),
a small CLI for project maintainers that signs an update manifest
with an Ed25519 private key. Distributors run this against their
build artefacts and host the resulting `.json` manifest at the URL
users configure.

## Manual checks

Use the toolbar icon whose tooltip reads `"Check for updates"` to run
the same flow on demand. The dialog reports:

* **Up to date** — current version matches or exceeds the manifest.
* **Update available** — version, notes, and a download link.
* **Check failed** — with the underlying reason (network, signature,
  version parse, …).

Manual checks honour the same signature verification as automatic
ones; you cannot use the manual button to bypass an unsigned manifest.

## Disabling the feature

* Toggle **Auto-update enabled** off in Settings to disable the
  background timer.
* Leave **Auto-update URL** empty to disable both the timer and the
  manual check.

## Related buttons

| Tooltip | What it does |
|---|---|
| `Settings…` | Where you set the manifest URL and public key. |
| `Check for updates` | Run a manual update check now. |
| `View logs…` | Diagnose failed checks. |
