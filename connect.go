package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
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

type Connection struct {
	Server              string
	File                FileMetaData
	NumberOfConnections int
	Code                string
	HashedCode          string
	IsSender            bool
	Debug               bool
	DontEncrypt         bool
	bars                []*uiprogress.Bar
}

type FileMetaData struct {
	Name  string
	Size  int
	Hash  string
	IV    string
	Salt  string
	bytes []byte
}

func NewConnection(flags *Flags) *Connection {
	c := new(Connection)
	c.Debug = flags.Debug
	c.DontEncrypt = flags.DontEncrypt
	c.Server = flags.Server
	c.Code = flags.Code
	c.NumberOfConnections = flags.NumberOfConnections
	if len(flags.File) > 0 {
		c.File.Name = flags.File
		c.IsSender = true
	} else {
		c.IsSender = false
	}

	log.SetFormatter(&log.TextFormatter{})
	if c.Debug {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.WarnLevel)
	}

	return c
}

func (c *Connection) Run() {
	log.Debug("checking code validity")
	for {
		// check code
		goodCode := true
		m := strings.Split(c.Code, "-")
		numThreads, errParse := strconv.Atoi(m[0])
		if len(m) < 2 {
			goodCode = false
		} else if numThreads > MAX_NUMBER_THREADS || numThreads < 1 {
			c.NumberOfConnections = MAX_NUMBER_THREADS
			goodCode = false
		} else if errParse != nil {
			goodCode = false
		}
		log.Debug(m)
		if !goodCode {
			if c.IsSender {
				c.Code = strconv.Itoa(c.NumberOfConnections) + "-" + GetRandomName()
			} else {
				if len(c.Code) != 0 {
					fmt.Println("Code must begin with number of threads (e.g. 3-some-code)")
				}
				c.Code = getInput("Enter receive code: ")
			}
		} else {
			break
		}
	}
	// assign number of connections
	c.NumberOfConnections, _ = strconv.Atoi(strings.Split(c.Code, "-")[0])

	if c.IsSender {
		// encrypt the file
		log.Debug("encrypting...")
		fdata, err := ioutil.ReadFile(c.File.Name)
		if err != nil {
			log.Fatal(err)
			return
		}
		c.File.bytes, c.File.Salt, c.File.IV = Encrypt(fdata, c.Code, c.DontEncrypt)
		log.Debug("...finished encryption")
		c.File.Hash = HashBytes(fdata)
		c.File.Size = len(c.File.bytes)
		if c.Debug {
			ioutil.WriteFile(c.File.Name+".encrypted", c.File.bytes, 0644)
		}
		fmt.Printf("Sending %d byte file named '%s'\n", c.File.Size, c.File.Name)
		fmt.Printf("Code is: %s\n", c.Code)
	}

	c.runClient()
}

// runClient spawns threads for parallel uplink/downlink via TCP
func (c *Connection) runClient() {
	logger := log.WithFields(log.Fields{
		"code":    c.Code,
		"sender?": c.IsSender,
	})

	c.HashedCode = Hash(c.Code)

	var wg sync.WaitGroup
	wg.Add(c.NumberOfConnections)

	uiprogress.Start()
	if !c.Debug {
		c.bars = make([]*uiprogress.Bar, c.NumberOfConnections)
	}
	gotOK := false
	gotResponse := false
	for id := 0; id < c.NumberOfConnections; id++ {
		go func(id int) {
			defer wg.Done()
			port := strconv.Itoa(27001 + id)
			connection, err := net.Dial("tcp", c.Server+":"+port)
			if err != nil {
				panic(err)
			}
			defer connection.Close()

			message := receiveMessage(connection)
			logger.Debugf("relay says: %s", message)
			if c.IsSender {
				logger.Debugf("telling relay: %s", "s."+c.Code)
				metaData, err := json.Marshal(c.File)
				if err != nil {
					log.Error(err)
				}
				encryptedMetaData, salt, iv := Encrypt(metaData, c.Code)
				sendMessage("s."+c.HashedCode+"."+hex.EncodeToString(encryptedMetaData)+"-"+salt+"-"+iv, connection)
			} else {
				logger.Debugf("telling relay: %s", "r."+c.Code)
				sendMessage("r."+c.HashedCode+".0.0.0", connection)
			}
			if c.IsSender { // this is a sender
				if id == 0 {
					fmt.Printf("\nSending (<-%s)..\n", connection.RemoteAddr().String())
				}
				logger.Debug("waiting for ok from relay")
				message = receiveMessage(connection)
				logger.Debug("got ok from relay")
				// wait for pipe to be made
				time.Sleep(100 * time.Millisecond)
				// Write data from file
				logger.Debug("send file")
				c.sendFile(id, connection)
			} else { // this is a receiver
				logger.Debug("waiting for meta data from sender")
				message = receiveMessage(connection)
				m := strings.Split(message, "-")
				encryptedData, salt, iv := m[0], m[1], m[2]
				encryptedBytes, err := hex.DecodeString(encryptedData)
				if err != nil {
					log.Error(err)
					return
				}
				decryptedBytes, _ := Decrypt(encryptedBytes, c.Code, salt, iv, c.DontEncrypt)
				err = json.Unmarshal(decryptedBytes, &c.File)
				if err != nil {
					log.Error(err)
					return
				}
				log.Debugf("meta data received: %v", c.File)
				// have the main thread ask for the okay
				if id == 0 {
					fmt.Printf("Receiving file (%d bytes) into: %s\n", c.File.Size, c.File.Name)
					getOK := getInput("ok? (y/n): ")
					if getOK == "y" {
						gotOK = true
					}
					gotResponse = true
				}
				// wait for the main thread to get the okay
				for limit := 0; limit < 1000; limit++ {
					if gotResponse {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				if !gotOK {
					sendMessage("not ok", connection)
				} else {
					sendMessage("ok", connection)
					logger.Debug("receive file")
					c.receiveFile(id, connection)
				}
			}
		}(id)
	}
	wg.Wait()

	if !c.IsSender {
		if !gotOK {
			return
		}
		c.catFile(c.File.Name)
		encrypted, err := ioutil.ReadFile(c.File.Name + ".encrypted")
		if err != nil {
			log.Error(err)
			return
		}
		fmt.Println("\n\ndecrypting...")
		log.Debugf("Code: [%s]", c.Code)
		log.Debugf("Salt: [%s]", c.File.Salt)
		log.Debugf("IV: [%s]", c.File.IV)
		decrypted, err := Decrypt(encrypted, c.Code, c.File.Salt, c.File.IV, c.DontEncrypt)
		if err != nil {
			log.Error(err)
			return
		}
		log.Debugf("writing %d bytes to %s", len(decrypted), c.File.Name)
		err = ioutil.WriteFile(c.File.Name, decrypted, 0644)
		if err != nil {
			log.Error(err)
		}
		if !c.Debug {
			os.Remove(c.File.Name + ".encrypted")
		}
		log.Debugf("\n\n\ndownloaded hash: [%s]", HashBytes(decrypted))
		log.Debugf("\n\n\nrelayed hash: [%s]", c.File.Hash)

		if c.File.Hash != HashBytes(decrypted) {
			fmt.Printf("\nUh oh! %s is corrupted! Sorry, try again.\n", c.File.Name)
		} else {
			fmt.Printf("\nReceived file written to %s", c.File.Name)
		}
	} else {
		fmt.Println("File sent.")
		// TODO: Add confirmation
	}
}

func (c *Connection) catFile(fname string) {
	// cat the file
	os.Remove(fname)
	finished, err := os.Create(fname + ".encrypted")
	defer finished.Close()
	if err != nil {
		log.Fatal(err)
	}
	for id := 0; id < c.NumberOfConnections; id++ {
		fh, err := os.Open(fname + "." + strconv.Itoa(id))
		if err != nil {
			log.Fatal(err)
		}

		_, err = io.Copy(finished, fh)
		if err != nil {
			log.Fatal(err)
		}
		fh.Close()
		os.Remove(fname + "." + strconv.Itoa(id))
	}

}

func (c *Connection) receiveFile(id int, connection net.Conn) error {
	logger := log.WithFields(log.Fields{
		"function": "receiveFile #" + strconv.Itoa(id),
	})

	logger.Debug("waiting for chunk size from sender")
	fileSizeBuffer := make([]byte, 10)
	connection.Read(fileSizeBuffer)
	fileDataString := strings.Trim(string(fileSizeBuffer), ":")
	fileSizeInt, _ := strconv.Atoi(fileDataString)
	chunkSize := int64(fileSizeInt)
	logger.Debugf("chunk size: %d", chunkSize)

	os.Remove(c.File.Name + "." + strconv.Itoa(id))
	newFile, err := os.Create(c.File.Name + "." + strconv.Itoa(id))
	if err != nil {
		panic(err)
	}
	defer newFile.Close()

	if !c.Debug {
		c.bars[id] = uiprogress.AddBar(int(chunkSize)/1024 + 1).AppendCompleted().PrependElapsed()
	}

	logger.Debug("waiting for file")
	var receivedBytes int64
	for {
		if !c.Debug {
			c.bars[id].Incr()
		}
		if (chunkSize - receivedBytes) < BUFFERSIZE {
			logger.Debug("at the end")
			io.CopyN(newFile, connection, (chunkSize - receivedBytes))
			// Empty the remaining bytes that we don't need from the network buffer
			if (receivedBytes+BUFFERSIZE)-chunkSize < BUFFERSIZE {
				logger.Debug("empty remaining bytes from network buffer")
				connection.Read(make([]byte, (receivedBytes+BUFFERSIZE)-chunkSize))
			}
			break
		}
		io.CopyN(newFile, connection, BUFFERSIZE)
		receivedBytes += BUFFERSIZE
	}
	logger.Debug("received file")
	return nil
}

func (c *Connection) sendFile(id int, connection net.Conn) {
	logger := log.WithFields(log.Fields{
		"function": "sendFile #" + strconv.Itoa(id),
	})
	defer connection.Close()

	var err error

	numChunks := math.Ceil(float64(c.File.Size) / float64(BUFFERSIZE))
	chunksPerWorker := int(math.Ceil(numChunks / float64(c.NumberOfConnections)))

	chunkSize := int64(chunksPerWorker * BUFFERSIZE)
	if id+1 == c.NumberOfConnections {
		chunkSize = int64(c.File.Size) - int64(c.NumberOfConnections-1)*chunkSize
	}

	if id == 0 || id == c.NumberOfConnections-1 {
		logger.Debugf("numChunks: %v", numChunks)
		logger.Debugf("chunksPerWorker: %v", chunksPerWorker)
		logger.Debugf("bytesPerchunkSizeConnection: %v", chunkSize)
	}

	logger.Debugf("sending chunk size: %d", chunkSize)
	connection.Write([]byte(fillString(strconv.FormatInt(int64(chunkSize), 10), 10)))

	sendBuffer := make([]byte, BUFFERSIZE)
	file := bytes.NewBuffer(c.File.bytes)
	chunkI := 0
	if !c.Debug {
		c.bars[id] = uiprogress.AddBar(chunksPerWorker).AppendCompleted().PrependElapsed()
	}
	for {
		_, err = file.Read(sendBuffer)
		if err == io.EOF {
			//End of file reached, break out of for loop
			logger.Debug("EOF")
			break
		}
		if (chunkI >= chunksPerWorker*id && chunkI < chunksPerWorker*id+chunksPerWorker) || (id == c.NumberOfConnections-1 && chunkI >= chunksPerWorker*id) {
			connection.Write(sendBuffer)
			if !c.Debug {
				c.bars[id].Incr()
			}
		}
		chunkI++
	}
	logger.Debug("file is sent")
	return
}
