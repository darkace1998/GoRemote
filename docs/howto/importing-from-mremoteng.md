# Importing from mRemoteNG

GoRemote ships a streaming importer for legacy **mRemoteNG**
connection trees. Both `.xml` (the standard mRemoteNG export) and
`.csv` (a flatter alternative) are supported.

> _Screenshot pending — see [contributing-screenshots.md](./contributing-screenshots.md). Suggested filename: `import-mremoteng-dialog.png`._

## 1. Open the importer

Click the toolbar icon whose tooltip reads `"Import from mRemoteNG
(XML/CSV)…"`. Pick the file with the system file picker.

## 2. What gets imported

| Source field | Becomes |
|---|---|
| Folder hierarchy | A folder hierarchy with the same shape |
| `Name`, `Hostname`, `Port`, `Username`, `Domain` | Equivalent connection fields |
| `Protocol` | A built-in protocol when supported, or marked unsupported with a warning |
| Inheritance flags | Honoured — fields that inherit from a parent stay inherited in GoRemote |
| `Description` / `Notes` | Connection description |

## 3. Warnings vs silent drops

GoRemote will **never silently drop data**. If a row references a
protocol GoRemote does not yet support, an inherited field that cannot
be resolved, or a credential format that requires manual intervention,
the import dialog reports it with a warning and the connection is
imported in a "needs attention" state rather than skipped.

A typical warning looks like:

```
Connection "DC01" — protocol "RDP" requires an external launcher.
Configure xfreerdp / mstsc and re-open to use it.
```

Acknowledge the warnings (the dialog will not close while there are
unread ones) and click **Import**. The tree refreshes once the import
completes.

## 4. Passwords

mRemoteNG can store passwords with a master-password-derived AES key.
GoRemote reads the encrypted blob but does **not** roll the secret
into a GoRemote credential vault automatically — you decide which
provider should hold the password. After import:

1. Set up a credential provider — see [credentials.md](./credentials.md).
2. Edit each imported connection and pick a credential reference.
3. Optionally delete the imported plaintext from the connection.

If a password cannot be decrypted at all (custom KDF, missing master
password), the importer surfaces the warning and leaves the field
blank rather than guessing.

## 5. CSV imports

CSV is a flat format — the importer infers folder paths from a
configurable separator in the `Name` / `Path` column. The
[`internal/import/mremoteng`](../../internal/import/mremoteng/) package
documents the exact column conventions accepted; the dialog itself
shows a preview of the first few rows so you can spot misalignments
before committing.

## 6. Re-importing

Importing the same file twice produces duplicate folders/connections;
the importer does not de-duplicate by name. If you need to re-sync,
delete the previously-imported subtree first (use the tree-toolbar
`"Bulk delete selected"` action against a multi-selection).

## Related buttons

| Tooltip | What it does |
|---|---|
| `Import from mRemoteNG (XML/CSV)…` | Open the importer dialog. |
| `Bulk delete selected` | Remove a previously-imported subtree before re-importing. |
| `Reload from disk` | Refresh the tree if the import completed in the background. |
