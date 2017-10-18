package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"net"
	"os"
	"path"
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

			sendMessage(connectionType+"."+Hash(codePhrase), connection)
			if connectionType == "s" { // this is a sender
				if id == 0 {
					fmt.Println("waiting for other to connect")
				}
				logger.Debug("waiting for ok from relay")
				message = receiveMessage(connection)
				logger.Debug("got ok from relay")
				// wait for pipe to be made
				time.Sleep(100 * time.Millisecond)
				// Write data from file
				logger.Debug("send file")
				sendFile(id, connection, codePhrase)
			} else { // this is a receiver
				// receive file
				logger.Debug("receive file")
				fileName, fileIV, fileSalt, fileHash = receiveFile(id, connection, codePhrase)
			}

		}(id)
	}
	wg.Wait()

	if connectionType == "r" {
		catFile(fileName)
		encrypted, err := ioutil.ReadFile(fileName + ".encrypted")
		if err != nil {
			log.Error(err)
			return
		}
		fmt.Println("\n\ndecrypting...")
		decrypted, err := Decrypt(encrypted, codePhrase, fileSalt, fileIV)
		if err != nil {
			log.Error(err)
			return
		}
		ioutil.WriteFile(fileName, decrypted, 0644)
		os.Remove(fileName + ".encrypted")
		log.Debugf("\n\n\ndownloaded hash: %s", HashBytes(decrypted))
		log.Debugf("\n\n\nrelayed hash: %s", fileHash)

		if fileHash != HashBytes(decrypted) {
			fmt.Printf("\nUh oh! %s is corrupted! Sorry, try again.\n", fileName)
		} else {
			fmt.Printf("\nDownloaded %s!", fileName)
		}
	}
}

func catFile(fileNameToReceive string) {
	// cat the file
	os.Remove(fileNameToReceive)
	finished, err := os.Create(fileNameToReceive + ".encrypted")
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

}

func receiveFile(id int, connection net.Conn, codePhrase string) (fileNameToReceive string, iv string, salt string, hashOfFile string) {
	logger := log.WithFields(log.Fields{
		"function": "receiveFile #" + strconv.Itoa(id),
	})

	logger.Debug("waiting for file size")

	bufferFileSize := make([]byte, 10)
	connection.Read(bufferFileSize)
	fileSize, _ := strconv.ParseInt(strings.Trim(string(bufferFileSize), ":"), 10, 64)
	logger.Debugf("filesize: %d", fileSize)

	bufferFileName := make([]byte, 64)
	connection.Read(bufferFileName)
	fileNameToReceive = strings.Trim(string(bufferFileName), ":")
	logger.Debugf("fileName: %v", fileNameToReceive)

	ivHex := make([]byte, BUFFERSIZE)
	connection.Read(ivHex)
	iv = strings.Trim(string(ivHex), ":")
	logger.Debugf("iv: %s", iv)

	saltHex := make([]byte, BUFFERSIZE)
	connection.Read(saltHex)
	salt = strings.Trim(string(saltHex), ":")
	logger.Debugf("salt: %s", salt)

	hashOfFileBytes := make([]byte, BUFFERSIZE)
	connection.Read(hashOfFileBytes)
	hashOfFile = strings.Trim(string(hashOfFileBytes), ":")
	logger.Debugf("hashOfFile: %s", hashOfFile)

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
	return
}

func sendFile(id int, connection net.Conn, codePhrase string) {
	logger := log.WithFields(log.Fields{
		"function": "sendFile #" + strconv.Itoa(id),
	})
	defer connection.Close()

	var err error

	// // Open the file that needs to be send to the client
	// file, err := os.Open(fileName + ".encrypted")
	// if err != nil {
	// 	logger.Error(err)
	// 	return
	// }
	// defer file.Close()
	// // Get the filename and filesize
	// fileInfo, err := file.Stat()
	// if err != nil {
	// 	logger.Error(err)
	// 	return
	// }

	numChunks := math.Ceil(float64(len(fileBytes)) / float64(BUFFERSIZE))
	chunksPerWorker := int(math.Ceil(numChunks / float64(numberConnections)))

	bytesPerConnection := int64(chunksPerWorker * BUFFERSIZE)
	if id+1 == numberConnections {
		bytesPerConnection = int64(len(fileBytes)) - (numberConnections-1)*bytesPerConnection
	}
	fileSize := fillString(strconv.FormatInt(int64(bytesPerConnection), 10), 10)

	fileNameToSend := fillString(path.Base(fileName), 64)

	if id == 0 || id == numberConnections-1 {
		logger.Debugf("numChunks: %v", numChunks)
		logger.Debugf("chunksPerWorker: %v", chunksPerWorker)
		logger.Debugf("bytesPerConnection: %v", bytesPerConnection)
		logger.Debugf("fileNameToSend: %v", path.Base(fileName))
	}

	// send file size
	logger.Debugf("sending fileSize: %s", fileSize)
	connection.Write([]byte(fileSize))

	// send fileName
	logger.Debugf("sending fileNameToSend: %s", fileNameToSend)
	connection.Write([]byte(fileNameToSend))

	// send iv
	logger.Debugf("sending iv: %s", fileIV)
	connection.Write([]byte(fillString(fileIV, BUFFERSIZE)))

	// send salt
	logger.Debugf("sending salt: %s", fileSalt)
	connection.Write([]byte(fillString(fileSalt, BUFFERSIZE)))

	// send sha256sum of file
	logger.Debugf("sending sha256sum: %s", fileHash)
	connection.Write([]byte(fillString(fileHash, BUFFERSIZE)))

	sendBuffer := make([]byte, BUFFERSIZE)
	file := bytes.NewBuffer(fileBytes)
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
