//go:build croc_attach_bench

package croc

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/croc/v11/src/crypt"
	"github.com/schollz/croc/v11/src/models"
	"github.com/shayne/derphole/pkg/session"
)

const derpBenchmarkChunkSize = models.TCP_BUFFER_SIZE / 2

// BenchmarkDERPAttachFramedTransfer compares the public legacy Attach API with
// AttachGroup while exercising croc's actual frame and encryption path. The
// canonical invocation and live-network gate are documented in
// docs/derp-attach-group-benchmark.md.
func BenchmarkDERPAttachFramedTransfer(b *testing.B) {
	b.Run("legacy/1", func(b *testing.B) {
		benchmarkDERPAttachFrames(b, 1, false)
	})
	const streams = 8
	b.Run(fmt.Sprintf("group/%d", streams), func(b *testing.B) {
		benchmarkDERPAttachFrames(b, streams, true)
	})
}

func benchmarkDERPAttachFrames(b *testing.B, streamCount int, grouped bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	dialed, accepted, cleanup := benchmarkDERPConnections(b, ctx, streamCount, grouped)
	defer cleanup()

	senders := make([]*comm.Comm, len(dialed))
	receivers := make([]*comm.Comm, len(accepted))
	for i := range dialed {
		senders[i] = comm.New(dialed[i])
		receivers[i] = comm.New(accepted[i])
	}
	payload := deterministicIncompressibleChunk()
	key := sha256.Sum256([]byte("croc AttachGroup benchmark key v1"))
	errCh := make(chan error, streamCount*2)

	b.SetBytes(derpBenchmarkChunkSize)
	b.ReportAllocs()
	b.ResetTimer()
	for i := range streamCount {
		go func(stream int) {
			aead, err := crypt.NewAESGCM(key[:])
			if err != nil {
				errCh <- err
				return
			}
			plain := append([]byte(nil), payload...)
			var encrypted []byte
			for frame := stream; frame < b.N; frame += streamCount {
				binary.LittleEndian.PutUint64(plain[:8], uint64(frame*derpBenchmarkChunkSize))
				encrypted, err = crypt.EncryptAEADTo(encrypted, plain, aead)
				if err == nil {
					err = senders[stream].Send(encrypted)
				}
				if err != nil {
					errCh <- err
					return
				}
			}
			errCh <- nil
		}(i)
		go func(stream int) {
			aead, err := crypt.NewAESGCM(key[:])
			if err != nil {
				errCh <- err
				return
			}
			var frameBuffer []byte
			for frame := stream; frame < b.N; frame += streamCount {
				frameBuffer, err = receivers[stream].ReceiveInto(frameBuffer)
				if err == nil {
					var plain []byte
					plain, err = crypt.DecryptAEADInPlace(frameBuffer, aead)
					if err == nil && len(plain) != derpBenchmarkChunkSize+8 {
						err = fmt.Errorf("decrypted frame size = %d", len(plain))
					}
				}
				if err != nil {
					errCh <- err
					return
				}
			}
			errCh <- nil
		}(i)
	}
	for range streamCount * 2 {
		if err := <-errCh; err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
}

func benchmarkDERPConnections(b *testing.B, ctx context.Context, streamCount int, grouped bool) (dialed, accepted []net.Conn, cleanup func()) {
	b.Helper()
	if !grouped {
		listener, err := session.ListenAttach(ctx, session.AttachListenConfig{})
		if err != nil {
			b.Fatal(err)
		}
		acceptedCh := make(chan net.Conn, 1)
		errCh := make(chan error, 1)
		go func() {
			conn, acceptErr := listener.Accept(ctx)
			if acceptErr != nil {
				errCh <- acceptErr
				return
			}
			acceptedCh <- conn
		}()
		conn, err := session.DialAttach(ctx, session.AttachDialConfig{Token: listener.Token})
		if err != nil {
			_ = listener.Close()
			b.Fatal(err)
		}
		var peer net.Conn
		select {
		case peer = <-acceptedCh:
		case err := <-errCh:
			_ = conn.Close()
			_ = listener.Close()
			b.Fatal(err)
		case <-ctx.Done():
			_ = conn.Close()
			_ = listener.Close()
			b.Fatal(ctx.Err())
		}
		return []net.Conn{conn}, []net.Conn{peer}, func() {
			_ = conn.Close()
			_ = peer.Close()
			_ = listener.Close()
		}
	}

	listener, err := session.ListenAttachGroup(ctx, session.AttachGroupListenConfig{MaxStreams: streamCount})
	if err != nil {
		b.Fatal(err)
	}
	acceptedCh := make(chan *session.AttachGroup, 1)
	errCh := make(chan error, 1)
	go func() {
		group, acceptErr := listener.Accept(ctx)
		if acceptErr != nil {
			errCh <- acceptErr
			return
		}
		acceptedCh <- group
	}()
	dialedGroup, err := session.DialAttachGroup(ctx, session.AttachGroupDialConfig{
		Token:       listener.Token,
		StreamCount: streamCount,
	})
	if err != nil {
		_ = listener.Close()
		b.Fatal(err)
	}
	var acceptedGroup *session.AttachGroup
	select {
	case acceptedGroup = <-acceptedCh:
	case err := <-errCh:
		_ = dialedGroup.Close()
		_ = listener.Close()
		b.Fatal(err)
	case <-ctx.Done():
		_ = dialedGroup.Close()
		_ = listener.Close()
		b.Fatal(ctx.Err())
	}
	return dialedGroup.Connections(), acceptedGroup.Connections(), func() {
		_ = dialedGroup.Close()
		_ = acceptedGroup.Close()
		_ = listener.Close()
	}
}

func deterministicIncompressibleChunk() []byte {
	payload := make([]byte, derpBenchmarkChunkSize+8)
	for offset, counter := 8, uint64(0); offset < len(payload); counter++ {
		var seed [8]byte
		binary.LittleEndian.PutUint64(seed[:], counter)
		sum := sha256.Sum256(seed[:])
		offset += copy(payload[offset:], sum[:])
	}
	return payload
}
