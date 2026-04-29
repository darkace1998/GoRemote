# Backup & restore

GoRemote can produce a self-contained zip archive of your workspace
and restore from one. Use this for ad-hoc backups, machine migrations,
or to roll back to a known-good state after a bad import.

> _Screenshot pending — see [contributing-screenshots.md](./contributing-screenshots.md). Suggested filename: `backup-restore.png`._

## What is included

The backup archive contains:

* `workspace.json` — the connection tree (folders, connections,
  inheritance state).
* Workspace-adjacent metadata maintained by the persistence layer
  (search index, recent/favorite ledgers).
* Settings derived from the workspace, where they are workspace-scoped.

It deliberately does **not** include:

* The credential vault or any keychain entries.
* Logs, crash dumps, or diagnostic data.
* Plugin binaries.

This is the same boundary git-sync uses, for the same reason.

## Create a backup

1. Click the toolbar icon whose tooltip reads `"Backup connections to
   a zip…"`.
2. Pick a destination path. The dialog suggests
   `goremote-backup-<timestamp>.zip` by default.
3. The archive is written atomically — on failure no partial file is
   left behind.

## Restore from a backup

1. Click the toolbar icon whose tooltip reads `"Restore from a zip…"`.
2. Pick the archive.
3. GoRemote runs the **integrity validator** on the archive contents
   before touching your workspace. If the validator finds:
   * A version it does not recognise → restore aborts with an
     explanatory error.
   * Workspace JSON that fails schema validation → restore aborts.
   * Tampered or truncated entries → restore aborts.
4. On success, the existing workspace is moved aside (renamed to
   `workspace.json.bak-<timestamp>`) before the restored content is
   written, so you can recover manually if you change your mind.

## Re-syncing git-sync after restore

If git-sync is enabled, the restored workspace is committed and pushed
on the next save. You can also click `"Sync now (commit & push to
Git)"` to commit the restored state immediately. See
[git-sync.md](./git-sync.md).

## Related buttons

| Tooltip | What it does |
|---|---|
| `Backup connections to a zip…` | Write the workspace archive. |
| `Restore from a zip…` | Replace the workspace from an archive (with safety checks). |
| `Reload from disk` | Reload the in-memory tree after a manual restore. |
| `Sync now (commit & push to Git)` | Push the restored state to your git remote. |
