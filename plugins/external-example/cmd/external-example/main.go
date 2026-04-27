// Command external-example is the reference out-of-process goremote
// plugin. It listens on a Unix domain socket supplied via --socket or the
// GOREMOTE_PLUGIN_SOCKET environment variable, and serves the
// PluginHandshake and Echo gRPC services defined in proto/plugin/v1.
//
// Usage:
//
//	external-example --socket /tmp/goremote-example.sock
//
// On SIGINT/SIGTERM the process performs a graceful shutdown of the gRPC
// server and removes the on-disk socket file before exiting.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/goremote/goremote/host/plugin/ipc"
	pluginv1 "github.com/goremote/goremote/proto/plugin/v1"
)

const (
	pluginID      = "io.goremote.example.external"
	pluginVersion = "0.1.0"
)

type handshake struct{}

func (handshake) Hello(_ context.Context, req *pluginv1.HelloRequest) (*pluginv1.HelloResponse, error) {
	return &pluginv1.HelloResponse{
		PluginVersion:  pluginVersion,
		Capabilities:   []string{"echo"},
		Status:         "ready",
		ServerTimeUnix: time.Now().Unix(),
	}, nil
}

type echo struct{}

func (echo) Ping(_ context.Context, req *pluginv1.PingRequest) (*pluginv1.PingResponse, error) {
	cp := append([]byte(nil), req.Payload...)
	return &pluginv1.PingResponse{Payload: cp, ReceivedAtUnix: time.Now().Unix()}, nil
}

func main() {
	var socketPath string
	flag.StringVar(&socketPath, "socket", os.Getenv("GOREMOTE_PLUGIN_SOCKET"), "Unix socket path to listen on (or GOREMOTE_PLUGIN_SOCKET)")
	flag.Parse()

	if socketPath == "" {
		fmt.Fprintln(os.Stderr, "external-example: --socket or GOREMOTE_PLUGIN_SOCKET is required")
		os.Exit(2)
	}

	if err := run(socketPath); err != nil {
		log.Fatalf("external-example: %v", err)
	}
}

func run(socketPath string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	ln, err := ipc.ListenUnix(ctx, socketPath)
	if err != nil {
		return fmt.Errorf("listen %q: %w", socketPath, err)
	}

	srv := ipc.NewServer(ln, handshake{}, echo{})
	log.Printf("external-example: listening on %s (pid=%d)", socketPath, os.Getpid())

	if err := srv.Serve(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("serve: %w", err)
	}
	log.Printf("external-example: graceful shutdown complete")
	return nil
}
