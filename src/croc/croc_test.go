package croc

import (
	"sync"
	"testing"
	"time"

	log "github.com/cihub/seelog"
	"github.com/schollz/croc/v6/src/tcp"
)

func TestCroc(t *testing.T) {
	defer log.Flush()

	go tcp.Run("debug", "8081", "8082,8083,8084,8085")
	go tcp.Run("debug", "8082")
	go tcp.Run("debug", "8083")
	go tcp.Run("debug", "8084")
	go tcp.Run("debug", "8085")
	time.Sleep(300 * time.Millisecond)

	log.Flush()
	log.Debug("setting up sender")
	log.Flush()
	sender, err := New(Options{
		IsSender:     true,
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
