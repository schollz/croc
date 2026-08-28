//go:build !dragonfly && !netbsd

package tailcattransport

import (
	"crypto/hkdf"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/schollz/croc/v11/internal/tailcat"
	go4mem "go4.org/mem"
	"tailscale.com/types/key"
)

const (
	keySalt          = "croc/tailcat/experimental-v1"
	senderKeyLabel   = "sender-server-node"
	receiverKeyLabel = "receiver-client-node"
)

// Available reports whether Tailcat is compiled for this platform.
func Available() bool { return true }

// DerivePublicKeys returns the PAKE-bound sender and receiver identities.
// It is exported so negotiation tests can verify identity binding without
// exposing either derived private key.
func DerivePublicKeys(sessionKey []byte) (sender, receiver key.NodePublic, err error) {
	identities, err := deriveSessionIdentities(sessionKey)
	if err != nil {
		return sender, receiver, err
	}
	return identities.sender.Public(), identities.receiver.Public(), nil
}

type sessionIdentities struct {
	sender   key.NodePrivate
	receiver key.NodePrivate
}

func deriveSessionIdentities(sessionKey []byte) (sessionIdentities, error) {
	sender, err := deriveNodeKey(sessionKey, senderKeyLabel)
	if err != nil {
		return sessionIdentities{}, err
	}
	receiver, err := deriveNodeKey(sessionKey, receiverKeyLabel)
	if err != nil {
		return sessionIdentities{}, err
	}
	return sessionIdentities{sender: sender, receiver: receiver}, nil
}

func deriveNodeKey(sessionKey []byte, label string) (key.NodePrivate, error) {
	if len(sessionKey) == 0 {
		return key.NodePrivate{}, errors.New("Tailcat session key is empty")
	}
	raw, err := hkdf.Key(sha256.New, sessionKey, []byte(keySalt), label, 32)
	if err != nil {
		return key.NodePrivate{}, fmt.Errorf("derive Tailcat %s identity: %w", label, err)
	}
	return key.NodePrivateFromRaw32(go4mem.B(raw)), nil
}

// ValidateOffer rejects non-embedded, malformed, oversized, relay-less, or
// incorrectly authenticated Tailcat connection blobs.
func ValidateOffer(offer string, sessionKey []byte) error {
	identities, err := deriveSessionIdentities(sessionKey)
	if err != nil {
		return err
	}
	return validateOfferForSender(offer, identities.sender.Public())
}

func validateOfferForSender(offer string, expected key.NodePublic) error {
	if len(offer) == 0 || len(offer) > MaxOfferSize {
		return fmt.Errorf("%w: size must be between 1 and %d bytes", ErrInvalidOffer, MaxOfferSize)
	}
	info, err := tailcat.ParseConnBlob(tailcat.ConnBlob(offer))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidOffer, err)
	}
	if len(info.Region) != 1 || info.RegionID != 0 {
		return fmt.Errorf("%w: offer must embed exactly one relay region", ErrInvalidOffer)
	}
	if len(info.Region[0].Nodes) == 0 {
		return fmt.Errorf("%w: embedded relay region has no relay nodes", ErrInvalidOffer)
	}
	for _, node := range info.Region[0].Nodes {
		if node == nil || (node.HostName == "" && node.IPv4 == "" && node.IPv6 == "") {
			return fmt.Errorf("%w: embedded relay node is incomplete", ErrInvalidOffer)
		}
	}
	if info.ServerPublic.NodePublic != expected {
		return fmt.Errorf("%w: server identity does not match the authenticated session", ErrInvalidOffer)
	}
	return nil
}
