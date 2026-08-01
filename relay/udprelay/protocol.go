package udprelay

import (
	"errors"

	"github.com/google/uuid"
)

// Wire protocol between a NexusVPN client and a relay node.
//
// Every datagram starts with a one-byte frame type. Relayed payloads are
// opaque: the relay forwards WireGuard datagrams byte-for-byte and never
// holds the keys to decrypt them, so confidentiality and integrity remain
// end-to-end between the two peers.
//
// A raw WireGuard datagram carries no addressing the relay can route on
// (the receiver index is meaningful only to the endpoints), so DATA frames
// name their destination device explicitly. The relay rewrites that field
// to the *sender's* device ID before forwarding, so the receiving peer
// learns who a packet came from. This mirrors how Tailscale's DERP relays
// frame traffic.
//
//	BIND      0x01  [type][16-byte device UUID][token bytes...]
//	BIND_ACK  0x02  [type]
//	DATA      0x03  [type][16-byte peer UUID][opaque WireGuard payload...]
//	KEEPALIVE 0x04  [type]
//	ERROR     0x05  [type][utf-8 reason...]
const (
	FrameBind      byte = 0x01
	FrameBindAck   byte = 0x02
	FrameData      byte = 0x03
	FrameKeepalive byte = 0x04
	FrameError     byte = 0x05
)

// headerLen is the frame type byte plus the 16-byte device UUID carried by
// BIND and DATA frames.
const headerLen = 1 + 16

// MaxPacketSize bounds a single relayed datagram. WireGuard's own maximum
// transport packet is well under this once the frame header is added.
const MaxPacketSize = 1500 + headerLen

var (
	// ErrShortFrame indicates a datagram too small to be a valid frame.
	ErrShortFrame = errors.New("relay: short frame")
	// ErrUnknownFrame indicates an unrecognized frame type byte.
	ErrUnknownFrame = errors.New("relay: unknown frame type")
)

// EncodeBind builds a BIND frame carrying the sender's device ID and its
// relay session token.
func EncodeBind(deviceID uuid.UUID, sessionToken string) []byte {
	buf := make([]byte, headerLen+len(sessionToken))
	buf[0] = FrameBind
	copy(buf[1:headerLen], deviceID[:])
	copy(buf[headerLen:], sessionToken)
	return buf
}

// EncodeBindAck builds a BIND_ACK frame.
func EncodeBindAck() []byte { return []byte{FrameBindAck} }

// EncodeKeepalive builds a KEEPALIVE frame.
func EncodeKeepalive() []byte { return []byte{FrameKeepalive} }

// EncodeError builds an ERROR frame carrying a human-readable reason.
func EncodeError(reason string) []byte {
	return append([]byte{FrameError}, reason...)
}

// EncodeData wraps an opaque payload (a WireGuard datagram) in a DATA frame
// addressed to peerID.
func EncodeData(peerID uuid.UUID, payload []byte) []byte {
	buf := make([]byte, headerLen+len(payload))
	buf[0] = FrameData
	copy(buf[1:headerLen], peerID[:])
	copy(buf[headerLen:], payload)
	return buf
}

// DecodeBind extracts the device ID and session token from a BIND frame.
func DecodeBind(frame []byte) (uuid.UUID, string, error) {
	if len(frame) < headerLen || frame[0] != FrameBind {
		return uuid.Nil, "", ErrShortFrame
	}
	var id uuid.UUID
	copy(id[:], frame[1:headerLen])
	return id, string(frame[headerLen:]), nil
}

// DecodeData extracts the destination device ID and opaque payload from a
// DATA frame. The returned payload aliases frame; callers that retain it
// beyond the current read buffer must copy it.
func DecodeData(frame []byte) (uuid.UUID, []byte, error) {
	if len(frame) < headerLen || frame[0] != FrameData {
		return uuid.Nil, nil, ErrShortFrame
	}
	var id uuid.UUID
	copy(id[:], frame[1:headerLen])
	return id, frame[headerLen:], nil
}

// rewriteDataPeer replaces the peer-UUID field of a DATA frame in place, so
// a frame addressed *to* a peer becomes a frame stamped *from* the sender.
func rewriteDataPeer(frame []byte, senderID uuid.UUID) {
	copy(frame[1:headerLen], senderID[:])
}

// DecodeError extracts the reason from an ERROR frame.
func DecodeError(frame []byte) (string, error) {
	if len(frame) < 1 || frame[0] != FrameError {
		return "", ErrShortFrame
	}
	return string(frame[1:]), nil
}
