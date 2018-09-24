package croc

import (
	"crypto/rand"
	"io/ioutil"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSendReceive(t *testing.T) {
	generateRandomFile(100)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		c := Init(true)
		assert.Nil(t, c.Send("100mb.file", "test"))
	}()
	go func() {
		defer wg.Done()
		time.Sleep(100 * time.Millisecond)
		os.MkdirAll("test", 0755)
		os.Chdir("test")
		c := Init(true)
		assert.Nil(t, c.Receive("test"))
	}()
	wg.Wait()
}

func generateRandomFile(megabytes int) {
	// generate a random file
	bigBuff := make([]byte, 1024*1024*megabytes)
	rand.Read(bigBuff)
	ioutil.WriteFile("100mb.file", bigBuff, 0666)
}
