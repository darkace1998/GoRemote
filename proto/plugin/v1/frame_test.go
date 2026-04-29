package pluginv1

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func TestWriteFrameRejectsOversize(t *testing.T) {
	var buf bytes.Buffer
	err := WriteFrame(&buf, Frame{Method: strings.Repeat("a", maxFrameSize)})
	if err == nil {
		t.Fatal("expected oversized frame to fail")
	}
}

func TestReadFrameRejectsOversizeHeader(t *testing.T) {
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], maxFrameSize+1)
	_, err := ReadFrame(bytes.NewReader(hdr[:]))
	if err == nil {
		t.Fatal("expected oversize header to fail")
	}
}

func TestCheckedFrameLenRejectsPlatformOverflow(t *testing.T) {
	_, err := checkedFrameLen(^uint32(0))
	if err == nil {
		t.Fatal("expected large frame length to fail when beyond configured max")
	}
}
