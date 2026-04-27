# Bitwarden credential provider TODO

Status: planned. Operational methods currently return `ErrNotImplemented`.

- Shell out to the Bitwarden CLI (`bw`) with a per-session BW_SESSION token.
- Implement Unlock via master password; cache the session token in memory only.
- Translate bw item JSON (login.username/password) into credential.Material.
- Support Lookup by Hints (name/url).
