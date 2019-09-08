package croc

import (
	"io/ioutil"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/schollz/croc/v6/src/tcp"
	log "github.com/schollz/logger"
	"github.com/stretchr/testify/assert"
)

func TestCroc(t *testing.T) {
	log.SetLevel("trace")
	defer os.Remove("README.md")
	go tcp.Run("debug", "8081", "8082,8083,8084,8085")
	go tcp.Run("debug", "8082")
	go tcp.Run("debug", "8083")
	go tcp.Run("debug", "8084")
	go tcp.Run("debug", "8085")
	time.Sleep(300 * time.Millisecond)

	log.Debug("setting up sender")
	sender, err := New(Options{
		IsSender:     true,
		SharedSecret: "test",
		Debug:        true,
		RelayAddress: "localhost:8081",
		RelayPorts:   []string{"8081"},
		Stdout:       false,
		NoPrompt:     true,
		DisableLocal: true,
	})
	if err != nil {
		panic(err)
	}

	log.Debug("setting up receiver")
	receiver, err := New(Options{
		IsSender:     false,
		SharedSecret: "test",
		Debug:        true,
		RelayAddress: "localhost:8081",
		Stdout:       false,
		NoPrompt:     true,
		DisableLocal: true,
	})
	if err != nil {
		panic(err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		sender.Send(TransferOptions{
			PathToFiles: []string{"../../README.md"},
		})
		wg.Done()
	}()
	time.Sleep(100 * time.Millisecond)
	go func() {
		receiver.Receive()
		wg.Done()
	}()

	wg.Wait()
}

func TestCrocLocal(t *testing.T) {
	log.SetLevel("trace")
	defer os.Remove("LICENSE")
	defer os.Remove("touched")
	time.Sleep(300 * time.Millisecond)

	log.Debug("setting up sender")
	sender, err := New(Options{
		IsSender:     true,
		SharedSecret: "test",
		Debug:        true,
		RelayAddress: "localhost:8181",
		RelayPorts:   []string{"8181", "8182"},
		Stdout:       true,
		NoPrompt:     true,
		DisableLocal: false,
	})
	if err != nil {
		panic(err)
	}
	time.Sleep(1 * time.Second)

	log.Debug("setting up receiver")
	receiver, err := New(Options{
		IsSender:     false,
		SharedSecret: "test",
		Debug:        true,
		RelayAddress: "localhost:8181",
		Stdout:       true,
		NoPrompt:     true,
		DisableLocal: false,
	})
	if err != nil {
		panic(err)
	}

	var wg sync.WaitGroup
	os.Create("touched")
	wg.Add(2)
	go func() {
		sender.Send(TransferOptions{
			PathToFiles:      []string{"../../LICENSE", "touched"},
			KeepPathInRemote: false,
		})
		wg.Done()
	}()
	time.Sleep(100 * time.Millisecond)
	go func() {
		receiver.Receive()
		wg.Done()
	}()

	wg.Wait()
}

func TestCrocError(t *testing.T) {
	content := []byte("temporary file's content")
	tmpfile, err := ioutil.TempFile("", "example")
	if err != nil {
		panic(err)
	}

	defer os.Remove(tmpfile.Name()) // clean up

	if _, err := tmpfile.Write(content); err != nil {
		panic(err)
	}
	if err := tmpfile.Close(); err != nil {
		panic(err)
	}

	Debug(false)
	log.SetLevel("warn")
	sender, _ := New(Options{
		IsSender:     true,
		SharedSecret: "test33",
		Debug:        true,
		RelayAddress: "doesntexistok.com:8381",
		RelayPorts:   []string{"8381", "8382"},
		Stdout:       true,
		NoPrompt:     true,
		DisableLocal: true,
	})
	err = sender.Send(TransferOptions{
		PathToFiles:      []string{tmpfile.Name()},
		KeepPathInRemote: true,
	})
	log.Debug(err)
	assert.NotNil(t, err)

}
