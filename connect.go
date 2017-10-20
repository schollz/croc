package main

import (
	"encoding/hex"
	"encoding/json"
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
	"github.com/pkg/errors"
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
        Wait                bool
	bars                []*uiprogress.Bar
	rate                int
}

type FileMetaData struct {
	Name string
	Size int
	Hash string
}

func NewConnection(flags *Flags) *Connection {
	c := new(Connection)
	c.Debug = flags.Debug
	c.DontEncrypt = flags.DontEncrypt
        c.Wait = flags.Wait
	c.Server = flags.Server
	c.Code = flags.Code
	c.NumberOfConnections = flags.NumberOfConnections
	c.rate = flags.Rate
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

func (c *Connection) Run() error {
	forceSingleThreaded := false
	if c.IsSender {
		fsize, err := FileSize(c.File.Name)
		if err != nil {
			return err
		}
		if fsize < MAX_NUMBER_THREADS*BUFFERSIZE {
			forceSingleThreaded = true
			log.Debug("forcing single thread")
		}
	}
	log.Debug("checking code validity")
	for {
		// check code
		goodCode := true
		m := strings.Split(c.Code, "-")
		log.Debug(m)
		numThreads, errParse := strconv.Atoi(m[0])
		if len(m) < 2 {
			goodCode = false
			log.Debug("code too short")
		} else if numThreads > MAX_NUMBER_THREADS || numThreads < 1 || (forceSingleThreaded && numThreads != 1) {
			c.NumberOfConnections = MAX_NUMBER_THREADS
			goodCode = false
			log.Debug("incorrect number of threads")
		} else if errParse != nil {
			goodCode = false
			log.Debug("problem parsing threads")
		}
		log.Debug(m)
		log.Debug(goodCode)
		if !goodCode {
			if c.IsSender {
				if forceSingleThreaded {
					c.NumberOfConnections = 1
				}
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
		if c.DontEncrypt {
			// don't encrypt
			CopyFile(c.File.Name, c.File.Name+".enc")
		} else {
			// encrypt
			log.Debug("encrypting...")
			if err := EncryptFile(c.File.Name, c.File.Name+".enc", c.Code); err != nil {
				return err
			}
		}
		// get file hash
		var err error
		c.File.Hash, err = HashFile(c.File.Name)
		if err != nil {
			return err
		}
		// get file size
		c.File.Size, err = FileSize(c.File.Name + ".enc")
		if err != nil {
			return err
		}
		fmt.Printf("Sending %d byte file named '%s'\n", c.File.Size, c.File.Name)
		fmt.Printf("Code is: %s\n", c.Code)
	}

	return c.runClient()
}

// runClient spawns threads for parallel uplink/downlink via TCP
func (c *Connection) runClient() error {
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
        notPresent := false
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
                                if c.Wait {
				    sendMessage("r."+c.HashedCode+".0.0.0", connection)
                                } else {
				    sendMessage("c."+c.HashedCode+".0.0.0", connection)
                                }
			}
			if c.IsSender { // this is a sender
				logger.Debug("waiting for ok from relay")
				message = receiveMessage(connection)
				logger.Debug("got ok from relay")
				if id == 0 {
					fmt.Printf("\nSending (->%s)..\n", message)
				}
				// wait for pipe to be made
				time.Sleep(100 * time.Millisecond)
				// Write data from file
				logger.Debug("send file")
				c.sendFile(id, connection)
			} else { // this is a receiver
				logger.Debug("waiting for meta data from sender")
				message = receiveMessage(connection)
				m := strings.Split(message, "-")
				encryptedData, salt, iv, sendersAddress := m[0], m[1], m[2], m[3]
                                if sendersAddress == "0.0.0.0" {
                                        notPresent = true
                                        return
                                }
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
					var sentFileNames []string

					if fileAlreadyExists(sentFileNames, c.File.Name) {
						fmt.Printf("Will not overwrite file!")
						os.Exit(1)
					}
					getOK := getInput("ok? (y/n): ")
					if getOK == "y" {
						gotOK = true
						sentFileNames = append(sentFileNames, c.File.Name)
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
					if id == 0 {
						fmt.Printf("\n\nReceiving (<-%s)..\n", sendersAddress)
					}
					c.receiveFile(id, connection)
				}
			}
		}(id)
	}
	wg.Wait()

	if !c.IsSender {
                if notPresent {
                    fmt.Println("Sender/Code not present")
                    return nil
                }
		if !gotOK {
			return errors.New("Transfer interrupted")
		}
		c.catFile(c.File.Name)
		log.Debugf("Code: [%s]", c.Code)
		if c.DontEncrypt {
			if err := CopyFile(c.File.Name+".enc", c.File.Name); err != nil {
				return err
			}
		} else {
			if err := DecryptFile(c.File.Name+".enc", c.File.Name, c.Code); err != nil {
				return errors.Wrap(err, "Problem decrypting file")
			}
		}
		if !c.Debug {
			os.Remove(c.File.Name + ".enc")
		}

		fileHash, err := HashFile(c.File.Name)
		if err != nil {
			log.Error(err)
		}
		log.Debugf("\n\n\ndownloaded hash: [%s]", fileHash)
		log.Debugf("\n\n\nrelayed hash: [%s]", c.File.Hash)

		if c.File.Hash != fileHash {
			return fmt.Errorf("\nUh oh! %s is corrupted! Sorry, try again.\n", c.File.Name)
		} else {
			fmt.Printf("\nReceived file written to %s", c.File.Name)
		}
	} else {
		fmt.Println("File sent.")
		// TODO: Add confirmation
	}
	return nil
}

func fileAlreadyExists(s []string, f string) bool {
	for _, a := range s {
		if a == f {
			return true
		}
	}
	return false
}

func (c *Connection) catFile(fname string) {
	// cat the file
	os.Remove(fname)
	finished, err := os.Create(fname + ".enc")
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
	receivedFirstBytes := false
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
		if !receivedFirstBytes {
			receivedFirstBytes = true
			logger.Debug("Receieved first bytes!")
		}
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

	// open encrypted file
	file, err := os.OpenFile(c.File.Name+".enc", os.O_RDONLY, 0755)
	if err != nil {
		log.Error(err)
		return
	}
	defer file.Close()

	chunkI := 0
	if !c.Debug {
		c.bars[id] = uiprogress.AddBar(chunksPerWorker).AppendCompleted().PrependElapsed()
	}

	bufferSizeInKilobytes := BUFFERSIZE / 1024
	rate := float64(c.rate) / float64(c.NumberOfConnections*bufferSizeInKilobytes)
	throttle := time.NewTicker(time.Second / time.Duration(rate))
	defer throttle.Stop()

	for range throttle.C {
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
