# How-to guides

Practical, task-oriented documentation for GoRemote. If you are looking
for the architecture story, read [`architecture.md`](../../architecture.md);
if you want the parity matrix vs mRemoteNG, see
[`PARITY.md`](../../PARITY.md). The pages here are deliberately focused
on "how do I do X in the running app".

## Index

| Guide | Topic |
|---|---|
| [Getting started](./getting-started.md) | First launch, choosing a credential provider, creating folders & connections, opening a session |
| [Importing from mRemoteNG](./importing-from-mremoteng.md) | Streaming XML / CSV importer, inheritance, warnings vs silent drops |
| [Credentials](./credentials.md) | OS keychain, encrypted file vault, 1Password (`op`), Bitwarden (`bw`), lock/unlock |
| [Protocols](./protocols.md) | Built-in protocols, external-launcher protocols, prerequisites |
| [Plugins](./plugins.md) | Discovering, enabling, quarantining; trust policy & keys; marketplace |
| [Backup & restore](./backup-and-restore.md) | Zip backups, restore safety checks, integrity validator |
| [Git sync](./git-sync.md) | Mirror your workspace as a git repo and auto-commit on every save |
| [Updates](./updates.md) | Auto-update channel, manifest signing, manual "check for updates" |
| [Logs & diagnostics](./logs-and-diagnostics.md) | Log file location, redaction, Diagnostics dialog, support bundles |
| [Contributing screenshots](./contributing-screenshots.md) | How to capture and submit screenshots for these pages |

## Conventions used in these docs

* **Toolbar reference.** Every page that talks about a UI control names
  the exact hover-tooltip string used in the app, e.g. `"Sync now
  (commit & push to Git)"`. If you hover over an icon and see that
  label, you've found the right button.
* **Screenshot stubs.** Where a screenshot would be helpful but has not
  been captured yet, the page contains a stub like
  `> _Screenshot pending — see [contributing-screenshots.md](./contributing-screenshots.md)._`
  These are the slots a contributor with a working desktop build can fill in.
* **Pre-1.0 notes.** GoRemote is pre-1.0; some flows are configurable
  only via the JSON settings document on disk. Where that is the case
  the page calls it out and links to the file path.
