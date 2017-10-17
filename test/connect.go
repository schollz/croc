package main

import (
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// runClient spawns threads for parallel uplink/downlink via TCP
func runClient(connectionType string, codePhrase string) {
	logger := log.WithFields(log.Fields{
		"codePhrase": codePhrase,
		"connection": connectionType,
	})
	var wg sync.WaitGroup
	wg.Add(numberConnections)
	for id := 0; id < numberConnections; id++ {
		go func(id int) {
			defer wg.Done()
			port := strconv.Itoa(27001 + id)
			connection, err := net.Dial("tcp", "localhost:"+port)
			if err != nil {
				panic(err)
			}
			defer connection.Close()

			message := receiveMessage(connection)
			logger.Infof("message: %s", message)
			sendMessage(connectionType+"."+codePhrase, connection)
			if connectionType == "s" {
				logger.Info("waiting for ok from relay")
				message = receiveMessage(connection)
				logger.Info("got ok from relay")
				// wait for pipe to be made
				time.Sleep(1 * time.Second)
				// Send file name
				logger.Info("sending filename")
				sendMessage("filename", connection)
				// Send file size
				time.Sleep(3 * time.Second)
				logger.Info("sending filesize")
				sendMessage("filesize", connection)
				// TODO: Write data from file

				// TODO: Release from connection pool
				// POST /release
			} else {
				fileName := receiveMessage(connection)
				fileSize := receiveMessage(connection)
				logger.Infof("fileName: %s", fileName)
				logger.Infof("fileSize: %s", fileSize)
				// TODO: Pull data and write to file

				// TODO: Release from connection pool
				// POST /release
			}

		}(id)
	}
	wg.Wait()
}

func sendFileToClient(id int, connection net.Conn) {
	logger := log.WithFields(log.Fields{
		"function": "sendFileToClient #" + strconv.Itoa(id),
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

	fileName := fillString(fileInfo.Name(), 64)

	if id == 0 || id == numberConnections-1 {
		logger.Infof("numChunks: %v", numChunks)
		logger.Infof("chunksPerWorker: %v", chunksPerWorker)
		logger.Infof("bytesPerConnection: %v", bytesPerConnection)
		logger.Infof("fileName: %v", fileInfo.Name())
	}

	logger.Info("sending")
	connection.Write([]byte(fileSize))
	connection.Write([]byte(fileName))
	sendBuffer := make([]byte, BUFFERSIZE)

	chunkI := 0
	for {
		_, err = file.Read(sendBuffer)
		if err == io.EOF {
			//End of file reached, break out of for loop
			logger.Info("EOF")
			break
		}
		if (chunkI >= chunksPerWorker*id && chunkI < chunksPerWorker*id+chunksPerWorker) || (id == numberConnections-1 && chunkI >= chunksPerWorker*id) {
			connection.Write(sendBuffer)
		}
		chunkI++
	}
	fmt.Println("File has been sent, closing connection!")
	return
}
