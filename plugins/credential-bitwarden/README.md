# Bitwarden credential provider

Status: implemented. The provider shells out to the Bitwarden CLI (`bw`)
with a per-session BW_SESSION token captured from `bw unlock --raw`. It
supports State, Unlock, Lock, Resolve (`bw get item`), List (`bw list
items`) and Sync (`bw sync`). Writes are intentionally rejected
(ErrReadOnly) — Bitwarden is treated as the source of truth.

Configuration:
- The `bw` binary is auto-located via PATH; override with the
  `GOREMOTE_BW_BINARY` env var.
- For self-hosted servers, set `GOREMOTE_BW_SERVER_URL`; the provider
  runs `bw config server <url>` once at construction.

GUI: open the Credentials dialog from the toolbar (login icon) to
unlock, lock or inspect each registered provider including Bitwarden.
