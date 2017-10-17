package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gosuri/uiprogress"
	log "github.com/sirupsen/logrus"
)

func runClient() {
	uiprogress.Start()
	var wg sync.WaitGroup
	wg.Add(numberConnections)
	bars := make([]*uiprogress.Bar, numberConnections)
	for id := 0; id < numberConnections; id++ {
		go func(id int) {
			defer wg.Done()
			port := strconv.Itoa(27001 + id)
			connection, err := net.Dial("tcp", "localhost:"+port)
			if err != nil {
				panic(err)
			}
			defer connection.Close()

			bufferFileName := make([]byte, 64)
			bufferFileSize := make([]byte, 10)

			connection.Read(bufferFileSize)
			fileSize, _ := strconv.ParseInt(strings.Trim(string(bufferFileSize), ":"), 10, 64)
			bars[id] = uiprogress.AddBar(int(fileSize+1028) / 1024).AppendCompleted().PrependElapsed()

			connection.Read(bufferFileName)
			fileName = strings.Trim(string(bufferFileName), ":")
			os.Remove(fileName + "." + strconv.Itoa(id))
			newFile, err := os.Create(fileName + "." + strconv.Itoa(id))
			if err != nil {
				panic(err)
			}
			defer newFile.Close()

			var receivedBytes int64
			for {
				if (fileSize - receivedBytes) < BUFFERSIZE {
					io.CopyN(newFile, connection, (fileSize - receivedBytes))
					// Empty the remaining bytes that we don't need from the network buffer
					connection.Read(make([]byte, (receivedBytes+BUFFERSIZE)-fileSize))
					break
				}
				io.CopyN(newFile, connection, BUFFERSIZE)
				//Increment the counter
				receivedBytes += BUFFERSIZE
				bars[id].Incr()
			}
		}(id)
	}
	wg.Wait()

	// cat the file
	os.Remove(fileName)
	finished, err := os.Create(fileName)
	defer finished.Close()
	if err != nil {
		log.Fatal(err)
	}
	for id := 0; id < numberConnections; id++ {
		fh, err := os.Open(fileName + "." + strconv.Itoa(id))
		if err != nil {
			log.Fatal(err)
		}

		_, err = io.Copy(finished, fh)
		if err != nil {
			log.Fatal(err)
		}
		fh.Close()
		os.Remove(fileName + "." + strconv.Itoa(id))
	}
	cmd := exec.Command("cmd", "/c", "cls")
	cmd.Stdout = os.Stdout
	cmd.Run()
	fmt.Println("\n\n\nDownloaded " + fileName + "!")
	time.Sleep(1 * time.Second)
}
