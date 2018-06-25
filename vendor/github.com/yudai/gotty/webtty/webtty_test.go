package webtty

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"sync"
	"testing"
)

type pipePair struct {
	*io.PipeReader
	*io.PipeWriter
}

func TestWriteFromPTY(t *testing.T) {
	connInPipeReader, connInPipeWriter := io.Pipe() // in to conn
	connOutPipeReader, _ := io.Pipe()               // out from conn

	conn := pipePair{
		connOutPipeReader,
		connInPipeWriter,
	}
	dt, err := New(conn)
	if err != nil {
		t.Fatalf("Unexpected error from New(): %s", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		wg.Done()
		err := dt.Run(ctx)
		if err != nil {
			t.Fatalf("Unexpected error from Run(): %s", err)
		}
	}()

	message := []byte("foobar")
	n, err := dt.TTY().Write(message)
	if err != nil {
		t.Fatalf("Unexpected error from Write(): %s", err)
	}
	if n != len(message) {
		t.Fatalf("Write() accepted `%d` for message `%s`", n, message)
	}

	buf := make([]byte, 1024)
	n, err = connInPipeReader.Read(buf)
	if err != nil {
		t.Fatalf("Unexpected error from Read(): %s", err)
	}
	if buf[0] != Output {
		t.Fatalf("Unexpected message type `%c`", buf[0])
	}
	decoded := make([]byte, 1024)
	n, err = base64.StdEncoding.Decode(decoded, buf[1:n])
	if err != nil {
		t.Fatalf("Unexpected error from Decode(): %s", err)
	}
	if !bytes.Equal(decoded[:n], message) {
		t.Fatalf("Unexpected message received: `%s`", decoded[:n])
	}

	cancel()
	wg.Wait()
}

func TestWriteFromConn(t *testing.T) {
	connInPipeReader, connInPipeWriter := io.Pipe()   // in to conn
	connOutPipeReader, connOutPipeWriter := io.Pipe() // out from conn

	conn := pipePair{
		connOutPipeReader,
		connInPipeWriter,
	}

	dt, err := New(conn)
	if err != nil {
		t.Fatalf("Unexpected error from New(): %s", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		wg.Done()
		err := dt.Run(ctx)
		if err != nil {
			t.Fatalf("Unexpected error from Run(): %s", err)
		}
	}()

	var (
		message []byte
		n       int
	)
	readBuf := make([]byte, 1024)

	// input
	message = []byte("0hello\n") // line buffered canonical mode
	n, err = connOutPipeWriter.Write(message)
	if err != nil {
		t.Fatalf("Unexpected error from Write(): %s", err)
	}
	if n != len(message) {
		t.Fatalf("Write() accepted `%d` for message `%s`", n, message)
	}

	n, err = dt.TTY().Read(readBuf)
	if err != nil {
		t.Fatalf("Unexpected error from Write(): %s", err)
	}
	if !bytes.Equal(readBuf[:n], message[1:]) {
		t.Fatalf("Unexpected message received: `%s`", readBuf[:n])
	}

	// ping
	message = []byte("1\n") // line buffered canonical mode
	n, err = connOutPipeWriter.Write(message)
	if n != len(message) {
		t.Fatalf("Write() accepted `%d` for message `%s`", n, message)
	}

	n, err = connInPipeReader.Read(readBuf)
	if err != nil {
		t.Fatalf("Unexpected error from Read(): %s", err)
	}
	if !bytes.Equal(readBuf[:n], []byte{'1'}) {
		t.Fatalf("Unexpected message received: `%s`", readBuf[:n])
	}

	// TODO: resize

	cancel()
	wg.Wait()
}
