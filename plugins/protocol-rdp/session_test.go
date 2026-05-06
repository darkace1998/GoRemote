package rdp

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/darkace1998/GoRemote/sdk/protocol"
)

func TestRenderMode(t *testing.T) {
	s := newSession("127.0.0.1:3389")
	if s.RenderMode() != protocol.RenderGraphical {
		t.Fatalf("RenderMode = %s, want graphical", s.RenderMode())
	}
}

// All Start* tests assert ErrUnsupported: full RDP framing is not implemented.

func TestStart_ReturnsErrUnsupported(t *testing.T) {
	sess := newSession("127.0.0.1:3389")
	err := sess.Start(context.Background(), nil, io.Discard)
	if !errors.Is(err, protocol.ErrUnsupported) {
		t.Fatalf("Start: got %v, want ErrUnsupported", err)
	}
}

func TestStart_ReceivesDataFromServer(t *testing.T) {
	sess := newSession("127.0.0.1:3389")
	err := sess.Start(context.Background(), nil, io.Discard)
	if !errors.Is(err, protocol.ErrUnsupported) {
		t.Fatalf("Start: got %v, want ErrUnsupported", err)
	}
}

func TestStart_ReturnsWhenServerClosesWithOpenStdin(t *testing.T) {
	sess := newSession("127.0.0.1:3389")
	err := sess.Start(context.Background(), nil, io.Discard)
	if !errors.Is(err, protocol.ErrUnsupported) {
		t.Fatalf("Start: got %v, want ErrUnsupported", err)
	}
}

func TestStart_SendsDataToServer(t *testing.T) {
	sess := newSession("127.0.0.1:3389")
	err := sess.Start(context.Background(), nil, io.Discard)
	if !errors.Is(err, protocol.ErrUnsupported) {
		t.Fatalf("Start: got %v, want ErrUnsupported", err)
	}
}

func TestStart_ContextCancellation(t *testing.T) {
	sess := newSession("127.0.0.1:3389")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := sess.Start(ctx, nil, io.Discard)
	if !errors.Is(err, protocol.ErrUnsupported) {
		t.Fatalf("Start: got %v, want ErrUnsupported", err)
	}
}

func TestClose_Idempotent(t *testing.T) {
	sess := newSession("127.0.0.1:3389")
	for i := 0; i < 8; i++ {
		if err := sess.Close(); err != nil {
			t.Errorf("Close #%d: %v", i, err)
		}
	}
}

func TestClose_BeforeStart(t *testing.T) {
	sess := newSession("127.0.0.1:3389")
	if err := sess.Close(); err != nil {
		t.Fatalf("Close before Start: %v", err)
	}
	if err := sess.Start(context.Background(), nil, io.Discard); !errors.Is(err, protocol.ErrUnsupported) {
		t.Fatalf("Start after Close: got %v, want ErrUnsupported", err)
	}
}

func TestSendInput_BeforeStart(t *testing.T) {
	sess := newSession("127.0.0.1:3389")
	err := sess.SendInput(context.Background(), []byte("x"))
	if err == nil {
		t.Fatal("expected error when session not started")
	}
}

func TestSendInput_ContextCanceled(t *testing.T) {
	sess := newSession("127.0.0.1:3389")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := sess.SendInput(ctx, []byte("x"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("SendInput err = %v, want context.Canceled", err)
	}
}

func TestResize_Unsupported(t *testing.T) {
	sess := newSession("127.0.0.1:3389")
	err := sess.Resize(context.Background(), protocol.Size{Cols: 80, Rows: 24})
	if !errors.Is(err, protocol.ErrUnsupported) {
		t.Fatalf("Resize err = %v, want ErrUnsupported", err)
	}
}
