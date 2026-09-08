//go:build js && wasm

package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"syscall/js"
	"time"

	"github.com/schollz/croc/v11/src/logger"
	"github.com/schollz/croc/v11/src/tunnel"
	"golang.org/x/crypto/ssh"
)

const streamQueueChunks = 64

type bridge struct {
	mu         sync.Mutex
	nextHandle int
	sessions   map[int]*browserSession
	funcs      []js.Func
}
type operation struct {
	mu      sync.Mutex
	channel ssh.Channel
}
type browserSession struct {
	handle     int
	ctx        context.Context
	cancel     context.CancelFunc
	conn       *browserConn
	mu         sync.Mutex
	nextAck    int
	acks       map[int]chan error
	tunnel     *tunnel.Session
	operations map[int]*operation
}

func main() {
	logger.SetLevel("error")
	b := &bridge{sessions: make(map[int]*browserSession)}
	api := js.Global().Get("Object").New()
	b.expose(api, "start", b.start)
	b.expose(api, "feed", b.feed)
	b.expose(api, "endFeed", b.endFeed)
	b.expose(api, "ack", b.ack)
	b.expose(api, "close", b.close)
	b.expose(api, "open", b.open)
	b.expose(api, "write", b.write)
	b.expose(api, "closeChannel", b.closeChannel)
	js.Global().Set("crocTunnelWasm", api)
	select {}
}

// Operations must yield the JS event loop: SSH writes wait for network acks.
func (b *bridge) expose(api js.Value, name string, fn func([]js.Value) (any, error)) {
	wrapped := js.FuncOf(func(_ js.Value, args []js.Value) any {
		executor := js.FuncOf(func(_ js.Value, promise []js.Value) any {
			resolve := promise[0]
			go func() {
				result, err := safeCall(fn, args)
				response := js.Global().Get("Object").New()
				response.Set("ok", err == nil)
				if err != nil {
					response.Set("error", err.Error())
				} else if result != nil {
					response.Set("value", result)
				}
				resolve.Invoke(response)
			}()
			return nil
		})
		result := js.Global().Get("Promise").New(executor)
		executor.Release()
		return result
	})
	b.funcs = append(b.funcs, wrapped)
	api.Set(name, wrapped)
}
func safeCall(fn func([]js.Value) (any, error), args []js.Value) (result any, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("Tunnel WASM bridge: %v", recovered)
		}
	}()
	return fn(args)
}
func bytesFromJS(value js.Value) ([]byte, error) {
	if value.Type() != js.TypeObject {
		return nil, errors.New("expected Uint8Array")
	}
	size := value.Get("byteLength").Int()
	if size < 0 || size > tunnel.MaxWebSocketMessage {
		return nil, errors.New("input exceeds tunnel chunk limit")
	}
	b := make([]byte, size)
	if js.CopyBytesToGo(b, value) != size {
		return nil, errors.New("invalid bytes")
	}
	return b, nil
}
func bytesToJS(b []byte) js.Value {
	v := js.Global().Get("Uint8Array").New(len(b))
	js.CopyBytesToJS(v, b)
	return v
}
func (b *bridge) start(args []js.Value) (any, error) {
	if len(args) != 1 {
		return nil, errors.New("start expects an invitation")
	}
	code := args[0].String()
	b.mu.Lock()
	b.nextHandle++
	ctx, cancel := context.WithCancel(context.Background())
	s := &browserSession{handle: b.nextHandle, ctx: ctx, cancel: cancel, acks: make(map[int]chan error), operations: make(map[int]*operation)}
	s.conn = newBrowserConn(s)
	b.sessions[s.handle] = s
	b.mu.Unlock()
	go func() {
		peer, err := tunnel.Authenticate(s.ctx, s.conn, code, "p256", func(ctx context.Context, room string) (net.Conn, error) {
			connection := newBrowserConn(s)
			s.mu.Lock()
			s.conn = connection
			s.mu.Unlock()
			if err := s.emitBytes("handoff", []byte(room), time.Now().Add(30*time.Second)); err != nil {
				return nil, err
			}
			return connection, nil
		})
		clean := false
		if err == nil {
			s.mu.Lock()
			s.tunnel = peer
			s.mu.Unlock()
			s.postState("connected", fmt.Sprint(peer.Port), false)
			err = peer.Wait()
			clean = err == nil
			_ = peer.Close()
		}
		message := ""
		if err != nil {
			message = err.Error()
		}
		s.shutdown()
		s.postState("closed", message, clean)
		b.mu.Lock()
		delete(b.sessions, s.handle)
		b.mu.Unlock()
	}()
	return s.handle, nil
}
func (b *bridge) open(args []js.Value) (any, error) {
	if len(args) != 4 {
		return nil, errors.New("open expects session, operation, kind and metadata")
	}
	s, err := b.session(args[0].Int())
	if err != nil {
		return nil, err
	}
	id := args[1].Int()
	kind := args[2].String()
	metadata := []byte(args[3].String())
	s.mu.Lock()
	peer := s.tunnel
	_, exists := s.operations[id]
	if peer == nil || exists || len(s.operations) >= tunnel.MaxChannels {
		s.mu.Unlock()
		return nil, errors.New("tunnel is not ready or has too many requests")
	}
	s.operations[id] = &operation{}
	s.mu.Unlock()
	ch, err := peer.Open(s.ctx, kind, metadata)
	if err != nil {
		s.mu.Lock()
		delete(s.operations, id)
		s.mu.Unlock()
		return nil, err
	}
	op := &operation{channel: ch}
	s.mu.Lock()
	if _, ok := s.operations[id]; !ok {
		s.mu.Unlock()
		_ = ch.Close()
		return nil, errors.New("request cancelled")
	}
	s.operations[id] = op
	s.mu.Unlock()
	go func() {
		defer ch.Close()
		defer func() { s.mu.Lock(); delete(s.operations, id); s.mu.Unlock() }()
		for {
			kind, data, err := tunnel.ReadRecord(ch)
			if err != nil {
				kind = tunnel.RecordError
				data = []byte("Tunnel request interrupted")
			}
			payload := make([]byte, 5+len(data))
			binary.BigEndian.PutUint32(payload[:4], uint32(id))
			payload[4] = kind
			copy(payload[5:], data)
			if emitErr := s.emitBytes("output", payload, time.Time{}); emitErr != nil {
				return
			}
			if err != nil || kind == tunnel.RecordEnd || kind == tunnel.RecordClose || kind == tunnel.RecordError {
				return
			}
		}
	}()
	return nil, nil
}
func (b *bridge) write(args []js.Value) (any, error) {
	if len(args) != 4 {
		return nil, errors.New("write expects session, operation, kind and bytes")
	}
	s, err := b.session(args[0].Int())
	if err != nil {
		return nil, err
	}
	data, err := bytesFromJS(args[3])
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	op := s.operations[args[1].Int()]
	s.mu.Unlock()
	if op == nil || op.channel == nil {
		return nil, errors.New("request is closed")
	}
	op.mu.Lock()
	defer op.mu.Unlock()
	return nil, tunnel.WriteRecord(op.channel, byte(args[2].Int()), data)
}
func (b *bridge) closeChannel(args []js.Value) (any, error) {
	if len(args) != 2 {
		return nil, errors.New("closeChannel expects session and operation")
	}
	s, err := b.session(args[0].Int())
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	op := s.operations[args[1].Int()]
	delete(s.operations, args[1].Int())
	s.mu.Unlock()
	if op != nil && op.channel != nil {
		return nil, op.channel.Close()
	}
	return nil, nil
}
func (b *bridge) session(handle int) (*browserSession, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	session := b.sessions[handle]
	if session == nil {
		return nil, errors.New("unknown tunnel session")
	}
	return session, nil
}

func (b *bridge) feed(args []js.Value) (any, error) {
	if len(args) != 2 {
		return nil, errors.New("feed expects a handle and bytes")
	}
	session, err := b.session(args[0].Int())
	if err != nil {
		return nil, err
	}
	bytes, err := bytesFromJS(args[1])
	if err != nil {
		return nil, err
	}
	session.mu.Lock()
	connection := session.conn
	session.mu.Unlock()
	return nil, connection.feed(bytes)
}

func (b *bridge) endFeed(args []js.Value) (any, error) {
	if len(args) != 1 {
		return nil, errors.New("endFeed expects a handle")
	}
	s, err := b.session(args[0].Int())
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	connection := s.conn
	s.mu.Unlock()
	connection.incoming.close()
	return nil, nil
}

func (b *bridge) ack(args []js.Value) (any, error) {
	if len(args) != 3 {
		return nil, errors.New("ack expects a handle, sequence, and error")
	}
	session, err := b.session(args[0].Int())
	if err != nil {
		return nil, err
	}
	sequence := args[1].Int()
	message := args[2].String()
	session.mu.Lock()
	waiter := session.acks[sequence]
	delete(session.acks, sequence)
	session.mu.Unlock()
	if waiter == nil {
		return nil, errors.New("unknown tunnel event acknowledgement")
	}
	if message == "" {
		waiter <- nil
	} else {
		waiter <- errors.New(message)
	}
	return nil, nil
}

func (b *bridge) close(args []js.Value) (any, error) {
	if len(args) != 1 {
		return nil, errors.New("close expects a handle")
	}
	session, err := b.session(args[0].Int())
	if err != nil {
		return nil, err
	}
	session.shutdown()
	return nil, nil
}

func (s *browserSession) shutdown() {
	s.cancel()
	s.mu.Lock()
	connection := s.conn
	s.mu.Unlock()
	_ = connection.Close()
}

func (s *browserSession) postState(event, message string, clean bool) {
	payload := js.Global().Get("Object").New()
	payload.Set("type", "tunnel-event")
	payload.Set("event", event)
	payload.Set("handle", s.handle)
	payload.Set("message", message)
	payload.Set("clean", clean)
	js.Global().Call("postMessage", payload)
}

func (s *browserSession) emitBytes(event string, bytes []byte, deadline time.Time) error {
	copyBytes := append([]byte(nil), bytes...)
	s.mu.Lock()
	s.nextAck++
	sequence := s.nextAck
	waiter := make(chan error, 1)
	s.acks[sequence] = waiter
	s.mu.Unlock()

	value := bytesToJS(copyBytes)
	payload := js.Global().Get("Object").New()
	payload.Set("type", "tunnel-event")
	payload.Set("event", event)
	payload.Set("handle", s.handle)
	payload.Set("sequence", sequence)
	payload.Set("data", value)
	transfers := js.Global().Get("Array").New()
	transfers.Call("push", value.Get("buffer"))
	js.Global().Call("postMessage", payload, transfers)

	var timer <-chan time.Time
	if !deadline.IsZero() {
		duration := time.Until(deadline)
		if duration <= 0 {
			s.removeAck(sequence)
			return &timeoutError{}
		}
		t := time.NewTimer(duration)
		defer t.Stop()
		timer = t.C
	}
	select {
	case err := <-waiter:
		return err
	case <-s.ctx.Done():
		s.removeAck(sequence)
		return net.ErrClosed
	case <-timer:
		s.removeAck(sequence)
		return &timeoutError{}
	}
}

func (s *browserSession) removeAck(sequence int) {
	s.mu.Lock()
	delete(s.acks, sequence)
	s.mu.Unlock()
}

type chunkReader struct {
	chunks    chan []byte
	closed    chan struct{}
	closeOnce sync.Once
	current   []byte
}

func newChunkReader() *chunkReader {
	return &chunkReader{chunks: make(chan []byte, streamQueueChunks), closed: make(chan struct{})}
}

func (r *chunkReader) push(bytes []byte) error {
	if len(bytes) == 0 {
		return nil
	}
	copyBytes := append([]byte(nil), bytes...)
	select {
	case r.chunks <- copyBytes:
		return nil
	case <-r.closed:
		return net.ErrClosed
	}
}

func (r *chunkReader) Read(bytes []byte) (int, error) {
	for len(r.current) == 0 {
		select {
		case r.current = <-r.chunks:
		case <-r.closed:
			select {
			case r.current = <-r.chunks:
			default:
				return 0, io.EOF
			}
		}
	}
	n := copy(bytes, r.current)
	r.current = r.current[n:]
	return n, nil
}

func (r *chunkReader) close() { r.closeOnce.Do(func() { close(r.closed) }) }

type browserConn struct {
	session   *browserSession
	incoming  *chunkReader
	closed    chan struct{}
	closeOnce sync.Once

	deadlineMu      sync.Mutex
	readDeadline    time.Time
	writeDeadline   time.Time
	deadlineChanged chan struct{}
}

func newBrowserConn(session *browserSession) *browserConn {
	return &browserConn{
		session:         session,
		incoming:        newChunkReader(),
		closed:          make(chan struct{}),
		deadlineChanged: make(chan struct{}),
	}
}

func (c *browserConn) feed(bytes []byte) error { return c.incoming.push(bytes) }

func (c *browserConn) Read(bytes []byte) (int, error) {
	for {
		if len(c.incoming.current) > 0 {
			n := copy(bytes, c.incoming.current)
			c.incoming.current = c.incoming.current[n:]
			return n, nil
		}
		deadline, changed := c.deadline(true)
		var timer *time.Timer
		var timeout <-chan time.Time
		if !deadline.IsZero() {
			duration := time.Until(deadline)
			if duration <= 0 {
				return 0, &timeoutError{}
			}
			timer = time.NewTimer(duration)
			timeout = timer.C
		}
		select {
		case c.incoming.current = <-c.incoming.chunks:
			if timer != nil {
				timer.Stop()
			}
			continue
		case <-changed:
			if timer != nil {
				timer.Stop()
			}
			continue
		case <-timeout:
			return 0, &timeoutError{}
		case <-c.incoming.closed:
			if timer != nil {
				timer.Stop()
			}
			select {
			case c.incoming.current = <-c.incoming.chunks:
				continue
			default:
				return 0, io.EOF
			}
		case <-c.closed:
			if timer != nil {
				timer.Stop()
			}
			return 0, net.ErrClosed
		}
	}
}

func (c *browserConn) Write(bytes []byte) (int, error) {
	deadline, _ := c.deadline(false)
	if err := c.session.emitBytes("network", bytes, deadline); err != nil {
		return 0, err
	}
	return len(bytes), nil
}

func (c *browserConn) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
		c.incoming.close()
	})
	return nil
}

func (c *browserConn) LocalAddr() net.Addr  { return browserAddr("browser") }
func (c *browserConn) RemoteAddr() net.Addr { return browserAddr("croc-tunnel") }

func (c *browserConn) SetDeadline(deadline time.Time) error {
	c.deadlineMu.Lock()
	c.readDeadline = deadline
	c.writeDeadline = deadline
	close(c.deadlineChanged)
	c.deadlineChanged = make(chan struct{})
	c.deadlineMu.Unlock()
	return nil
}

func (c *browserConn) SetReadDeadline(deadline time.Time) error {
	c.deadlineMu.Lock()
	c.readDeadline = deadline
	close(c.deadlineChanged)
	c.deadlineChanged = make(chan struct{})
	c.deadlineMu.Unlock()
	return nil
}

func (c *browserConn) SetWriteDeadline(deadline time.Time) error {
	c.deadlineMu.Lock()
	c.writeDeadline = deadline
	close(c.deadlineChanged)
	c.deadlineChanged = make(chan struct{})
	c.deadlineMu.Unlock()
	return nil
}

func (c *browserConn) deadline(read bool) (time.Time, <-chan struct{}) {
	c.deadlineMu.Lock()
	defer c.deadlineMu.Unlock()
	if read {
		return c.readDeadline, c.deadlineChanged
	}
	return c.writeDeadline, c.deadlineChanged
}

type browserAddr string

func (a browserAddr) Network() string { return "wasm" }
func (a browserAddr) String() string  { return string(a) }

type timeoutError struct{}

func (*timeoutError) Error() string   { return "Tunnel stream deadline exceeded" }
func (*timeoutError) Timeout() bool   { return true }
func (*timeoutError) Temporary() bool { return true }
