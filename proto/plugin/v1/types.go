// Package pluginv1 defines the IPC message types for the goremote plugin
// transport. The wire format is length-prefixed JSON over a Unix domain socket.
package pluginv1

// HelloRequest is sent by the host to identify itself.
type HelloRequest struct {
	HostVersion string `json:"host_version"`
	PluginID    string `json:"plugin_id"`
}

// HelloResponse is returned by the plugin.
type HelloResponse struct {
	PluginVersion  string   `json:"plugin_version"`
	Capabilities   []string `json:"capabilities"`
	Status         string   `json:"status"`           // "ready" or "degraded:<reason>"
	ServerTimeUnix int64    `json:"server_time_unix"`
}

// PingRequest is used for echo / keepalive.
type PingRequest struct {
	Payload []byte `json:"payload"`
}

// PingResponse echoes the payload.
type PingResponse struct {
	Payload        []byte `json:"payload"`
	ReceivedAtUnix int64  `json:"received_at_unix"`
}
