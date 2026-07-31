// Package disco implements NexusVPN's peer discovery protocol: small
// ping/pong probes that share a UDP socket with WireGuard.
//
// Sharing the socket is the whole point. Probes sent from a separate socket
// open a NAT mapping for *that* socket, not the one WireGuard transmits
// from, so a "successful" hole punch can leave the real tunnel unreachable.
// Multiplexing onto WireGuard's own socket (see Bind) means a probe that
// gets through proves the path WireGuard will actually use.
//
// The approach follows Tailscale's disco protocol, which solves the same
// problem in its magicsock layer.
package disco

import (
	"crypto/rand"
	"encoding/binary"
	"errors"

	"github.com/google/uuid"
)

// Magic prefixes every disco packet. A WireGuard transport packet begins
// with a message type of 1-4 followed by three zero bytes, so a leading 'N'
// (0x4E) can never be mistaken for one, in either direction.
var Magic = [6]byte{'N', 'X', 'd', 'i', 's', 'c'}

// Message types.
const (
	TypePing byte = 1
	TypePong byte = 2
)

// TxIDLen is the length of the transaction ID correlating a pong to a ping.
const TxIDLen = 8

// PacketLen is the fixed on-wire size: magic + type + txid + device ID.
const PacketLen = len(Magic) + 1 + TxIDLen + 16

// ErrNotDisco means the datagram is not a disco packet (most likely it is
// WireGuard traffic, which the caller should pass through untouched).
var ErrNotDisco = errors.New("disco: not a disco packet")

// TxID correlates a pong with the ping that provoked it.
type TxID [TxIDLen]byte

// NewTxID generates a random transaction ID.
func NewTxID() (TxID, error) {
	var id TxID
	_, err := rand.Read(id[:])
	return id, err
}

// Message is a decoded disco packet.
type Message struct {
	Type byte
	TxID TxID
	// Sender is the device ID of whoever sent the packet, so the receiver
	// can attribute a probe without relying on the source address (which
	// NAT may have rewritten).
	Sender uuid.UUID
}

// IsDisco reports whether a datagram is a disco packet. It is deliberately
// cheap: it runs on every inbound packet.
func IsDisco(b []byte) bool {
	return len(b) >= len(Magic) && string(b[:len(Magic)]) == string(Magic[:])
}

// Encode serialises a disco packet.
func Encode(msgType byte, txID TxID, sender uuid.UUID) []byte {
	buf := make([]byte, PacketLen)
	copy(buf, Magic[:])
	buf[len(Magic)] = msgType
	copy(buf[len(Magic)+1:], txID[:])
	copy(buf[len(Magic)+1+TxIDLen:], sender[:])
	return buf
}

// Decode parses a disco packet.
func Decode(b []byte) (Message, error) {
	if !IsDisco(b) || len(b) < PacketLen {
		return Message{}, ErrNotDisco
	}
	var msg Message
	msg.Type = b[len(Magic)]
	copy(msg.TxID[:], b[len(Magic)+1:])
	copy(msg.Sender[:], b[len(Magic)+1+TxIDLen:])
	if msg.Type != TypePing && msg.Type != TypePong {
		return Message{}, ErrNotDisco
	}
	return msg, nil
}

// txIDKey renders a TxID as a map key.
func txIDKey(id TxID) uint64 { return binary.BigEndian.Uint64(id[:]) }
