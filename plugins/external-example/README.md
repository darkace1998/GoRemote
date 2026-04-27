# external-example plugin

This is the **reference out-of-process plugin** for goremote. It exists to
demonstrate the IPC contract end-to-end and to give the host integration
tests something concrete to launch.

It implements two services from `proto/plugin/v1`:

- **PluginHandshake** – returns the plugin id, version, capabilities, and
  `status: "ready"`.
- **Echo** – `Ping` echoes the supplied payload.

The plugin is marked `Experimental` in [`manifest.go`](./manifest.go) and
declares no special capabilities.

## Running it manually

```sh
go run ./plugins/external-example/cmd/external-example -socket /tmp/goremote-example.sock
```

Or via the environment:

```sh
GOREMOTE_PLUGIN_SOCKET=/tmp/goremote-example.sock \
    go run ./plugins/external-example/cmd/external-example
```

The process listens until it receives `SIGINT` or `SIGTERM`, then performs a
graceful shutdown (≤ 5 seconds) and removes the socket file.

## Connecting from the host

Use `host/plugin/ipc.DialUnix`. See
[`host/plugin/ipc/ipc_test.go`](../../host/plugin/ipc/ipc_test.go) for a
complete in-process round-trip example, and
[`e2e_test.go`](./e2e_test.go) for an end-to-end test that builds the
plugin binary, spawns it, and exercises both RPCs over the real socket.
