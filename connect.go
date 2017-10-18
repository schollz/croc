package main

import (
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
	var iv, salt, fileNameToReceive string
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
				fileNameToReceive, iv, salt = receiveFile(id, connection, codePhrase)
			}

		}(id)
	}
	wg.Wait()

	if connectionType == "r" {
		catFile(fileNameToReceive)
		encrypted, err := ioutil.ReadFile(fileNameToReceive + ".encrypted")
		if err != nil {
			log.Error(err)
			return
		}
		fmt.Println("\n\ndecrypting...")
		decrypted, err := Decrypt(encrypted, codePhrase, salt, iv)
		if err != nil {
			log.Error(err)
			return
		}
		ioutil.WriteFile(fileNameToReceive, decrypted, 0644)
		os.Remove(fileNameToReceive + ".encrypted")
		fmt.Println("\nDownloaded " + fileNameToReceive + "!")
	} else {
		log.Info("cleaning up")
		os.Remove(fileName + ".encrypted")
		os.Remove(fileName + ".iv")
		os.Remove(fileName + ".salt")
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

func receiveFile(id int, connection net.Conn, codePhrase string) (fileNameToReceive string, iv string, salt string) {
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
	logger.Debugf("iv: %v", iv)

	saltHex := make([]byte, BUFFERSIZE)
	connection.Read(saltHex)
	salt = strings.Trim(string(saltHex), ":")
	logger.Debugf("salt: %v", salt)

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

	// Open the file that needs to be send to the client
	file, err := os.Open(fileName + ".encrypted")
	if err != nil {
		logger.Error(err)
		return
	}
	defer file.Close()
	// Get the filename and filesize
	fileInfo, err := file.Stat()
	if err != nil {
		logger.Error(err)
		return
	}

	numChunks := math.Ceil(float64(fileInfo.Size()) / float64(BUFFERSIZE))
	chunksPerWorker := int(math.Ceil(numChunks / float64(numberConnections)))

	bytesPerConnection := int64(chunksPerWorker * BUFFERSIZE)
	if id+1 == numberConnections {
		bytesPerConnection = fileInfo.Size() - (numberConnections-1)*bytesPerConnection
	}
	fileSize := fillString(strconv.FormatInt(int64(bytesPerConnection), 10), 10)

	fileNameToSend := fillString(path.Base(fileName), 64)

	if id == 0 || id == numberConnections-1 {
		logger.Debugf("numChunks: %v", numChunks)
		logger.Debugf("chunksPerWorker: %v", chunksPerWorker)
		logger.Debugf("bytesPerConnection: %v", bytesPerConnection)
		logger.Debugf("fileNameToSend: %v", path.Base(fileName))
	}

	logger.Debugf("sending fileSize: %s", fileSize)
	connection.Write([]byte(fileSize))
	logger.Debugf("sending fileNameToSend: %s", fileNameToSend)
	connection.Write([]byte(fileNameToSend))

	// send iv
	iv, err := ioutil.ReadFile(fileName + ".iv")
	if err != nil {
		log.Error(err)
		return
	}
	logger.Debugf("sending iv: %s", iv)
	connection.Write([]byte(fillString(string(iv), BUFFERSIZE)))

	// send salt
	salt, err := ioutil.ReadFile(fileName + ".salt")
	if err != nil {
		log.Error(err)
		return
	}
	logger.Debugf("sending salt: %s", salt)
	connection.Write([]byte(fillString(string(salt), BUFFERSIZE)))

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
