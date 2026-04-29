# Plugin IPC transport

`host/plugin/ipc` implements the reference transport between the goremote
host and out-of-process plugins, as outlined in `architecture.md`.

## Wire format

- **Transport**: a Unix domain socket. Supported on Linux and macOS as well as
  Windows 10 1809+ (Go's `net` package supports `net.Listen("unix", …)` on
  every supported Windows release).
- **Codec**: length-prefixed JSON frames defined in `proto/plugin/v1/frame.go`
  (4-byte big-endian length, then a JSON `Frame` envelope carrying `method`,
  `id`, `payload`, and optional `error`). Maximum frame size is 4 MiB. The
  payload of each frame is itself a JSON object whose schema is the request
  / response type from `proto/plugin/v1/types.go`.
- **`plugin.proto` is not yet wired up.** The .proto file documents the
  intended gRPC contract for a future migration, but the reference transport
  shipped today is the JSON framing above. Migrating to gRPC would not
  change the public Go API (`ListenUnix`, `DialUnix`, `Server`, `Client`).

## Services (`proto/plugin/v1/types.go`)

- `Hello` — first call after connect; the plugin declares its id, version,
  capabilities, and a status string of `ready` or `degraded:<reason>`.
- `Ping` — smoke test / liveness probe; the plugin echoes the request payload.

Higher-level protocol and credential RPCs will layer on top of the
same connection; only the contract above is part of the v1 surface.

## Build-tag policy

Platform-specific socket primitives live in two files guarded by build
tags:

| File                     | Build tag            | Purpose                                                    |
|--------------------------|----------------------|------------------------------------------------------------|
| `socket_unix.go`         | `//go:build unix`    | `net.Listen("unix", …)` plus `chmod 0600` on the socket.   |
| `socket_windows.go`      | `//go:build windows` | `net.Listen("unix", …)` (Win10 1809+); ACLs replace chmod. |

The public package API (`ListenUnix`, `DialUnix`, `Server`, `Client`)
compiles and runs on every supported platform.

## Listener semantics

- `ListenUnix` removes a stale socket file but refuses
  (`ErrSocketInUse`) if an active process is accepting on it.
- On Unix the socket is `chmod 0600` so only the owning user can connect;
  on Windows access is governed by the inherited file ACLs.
- `Listener.Close` and `Server.Stop` both unlink the socket file.

## Server lifecycle

`Server.Serve(ctx)` blocks until `ctx` is cancelled. On cancel it stops
accepting new connections, waits up to a short grace period for in-flight
calls to drain, and then closes outstanding connections. The socket file is
always cleaned up before `Serve` returns.

## Client dial

`DialUnix` waits for the underlying connection to be ready (default
timeout 5 seconds, override with `WithDialTimeout`) so a missing or
unresponsive socket fails fast at dial time rather than on the first
RPC. A chaos test (`chaos_test.go`) covers server stop, dial-after-stop
fast-fail, and caller-side context cancellation across many concurrent
callers.
