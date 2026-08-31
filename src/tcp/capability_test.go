package tcp

import (
	"bytes"
	"errors"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/croc/v11/src/crypt"
	"github.com/schollz/pake/v3"
)

func readyCapabilitySet(t *testing.T, streams int) *RelayCapabilitySet {
	t.Helper()
	set, err := NewRelayCapabilitySet(streams)
	if err != nil {
		t.Fatal(err)
	}
	for range streams + 1 {
		set.registerPort()
	}
	return set
}

func TestRelayCapabilityValidationAndOneUseSuffixes(t *testing.T) {
	set := readyCapabilitySet(t, 2)
	token, err := set.issue("192.0.2.10", "parent-room")
	if err != nil {
		t.Fatal(err)
	}
	if err := set.verifyAndUse("192.0.2.11", "parent-room-0", token); err == nil {
		t.Fatal("wrong source was accepted")
	}
	if err := set.verifyAndUse("192.0.2.10", "other-room-0", token); err == nil {
		t.Fatal("wrong parent room was accepted")
	}
	if err := set.verifyAndUse("192.0.2.10", "parent-room-2", token); err == nil {
		t.Fatal("out-of-range suffix was accepted")
	}
	if err := set.verifyAndUse("192.0.2.10", "parent-room-0", token); err != nil {
		t.Fatal(err)
	}
	if err := set.verifyAndUse("192.0.2.10", "parent-room-0", token); err == nil {
		t.Fatal("capability suffix replay was accepted")
	}
	if err := set.verifyAndUse("192.0.2.10", "parent-room-1", token); err != nil {
		t.Fatalf("independent suffix rejected: %v", err)
	}
	other := readyCapabilitySet(t, 2)
	if err := other.verifyAndUse("192.0.2.10", "parent-room-1", token); err == nil {
		t.Fatal("capability from another relay instance was accepted")
	}
}

func legacyHandshakeAfterBanner(c *comm.Comm, password, room string) error {
	initiator, err := pake.InitCurve(weakKey, 0, "siec")
	if err != nil {
		return err
	}
	if err = c.Send(initiator.Bytes()); err != nil {
		return err
	}
	responder, err := c.Receive()
	if err != nil {
		return err
	}
	if err = initiator.Update(responder); err != nil {
		return err
	}
	shared, err := initiator.SessionKey()
	if err != nil {
		return err
	}
	key, salt, err := crypt.New(shared, nil)
	if err != nil {
		return err
	}
	if err = c.Send(salt); err != nil {
		return err
	}
	encryptedPassword, err := crypt.Encrypt([]byte(password), key)
	if err != nil {
		return err
	}
	if err = c.Send(encryptedPassword); err != nil {
		return err
	}
	// This is the v11.3 ordering: wait for the banner before sending room.
	encryptedBanner, err := c.Receive()
	if err != nil {
		return err
	}
	if _, err = crypt.Decrypt(encryptedBanner, key); err != nil {
		return err
	}
	encryptedRoom, err := crypt.Encrypt([]byte(room), key)
	if err != nil {
		return err
	}
	if err = c.Send(encryptedRoom); err != nil {
		return err
	}
	confirmation, err := c.Receive()
	if err != nil {
		return err
	}
	confirmation, err = crypt.Decrypt(confirmation, key)
	if err != nil {
		return err
	}
	if !bytes.Equal(confirmation, []byte("ok")) {
		return errors.New("legacy room was rejected")
	}
	return nil
}

func TestRelayCapabilityExpiryAndMixedPortRegistration(t *testing.T) {
	set, err := NewRelayCapabilitySet(2)
	if err != nil {
		t.Fatal(err)
	}
	set.registerPort()
	set.registerPort()
	if _, err := set.issue("192.0.2.1", "room"); err == nil {
		t.Fatal("capability emitted before every port registered")
	}
	set.registerPort()
	now := time.Unix(1_900_000_000, 0)
	set.now = func() time.Time { return now }
	token, err := set.issue("192.0.2.1", "room")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(fastAdmissionTTL + time.Second)
	if err := set.verifyAndUse("192.0.2.1", "room-0", token); err == nil {
		t.Fatal("expired capability was accepted")
	}
}

func freeTCPPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	listener.Close()
	return port
}

func waitForRelayPort(t *testing.T, address string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := PingServer(address); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("relay %s did not start", address)
}

func TestFastRelayAdmissionAndLegacyFallback(t *testing.T) {
	controlPort, dataPort := freeTCPPort(t), freeTCPPort(t)
	ctx := t.Context()
	set, err := NewRelayCapabilitySet(1)
	if err != nil {
		t.Fatal(err)
	}
	go RunWithOptionsAsync("127.0.0.1", dataPort, "pass", WithCtx(ctx), WithFastAdmission(set))
	go RunWithOptionsAsync("127.0.0.1", controlPort, "pass", WithCtx(ctx), WithBanner(dataPort), WithFastAdmission(set))
	controlAddress := net.JoinHostPort("127.0.0.1", controlPort)
	dataAddress := net.JoinHostPort("127.0.0.1", dataPort)
	waitForRelayPort(t, controlAddress)
	waitForRelayPort(t, dataAddress)

	controlA, err := comm.NewConnection(controlAddress)
	if err != nil {
		t.Fatal(err)
	}
	_, _, capabilityA, err := HandshakeTCPServerCapability(controlA, "pass", "parent")
	if err != nil || capabilityA == "" {
		t.Fatalf("first capability = %q, err=%v", capabilityA, err)
	}
	controlB, err := comm.NewConnection(controlAddress)
	if err != nil {
		t.Fatal(err)
	}
	_, _, capabilityB, err := HandshakeTCPServerCapability(controlB, "pass", "parent")
	if err != nil || capabilityB == "" {
		t.Fatalf("second capability = %q, err=%v", capabilityB, err)
	}
	dataA, _, _, fastA, err := ConnectToTCPServerWithCapability(dataAddress, "pass", "parent-0", capabilityA)
	if err != nil {
		t.Fatal(err)
	}
	dataB, _, _, fastB, err := ConnectToTCPServerWithCapability(dataAddress, "pass", "parent-0", capabilityB)
	if err != nil {
		t.Fatal(err)
	}
	if !fastA || !fastB {
		t.Fatalf("fast admission = %v/%v", fastA, fastB)
	}
	dataA.Close()
	dataB.Close()
	controlA.Close()
	controlB.Close()

	legacyControlA, err := comm.NewConnection(controlAddress)
	if err != nil {
		t.Fatal(err)
	}
	if err = legacyHandshakeAfterBanner(legacyControlA, "pass", "legacy-parent"); err != nil {
		t.Fatal(err)
	}
	legacyControlB, err := comm.NewConnection(controlAddress)
	if err != nil {
		t.Fatal(err)
	}
	if err = legacyHandshakeAfterBanner(legacyControlB, "pass", "legacy-parent"); err != nil {
		t.Fatal(err)
	}
	legacyControlA.Close()
	legacyControlB.Close()

	// A capability offered by an upgraded control port must be harmless when
	// a data port is still legacy: the client reconnects through PAKE.
	legacyPort := freeTCPPort(t)
	go RunWithOptionsAsync("127.0.0.1", legacyPort, "pass", WithCtx(ctx))
	legacyAddress := net.JoinHostPort("127.0.0.1", legacyPort)
	waitForRelayPort(t, legacyAddress)
	legacyA, _, _, usedFastA, err := ConnectToTCPServerWithCapability(legacyAddress, "pass", "legacy-0", capabilityA)
	if err != nil {
		t.Fatal(err)
	}
	legacyB, _, _, usedFastB, err := ConnectToTCPServerWithCapability(legacyAddress, "pass", "legacy-0", capabilityB)
	if err != nil {
		t.Fatal(err)
	}
	if usedFastA || usedFastB {
		t.Fatal("legacy data port reported fast admission")
	}
	legacyA.Close()
	legacyB.Close()
}
