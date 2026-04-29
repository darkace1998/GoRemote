# Setting up Git sync

Git sync mirrors your **workspace directory** as a git repository.
Every successful save (creating, editing, deleting a folder or
connection, reordering, etc.) automatically stages the change, commits
it with a generated message, and — if a remote is configured — pushes
it to that remote.

Use it to:

* Back your workspace up to a private repository.
* Roam your connection list between machines.
* Keep an audit trail of structural changes to your tree.

> Git sync only mirrors **structure**, not raw secrets. Credential
> references (vault IDs, keychain handles, 1Password / Bitwarden
> identifiers) are stored, but plaintext passwords are not — provided
> you store credentials through a credential provider rather than
> inline. See [credentials.md](./credentials.md).

> _Screenshot pending — see [contributing-screenshots.md](./contributing-screenshots.md). Suggested filename: `settings-git-sync.png`._

## Prerequisites

* `git` must be reachable on `PATH`. GoRemote shells out to the system
  `git` CLI rather than embedding a Go-native git library — auth,
  proxies, SSH config, GPG signing, and credential helpers all "just
  work" the way they do for `git` on the command line.
* If you push to a remote, that remote must already accept your
  current git configuration's auth method:
  * **SSH remote** (`git@github.com:user/repo.git`) → an `ssh-agent`
    or a passphrase-less key referenced by your `~/.ssh/config`.
  * **HTTPS remote** with 2FA → a credential helper that supplies a
    Personal Access Token (PAT). On Windows this is usually
    `manager-core`; on macOS, `osxkeychain`; on Linux,
    `libsecret`. GoRemote does **not** prompt for HTTPS passwords —
    it expects the helper to provide them non-interactively.

## 1. Enable it from settings

Click the toolbar icon whose tooltip reads `"Settings…"`, then open the
**Git sync** section. The fields:

| Field | What to enter | Notes |
|---|---|---|
| **Enable git sync** | Toggle on | Without this, the rest is inert. |
| **Remote** | `git@host:org/repo.git` or `https://host/org/repo.git` | Empty = commit-only; nothing is pushed. |
| **Branch** | e.g. `main` | Defaults to `main` when blank. |

Save. GoRemote will lazily run `git init -b <branch>` on the workspace
directory the next time it commits, register the remote you supplied,
and configure a local `user.name` / `user.email` of `goremote@localhost`
so commits never look like they came from your personal identity.

## 2. Commit cadence

* **Automatic.** Every successful Save triggers `git add -A`,
  `git commit -m "<short message>"`, and (if a remote is configured)
  `git push <branch>`.
* **Manual.** Click the toolbar icon whose tooltip reads
  `"Sync now (commit & push to Git)"` to fire the same flow on demand,
  for example after restoring a backup.

Each `git` invocation runs with a 30-second timeout, so a wedged remote
cannot block the UI thread. Failures are logged at warn level and
surfaced in the Diagnostics dialog (tooltip: `"Diagnostics"`); they do
not interrupt your editing session.

## 3. What lives in the repo

The workspace document at `<state>/goremote/workspace.json` (path is
OS-specific — see [logs-and-diagnostics.md](./logs-and-diagnostics.md))
plus any related files in the same directory. Settings, logs, plugin
binaries, and credential vault files live elsewhere and are **not**
committed.

## 4. Conflict handling

Git sync does **not** merge. If the remote has diverged from your
local copy, the next push fails fast and the failure is logged. To
recover:

1. Open a terminal in the workspace directory (`Settings → Logs &
   diagnostics` shows the path; or right-click any folder in the
   plugins dialog "Open folder" tooltip to find related state dirs).
2. Resolve the divergence with regular git tooling (`git pull --rebase`,
   `git reset --hard origin/<branch>`, etc.).
3. Click `"Sync now (commit & push to Git)"` to verify recovery.

This is by design: silently merging machine-generated commits is a
class of footgun we'd rather you opt into explicitly.

## 5. Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `git binary not found on PATH` | git not installed | Install git for your OS. |
| Push fails: `authentication required` | No credential helper / no agent | Run `git push` once from the workspace directory to seed your credential helper, or add the SSH key to `ssh-agent`. |
| Push fails: `non-fast-forward` | Remote diverged | See "Conflict handling" above. |
| Commits succeed but nothing pushes | Empty `Remote` field in settings | Fill in the remote URL. |
| All saves work but nothing commits | `git init` failed (read-only state dir, etc.) | See the **Diagnostics** bundle for the underlying error. |

## 6. Disabling

Toggle **Enable git sync** off in Settings. The repository on disk is
untouched — you can re-enable later or delete the `.git` folder by
hand.

## Related buttons

| Tooltip | What it does |
|---|---|
| `Settings…` | Opens settings — including the Git sync section. |
| `Sync now (commit & push to Git)` | Force a manual commit + push. |
| `Diagnostics` | Build a support bundle if syncing fails. |
| `View logs…` | Tail the log file to see git command failures. |
