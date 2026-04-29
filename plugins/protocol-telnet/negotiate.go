package telnet

import (
	"net"
	"sync"
)

// Telnet command bytes (RFC 854 §Definitions of Commands).
const (
	// IAC (Interpret As Command) is the escape byte that introduces every
	// Telnet command.
	IAC byte = 255
	// DONT requests that the peer stop performing the option.
	DONT byte = 254
	// DO requests that the peer perform the option.
	DO byte = 253
	// WONT indicates refusal (or agreement to stop) performing an option.
	WONT byte = 252
	// WILL indicates willingness to perform an option.
	WILL byte = 251
	// SB begins a subnegotiation block.
	SB byte = 250
	// SE ends a subnegotiation block.
	SE byte = 240
	// NOP is "no operation" (RFC 854) – ignored on the data path.
	NOP byte = 241
)

// Telnet option codes used by this plugin.
const (
	// OptBINARY enables 8-bit binary transmission (RFC 856).
	OptBINARY byte = 0
	// OptECHO requests the peer to echo what it receives (RFC 857).
	OptECHO byte = 1
	// OptSGA suppresses the classical "go-ahead" signal (RFC 858).
	OptSGA byte = 3
	// OptTTYPE negotiates the terminal type (RFC 1091).
	OptTTYPE byte = 24
	// OptNAWS negotiates the window size (RFC 1073).
	OptNAWS byte = 31
	// OptLINEMODE is the LINEMODE option (RFC 1184). This plugin refuses it
	// and forces character-at-a-time via SGA.
	OptLINEMODE byte = 34
)

// TTYPE subnegotiation command bytes (RFC 1091).
const (
	// ttypeIS is used in "IAC SB TTYPE IS <name> IAC SE" replies.
	ttypeIS byte = 0
	// ttypeSEND is used in "IAC SB TTYPE SEND IAC SE" requests.
	ttypeSEND byte = 1
)

// read state machine modes.
const (
	stNormal = iota // regular data byte
	stIAC           // got IAC, awaiting command byte
	stWill          // got IAC WILL, awaiting option
	stWont          // got IAC WONT, awaiting option
	stDo            // got IAC DO,   awaiting option
	stDont          // got IAC DONT, awaiting option
	stSB            // got IAC SB,   awaiting option
	stSBData        // inside SB data
	stSBIAC         // inside SB, got IAC (either IAC IAC or IAC SE)
)

// negState is the byte-level Telnet negotiation state machine. It is not safe
// for concurrent use; callers must serialise access (typically by only
// invoking it from a single Read goroutine).
type negState struct {
	termType string

	// Last advertised window size (mutex-protected because SendNAWS may be
	// called from arbitrary goroutines while the read loop advances the
	// state machine).
	sizeMu sync.Mutex
	cols   int
	rows   int

	mode  int
	sbOpt byte
	sbBuf []byte
}

// feed consumes input bytes, writing filtered application data into out and
// returning any negotiation reply bytes that must be written back to the
// peer. It returns the number of bytes written to out and the reply buffer
// (which may be nil if nothing needs to be sent).
//
// len(out) MUST be >= len(in) because filtered data can never exceed input
// size (we only ever strip bytes, never inject).
func (s *negState) feed(in, out []byte) (int, []byte) {
	var reply []byte
	n := 0
	for _, b := range in {
		switch s.mode {
		case stNormal:
			if b == IAC {
				s.mode = stIAC
			} else {
				out[n] = b
				n++
			}
		case stIAC:
			switch b {
			case IAC:
				out[n] = 0xFF
				n++
				s.mode = stNormal
			case WILL:
				s.mode = stWill
			case WONT:
				s.mode = stWont
			case DO:
				s.mode = stDo
			case DONT:
				s.mode = stDont
			case SB:
				s.mode = stSB
			default:
				// NOP, DM, BRK, AYT, etc – no reply, just consume.
				s.mode = stNormal
			}
		case stWill:
			reply = append(reply, s.handleWill(b)...)
			s.mode = stNormal
		case stWont:
			// Peer will not perform option; acknowledge silently.
			s.mode = stNormal
		case stDo:
			reply = append(reply, s.handleDo(b)...)
			s.mode = stNormal
		case stDont:
			// Peer asking us not to perform; accept silently.
			s.mode = stNormal
		case stSB:
			s.sbOpt = b
			s.sbBuf = s.sbBuf[:0]
			s.mode = stSBData
		case stSBData:
			if b == IAC {
				s.mode = stSBIAC
			} else {
				s.sbBuf = append(s.sbBuf, b)
			}
		case stSBIAC:
			switch b {
			case IAC:
				// Escaped literal 0xFF inside subnegotiation data.
				s.sbBuf = append(s.sbBuf, 0xFF)
				s.mode = stSBData
			case SE:
				reply = append(reply, s.handleSB()...)
				s.mode = stNormal
			default:
				// Malformed SB; abandon and resync.
				s.mode = stNormal
			}
		}
	}
	return n, reply
}

// handleWill responds to an incoming IAC WILL <opt> negotiation from the peer.
func (s *negState) handleWill(opt byte) []byte {
	switch opt {
	case OptECHO:
		// Server offers to echo: accept so it drives character echo.
		return []byte{IAC, DO, OptECHO}
	case OptSGA:
		// Accept: run in character-at-a-time mode.
		return []byte{IAC, DO, OptSGA}
	case OptBINARY:
		return []byte{IAC, DO, OptBINARY}
	default:
		return []byte{IAC, DONT, opt}
	}
}

// handleDo responds to an incoming IAC DO <opt> negotiation from the peer.
func (s *negState) handleDo(opt byte) []byte {
	switch opt {
	case OptECHO:
		// We never echo locally on behalf of the remote.
		return []byte{IAC, WONT, OptECHO}
	case OptSGA:
		return []byte{IAC, WILL, OptSGA}
	case OptTTYPE:
		return []byte{IAC, WILL, OptTTYPE}
	case OptNAWS:
		// Announce willingness, then send current size unsolicited –
		// every server we care about expects this.
		reply := []byte{IAC, WILL, OptNAWS}
		s.sizeMu.Lock()
		cols, rows := s.cols, s.rows
		s.sizeMu.Unlock()
		reply = append(reply, buildNAWS(cols, rows)...)
		return reply
	case OptBINARY:
		return []byte{IAC, WILL, OptBINARY}
	default:
		return []byte{IAC, WONT, opt}
	}
}

// handleSB is called when a complete "IAC SB <opt> ... IAC SE" block has been
// collected in s.sbBuf (which does NOT include the framing bytes).
func (s *negState) handleSB() []byte {
	switch s.sbOpt {
	case OptTTYPE:
		// RFC 1091: "IAC SB TTYPE SEND IAC SE" requests a TTYPE IS reply.
		if len(s.sbBuf) >= 1 && s.sbBuf[0] == ttypeSEND {
			out := []byte{IAC, SB, OptTTYPE, ttypeIS}
			out = append(out, escapeIAC([]byte(s.termType))...)
			out = append(out, IAC, SE)
			return out
		}
	}
	return nil
}

// buildNAWS encodes a window size change as the full
// "IAC SB NAWS <hi1> <lo1> <hi2> <lo2> IAC SE" frame (RFC 1073), doubling
// any 0xFF bytes in the four parameter bytes.
func buildNAWS(cols, rows int) []byte {
	if cols < 0 {
		cols = 0
	}
	if cols > 0xFFFF {
		cols = 0xFFFF
	}
	if rows < 0 {
		rows = 0
	}
	if rows > 0xFFFF {
		rows = 0xFFFF
	}
	params := []byte{
		byte(cols >> 8), byte(cols & 0xFF),
		byte(rows >> 8), byte(rows & 0xFF),
	}
	out := []byte{IAC, SB, OptNAWS}
	for _, b := range params {
		if b == IAC {
			out = append(out, IAC, IAC)
		} else {
			out = append(out, b)
		}
	}
	out = append(out, IAC, SE)
	return out
}

// parseNAWS decodes the data portion of a NAWS subnegotiation (i.e. the bytes
// between "IAC SB NAWS" and "IAC SE", with any doubled 0xFF already collapsed
// to a single 0xFF). It returns (cols, rows, ok).
func parseNAWS(sb []byte) (int, int, bool) {
	if len(sb) != 4 {
		return 0, 0, false
	}
	cols := int(sb[0])<<8 | int(sb[1])
	rows := int(sb[2])<<8 | int(sb[3])
	return cols, rows, true
}

// escapeIAC returns a copy of p with every 0xFF byte doubled, as required
// inside subnegotiation payloads and on the main data channel.
func escapeIAC(p []byte) []byte {
	// Fast path: nothing to escape.
	hasIAC := false
	for _, b := range p {
		if b == IAC {
			hasIAC = true
			break
		}
	}
	if !hasIAC {
		out := make([]byte, len(p))
		copy(out, p)
		return out
	}
	out := make([]byte, 0, len(p))
	for _, b := range p {
		if b == IAC {
			out = append(out, IAC, IAC)
		} else {
			out = append(out, b)
		}
	}
	return out
}

// Negotiator wraps a net.Conn and transparently handles Telnet option
// negotiation on the read path and IAC-escaping on the write path. It
// implements net.Conn-like Read/Write and is safe for concurrent Read/Write
// from separate goroutines (as the underlying net.Conn is).
type Negotiator struct {
	conn    net.Conn
	state   negState
	writeMu sync.Mutex

	// readBuf is a reusable scratch buffer for conn reads.
	readBuf []byte
}

// NewNegotiator builds a Negotiator around conn using the given advertised
// terminal type and initial window size. A zero size (0,0) means "unknown";
// NAWS responses in that state will advertise 0x0.
func NewNegotiator(conn net.Conn, termType string, cols, rows int) *Negotiator {
	if termType == "" {
		termType = "xterm"
	}
	n := &Negotiator{conn: conn}
	n.state.termType = termType
	n.state.cols = cols
	n.state.rows = rows
	return n
}

// Conn returns the wrapped net.Conn. Callers must not Read from it directly
// once the Negotiator is in use, but may use it for deadlines/metadata.
func (n *Negotiator) Conn() net.Conn { return n.conn }

// Read fills p with decoded application data. Negotiation bytes are consumed
// internally and the corresponding responses are written back to the peer.
//
// Because a chunk from the underlying conn may contain nothing but negotiation
// bytes, Read loops internally until it has at least one application byte or
// an error.
func (n *Negotiator) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if cap(n.readBuf) < len(p) {
		n.readBuf = make([]byte, len(p))
	}
	buf := n.readBuf[:len(p)]
	for {
		nr, err := n.conn.Read(buf)
		var nOut int
		var reply []byte
		if nr > 0 {
			nOut, reply = n.state.feed(buf[:nr], p)
		}
		if len(reply) > 0 {
			if werr := n.writeRaw(reply); werr != nil && err == nil {
				err = werr
			}
		}
		if nOut > 0 || err != nil {
			return nOut, err
		}
		// Pure negotiation chunk with no error: read more.
	}
}

// Write sends p to the peer, doubling any embedded 0xFF bytes so they are not
// interpreted as IAC. Returns len(p) on success (matching the io.Writer
// contract against the caller's view of the data), not the number of bytes
// actually placed on the wire.
func (n *Negotiator) Write(p []byte) (int, error) {
	escaped := escapeIAC(p)
	if err := n.writeRaw(escaped); err != nil {
		return 0, err
	}
	return len(p), nil
}

// writeRaw is the single chokepoint for bytes placed on the wire; it holds
// writeMu so replies generated by the read state machine do not interleave
// with user data.
func (n *Negotiator) writeRaw(p []byte) error {
	n.writeMu.Lock()
	defer n.writeMu.Unlock()
	for len(p) > 0 {
		nw, err := n.conn.Write(p)
		if err != nil {
			return err
		}
		p = p[nw:]
	}
	return nil
}

// SendNAWS sends an unsolicited NAWS subnegotiation announcing the given
// window size and stores it so future "DO NAWS" replies advertise the new
// value.
func (n *Negotiator) SendNAWS(cols, rows int) error {
	n.state.sizeMu.Lock()
	n.state.cols = cols
	n.state.rows = rows
	n.state.sizeMu.Unlock()
	return n.writeRaw(buildNAWS(cols, rows))
}

// Close closes the underlying connection.
func (n *Negotiator) Close() error { return n.conn.Close() }
