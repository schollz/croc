package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	humanize "github.com/dustin/go-humanize"
	"github.com/verybluebot/tarinator-go"

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
	Path                string
	IsSender            bool
	AskPath             bool
	Debug               bool
	DontEncrypt         bool
	Wait                bool
	bars                []*uiprogress.Bar
	rate                int
}

type FileMetaData struct {
	Name        string
	Size        int
	Hash        string
	Path        string
	IsDir       bool
	IsEncrypted bool
}

const (
	crocReceiveDir   = "croc_received"
	tmpTarGzFileName = "to_send.tmp.tar.gz"
)

func NewConnection(flags *Flags) (*Connection, error) {
	c := new(Connection)
	c.Debug = flags.Debug
	c.DontEncrypt = flags.DontEncrypt
	c.Wait = flags.Wait
	c.Server = flags.Server
	c.Code = flags.Code
	c.NumberOfConnections = flags.NumberOfConnections
	c.rate = flags.Rate
	if len(flags.File) > 0 {
		// check wether the file is a dir
		info, err := os.Stat(flags.File)
		if err != nil {
			return c, err
		}

		if info.Mode().IsDir() { // if our file is a dir
			fmt.Println("compressing directory...")

			// we "tarify" the file
			err = tarinator.Tarinate([]string{flags.File}, path.Base(flags.File)+".tar")
			if err != nil {
				return c, err
			}

			// now, we change the target file name to match the new archive created
			flags.File = path.Base(flags.File) + ".tar"
			// we set the value IsDir to true
			c.File.IsDir = true
		}
		c.File.Name = path.Base(flags.File)
		c.File.Path = path.Dir(flags.File)
		c.IsSender = true
	} else {
		c.IsSender = false
		c.AskPath = flags.PathSpec
		c.Path = flags.Path
	}

	log.SetFormatter(&log.TextFormatter{})
	if c.Debug {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.WarnLevel)
	}

	return c, nil
}

func (c *Connection) Run() error {
	forceSingleThreaded := false
	if c.IsSender {
		fsize, err := FileSize(path.Join(c.File.Path, c.File.Name))
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
			CopyFile(path.Join(c.File.Path, c.File.Name), c.File.Name+".enc")
			c.File.IsEncrypted = false
		} else {
			// encrypt
			log.Debug("encrypting...")
			if err := EncryptFile(path.Join(c.File.Path, c.File.Name), c.File.Name+".enc", c.Code); err != nil {
				return err
			}
			c.File.IsEncrypted = true
		}
		// split file into pieces to send
		if err := SplitFile(c.File.Name+".enc", c.NumberOfConnections); err != nil {
			return err
		}

		// get file hash
		var err error
		c.File.Hash, err = HashFile(path.Join(c.File.Path, c.File.Name))
		if err != nil {
			return err
		}
		// get file size
		c.File.Size, err = FileSize(c.File.Name + ".enc")
		if err != nil {
			return err
		}
		// remove the file now since we still have pieces
		if err := os.Remove(c.File.Name + ".enc"); err != nil {
			return err
		}

		// remove compressed archive
		if c.File.IsDir {
			log.Debug("removing archive: " + c.File.Name)
			if err := os.Remove(c.File.Name); err != nil {
				return err
			}
		}

		if c.File.IsDir {
			fmt.Printf("Sending %s folder named '%s'\n", humanize.Bytes(uint64(c.File.Size)), c.File.Name[:len(c.File.Name)-4])
		} else {
			fmt.Printf("Sending %s file named '%s'\n", humanize.Bytes(uint64(c.File.Size)), c.File.Name)

		}
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

	startTime:= time.Now()
	c.HashedCode = Hash(c.Code)

	var wg sync.WaitGroup
	wg.Add(c.NumberOfConnections)

	uiprogress.Start()
	if !c.Debug {
		c.bars = make([]*uiprogress.Bar, c.NumberOfConnections)
	}
	type responsesStruct struct {
		gotTimeout         bool
		gotOK              bool
		gotResponse        bool
		gotConnectionInUse bool
		notPresent         bool
		sync.RWMutex
	}

	responses := new(responsesStruct)
	for id := 0; id < c.NumberOfConnections; id++ {
		go func(id int) {
			defer wg.Done()
			port := strconv.Itoa(27001 + id)
			connection, err := net.Dial("tcp", c.Server+":"+port)
			if err != nil {
				if c.Server == "cowyo.com" {
					fmt.Println("\nCheck http://bit.ly/croc-relay to see if the public server is down or contact the webmaster: @yakczar")
				} else {
					fmt.Printf("\nCould not connect to relay %s\n", c.Server)
				}
				os.Exit(1)
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
					// tell server to wait for sender
					sendMessage("r."+c.HashedCode+".0.0.0", connection)
				} else {
					// tell server to cancel if sender doesn't exist
					sendMessage("c."+c.HashedCode+".0.0.0", connection)
				}
			}
			if c.IsSender { // this is a sender
				logger.Debug("waiting for ok from relay")
				message = receiveMessage(connection)
				if message == "timeout" {
					responses.Lock()
					responses.gotTimeout = true
					responses.Unlock()
					fmt.Println("You've just exceeded limit waiting time.")
					return
				}
				if message == "no" {
					if id == 0 {
						fmt.Println("The specifed code is already in use by a sender.")
					}
					responses.Lock()
					responses.gotConnectionInUse = true
					responses.Unlock()
				} else {
					logger.Debug("got ok from relay")
					if id == 0 {
						fmt.Printf("\nSending (->%s)..\n", message)
					}
					// wait for pipe to be made
					time.Sleep(100 * time.Millisecond)
					// Write data from file
					logger.Debug("send file")
					if err := c.sendFile(id, connection); err != nil {
						log.Error(err)
					}
				}
			} else { // this is a receiver
				logger.Debug("waiting for meta data from sender")
				message = receiveMessage(connection)
				if message == "no" {
					if id == 0 {
						fmt.Println("The specifed code is already in use by a sender.")
					}
					responses.Lock()
					responses.gotConnectionInUse = true
					responses.Unlock()
				} else {
					m := strings.Split(message, "-")
					encryptedData, salt, iv, sendersAddress := m[0], m[1], m[2], m[3]
					if sendersAddress == "0.0.0.0" {
						responses.Lock()
						responses.notPresent = true
						responses.Unlock()
						time.Sleep(1 * time.Second)
						return
					}
					// have the main thread ask for the okay
					if id == 0 {
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
						fType := "file"
						fName := path.Join(c.Path, c.File.Name)
						if c.File.IsDir {
							fType = "folder"
							fName = fName[:len(fName)-4]
						}
						if _, err := os.Stat(path.Join(c.Path, c.File.Name)); os.IsNotExist(err) {
							fmt.Printf("Receiving %s (%s) into: %s\n", fType, humanize.Bytes(uint64(c.File.Size)), fName)
						} else {
							fmt.Printf("Overwriting %s %s (%s)\n", fType, fName, humanize.Bytes(uint64(c.File.Size)))
						}
						var sentFileNames []string

						if c.AskPath {
							getPath := getInput("path: ")
							if len(getPath) > 0 {
								c.Path = path.Clean(getPath)
							}
						}
						if fileAlreadyExists(sentFileNames, c.File.Name) {
							fmt.Printf("Will not overwrite file!")
							os.Exit(1)
						}
						getOK := getInput("ok? (y/n): ")
						if getOK == "y" {
							responses.Lock()
							responses.gotOK = true
							responses.Unlock()
							sentFileNames = append(sentFileNames, c.File.Name)
						}
						responses.Lock()
						responses.gotResponse = true
						responses.Unlock()
					}
					// wait for the main thread to get the okay
					for limit := 0; limit < 1000; limit++ {
						responses.Lock()
						gotResponse := responses.gotResponse
						responses.Unlock()
						if gotResponse {
							break
						}
						time.Sleep(10 * time.Millisecond)
					}
					responses.RLock()
					gotOK := responses.gotOK
					responses.RUnlock()
					if !gotOK {
						sendMessage("not ok", connection)
					} else {
						sendMessage("ok", connection)
						logger.Debug("receive file")
						if id == 0 {
							fmt.Printf("\n\nReceiving (<-%s)..\n", sendersAddress)
						}
						if err := c.receiveFile(id, connection); err != nil {
							log.Error(errors.Wrap(err, "Problem receiving the file: "))
						}
					}
				}
			}
		}(id)
	}
	wg.Wait()

	responses.Lock()
	defer responses.Unlock()
	if responses.gotConnectionInUse {
		return nil // connection was in use, just quit cleanly
	}

	if c.IsSender {
		if responses.gotTimeout {
			fmt.Println("Timeout waiting for receiver")
			return nil
		}
		fmt.Println("\nFile sent.")
	} else { // Is a Receiver
		if responses.notPresent {
			fmt.Println("Sender is not ready. Use -wait to wait until sender connects.")
			return nil
		}
		if !responses.gotOK {
			return errors.New("Transfer interrupted")
		}
		if err := c.catFile(); err != nil {
			return err
		}
		log.Debugf("Code: [%s]", c.Code)
		if c.DontEncrypt {
			if err := CopyFile(path.Join(c.Path, c.File.Name+".enc"), path.Join(c.Path, c.File.Name)); err != nil {
				return err
			}
		} else {
			if c.File.IsEncrypted {
				if err := DecryptFile(path.Join(c.Path, c.File.Name+".enc"), path.Join(c.Path, c.File.Name), c.Code); err != nil {
					return errors.Wrap(err, "Problem decrypting file")
				}
			} else {
				if err := CopyFile(path.Join(c.Path, c.File.Name+".enc"), path.Join(c.Path, c.File.Name)); err != nil {
					return errors.Wrap(err, "Problem copying file")
				}
			}
		}
		if !c.Debug {
			os.Remove(path.Join(c.Path, c.File.Name+".enc"))
		}

		fileHash, err := HashFile(path.Join(c.Path, c.File.Name))
		if err != nil {
			log.Error(err)
		}
		log.Debugf("\n\n\ndownloaded hash: [%s]", fileHash)
		log.Debugf("\n\n\nrelayed hash: [%s]", c.File.Hash)

		if c.File.Hash != fileHash {
			return fmt.Errorf("\nUh oh! %s is corrupted! Sorry, try again.\n", c.File.Name)
		}
		if c.File.IsDir { // if the file was originally a dir
			fmt.Print("decompressing folder")
			log.Debug("untarring " + c.File.Name)
			err := tarinator.UnTarinate(c.Path, path.Join(c.Path, c.File.Name))

			if err != nil {
				return err
			}
			// we remove the old tar.gz file
			err = os.Remove(path.Join(c.Path, c.File.Name))
			if err != nil {
				return err
			}
			fmt.Printf("\nReceived folder written to %s\n", path.Join(c.Path, c.File.Name[:len(c.File.Name)-4]))
		} else {
			fmt.Printf("\nReceived file written to %s\n", path.Join(c.Path, c.File.Name))
		}
	}
	timeSinceStart:= time.Since(startTime) / time.Second
	fmt.Printf("\nTransfered at an average speed of %s/s", humanize.Bytes(uint64(float64(c.File.Size)/float64(timeSinceStart))))
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

func (c *Connection) catFile() error {
	// cat the file
	files := make([]string, c.NumberOfConnections)
	for id := range files {
		files[id] = path.Join(c.Path, c.File.Name+".enc."+strconv.Itoa(id))
	}
	toRemove := !c.Debug
	return CatFiles(files, path.Join(c.Path, c.File.Name+".enc"), toRemove)
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
	if chunkSize == 0 {
		logger.Debug(fileSizeBuffer)
		return errors.New("chunk size is empty!")
	}

	os.Remove(path.Join(c.Path, c.File.Name+".enc."+strconv.Itoa(id)))
	log.Debug("Making " + c.File.Name + ".enc." + strconv.Itoa(id))
	newFile, err := os.Create(path.Join(c.Path, c.File.Name+".enc."+strconv.Itoa(id)))
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

func (c *Connection) sendFile(id int, connection net.Conn) error {
	logger := log.WithFields(log.Fields{
		"function": "sendFile #" + strconv.Itoa(id),
	})
	defer connection.Close()

	// open encrypted file chunk
	logger.Debug("opening encrypted file chunk: " + c.File.Name + ".enc." + strconv.Itoa(id))
	file, err := os.Open(c.File.Name + ".enc." + strconv.Itoa(id))
	if err != nil {
		return err
	}
	defer file.Close()

	// determine and send the file size to client
	fi, err := file.Stat()
	if err != nil {
		return err
	}
	logger.Debugf("sending chunk size: %d", fi.Size())
	_, err = connection.Write([]byte(fillString(strconv.FormatInt(int64(fi.Size()), 10), 10)))
	if err != nil {
		return errors.Wrap(err, "Problem sending chunk data: ")
	}

	// show the progress
	if !c.Debug {
		logger.Debug("going to show progress")
		c.bars[id] = uiprogress.AddBar(int(fi.Size())).AppendCompleted().PrependElapsed()
	}

	// rate limit the bandwidth
	logger.Debug("determining rate limiting")
	bufferSizeInKilobytes := BUFFERSIZE / 1024
	rate := float64(c.rate) / float64(c.NumberOfConnections*bufferSizeInKilobytes)
	throttle := time.NewTicker(time.Second / time.Duration(rate))
	defer throttle.Stop()

	// send the file
	sendBuffer := make([]byte, BUFFERSIZE)
	totalBytesSent := 0
	for range throttle.C {
		n, err := file.Read(sendBuffer)
		connection.Write(sendBuffer)
		totalBytesSent += n
		if !c.Debug {
			c.bars[id].Set(totalBytesSent)
		}
		if err == io.EOF {
			//End of file reached, break out of for loop
			logger.Debug("EOF")
			break
		}
	}
	logger.Debug("file is sent")
	logger.Debug("removing piece")
	if !c.Debug {
		file.Close()
		err = os.Remove(c.File.Name + ".enc." + strconv.Itoa(id))
	}
	return err
}
