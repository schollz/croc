// Package pakekey binds croc's peer PAKE exchanges to their participants,
// session, purpose, and exact wire transcript.
package pakekey

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/schollz/pake/v3"
	"golang.org/x/crypto/hkdf"
)

const (
	// ProtocolVersion is the fail-closed croc peer PAKE protocol version.
	ProtocolVersion = 2
	// SaltSize is the required peer key-schedule salt size.
	SaltSize = 32

	PurposeTransfer   = "peer-transfer"
	PurposeLocalProbe = "local-ip-probe"

	identityDomain   = "croc/spake2/participant/v2"
	transcriptDomain = "croc/spake2/channel/v2"
)

// Context contains the public session values authenticated by the channel key
// schedule. Initiator is party A's initial PAKE value and Responder is party
// B's response.
type Context struct {
	Purpose   string
	Room      string
	Curve     string
	Initiator []byte
	Responder []byte
	Salt      []byte
}

// Keys contains the traffic key and the role-specific confirmation tags.
type Keys struct {
	EncryptionKey []byte
	ConfirmationA []byte
	ConfirmationB []byte
}

// Identities returns the ordered, implicit participant identities for a croc
// PAKE exchange. Party A is always the receiver and party B is the sender.
func Identities(purpose, room string) (idA, idB []byte, err error) {
	if purpose != PurposeTransfer && purpose != PurposeLocalProbe {
		return nil, nil, fmt.Errorf("unsupported PAKE purpose %q", purpose)
	}
	if room == "" {
		return nil, nil, fmt.Errorf("PAKE room is required")
	}
	version := make([]byte, 8)
	binary.LittleEndian.PutUint64(version, ProtocolVersion)
	idA = frame([]byte(identityDomain), version, []byte(purpose), []byte(room), []byte("receiver"))
	idB = frame([]byte(identityDomain), version, []byte(purpose), []byte(room), []byte("sender"))
	return idA, idB, nil
}

// Init initializes the identity-bound PAKE instance for the supplied croc
// session context.
func Init(password []byte, role int, curve, purpose, room string) (*pake.Pake, error) {
	idA, idB, err := Identities(purpose, room)
	if err != nil {
		return nil, err
	}
	return pake.InitCurveWithIdentities(password, role, curve, idA, idB)
}

// Derive expands a PAKE session key into a traffic key and mutual key-
// confirmation tags bound to the exact croc transcript.
func Derive(sharedKey []byte, context Context) (Keys, error) {
	if len(sharedKey) == 0 {
		return Keys{}, fmt.Errorf("PAKE session key is required")
	}
	if context.Curve == "" {
		return Keys{}, fmt.Errorf("PAKE curve is required")
	}
	if len(context.Initiator) == 0 || len(context.Responder) == 0 {
		return Keys{}, fmt.Errorf("both PAKE wire values are required")
	}
	if len(context.Salt) != SaltSize {
		return Keys{}, fmt.Errorf("PAKE salt must be %d bytes", SaltSize)
	}
	idA, idB, err := Identities(context.Purpose, context.Room)
	if err != nil {
		return Keys{}, err
	}
	version := make([]byte, 8)
	binary.LittleEndian.PutUint64(version, ProtocolVersion)
	transcript := frame(
		[]byte(transcriptDomain),
		version,
		[]byte(context.Purpose),
		[]byte(context.Room),
		[]byte(context.Curve),
		idA,
		idB,
		context.Initiator,
		context.Responder,
		context.Salt,
	)

	material := make([]byte, 96)
	if _, err = io.ReadFull(hkdf.New(sha256.New, sharedKey, context.Salt, transcript), material); err != nil {
		return Keys{}, fmt.Errorf("derive PAKE channel keys: %w", err)
	}
	encryptionKey := append([]byte(nil), material[:32]...)
	confirmationKeyA := material[32:64]
	confirmationKeyB := material[64:96]
	return Keys{
		EncryptionKey: encryptionKey,
		ConfirmationA: confirmationTag(confirmationKeyA, transcript),
		ConfirmationB: confirmationTag(confirmationKeyB, transcript),
	}, nil
}

// Confirm verifies a role-specific confirmation tag in constant time.
func Confirm(expected, received []byte) bool {
	return len(expected) == sha256.Size && hmac.Equal(expected, received)
}

func confirmationTag(key, transcript []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(transcript)
	return mac.Sum(nil)
}

func frame(fields ...[]byte) []byte {
	total := 0
	for _, field := range fields {
		total += 8 + len(field)
	}
	encoded := make([]byte, 0, total)
	var length [8]byte
	for _, field := range fields {
		binary.LittleEndian.PutUint64(length[:], uint64(len(field)))
		encoded = append(encoded, length[:]...)
		encoded = append(encoded, field...)
	}
	return encoded
}
