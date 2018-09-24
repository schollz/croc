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

func TestSendReceive(t *testing.T) {
	forceSend := 0
	var startTime time.Time
	var durationPerMegabyte float64
	generateRandomFile(100)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		c := Init(true)
		c.ForceSend = forceSend
		assert.Nil(t, c.Send("100mb.file", "test"))
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
		assert.True(t, utils.Exists("100mb.file"))
	}()
	wg.Wait()
	os.Chdir("..")
	os.RemoveAll("test")
	os.Remove("100mb.file")
	fmt.Printf("\n-----\n%2.1f MB/s\n----\n", durationPerMegabyte)
}

func generateRandomFile(megabytes int) {
	// generate a random file
	bigBuff := make([]byte, 1024*1024*megabytes)
	rand.Read(bigBuff)
	ioutil.WriteFile("100mb.file", bigBuff, 0666)
}
