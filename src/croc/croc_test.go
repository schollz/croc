package croc

import (
	"crypto/rand"
	"fmt"
	"io/ioutil"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/schollz/croc/src/utils"
	"github.com/stretchr/testify/assert"
)

func sendAndReceive(t *testing.T, forceSend int) {
	var startTime time.Time
	var durationPerMegabyte float64
	fname := generateRandomFile(100)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		c := Init(true)
		c.ForceSend = forceSend
		assert.Nil(t, c.Send(fname, "test"))
	}()
	go func() {
		defer wg.Done()
		time.Sleep(3 * time.Second)
		os.MkdirAll("test", 0755)
		os.Chdir("test")
		c := Init(true)
		c.ForceSend = forceSend
		startTime = time.Now()
		assert.Nil(t, c.Receive("test"))
		durationPerMegabyte = 100.0 / time.Since(startTime).Seconds()
		assert.True(t, utils.Exists(fname))
	}()
	wg.Wait()
	os.Chdir("..")
	os.RemoveAll("test")
	os.Remove(fname)
	fmt.Printf("\n-----\n%2.1f MB/s\n----\n", durationPerMegabyte)
}
func TestSendReceiveWebsockets(t *testing.T) {
	sendAndReceive(t, 1)
}
func TestSendReceiveTCP(t *testing.T) {
	sendAndReceive(t, 1)
}

func generateRandomFile(megabytes int) (fname string) {
	// generate a random file
	bigBuff := make([]byte, 1024*1024*megabytes)
	rand.Read(bigBuff)
	fname = fmt.Sprintf("%dmb.file", megabytes)
	ioutil.WriteFile(fname, bigBuff, 0666)
	return
}
