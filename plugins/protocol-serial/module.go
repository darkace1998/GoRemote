package serial

import (
	"context"
	"errors"
	"fmt"
	"strings"

	goserial "go.bug.st/serial"

	"github.com/goremote/goremote/sdk/plugin"
	"github.com/goremote/goremote/sdk/protocol"
)

// Setting keys exposed by the Serial plugin.
const (
	SettingDevice   = "device"
	SettingBaud     = "baud_rate"
	SettingDataBits = "data_bits"
	SettingParity   = "parity"
	SettingStopBits = "stop_bits"
	SettingEOLMode  = "eol_mode"
	SettingEncoding = "encoding"
)

// Parity option values matching the user-visible enum.
const (
	ParityNone = "none"
	ParityOdd  = "odd"
	ParityEven = "even"
)

// StopBits option values.
const (
	StopBitsOne        = "1"
	StopBitsOneAndHalf = "1.5"
	StopBitsTwo        = "2"
)

// End-of-line modes used by SendInput when the supplied data does not
// already end in a newline.
const (
	EOLModeLF   = "lf"
	EOLModeCRLF = "crlf"
	EOLModeCR   = "cr"
	EOLModeNone = "none"
)

// Module is the built-in Serial protocol module. The zero value is a
// ready-to-use Module; [Module.Open] creates an independent [Session] per
// call.
type Module struct{}

// New returns a ready-to-use [Module].
func New() *Module { return &Module{} }

// Manifest returns the static manifest for this plugin.
func (m *Module) Manifest() plugin.Manifest { return Manifest }

func ptrInt(v int) *int { return &v }

// Settings returns the protocol-specific setting schema.
func (m *Module) Settings() []protocol.SettingDef {
	return []protocol.SettingDef{
		{
			Key: SettingDevice, Label: "Device", Type: protocol.SettingString,
			Required:    true,
			Description: "Serial device path. On Linux/macOS something like /dev/ttyUSB0 or /dev/tty.usbserial-XYZ; on Windows COM3, COM4, etc.",
		},
		{
			Key: SettingBaud, Label: "Baud rate", Type: protocol.SettingInt,
			Default:     115200,
			Min:         ptrInt(50),
			Max:         ptrInt(4000000),
			Description: "Bitrate. Common values: 9600, 19200, 38400, 57600, 115200.",
		},
		{
			Key: SettingDataBits, Label: "Data bits", Type: protocol.SettingInt,
			Default:     8,
			Min:         ptrInt(5),
			Max:         ptrInt(8),
			Description: "Character size in bits (5, 6, 7, or 8).",
		},
		{
			Key: SettingParity, Label: "Parity", Type: protocol.SettingEnum,
			Default:     ParityNone,
			EnumValues:  []string{ParityNone, ParityOdd, ParityEven},
			Description: "Parity bit setting.",
		},
		{
			Key: SettingStopBits, Label: "Stop bits", Type: protocol.SettingEnum,
			Default:     StopBitsOne,
			EnumValues:  []string{StopBitsOne, StopBitsOneAndHalf, StopBitsTwo},
			Description: "Number of stop bits framing each character.",
		},
		{
			Key: SettingEOLMode, Label: "End-of-line mode", Type: protocol.SettingEnum,
			Default:     EOLModeLF,
			EnumValues:  []string{EOLModeLF, EOLModeCRLF, EOLModeCR, EOLModeNone},
			Description: "Line ending appended by SendInput when data does not already end in a newline. \"none\" sends bytes verbatim.",
		},
		{
			Key: SettingEncoding, Label: "Character encoding", Type: protocol.SettingEnum,
			Default:     "utf-8",
			EnumValues:  []string{"utf-8", "iso-8859-1"},
			Description: "Advertised encoding for the rendered terminal view. The raw byte stream is transmitted unchanged.",
		},
	}
}

// Capabilities reports the runtime capabilities advertised by the Serial
// module.
func (m *Module) Capabilities() protocol.Capabilities {
	return protocol.Capabilities{
		RenderModes:       []protocol.RenderMode{protocol.RenderTerminal},
		AuthMethods:       []protocol.AuthMethod{protocol.AuthNone},
		SupportsResize:    false,
		SupportsClipboard: false,
		SupportsLogging:   true,
		SupportsReconnect: true,
	}
}

// settingsView is a typed view over the untyped settings map.
type settingsView struct{ m map[string]any }

func (s settingsView) stringOr(key, def string) string {
	if v, ok := s.m[key]; ok {
		if x, ok := v.(string); ok && x != "" {
			return x
		}
	}
	return def
}

func (s settingsView) intOr(key string, def int) int {
	if v, ok := s.m[key]; ok {
		switch x := v.(type) {
		case int:
			return x
		case int64:
			return int(x)
		case float64:
			return int(x)
		}
	}
	return def
}

// Open validates the settings, opens the serial port via go.bug.st/serial,
// and returns a [Session] that owns the underlying port handle.
//
// ctx is honored at validation/open time; once the port is open, ctx
// cancellation is propagated through Session.Start (which closes the port
// to unblock the read loop).
func (m *Module) Open(ctx context.Context, req protocol.OpenRequest) (protocol.Session, error) {
	view := settingsView{m: req.Settings}

	device := view.stringOr(SettingDevice, "")
	if device == "" {
		// Allow Host to substitute for Device, so existing connection
		// definitions that use the generic Host field still work.
		device = strings.TrimSpace(req.Host)
	}
	if device == "" {
		return nil, errors.New("serial: device is required")
	}

	baud := view.intOr(SettingBaud, 115200)
	if baud < 50 || baud > 4000000 {
		return nil, fmt.Errorf("serial: baud rate %d out of range [50, 4000000]", baud)
	}

	dataBits := view.intOr(SettingDataBits, 8)
	switch dataBits {
	case 5, 6, 7, 8:
	default:
		return nil, fmt.Errorf("serial: data_bits must be 5/6/7/8, got %d", dataBits)
	}

	var parity goserial.Parity
	switch view.stringOr(SettingParity, ParityNone) {
	case ParityNone:
		parity = goserial.NoParity
	case ParityOdd:
		parity = goserial.OddParity
	case ParityEven:
		parity = goserial.EvenParity
	default:
		return nil, fmt.Errorf("serial: invalid parity")
	}

	var stopBits goserial.StopBits
	switch view.stringOr(SettingStopBits, StopBitsOne) {
	case StopBitsOne:
		stopBits = goserial.OneStopBit
	case StopBitsOneAndHalf:
		stopBits = goserial.OnePointFiveStopBits
	case StopBitsTwo:
		stopBits = goserial.TwoStopBits
	default:
		return nil, fmt.Errorf("serial: invalid stop_bits")
	}

	eolMode := view.stringOr(SettingEOLMode, EOLModeLF)
	switch eolMode {
	case EOLModeLF, EOLModeCRLF, EOLModeCR, EOLModeNone:
	default:
		return nil, fmt.Errorf("serial: invalid eol_mode %q", eolMode)
	}

	mode := &goserial.Mode{
		BaudRate: baud,
		DataBits: dataBits,
		Parity:   parity,
		StopBits: stopBits,
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	port, err := goserial.Open(device, mode)
	if err != nil {
		return nil, fmt.Errorf("serial: open %s: %w", device, err)
	}

	return newSession(port, eolMode), nil
}
