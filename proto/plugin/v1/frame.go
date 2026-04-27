package pluginv1

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

const maxFrameSize = 4 << 20 // 4 MB

// Frame is the envelope for request and response messages.
type Frame struct {
	Method  string          `json:"method,omitempty"`  // set on requests
	ID      uint64          `json:"id"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   string          `json:"error,omitempty"` // set on error responses
}

// WriteFrame encodes f as JSON and writes it with a 4-byte length prefix.
func WriteFrame(w io.Writer, f Frame) error {
	data, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("pluginv1: marshal frame: %w", err)
	}
	if len(data) > maxFrameSize {
		return fmt.Errorf("pluginv1: frame too large (%d bytes)", len(data))
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(data)))
	if _, err := w.Write(hdr[:]); err != nil {
		return fmt.Errorf("pluginv1: write header: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("pluginv1: write body: %w", err)
	}
	return nil
}

// ReadFrame reads a length-prefixed JSON frame.
func ReadFrame(r io.Reader) (Frame, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return Frame{}, fmt.Errorf("pluginv1: read header: %w", err)
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > maxFrameSize {
		return Frame{}, fmt.Errorf("pluginv1: frame size %d exceeds limit", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return Frame{}, fmt.Errorf("pluginv1: read body: %w", err)
	}
	var f Frame
	if err := json.Unmarshal(buf, &f); err != nil {
		return Frame{}, fmt.Errorf("pluginv1: unmarshal frame: %w", err)
	}
	return f, nil
}
