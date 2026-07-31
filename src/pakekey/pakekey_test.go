package pakekey

import (
	"bytes"
	"testing"
)

func TestIdentityBoundAgreementAcrossCurves(t *testing.T) {
	for _, curve := range []string{"p256", "p384", "p521", "siec", "ed25519"} {
		t.Run(curve, func(t *testing.T) {
			A, err := Init([]byte("shared password"), 0, curve, PurposeTransfer, "room-one")
			if err != nil {
				t.Fatal(err)
			}
			B, err := Init([]byte("shared password"), 1, curve, PurposeTransfer, "room-one")
			if err != nil {
				t.Fatal(err)
			}
			initiator := A.Bytes()
			if err = B.Update(initiator); err != nil {
				t.Fatal(err)
			}
			responder := B.Bytes()
			if err = A.Update(responder); err != nil {
				t.Fatal(err)
			}
			sharedA, _ := A.SessionKey()
			sharedB, _ := B.SessionKey()
			salt := bytes.Repeat([]byte{0x42}, SaltSize)
			context := Context{
				Purpose: PurposeTransfer, Room: "room-one", Curve: curve,
				Initiator: initiator, Responder: responder, Salt: salt,
			}
			keysA, err := Derive(sharedA, context)
			if err != nil {
				t.Fatal(err)
			}
			keysB, err := Derive(sharedB, context)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(keysA.EncryptionKey, keysB.EncryptionKey) ||
				!bytes.Equal(keysA.ConfirmationA, keysB.ConfirmationA) ||
				!bytes.Equal(keysA.ConfirmationB, keysB.ConfirmationB) {
				t.Fatal("peers derived different channel material")
			}
		})
	}
}

func TestCrossWiredRoomsCannotConfirm(t *testing.T) {
	password := []byte("same passphrase")
	A, err := Init(password, 0, "p256", PurposeTransfer, "room-one")
	if err != nil {
		t.Fatal(err)
	}
	B, err := Init(password, 1, "p256", PurposeTransfer, "room-two")
	if err != nil {
		t.Fatal(err)
	}
	initiator := A.Bytes()
	if err = B.Update(initiator); err != nil {
		t.Fatal(err)
	}
	responder := B.Bytes()
	if err = A.Update(responder); err != nil {
		t.Fatal(err)
	}
	sharedA, _ := A.SessionKey()
	sharedB, _ := B.SessionKey()
	salt := bytes.Repeat([]byte{0x24}, SaltSize)
	keysA, err := Derive(sharedA, Context{
		Purpose: PurposeTransfer, Room: "room-one", Curve: "p256",
		Initiator: initiator, Responder: responder, Salt: salt,
	})
	if err != nil {
		t.Fatal(err)
	}
	keysB, err := Derive(sharedB, Context{
		Purpose: PurposeTransfer, Room: "room-two", Curve: "p256",
		Initiator: initiator, Responder: responder, Salt: salt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if Confirm(keysB.ConfirmationA, keysA.ConfirmationA) ||
		Confirm(keysA.ConfirmationB, keysB.ConfirmationB) {
		t.Fatal("cross-wired rooms accepted key confirmation")
	}
}

func TestTranscriptChangesInvalidateConfirmation(t *testing.T) {
	base := Context{
		Purpose: PurposeTransfer,
		Room:    "room-one", Curve: "p256",
		Initiator: []byte("initiator"), Responder: []byte("responder"),
		Salt: bytes.Repeat([]byte{1}, SaltSize),
	}
	shared := []byte("a fixed shared session key")
	want, err := Derive(shared, base)
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]Context{
		"room":      {Purpose: PurposeTransfer, Room: "room-two", Curve: "p256", Initiator: base.Initiator, Responder: base.Responder, Salt: base.Salt},
		"purpose":   {Purpose: PurposeLocalProbe, Room: base.Room, Curve: "p256", Initiator: base.Initiator, Responder: base.Responder, Salt: base.Salt},
		"curve":     {Purpose: PurposeTransfer, Room: base.Room, Curve: "p384", Initiator: base.Initiator, Responder: base.Responder, Salt: base.Salt},
		"initiator": {Purpose: PurposeTransfer, Room: base.Room, Curve: "p256", Initiator: []byte("other"), Responder: base.Responder, Salt: base.Salt},
		"responder": {Purpose: PurposeTransfer, Room: base.Room, Curve: "p256", Initiator: base.Initiator, Responder: []byte("other"), Salt: base.Salt},
		"salt":      {Purpose: PurposeTransfer, Room: base.Room, Curve: "p256", Initiator: base.Initiator, Responder: base.Responder, Salt: bytes.Repeat([]byte{2}, SaltSize)},
	}
	for name, changed := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := Derive(shared, changed)
			if err != nil {
				t.Fatal(err)
			}
			if Confirm(want.ConfirmationA, got.ConfirmationA) || Confirm(want.ConfirmationB, got.ConfirmationB) {
				t.Fatal("changed transcript retained a valid confirmation")
			}
		})
	}
}

func TestDeriveValidation(t *testing.T) {
	valid := Context{
		Purpose: PurposeTransfer, Room: "room", Curve: "p256",
		Initiator: []byte("a"), Responder: []byte("b"),
		Salt: make([]byte, SaltSize),
	}
	if _, err := Derive(nil, valid); err == nil {
		t.Fatal("expected missing session key error")
	}
	invalidSalt := valid
	invalidSalt.Salt = make([]byte, SaltSize-1)
	if _, err := Derive([]byte("key"), invalidSalt); err == nil {
		t.Fatal("expected salt length error")
	}
	if Confirm(make([]byte, 32), make([]byte, 31)) {
		t.Fatal("accepted wrong-size confirmation")
	}
}

func TestFramingIsUnambiguous(t *testing.T) {
	left := frame([]byte("ab"), []byte("c"))
	right := frame([]byte("a"), []byte("bc"))
	if bytes.Equal(left, right) {
		t.Fatal("length-prefixed transcript framing is ambiguous")
	}
}
