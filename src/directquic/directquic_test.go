package directquic

import (
	"bytes"
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/pion/stun/v3"
)

func TestDirectQUICLoopback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	key := bytes.Repeat([]byte{9}, 32)

	sender, err := New(ctx, Config{Role: RoleSender, Key: key, DisableSTUN: true})
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	receiver, err := New(ctx, Config{Role: RoleReceiver, Key: key, SessionID: sender.Offer().SessionID, DisableSTUN: true})
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()

	type result struct {
		conn *Conn
		err  error
	}
	accepted := make(chan result, 1)
	go func() {
		conn, acceptErr := receiver.Accept(ctx, sender.Offer())
		accepted <- result{conn: conn, err: acceptErr}
	}()

	senderConn, err := sender.Dial(ctx, receiver.Offer())
	if err != nil {
		t.Fatal(err)
	}
	receiverResult := <-accepted
	if receiverResult.err != nil {
		t.Fatal(receiverResult.err)
	}

	want := []byte("direct payload")
	stream, err := senderConn.OpenStream(ctx, StreamHeader{
		SessionID: sender.Offer().SessionID,
		FileIndex: 2,
		Lane:      0,
		LaneCount: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = WriteFrame(stream, want); err != nil {
		t.Fatal(err)
	}
	if err = stream.Close(); err != nil {
		t.Fatal(err)
	}

	receiveStream, header, err := receiverResult.conn.AcceptStream(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer receiveStream.Close()
	if header.FileIndex != 2 || header.Lane != 0 || header.LaneCount != 1 {
		t.Fatalf("unexpected stream header: %#v", header)
	}
	got, err := ReadFrame(receiveStream, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if _, err = ReadFrame(receiveStream, got); err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestDirectQUICRejectsWrongCertificateFingerprint(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	key := bytes.Repeat([]byte{4}, 32)
	sender, err := New(ctx, Config{Role: RoleSender, Key: key, DisableSTUN: true})
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	receiver, err := New(ctx, Config{Role: RoleReceiver, Key: key, SessionID: sender.Offer().SessionID, DisableSTUN: true})
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()

	accepted := make(chan error, 1)
	go func() {
		_, acceptErr := receiver.Accept(ctx, sender.Offer())
		accepted <- acceptErr
	}()
	badOffer := receiver.Offer()
	badOffer.CertSHA256[0] ^= 1
	if _, err = sender.Dial(ctx, badOffer); err == nil {
		t.Fatal("QUIC handshake accepted the wrong certificate fingerprint")
	}
	cancel()
	select {
	case <-accepted:
	case <-time.After(time.Second):
		t.Fatal("receiver did not stop after rejected handshake")
	}
}

func TestProbeAndOfferAuthentication(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	key := bytes.Repeat([]byte{3}, 32)
	sender, err := New(ctx, Config{Role: RoleSender, Key: key, DisableSTUN: true})
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	receiver, err := New(ctx, Config{Role: RoleReceiver, Key: key, SessionID: sender.Offer().SessionID, DisableSTUN: true})
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()

	probe := sender.newProbe()
	if !receiver.validProbe(probe) {
		t.Fatal("peer probe was not authenticated")
	}
	probe[len(probe)-1] ^= 1
	if receiver.validProbe(probe) {
		t.Fatal("tampered probe was authenticated")
	}

	offer := sender.Offer()
	offer.SessionID[0] ^= 1
	if _, err = validateOffer(offer, receiver.sessionID); err == nil {
		t.Fatal("offer with wrong session ID accepted")
	}
}

func TestSessionGathersServerReflexiveCandidate(t *testing.T) {
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	serverDone := make(chan error, 1)
	go func() {
		buffer := make([]byte, 2048)
		n, clientAddr, readErr := server.ReadFromUDP(buffer)
		if readErr != nil {
			serverDone <- readErr
			return
		}
		request := new(stun.Message)
		request.Raw = append(request.Raw, buffer[:n]...)
		if decodeErr := request.Decode(); decodeErr != nil {
			serverDone <- decodeErr
			return
		}
		response, buildErr := stun.Build(
			stun.NewTransactionIDSetter(request.TransactionID),
			stun.BindingSuccess,
			&stun.XORMappedAddress{IP: net.IPv4(203, 0, 113, 10), Port: clientAddr.Port},
		)
		if buildErr != nil {
			serverDone <- buildErr
			return
		}
		_, writeErr := server.WriteToUDP(response.Raw, clientAddr)
		serverDone <- writeErr
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	session, err := New(ctx, Config{
		Role: RoleSender, Key: bytes.Repeat([]byte{8}, 32),
		STUNServer: server.LocalAddr().String(), GatherTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if err = <-serverDone; err != nil {
		t.Fatal(err)
	}
	found := false
	for _, candidate := range session.Offer().Candidates {
		if candidate.Kind == "srflx" && candidate.Address == net.JoinHostPort("203.0.113.10", candidatePort(candidate.Address)) {
			found = true
		}
	}
	if !found {
		t.Fatalf("server-reflexive candidate missing from %#v", session.Offer().Candidates)
	}
}

func candidatePort(address string) string {
	_, port, _ := net.SplitHostPort(address)
	return port
}

func BenchmarkDirectQUICStream(b *testing.B) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	key := bytes.Repeat([]byte{11}, 32)
	sender, err := New(ctx, Config{Role: RoleSender, Key: key, DisableSTUN: true})
	if err != nil {
		b.Fatal(err)
	}
	defer sender.Close()
	receiver, err := New(ctx, Config{Role: RoleReceiver, Key: key, SessionID: sender.Offer().SessionID, DisableSTUN: true})
	if err != nil {
		b.Fatal(err)
	}
	defer receiver.Close()

	accepted := make(chan *Conn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		conn, acceptFailure := receiver.Accept(ctx, sender.Offer())
		if acceptFailure != nil {
			acceptErr <- acceptFailure
			return
		}
		accepted <- conn
	}()
	senderConn, err := sender.Dial(ctx, receiver.Offer())
	if err != nil {
		b.Fatal(err)
	}
	var receiverConn *Conn
	select {
	case receiverConn = <-accepted:
	case err = <-acceptErr:
		b.Fatal(err)
	}
	sendStream, err := senderConn.OpenStream(ctx, StreamHeader{SessionID: sender.Offer().SessionID, LaneCount: 1})
	if err != nil {
		b.Fatal(err)
	}
	receiveStream, _, err := receiverConn.AcceptStream(ctx)
	if err != nil {
		b.Fatal(err)
	}
	payload := bytes.Repeat([]byte{0xa5}, 32*1024)
	done := make(chan error, 1)
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	go func(iterations int) {
		var reuse []byte
		for i := 0; i < iterations; i++ {
			var readErr error
			reuse, readErr = ReadFrame(receiveStream, reuse)
			if readErr != nil {
				done <- readErr
				return
			}
		}
		done <- nil
	}(b.N)
	for i := 0; i < b.N; i++ {
		if err = WriteFrame(sendStream, payload); err != nil {
			b.Fatal(err)
		}
	}
	if err = <-done; err != nil {
		b.Fatal(err)
	}
	b.StopTimer()
	_ = sendStream.Close()
	_ = receiveStream.Close()
}
