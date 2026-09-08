//go:build !js

package tunnel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/schollz/croc/v11/src/codephrase"
	"github.com/schollz/croc/v11/src/publicrelay"
	"github.com/schollz/croc/v11/src/tcp"
)

type ClientConfig struct{ Code, RelayAddress, RelayPassword, Curve string }

func relayForCode(explicit, code string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	relays := publicrelay.Relays()
	i, err := codephrase.RelayIndex(code, len(relays))
	if err != nil {
		return "", err
	}
	return relays[i], nil
}
func Join(ctx context.Context, config ClientConfig) (*Session, error) {
	if _, err := codephrase.ParseTunnel(config.Code); err != nil {
		return nil, err
	}
	relay, err := relayForCode(config.RelayAddress, config.Code)
	if err != nil {
		return nil, err
	}
	dialRoom := func(ctx context.Context, room string) (net.Conn, error) {
		deadline := time.Now().Add(10 * time.Second)
		for {
			c, _, _, _, err := tcp.ConnectToTCPServerControlContext(ctx, relay, config.RelayPassword, room, 10*time.Second)
			if err == nil {
				return c.Connection(), nil
			}
			if !errors.Is(err, tcp.ErrRoomFull) || time.Now().After(deadline) {
				return nil, err
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(250 * time.Millisecond):
			}
		}
	}
	deadline := time.Now().Add(authTimeout)
	for {
		connection, err := dialRoom(ctx, mustRoom(config.Code))
		if err != nil {
			return nil, err
		}
		session, err := Authenticate(ctx, connection, config.Code, config.Curve, dialRoom)
		if !errors.Is(err, ErrRendezvousBusy) || time.Now().After(deadline) {
			return session, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(500+rand.IntN(1000)) * time.Millisecond):
		}
	}
}
func mustRoom(code string) string { c, _ := codephrase.ParseTunnel(code); return c.RoomName }

type duplex interface {
	io.ReadWriteCloser
	CloseWrite() error
}

func copyDuplex(ctx context.Context, a, b duplex) {
	stop := context.AfterFunc(ctx, func() { _ = a.Close(); _ = b.Close() })
	defer stop()
	defer a.Close()
	defer b.Close()
	var wg sync.WaitGroup
	wg.Add(2)
	copyOne := func(dst, src duplex) {
		defer wg.Done()
		_, err := io.Copy(dst, src)
		_ = dst.CloseWrite()
		if err != nil {
			_ = a.Close()
			_ = b.Close()
		}
	}
	go copyOne(a, b)
	go copyOne(b, a)
	wg.Wait()
}

// Forward runs a loopback listener across reauthenticated guest sessions.
// It leaves interrupted streams failed and accepts new ones after reconnecting.
func Forward(ctx context.Context, config ClientConfig, port int, onStatus func(string)) error {
	session, err := Join(ctx, config)
	if err != nil {
		return err
	}
	if port == 0 {
		port = session.Port
	}
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		_ = session.Close()
		return fmt.Errorf("listen on local port %d: %w; choose another port with --local-port PORT", port, err)
	}
	defer listener.Close()
	stop := context.AfterFunc(ctx, func() { _ = listener.Close() })
	defer stop()
	var mu sync.Mutex
	current := session
	var wg sync.WaitGroup
	doneAccept := make(chan struct{})
	go func() {
		defer close(doneAccept)
		for {
			c, e := listener.Accept()
			if e != nil {
				return
			}
			mu.Lock()
			s := current
			mu.Unlock()
			if s == nil {
				_ = c.Close()
				continue
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				ch, e := s.OpenTCP(ctx)
				if e != nil {
					_ = c.Close()
					return
				}
				copyDuplex(ctx, c.(*net.TCPConn), ch)
			}()
		}
	}()
	defer func() {
		_ = listener.Close()
		<-doneAccept
		mu.Lock()
		if current != nil {
			_ = current.Close()
		}
		mu.Unlock()
		wg.Wait()
	}()
	for {
		if onStatus != nil {
			onStatus("Connected! Open http://localhost:" + strconv.Itoa(port) + " (Ctrl-C to disconnect)")
		}
		err = session.Wait()
		_ = session.Close()
		mu.Lock()
		current = nil
		mu.Unlock()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil {
			return nil
		}
		deadline := time.Now().Add(2 * time.Minute)
		for {
			if onStatus != nil {
				onStatus("Connection lost; reconnecting…")
			}
			timer := time.NewTimer(5 * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			session, err = Join(ctx, config)
			if err == nil {
				mu.Lock()
				current = session
				mu.Unlock()
				break
			}
			if time.Now().After(deadline) {
				return errors.New("tunnel reconnect window expired")
			}
		}
	}
}
