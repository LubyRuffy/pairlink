package protocol

import (
	"encoding/binary"
	"fmt"
)

const (
	Magic              = "PLK1"
	TypeHandshake byte = 0x01
	TypeData           = 0x02
	TypeDisco          = 0x03
	TypeObserved       = 0x04
	TypePath           = 0x05
	// TypeLabel is a hub-visible name announcement, same class as TypePath:
	// the hub stores it and does not forward it. It is not a handshake and
	// not an application payload.
	TypeLabel     = 0x06
	TypePunchPing = 0x10
	TypePunchPong = 0x11
	SessionIDSize = 16
	KeySize       = 32
	maxPayload    = 64 << 10
)

// Path names the data plane the client last used. Hub never sets this on
// application frames; it is troubleshooting metadata from the endpoints.
const (
	PathRelay  = "relay"
	PathDirect = "direct"
)

// Path codes are the only legal TypePath payload. One byte, not application
// JSON, so a hub can record the data plane without opening a session.
const (
	PathCodeRelay  byte = 1
	PathCodeDirect byte = 2
)

// PathFromCode maps a TypePath payload byte to relay or direct.
func PathFromCode(code byte) (string, bool) {
	switch code {
	case PathCodeRelay:
		return PathRelay, true
	case PathCodeDirect:
		return PathDirect, true
	default:
		return "", false
	}
}

// Frame is one hop on the relay WebSocket or a direct UDP datagram.
type Frame struct {
	Type      byte
	Dst       [KeySize]byte
	Src       [KeySize]byte
	SessionID [SessionIDSize]byte
	Payload   []byte
}

// Marshal packs a frame. The hub copies these bytes; it does not parse Payload
// for TypeData / TypeHandshake / TypeDisco.
func (f Frame) Marshal() ([]byte, error) {
	if len(f.Payload) > maxPayload {
		return nil, fmt.Errorf("protocol: payload too large")
	}
	out := make([]byte, 4+1+KeySize+KeySize+SessionIDSize+4+len(f.Payload))
	copy(out[0:4], Magic)
	out[4] = f.Type
	copy(out[5:37], f.Dst[:])
	copy(out[37:69], f.Src[:])
	copy(out[69:85], f.SessionID[:])
	binary.BigEndian.PutUint32(out[85:89], uint32(len(f.Payload)))
	copy(out[89:], f.Payload)
	return out, nil
}

// UnmarshalFrame reads one frame. Extra trailing bytes are an error so UDP
// padding cannot smuggle a second message.
func UnmarshalFrame(b []byte) (Frame, error) {
	var f Frame
	if len(b) < 89 {
		return f, fmt.Errorf("protocol: short frame")
	}
	if string(b[0:4]) != Magic {
		return f, fmt.Errorf("protocol: bad magic")
	}
	f.Type = b[4]
	copy(f.Dst[:], b[5:37])
	copy(f.Src[:], b[37:69])
	copy(f.SessionID[:], b[69:85])
	n := binary.BigEndian.Uint32(b[85:89])
	if n > maxPayload {
		return f, fmt.Errorf("protocol: payload too large")
	}
	if len(b) != 89+int(n) {
		return f, fmt.Errorf("protocol: length mismatch")
	}
	f.Payload = append([]byte(nil), b[89:]...)
	return f, nil
}
