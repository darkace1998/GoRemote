package mosh

import (
	"context"
	"errors"
	"testing"

	"github.com/darkace1998/GoRemote/sdk/protocol"
)

var _ protocol.Session = (*Session)(nil)

func TestSession_RenderMode(t *testing.T) {
	cfg := &config{Host: "h", Port: 22}
	sess := newSession(cfg, "h:22")
	if sess.RenderMode() != protocol.RenderTerminal {
		t.Fatalf("RenderMode = %s, want terminal", sess.RenderMode())
	}
}

func TestSession_ResizeUnsupported(t *testing.T) {
	cfg := &config{Host: "h", Port: 22}
	sess := newSession(cfg, "h:22")
	if err := sess.Resize(context.Background(), protocol.Size{Cols: 80, Rows: 24}); !errors.Is(err, protocol.ErrUnsupported) {
		t.Fatalf("Resize err = %v, want ErrUnsupported", err)
	}
}

func TestSession_SendInputUnsupported(t *testing.T) {
	cfg := &config{Host: "h", Port: 22}
	sess := newSession(cfg, "h:22")
	if err := sess.SendInput(context.Background(), []byte("x")); !errors.Is(err, protocol.ErrUnsupported) {
		t.Fatalf("SendInput err = %v, want ErrUnsupported", err)
	}
}

func TestSession_CloseIdempotent(t *testing.T) {
	cfg := &config{Host: "h", Port: 22}
	sess := newSession(cfg, "h:22")
	for i := 0; i < 4; i++ {
		if err := sess.Close(); err != nil {
			t.Errorf("Close #%d: %v", i, err)
		}
	}
}

func TestSession_StartDialFailure(t *testing.T) {
	cfg := &config{Host: "127.0.0.1", Port: 1}
	sess := newSession(cfg, "127.0.0.1:1")
	ctx, cancel := context.WithTimeout(context.Background(), 2000000000) // 2s
	defer cancel()
	err := sess.Start(ctx, nil, nil)
	if err == nil {
		t.Fatal("expected dial error for unreachable SSH port")
	}
}

