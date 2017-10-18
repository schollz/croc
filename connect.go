package main

import (
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gosuri/uiprogress"
	log "github.com/sirupsen/logrus"
)

var bars []*uiprogress.Bar

// runClient spawns threads for parallel uplink/downlink via TCP
func runClient(connectionType string, codePhrase string) {
	logger := log.WithFields(log.Fields{
		"codePhrase": codePhrase,
		"connection": connectionType,
	})
	var wg sync.WaitGroup
	wg.Add(numberConnections)

	uiprogress.Start()
	bars = make([]*uiprogress.Bar, numberConnections)
	fileNameToReceive := ""
	for id := 0; id < numberConnections; id++ {
		go func(id int) {
			defer wg.Done()
			port := strconv.Itoa(27001 + id)
			connection, err := net.Dial("tcp", serverAddress+":"+port)
			if err != nil {
				panic(err)
			}
			defer connection.Close()

			message := receiveMessage(connection)
			logger.Debugf("relay says: %s", message)
			logger.Debugf("telling relay: %s", connectionType+"."+codePhrase)
			sendMessage(connectionType+"."+codePhrase, connection)
			if connectionType == "s" { // this is a sender
				logger.Debug("waiting for ok from relay")
				message = receiveMessage(connection)
				logger.Debug("got ok from relay")
				// wait for pipe to be made
				time.Sleep(100 * time.Millisecond)

				// Write data from file
				logger.Debug("send file")
				sendFile(id, connection)
			} else { // this is a receiver
				// receive file
				logger.Debug("receive file")
				fileNameToReceive = receiveFile(id, connection)
			}

		}(id)
	}
	wg.Wait()

	if connectionType == "r" {
		catFile(fileNameToReceive)
	}
}

func catFile(fileNameToReceive string) {
	// cat the file
	os.Remove(fileNameToReceive)
	finished, err := os.Create(fileNameToReceive)
	defer finished.Close()
	if err != nil {
		log.Fatal(err)
	}
	for id := 0; id < numberConnections; id++ {
		fh, err := os.Open(fileNameToReceive + "." + strconv.Itoa(id))
		if err != nil {
			log.Fatal(err)
		}

		_, err = io.Copy(finished, fh)
		if err != nil {
			log.Fatal(err)
		}
		fh.Close()
		os.Remove(fileNameToReceive + "." + strconv.Itoa(id))
	}

	fmt.Println("\n\n\nDownloaded " + fileNameToReceive + "!")
}

func receiveFile(id int, connection net.Conn) string {
	logger := log.WithFields(log.Fields{
		"function": "receiveFile #" + strconv.Itoa(id),
	})
	bufferFileName := make([]byte, 64)
	bufferFileSize := make([]byte, 10)

	logger.Debug("waiting for file size")
	connection.Read(bufferFileSize)
	fileSize, _ := strconv.ParseInt(strings.Trim(string(bufferFileSize), ":"), 10, 64)
	logger.Debugf("filesize: %d", fileSize)

	connection.Read(bufferFileName)
	fileNameToReceive := strings.Trim(string(bufferFileName), ":")
	logger.Debugf("fileName: %v", fileNameToReceive)
	os.Remove(fileNameToReceive + "." + strconv.Itoa(id))
	newFile, err := os.Create(fileNameToReceive + "." + strconv.Itoa(id))
	if err != nil {
		panic(err)
	}
	defer newFile.Close()

	bars[id] = uiprogress.AddBar(int(fileSize)/1024 + 1).AppendCompleted().PrependElapsed()

	logger.Debug("waiting for file")
	var receivedBytes int64
	for {
		bars[id].Incr()
		if (fileSize - receivedBytes) < BUFFERSIZE {
			logger.Debug("at the end")
			io.CopyN(newFile, connection, (fileSize - receivedBytes))
			// Empty the remaining bytes that we don't need from the network buffer
			if (receivedBytes+BUFFERSIZE)-fileSize < BUFFERSIZE {
				logger.Debug("empty remaining bytes from network buffer")
				connection.Read(make([]byte, (receivedBytes+BUFFERSIZE)-fileSize))
			}
			break
		}
		io.CopyN(newFile, connection, BUFFERSIZE)
		//Increment the counter
		receivedBytes += BUFFERSIZE
	}
	logger.Debug("received file")
	return fileNameToReceive
}

func sendFile(id int, connection net.Conn) {
	logger := log.WithFields(log.Fields{
		"function": "sendFile #" + strconv.Itoa(id),
	})
	defer connection.Close()
	//Open the file that needs to be send to the client
	file, err := os.Open(fileName)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer file.Close()
	//Get the filename and filesize
	fileInfo, err := file.Stat()
	if err != nil {
		fmt.Println(err)
		return
	}

	numChunks := math.Ceil(float64(fileInfo.Size()) / float64(BUFFERSIZE))
	chunksPerWorker := int(math.Ceil(numChunks / float64(numberConnections)))

	bytesPerConnection := int64(chunksPerWorker * BUFFERSIZE)
	if id+1 == numberConnections {
		bytesPerConnection = fileInfo.Size() - (numberConnections-1)*bytesPerConnection
	}
	fileSize := fillString(strconv.FormatInt(int64(bytesPerConnection), 10), 10)

	fileNameToSend := fillString(fileInfo.Name(), 64)

	if id == 0 || id == numberConnections-1 {
		logger.Debugf("numChunks: %v", numChunks)
		logger.Debugf("chunksPerWorker: %v", chunksPerWorker)
		logger.Debugf("bytesPerConnection: %v", bytesPerConnection)
		logger.Debugf("fileNameToSend: %v", fileInfo.Name())
	}

	logger.Debugf("sending %s", fileSize)
	connection.Write([]byte(fileSize))
	connection.Write([]byte(fileNameToSend))
	sendBuffer := make([]byte, BUFFERSIZE)

	chunkI := 0
	for {
		_, err = file.Read(sendBuffer)
		if err == io.EOF {
			//End of file reached, break out of for loop
			logger.Debug("EOF")
			break
		}
		if (chunkI >= chunksPerWorker*id && chunkI < chunksPerWorker*id+chunksPerWorker) || (id == numberConnections-1 && chunkI >= chunksPerWorker*id) {
			connection.Write(sendBuffer)
		}
		chunkI++
	}
	logger.Debug("file is sent")
	return
}
