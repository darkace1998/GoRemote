package ipc

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	pluginv1 "github.com/goremote/goremote/proto/plugin/v1"
)

// DialOption configures a client dial. The option set is intentionally small
// to keep the transport contract simple.
type DialOption func(*dialConfig)

type dialConfig struct {
	dialTimeout time.Duration
}

// WithDialTimeout caps how long DialUnix waits for the socket to accept a
// connection. Defaults to 5 seconds.
func WithDialTimeout(d time.Duration) DialOption {
	return func(c *dialConfig) { c.dialTimeout = d }
}

// Client wraps a net.Conn and exposes Hello and Ping RPCs with request
// multiplexing via a mutex-guarded in-flight map.
type Client struct {
	conn     net.Conn
	wmu      sync.Mutex             // serialises writes
	mu       sync.Mutex             // guards nextID and inflight
	nextID   uint64
	inflight map[uint64]chan pluginv1.Frame
}

// Close terminates the connection.
func (c *Client) Close() error { return c.conn.Close() }

// Hello calls PluginHandshake.Hello.
func (c *Client) Hello(ctx context.Context, req *pluginv1.HelloRequest) (*pluginv1.HelloResponse, error) {
	frame, err := c.call(ctx, "Hello", req)
	if err != nil {
		return nil, err
	}
	var resp pluginv1.HelloResponse
	if err := json.Unmarshal(frame.Payload, &resp); err != nil {
		return nil, fmt.Errorf("ipc: Hello decode: %w", err)
	}
	return &resp, nil
}

// Ping calls Echo.Ping.
func (c *Client) Ping(ctx context.Context, req *pluginv1.PingRequest) (*pluginv1.PingResponse, error) {
	frame, err := c.call(ctx, "Ping", req)
	if err != nil {
		return nil, err
	}
	var resp pluginv1.PingResponse
	if err := json.Unmarshal(frame.Payload, &resp); err != nil {
		return nil, fmt.Errorf("ipc: Ping decode: %w", err)
	}
	return &resp, nil
}

// DialUnix connects to a plugin process listening on the given Unix socket.
//
// The dial respects the configured timeout (default 5s) and returns
// immediately if the socket cannot be reached.
func DialUnix(ctx context.Context, socketPath string, opts ...DialOption) (*Client, error) {
	cfg := dialConfig{dialTimeout: 5 * time.Second}
	for _, o := range opts {
		o(&cfg)
	}

	dialCtx, cancel := context.WithTimeout(ctx, cfg.dialTimeout)
	defer cancel()

	conn, err := socketDial(dialCtx, socketPath)
	if err != nil {
		return nil, fmt.Errorf("ipc: dial %q: %w", socketPath, err)
	}

	c := &Client{
		conn:     conn,
		inflight: make(map[uint64]chan pluginv1.Frame),
	}
	go c.readLoop()
	return c, nil
}

// call sends a request frame and waits for the matching response frame,
// identified by ID.
func (c *Client) call(ctx context.Context, method string, payload interface{}) (pluginv1.Frame, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return pluginv1.Frame{}, fmt.Errorf("ipc: marshal %s: %w", method, err)
	}

	c.mu.Lock()
	if c.inflight == nil {
		c.mu.Unlock()
		return pluginv1.Frame{}, fmt.Errorf("ipc: connection closed")
	}
	id := c.nextID
	c.nextID++
	ch := make(chan pluginv1.Frame, 1)
	c.inflight[id] = ch
	c.mu.Unlock()

	req := pluginv1.Frame{Method: method, ID: id, Payload: json.RawMessage(data)}
	c.wmu.Lock()
	werr := pluginv1.WriteFrame(c.conn, req)
	c.wmu.Unlock()
	if werr != nil {
		c.mu.Lock()
		if c.inflight != nil {
			delete(c.inflight, id)
		}
		c.mu.Unlock()
		return pluginv1.Frame{}, fmt.Errorf("ipc: write %s: %w", method, werr)
	}

	select {
	case resp := <-ch:
		if resp.Error != "" {
			return pluginv1.Frame{}, fmt.Errorf("ipc: %s: %s", method, resp.Error)
		}
		return resp, nil
	case <-ctx.Done():
		c.mu.Lock()
		if c.inflight != nil {
			delete(c.inflight, id)
		}
		c.mu.Unlock()
		return pluginv1.Frame{}, fmt.Errorf("ipc: %s: %w", method, ctx.Err())
	}
}

// readLoop reads response frames and demultiplexes them to waiting callers.
// It exits when the connection is closed.
func (c *Client) readLoop() {
	for {
		frame, err := pluginv1.ReadFrame(c.conn)
		if err != nil {
			c.mu.Lock()
			pending := c.inflight
			c.inflight = nil
			c.mu.Unlock()
			errMsg := fmt.Sprintf("connection lost: %v", err)
			for _, ch := range pending {
				ch <- pluginv1.Frame{Error: errMsg}
			}
			return
		}
		c.mu.Lock()
		ch := c.inflight[frame.ID]
		if ch != nil {
			delete(c.inflight, frame.ID)
		}
		c.mu.Unlock()
		if ch != nil {
			ch <- frame
		}
	}
}
