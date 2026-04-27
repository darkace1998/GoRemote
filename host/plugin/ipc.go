package plugin

import (
	"context"
	"io"
	"time"
)

// IPCTransport is the boundary interface between the in-process plugin host
// and an external, out-of-process plugin. External plugin IPC
// (gRPC/Connect over named pipes / Unix sockets) is planned; this file
// declares the boundary so in-process and out-of-process plugins can share
// the same Host semantics.
//
// The in-process host above is sufficient for built-in plugins. No concrete
// IPC implementation is shipped yet in order to avoid pulling in gRPC as a
// dependency before the contract stabilizes.
type IPCTransport interface {
	// Connect establishes a transport to the external plugin identified by
	// endpoint (e.g. a Unix socket path or named-pipe name).
	Connect(ctx context.Context, endpoint string) error

	// Disconnect terminates the transport. Safe to call multiple times.
	Disconnect(ctx context.Context) error

	// Call invokes a remote method with a serialized request body and
	// returns the serialized response body. The specific wire format
	// (protobuf/Connect/JSON) is chosen by the concrete transport.
	Call(ctx context.Context, method string, request []byte) (response []byte, err error)

	// Stream opens a bidirectional stream for long-lived interactions such
	// as a protocol session's I/O loop.
	Stream(ctx context.Context, method string) (IPCStream, error)

	// Ping verifies liveness; used by the host to detect crashed plugins.
	Ping(ctx context.Context, deadline time.Duration) error
}

// IPCStream is a bidirectional message stream used for streaming RPCs such
// as interactive protocol sessions.
type IPCStream interface {
	io.Closer
	Send(ctx context.Context, msg []byte) error
	Recv(ctx context.Context) ([]byte, error)
}

// IPCRegistrar is what a transport presents to the Host when loading an
// external plugin: it produces the Module/Provider shim that the generic
// Host treats like any in-process plugin.
type IPCRegistrar interface {
	// Manifest returns the external plugin's advertised manifest. The host
	// validates it before activating the plugin.
	Manifest(ctx context.Context) ([]byte, error)

	// BuildModule wires the transport into a local shim satisfying the
	// appropriate SDK interface (sdk/protocol.Module or sdk/credential.Provider).
	// The returned value is passed to Host.Register as the module.
	BuildModule(ctx context.Context, transport IPCTransport) (any, error)
}
