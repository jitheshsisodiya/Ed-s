package relayproxy

import (
	"errors"

	"github.com/google/uuid"
)

// Relay wire protocol, mirroring relay/internal/relay/protocol.go.
//
// The definition is duplicated rather than imported so the client and the
// relay stay independent Go modules; the two files must be changed together.
// See relay/README.md for the authoritative description.
const (
	frameBind      byte = 0x01
	frameBindAck   byte = 0x02
	frameData      byte = 0x03
	frameKeepalive byte = 0x04
	frameError     byte = 0x05
)

// headerLen is the frame type byte plus a 16-byte device UUID.
const headerLen = 1 + 16

// maxPacketSize bounds a single relayed datagram.
const maxPacketSize = 1500 + headerLen

var errShortFrame = errors.New("relayproxy: short frame")

func encodeBind(deviceID uuid.UUID, sessionToken string) []byte {
	buf := make([]byte, headerLen+len(sessionToken))
	buf[0] = frameBind
	copy(buf[1:headerLen], deviceID[:])
	copy(buf[headerLen:], sessionToken)
	return buf
}

func encodeKeepalive() []byte { return []byte{frameKeepalive} }

// encodeData wraps an opaque WireGuard datagram in a DATA frame addressed
// to peerID.
func encodeData(peerID uuid.UUID, payload []byte) []byte {
	buf := make([]byte, headerLen+len(payload))
	buf[0] = frameData
	copy(buf[1:headerLen], peerID[:])
	copy(buf[headerLen:], payload)
	return buf
}

// decodeData extracts the sender device ID and opaque payload from a DATA
// frame. The payload aliases frame.
func decodeData(frame []byte) (uuid.UUID, []byte, error) {
	if len(frame) < headerLen || frame[0] != frameData {
		return uuid.Nil, nil, errShortFrame
	}
	var id uuid.UUID
	copy(id[:], frame[1:headerLen])
	return id, frame[headerLen:], nil
}

func decodeError(frame []byte) (string, error) {
	if len(frame) < 1 || frame[0] != frameError {
		return "", errShortFrame
	}
	return string(frame[1:]), nil
}
