package main

import (
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"strconv"
	"sync"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

func runServer() {
	logger := log.WithFields(log.Fields{
		"function": "main",
	})
	logger.Info("Initializing")
	var wg sync.WaitGroup
	wg.Add(numberConnections)
	for id := 0; id < numberConnections; id++ {
		go listenerThread(id, &wg)
	}
	wg.Wait()
}

func listenerThread(id int, wg *sync.WaitGroup) {
	logger := log.WithFields(log.Fields{
		"function": "listenerThread@" + serverAddress + ":" + strconv.Itoa(27000+id),
	})

	defer wg.Done()

	err := listener(id)
	if err != nil {
		logger.Error(err)
	}
}

func listener(id int) (err error) {
	port := strconv.Itoa(27001 + id)
	logger := log.WithFields(log.Fields{
		"function": "listener@" + serverAddress + ":" + port,
	})
	server, err := net.Listen("tcp", serverAddress+":"+port)
	if err != nil {
		return errors.Wrap(err, "Error listening on "+serverAddress+":"+port)
	}
	defer server.Close()
	logger.Info("waiting for connections")
	//Spawn a new goroutine whenever a client connects
	for {
		connection, err := server.Accept()
		if err != nil {
			return errors.Wrap(err, "problem accepting connection")
		}
		logger.Infof("Client %s connected", connection.RemoteAddr().String())
		go sendFileToClient(id, connection)
	}
}

//This function is to 'fill'
func fillString(retunString string, toLength int) string {
	for {
		lengthString := len(retunString)
		if lengthString < toLength {
			retunString = retunString + ":"
			continue
		}
		break
	}
	return retunString
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
