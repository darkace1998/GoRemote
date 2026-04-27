package telnet

import (
	"bytes"
	"testing"
)

// newTestState creates a freshly-initialised negState for tests.
func newTestState(term string, cols, rows int) *negState {
	s := &negState{termType: term, cols: cols, rows: rows}
	return s
}

// feedAll is a test helper that runs input through the state machine,
// returning all filtered data and replies.
func feedAll(s *negState, in []byte) (out, reply []byte) {
	buf := make([]byte, len(in))
	n, r := s.feed(in, buf)
	return append([]byte(nil), buf[:n]...), r
}

func TestFeed_PlainDataPassesThrough(t *testing.T) {
	s := newTestState("xterm", 80, 24)
	out, reply := feedAll(s, []byte("hello world"))
	if string(out) != "hello world" {
		t.Fatalf("got %q, want %q", out, "hello world")
	}
	if len(reply) != 0 {
		t.Fatalf("unexpected reply: % x", reply)
	}
}

func TestFeed_StripIACSequences(t *testing.T) {
	s := newTestState("xterm", 80, 24)
	// "A" IAC DO ECHO "B" IAC WILL SGA "C"
	in := []byte{'A', IAC, DO, OptECHO, 'B', IAC, WILL, OptSGA, 'C'}
	out, reply := feedAll(s, in)
	if string(out) != "ABC" {
		t.Fatalf("got %q, want ABC", out)
	}
	// Expect WONT ECHO, DO SGA.
	wantReply := []byte{IAC, WONT, OptECHO, IAC, DO, OptSGA}
	if !bytes.Equal(reply, wantReply) {
		t.Fatalf("reply = % x, want % x", reply, wantReply)
	}
}

func TestFeed_DoECHO_WONT(t *testing.T) {
	s := newTestState("xterm", 80, 24)
	_, reply := feedAll(s, []byte{IAC, DO, OptECHO})
	want := []byte{IAC, WONT, OptECHO}
	if !bytes.Equal(reply, want) {
		t.Fatalf("reply=% x want % x", reply, want)
	}
}

func TestFeed_DoTTYPE_WILL(t *testing.T) {
	s := newTestState("xterm", 80, 24)
	_, reply := feedAll(s, []byte{IAC, DO, OptTTYPE})
	want := []byte{IAC, WILL, OptTTYPE}
	if !bytes.Equal(reply, want) {
		t.Fatalf("reply=% x want % x", reply, want)
	}
}

func TestFeed_DoNAWS_WILLPlusUnsolicitedSize(t *testing.T) {
	s := newTestState("xterm", 120, 40)
	_, reply := feedAll(s, []byte{IAC, DO, OptNAWS})
	// Must start with IAC WILL NAWS followed by buildNAWS(120, 40).
	wantPrefix := []byte{IAC, WILL, OptNAWS}
	wantFrame := buildNAWS(120, 40)
	want := append(append([]byte{}, wantPrefix...), wantFrame...)
	if !bytes.Equal(reply, want) {
		t.Fatalf("reply=% x want % x", reply, want)
	}
}

func TestFeed_WillECHO_DO(t *testing.T) {
	s := newTestState("xterm", 80, 24)
	_, reply := feedAll(s, []byte{IAC, WILL, OptECHO})
	want := []byte{IAC, DO, OptECHO}
	if !bytes.Equal(reply, want) {
		t.Fatalf("reply=% x want % x", reply, want)
	}
}

func TestFeed_WillUnknown_DONT(t *testing.T) {
	s := newTestState("xterm", 80, 24)
	// option 99 is unknown.
	_, reply := feedAll(s, []byte{IAC, WILL, 99})
	want := []byte{IAC, DONT, 99}
	if !bytes.Equal(reply, want) {
		t.Fatalf("reply=% x want % x", reply, want)
	}
}

func TestFeed_DoUnknown_WONT(t *testing.T) {
	s := newTestState("xterm", 80, 24)
	_, reply := feedAll(s, []byte{IAC, DO, 99})
	want := []byte{IAC, WONT, 99}
	if !bytes.Equal(reply, want) {
		t.Fatalf("reply=% x want % x", reply, want)
	}
}

func TestFeed_IACIACLiteral(t *testing.T) {
	s := newTestState("xterm", 80, 24)
	out, reply := feedAll(s, []byte{'x', IAC, IAC, 'y'})
	want := []byte{'x', 0xFF, 'y'}
	if !bytes.Equal(out, want) {
		t.Fatalf("out=% x want % x", out, want)
	}
	if len(reply) != 0 {
		t.Fatalf("unexpected reply: % x", reply)
	}
}

func TestFeed_TTYPESubnegReply(t *testing.T) {
	s := newTestState("xterm-256color", 80, 24)
	// IAC SB TTYPE SEND IAC SE
	in := []byte{IAC, SB, OptTTYPE, ttypeSEND, IAC, SE}
	out, reply := feedAll(s, in)
	if len(out) != 0 {
		t.Fatalf("unexpected data: % x", out)
	}
	want := []byte{IAC, SB, OptTTYPE, ttypeIS}
	want = append(want, []byte("xterm-256color")...)
	want = append(want, IAC, SE)
	if !bytes.Equal(reply, want) {
		t.Fatalf("reply=% x want % x", reply, want)
	}
}

func TestFeed_SplitIACAcrossReads(t *testing.T) {
	s := newTestState("xterm", 80, 24)
	// Feed "IAC" alone.
	out1, r1 := feedAll(s, []byte{IAC})
	if len(out1) != 0 || len(r1) != 0 {
		t.Fatalf("first chunk should buffer: out=%q reply=% x", out1, r1)
	}
	// Feed "DO ECHO" separately.
	out2, r2 := feedAll(s, []byte{DO, OptECHO})
	if len(out2) != 0 {
		t.Fatalf("unexpected data: % x", out2)
	}
	want := []byte{IAC, WONT, OptECHO}
	if !bytes.Equal(r2, want) {
		t.Fatalf("reply=% x want % x", r2, want)
	}

	// Also: split right between SB option and data.
	s2 := newTestState("xt", 80, 24)
	_, _ = feedAll(s2, []byte{IAC, SB, OptTTYPE})
	_, _ = feedAll(s2, []byte{ttypeSEND})
	_, reply := feedAll(s2, []byte{IAC, SE})
	wantSB := []byte{IAC, SB, OptTTYPE, ttypeIS}
	wantSB = append(wantSB, []byte("xt")...)
	wantSB = append(wantSB, IAC, SE)
	if !bytes.Equal(reply, wantSB) {
		t.Fatalf("reply=% x want % x", reply, wantSB)
	}
}

func TestBuildNAWS_Encoding(t *testing.T) {
	// 80x24 -> 0 80 0 24 with no escaping.
	got := buildNAWS(80, 24)
	want := []byte{IAC, SB, OptNAWS, 0, 80, 0, 24, IAC, SE}
	if !bytes.Equal(got, want) {
		t.Fatalf("buildNAWS(80,24)=% x want % x", got, want)
	}
}

func TestBuildNAWS_EscapesIACParameter(t *testing.T) {
	// cols=255 => hi=0, lo=0xFF must be doubled.
	got := buildNAWS(255, 24)
	want := []byte{IAC, SB, OptNAWS, 0, 255, 255, 0, 24, IAC, SE}
	if !bytes.Equal(got, want) {
		t.Fatalf("buildNAWS(255,24)=% x want % x", got, want)
	}
}

func TestParseNAWS_RoundTrip(t *testing.T) {
	for _, tc := range []struct{ c, r int }{
		{80, 24},
		{132, 43},
		{0, 0},
		{255, 255},
		{1000, 500},
	} {
		frame := buildNAWS(tc.c, tc.r)
		// Strip framing and collapse 0xFF 0xFF pairs.
		if len(frame) < 5 || frame[0] != IAC || frame[1] != SB || frame[2] != OptNAWS {
			t.Fatalf("bad frame: % x", frame)
		}
		inner := frame[3 : len(frame)-2] // drop trailing IAC SE
		collapsed := make([]byte, 0, 4)
		for i := 0; i < len(inner); i++ {
			b := inner[i]
			if b == IAC && i+1 < len(inner) && inner[i+1] == IAC {
				collapsed = append(collapsed, 0xFF)
				i++
			} else {
				collapsed = append(collapsed, b)
			}
		}
		c, r, ok := parseNAWS(collapsed)
		if !ok {
			t.Fatalf("parseNAWS failed for cols=%d rows=%d", tc.c, tc.r)
		}
		if c != tc.c || r != tc.r {
			t.Fatalf("round-trip: got %dx%d want %dx%d", c, r, tc.c, tc.r)
		}
	}
}

func TestEscapeIAC(t *testing.T) {
	if got := escapeIAC([]byte("abc")); !bytes.Equal(got, []byte("abc")) {
		t.Fatalf("fast path: got % x", got)
	}
	got := escapeIAC([]byte{'a', 0xFF, 'b', 0xFF, 0xFF})
	want := []byte{'a', 0xFF, 0xFF, 'b', 0xFF, 0xFF, 0xFF, 0xFF}
	if !bytes.Equal(got, want) {
		t.Fatalf("got % x want % x", got, want)
	}
}
