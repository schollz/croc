//go:build js && wasm

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"syscall/js"
	"time"

	internalssh "github.com/schollz/croc/v11/internal/sshclient"
	gossh "golang.org/x/crypto/ssh"
)

const streamQueueChunks = 64

type bridge struct {
	mu         sync.Mutex
	nextHandle int
	sessions   map[int]*browserSession
	funcs      []js.Func
}

type browserSession struct {
	handle  int
	ctx     context.Context
	cancel  context.CancelFunc
	conn    *browserConn
	input   *chunkReader
	resizes chan internalssh.WindowSize

	mu      sync.Mutex
	nextAck int
	acks    map[int]chan error
}

func main() {
	b := &bridge{sessions: make(map[int]*browserSession)}
	api := js.Global().Get("Object").New()
	b.expose(api, "start", b.start)
	b.expose(api, "feed", b.feed)
	b.expose(api, "input", b.input)
	b.expose(api, "resize", b.resize)
	b.expose(api, "ack", b.ack)
	b.expose(api, "close", b.close)
	js.Global().Set("crocSSHWasm", api)
	select {}
}

func (b *bridge) expose(api js.Value, name string, fn func([]js.Value) (any, error)) {
	wrapped := js.FuncOf(func(_ js.Value, args []js.Value) any {
		result, err := safeCall(fn, args)
		response := js.Global().Get("Object").New()
		if err != nil {
			response.Set("ok", false)
			response.Set("error", err.Error())
			return response
		}
		response.Set("ok", true)
		if result != nil {
			response.Set("value", result)
		}
		return response
	})
	b.funcs = append(b.funcs, wrapped)
	api.Set(name, wrapped)
}

func safeCall(fn func([]js.Value) (any, error), args []js.Value) (result any, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("SSH WASM bridge panic: %v", recovered)
		}
	}()
	return fn(args)
}

func bytesFromJS(value js.Value) ([]byte, error) {
	if value.Type() != js.TypeObject {
		return nil, errors.New("expected Uint8Array")
	}
	bytes := make([]byte, value.Get("byteLength").Int())
	if copied := js.CopyBytesToGo(bytes, value); copied != len(bytes) {
		return nil, fmt.Errorf("copied %d of %d bytes", copied, len(bytes))
	}
	return bytes, nil
}

func bytesToJS(bytes []byte) js.Value {
	value := js.Global().Get("Uint8Array").New(len(bytes))
	js.CopyBytesToJS(value, bytes)
	return value
}

func (b *bridge) start(args []js.Value) (any, error) {
	if len(args) != 4 {
		return nil, errors.New("start expects a host key, client authentication, width, and height")
	}
	hostKeyBytes, err := bytesFromJS(args[0])
	if err != nil {
		return nil, err
	}
	hostKey, err := gossh.ParsePublicKey(hostKeyBytes)
	if err != nil {
		return nil, errors.New("host sent an invalid SSH host key")
	}
	clientAuth, err := bytesFromJS(args[1])
	if err != nil {
		return nil, err
	}
	if len(clientAuth) != 32 {
		return nil, errors.New("SSH client authentication is required")
	}
	width, height := args[2].Int(), args[3].Int()
	if width <= 0 || height <= 0 {
		return nil, errors.New("terminal size must be positive")
	}

	b.mu.Lock()
	b.nextHandle++
	handle := b.nextHandle
	ctx, cancel := context.WithCancel(context.Background())
	session := &browserSession{
		handle:  handle,
		ctx:     ctx,
		cancel:  cancel,
		input:   newChunkReader(),
		resizes: make(chan internalssh.WindowSize, 1),
		acks:    make(map[int]chan error),
	}
	session.conn = newBrowserConn(session)
	b.sessions[handle] = session
	b.mu.Unlock()

	go func() {
		defer clear(clientAuth)
		connected, runErr := internalssh.Run(ctx, session.conn, internalssh.Config{
			ExpectedHostKey: hostKey,
			ClientAuth:      clientAuth,
			TerminalName:    "xterm-256color",
			InitialSize:     internalssh.WindowSize{Width: width, Height: height},
			Input:           session.input,
			Output:          eventWriter{session: session},
			ErrorOutput:     eventWriter{session: session},
			Resizes:         session.resizes,
			OnConnected: func() {
				session.postState("connected", "", true)
			},
		})
		session.shutdown()
		message := ""
		if runErr != nil && !errors.Is(runErr, context.Canceled) && !errors.Is(runErr, net.ErrClosed) && !errors.Is(runErr, io.EOF) {
			message = runErr.Error()
		}
		session.postState("closed", message, connected && runErr == nil)
		b.mu.Lock()
		delete(b.sessions, handle)
		b.mu.Unlock()
	}()
	return handle, nil
}

func (b *bridge) session(handle int) (*browserSession, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	session := b.sessions[handle]
	if session == nil {
		return nil, errors.New("unknown SSH session")
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
	return nil, session.conn.feed(bytes)
}

func (b *bridge) input(args []js.Value) (any, error) {
	if len(args) != 2 {
		return nil, errors.New("input expects a handle and bytes")
	}
	session, err := b.session(args[0].Int())
	if err != nil {
		return nil, err
	}
	bytes, err := bytesFromJS(args[1])
	if err != nil {
		return nil, err
	}
	return nil, session.input.push(bytes)
}

func (b *bridge) resize(args []js.Value) (any, error) {
	if len(args) != 3 {
		return nil, errors.New("resize expects a handle, width, and height")
	}
	session, err := b.session(args[0].Int())
	if err != nil {
		return nil, err
	}
	size := internalssh.WindowSize{Width: args[1].Int(), Height: args[2].Int()}
	if size.Width <= 0 || size.Height <= 0 {
		return nil, errors.New("terminal size must be positive")
	}
	select {
	case session.resizes <- size:
	default:
		select {
		case <-session.resizes:
		default:
		}
		select {
		case session.resizes <- size:
		default:
		}
	}
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
		return nil, errors.New("unknown SSH event acknowledgement")
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
	_ = s.conn.Close()
	s.input.close()
}

func (s *browserSession) postState(event, message string, clean bool) {
	payload := js.Global().Get("Object").New()
	payload.Set("type", "ssh-event")
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
	payload.Set("type", "ssh-event")
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

type eventWriter struct{ session *browserSession }

func (w eventWriter) Write(bytes []byte) (int, error) {
	if err := w.session.emitBytes("output", bytes, time.Time{}); err != nil {
		return 0, err
	}
	return len(bytes), nil
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
	default:
		return errors.New("SSH stream input queue is full")
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
func (c *browserConn) RemoteAddr() net.Addr { return browserAddr("croc-ssh") }

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

func (*timeoutError) Error() string   { return "SSH stream deadline exceeded" }
func (*timeoutError) Timeout() bool   { return true }
func (*timeoutError) Temporary() bool { return true }
