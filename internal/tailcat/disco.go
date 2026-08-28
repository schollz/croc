// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

package tailcat

import (
	"go4.org/mem"
	"tailscale.com/types/key"
)

// Meow messages are sent as raw DERP packets (not disco-framed).
// They are identified by a 4-byte magic prefix, followed by a 1-byte
// message type and the message payload.

// meowMagic is the 4-byte prefix for all meow DERP packets.
// It's distinct from WireGuard message types (1-4) and
// disco's "TS💬" magic.
var meowMagic = [4]byte{'m', 'e', 'o', 'w'}

const (
	meowTypePing = 0x01 // client → server
	meowTypePong = 0x02 // server → client ("meowed")
)

// IsMeowPacket reports whether pkt starts with the meow magic prefix.
func IsMeowPacket(pkt []byte) bool {
	return len(pkt) >= 4 && [4]byte(pkt[:4]) == meowMagic
}

// EncodeMeowPing encodes a meow ping packet containing the sender's
// node public key and disco public key.
func EncodeMeowPing(nodeKey key.NodePublic, discoKey key.DiscoPublic) []byte {
	b := make([]byte, 0, 4+1+key.NodePublicRawLen+key.DiscoPublicRawLen)
	b = append(b, meowMagic[:]...)
	b = append(b, meowTypePing)
	b = nodeKey.AppendTo(b)
	b = discoKey.AppendTo(b)
	return b
}

// EncodeMeowed encodes a meowed (acknowledgment) packet.
func EncodeMeowed() []byte {
	b := make([]byte, 0, 4+1)
	b = append(b, meowMagic[:]...)
	b = append(b, meowTypePong)
	return b
}

// ParseMeowPing parses a meow ping packet, returning the sender's
// node public key and disco public key. The pkt must have already
// been verified with IsMeowPacket.
func ParseMeowPing(pkt []byte) (nodeKey key.NodePublic, discoKey key.DiscoPublic, ok bool) {
	if len(pkt) < 4+1+key.NodePublicRawLen+key.DiscoPublicRawLen {
		return nodeKey, discoKey, false
	}
	if pkt[4] != meowTypePing {
		return nodeKey, discoKey, false
	}
	nodeKey = key.NodePublicFromRaw32(mem.B(pkt[5 : 5+key.NodePublicRawLen]))
	discoKey = key.DiscoPublicFromRaw32(mem.B(pkt[5+key.NodePublicRawLen : 5+key.NodePublicRawLen+key.DiscoPublicRawLen]))
	return nodeKey, discoKey, true
}

// IsMeowedPacket reports whether pkt is a meowed (acknowledgment) packet.
func IsMeowedPacket(pkt []byte) bool {
	return len(pkt) >= 5 && [4]byte(pkt[:4]) == meowMagic && pkt[4] == meowTypePong
}
