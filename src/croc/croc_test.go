package croc

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/schollz/croc/v9/src/tcp"
	log "github.com/schollz/logger"
	"github.com/stretchr/testify/assert"
)

func init() {
	log.SetLevel("trace")

	go tcp.Run("debug", "localhost", "8281", "pass123", "8282,8283,8284,8285")
	go tcp.Run("debug", "localhost", "8282", "pass123")
	go tcp.Run("debug", "localhost", "8283", "pass123")
	go tcp.Run("debug", "localhost", "8284", "pass123")
	go tcp.Run("debug", "localhost", "8285", "pass123")
	time.Sleep(1 * time.Second)
}

func TestCrocReadme(t *testing.T) {
	defer os.Remove("README.md")

	log.Debug("setting up sender")
	sender, err := New(Options{
		IsSender:      true,
		SharedSecret:  "8123-testingthecroc",
		Debug:         true,
		RelayAddress:  "localhost:8281",
		RelayPorts:    []string{"8281"},
		RelayPassword: "pass123",
		Stdout:        false,
		NoPrompt:      true,
		DisableLocal:  true,
		Curve:         "siec",
		Overwrite:     true,
	})
	if err != nil {
		panic(err)
	}

	log.Debug("setting up receiver")
	receiver, err := New(Options{
		IsSender:      false,
		SharedSecret:  "8123-testingthecroc",
		Debug:         true,
		RelayAddress:  "localhost:8281",
		RelayPassword: "pass123",
		Stdout:        false,
		NoPrompt:      true,
		DisableLocal:  true,
		Curve:         "siec",
		Overwrite:     true,
	})
	if err != nil {
		panic(err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		err := sender.Send(TransferOptions{
			PathToFiles: []string{"../../README.md"},
		})
		if err != nil {
			t.Errorf("send failed: %v", err)
		}
		wg.Done()
	}()
	time.Sleep(100 * time.Millisecond)
	go func() {
		err := receiver.Receive()
		if err != nil {
			t.Errorf("receive failed: %v", err)
		}
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
		IsSender:      true,
		SharedSecret:  "8123-testingthecroc",
		Debug:         true,
		RelayAddress:  "localhost:8181",
		RelayPorts:    []string{"8181", "8182"},
		RelayPassword: "pass123",
		Stdout:        true,
		NoPrompt:      true,
		DisableLocal:  false,
		Curve:         "siec",
		Overwrite:     true,
	})
	if err != nil {
		panic(err)
	}
	time.Sleep(1 * time.Second)

	log.Debug("setting up receiver")
	receiver, err := New(Options{
		IsSender:      false,
		SharedSecret:  "8123-testingthecroc",
		Debug:         true,
		RelayAddress:  "localhost:8181",
		RelayPassword: "pass123",
		Stdout:        true,
		NoPrompt:      true,
		DisableLocal:  false,
		Curve:         "siec",
		Overwrite:     true,
	})
	if err != nil {
		panic(err)
	}

	var wg sync.WaitGroup
	os.Create("touched")
	wg.Add(2)
	go func() {
		err := sender.Send(TransferOptions{
			PathToFiles:      []string{"../../LICENSE", "touched"},
			KeepPathInRemote: false,
		})
		if err != nil {
			t.Errorf("send failed: %v", err)
		}
		wg.Done()
	}()
	time.Sleep(100 * time.Millisecond)
	go func() {
		err := receiver.Receive()
		if err != nil {
			t.Errorf("send failed: %v", err)
		}
		wg.Done()
	}()

	wg.Wait()
}

func TestCrocError(t *testing.T) {
	content := []byte("temporary file's content")
	tmpfile, err := os.CreateTemp("", "example")
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
		IsSender:      true,
		SharedSecret:  "8123-testingthecroc2",
		Debug:         true,
		RelayAddress:  "doesntexistok.com:8381",
		RelayPorts:    []string{"8381", "8382"},
		RelayPassword: "pass123",
		Stdout:        true,
		NoPrompt:      true,
		DisableLocal:  true,
		Curve:         "siec",
		Overwrite:     true,
	})
	err = sender.Send(TransferOptions{
		PathToFiles:      []string{tmpfile.Name()},
		KeepPathInRemote: true,
	})
	log.Debug(err)
	assert.NotNil(t, err)

}
