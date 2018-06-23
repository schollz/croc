package main

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/schollz/messagebox/keypair"
	"github.com/schollz/peerdiscovery"
	"github.com/schollz/progressbar"
	tarinator "github.com/schollz/tarinator-go"

	log "github.com/cihub/seelog"
	"github.com/pkg/errors"
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
	Yes                 bool
	Local               bool
	UseStdout           bool
	Wait                bool
	bar                 *progressbar.ProgressBar
	rate                int
	keypair             keypair.KeyPair
	encryptedPassword   string
}

type FileMetaData struct {
	Name               string
	Size               int
	Hash               string
	Path               string
	IsDir              bool
	IsEncrypted        bool
	DeleteAfterSending bool
}

const (
	crocReceiveDir   = "croc_received"
	tmpTarGzFileName = "to_send.tmp.tar.gz"
)

func NewConnection(config *AppConfig) (*Connection, error) {
	defer log.Flush()
	c := new(Connection)
	c.Debug = config.Debug
	c.DontEncrypt = config.DontEncrypt
	c.Wait = config.Wait
	c.Server = config.Server
	c.Code = config.Code
	c.NumberOfConnections = config.NumberOfConnections
	c.UseStdout = config.UseStdout
	c.Yes = config.Yes
	c.rate = config.Rate
	c.Local = config.Local
	c.keypair, _ = keypair.New()
	fmt.Fprintf(os.Stderr, "Your public key: %s\n", c.keypair.Public)

	if c.Local {
		c.Yes = true
		c.DontEncrypt = true
	}

	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		config.File = "stdin"
	}
	if len(config.File) > 0 {
		config.File = filepath.Clean(config.File)
		if config.File == "stdin" {
			f, err := ioutil.TempFile(".", "croc-stdin-")
			if err != nil {
				return c, err
			}
			_, err = io.Copy(f, os.Stdin)
			if err != nil {
				return c, err
			}
			config.File = f.Name()
			err = f.Close()
			if err != nil {
				return c, err
			}
			c.File.DeleteAfterSending = true
		}
		// check wether the file is a dir
		info, err := os.Stat(config.File)
		if err != nil {
			return c, err
		}

		if info.Mode().IsDir() { // if our file is a dir
			fmt.Println("Compressing folder...")

			// we "tarify" the file
			err = tarinator.Tarinate([]string{config.File}, path.Base(config.File)+".tar")
			if err != nil {
				return c, err
			}

			// now, we change the target file name to match the new archive created
			config.File = path.Base(config.File) + ".tar"
			// we set the value IsDir to true
			c.File.IsDir = true
		}
		c.File.Name = path.Base(config.File)
		c.File.Path = path.Dir(config.File)
		c.File.Size, _ = FileSize(config.File)
		c.IsSender = true
	} else {
		c.IsSender = false
		c.AskPath = config.PathSpec
		c.Path = config.Path
	}
	c.File.IsEncrypted = true
	if c.DontEncrypt {
		c.File.IsEncrypted = false
	}

	if c.Debug {
		SetLogLevel("debug")
	} else {
		SetLogLevel("warn")
	}

	return c, nil
}

func (c *Connection) cleanup() {
	log.Debug("cleaning")
	if c.Debug {
		return
	}
	for id := 0; id <= 8; id++ {
		err := os.Remove(path.Join(c.Path, c.File.Name+".enc."+strconv.Itoa(id)))
		if err == nil {
			log.Debugf("removed %s", path.Join(c.Path, c.File.Name+".enc."+strconv.Itoa(id)))
		}
	}
	os.Remove(path.Join(c.Path, c.File.Name+".enc"))
}

func (c *Connection) Run() error {
	// catch the Ctl+C
	catchCtlC := make(chan os.Signal, 2)
	signal.Notify(catchCtlC, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-catchCtlC
		c.cleanup()
		fmt.Println("\nExiting")
		os.Exit(1)
	}()
	defer c.cleanup()

	// calculate number of threads
	c.NumberOfConnections = MAX_NUMBER_THREADS
	if c.IsSender {
		fsize, err := FileSize(path.Join(c.File.Path, c.File.Name))
		if err != nil {
			return err
		}
		if fsize < MAX_NUMBER_THREADS*BUFFERSIZE {
			c.NumberOfConnections = 1
			log.Debug("forcing single thread")
		}
	}

	runClientError := make(chan error)
	if c.IsSender {
		if c.Code == "" {
			c.Code = GetRandomName()
		}
		if c.File.IsDir {
			fmt.Fprintf(os.Stderr, "Sending %s folder named '%s'\n", humanize.Bytes(uint64(c.File.Size)), c.File.Name[:len(c.File.Name)-4])
		} else {
			fmt.Fprintf(os.Stderr, "Sending %s file named '%s'\n", humanize.Bytes(uint64(c.File.Size)), c.File.Name)
		}

		log.Debug("starting relay in case local connections")
		relay := NewRelay(&AppConfig{
			Debug: c.Debug,
		})
		go relay.Run()
		time.Sleep(200 * time.Millisecond)

		// get file hash
		var err error
		c.File.Hash, err = HashFile(path.Join(c.File.Path, c.File.Name))
		if err != nil {
			return err
		}
		// get file size
		c.File.Size, err = FileSize(c.File.Name)
		if err != nil {
			return err
		}

		// remove compressed archive
		if c.File.IsDir {
			log.Debug("removing archive: " + c.File.Name)
			if err := os.Remove(c.File.Name); err != nil {
				return err
			}
		}

		// broadcast local connection from sender
		if c.Server == "" {
			log.Debug("settings payload to ", c.Code)
			go func() {
				go peerdiscovery.Discover(peerdiscovery.Settings{
					Limit:     1,
					TimeLimit: 600 * time.Second,
					Delay:     50 * time.Millisecond,
					Payload:   []byte(c.Code),
				})
				runClientError <- c.runClient("localhost")
			}()

		}
	}

	log.Debug("checking code validity")
	if len(c.Code) == 0 && !c.IsSender {
		log.Debug("Finding local croc relay...")
		discovered, _ := peerdiscovery.Discover(peerdiscovery.Settings{
			Limit:     1,
			TimeLimit: 1 * time.Second,
			Delay:     50 * time.Millisecond,
			Payload:   []byte(c.Code),
		})
		if len(discovered) > 0 {
			c.Server = discovered[0].Address
			log.Debug(discovered[0].Address)
			c.Code = string(discovered[0].Payload)
			log.Debugf("discovered code '%s'", c.Code)
			time.Sleep(200 * time.Millisecond)
		} else {
			c.Code = getInput("Enter receive code: ")
			log.Debug("changed code to ", c.Code)
		}
	}

	if !c.Local {
		go func() { runClientError <- c.runClient(c.Server) }()
	}
	return <-runClientError
}

// runClient spawns threads for parallel uplink/downlink via TCP
func (c *Connection) runClient(serverName string) error {

	c.HashedCode = Hash(c.Code)
	c.NumberOfConnections = MAX_NUMBER_THREADS
	var wg sync.WaitGroup
	wg.Add(c.NumberOfConnections)

	if !c.Debug {
		c.bar = progressbar.New(c.File.Size)
		c.bar.SetWriter(os.Stderr)
	}
	type responsesStruct struct {
		gotTimeout         bool
		gotOK              bool
		gotResponse        bool
		gotConnectionInUse bool
		notPresent         bool
		startTime          time.Time
		sync.RWMutex
	}

	responses := new(responsesStruct)
	responses.Lock()
	responses.startTime = time.Now()
	responses.Unlock()
	for id := 0; id < c.NumberOfConnections; id++ {
		go func(id int) {
			defer wg.Done()
			port := strconv.Itoa(27001 + id)
			connection, err := net.Dial("tcp", serverName+":"+port)
			if err != nil {
				if serverName == "cowyo.com" {
					fmt.Fprintf(os.Stderr, "\nCheck http://bit.ly/croc-relay to see if the public server is down or contact the webmaster: @yakczar")
				} else {
					fmt.Fprintf(os.Stderr, "\nCould not connect to relay %s\n", serverName)
				}
				fmt.Fprintf(os.Stderr, "Use --local to run locally")
				os.Exit(1)
			}
			defer connection.Close()
			err = connection.SetReadDeadline(time.Now().Add(1 * time.Hour))
			if err != nil {
				log.Warn(err)
			}
			err = connection.SetDeadline(time.Now().Add(1 * time.Hour))
			if err != nil {
				log.Warn(err)
			}
			err = connection.SetWriteDeadline(time.Now().Add(1 * time.Hour))
			if err != nil {
				log.Warn(err)
			}

			message := receiveMessage(connection)
			log.Debugf("relay says: %s", message)
			if c.IsSender {
				log.Debugf("telling relay (%s): %s", c.Server, "s."+c.Code)
				metaData, err := json.Marshal(c.File)
				if err != nil {
					log.Error(err)
				}
				encryptedMetaData, salt, iv := Encrypt(metaData, c.Code)
				sendMessage("s."+c.keypair.Public+"."+c.HashedCode+"."+hex.EncodeToString(encryptedMetaData)+"-"+salt+"-"+iv, connection)
			} else {
				log.Debugf("telling relay (%s): %s", c.Server, "r."+c.Code)
				if c.Wait {
					// tell server to wait for sender
					sendMessage("r."+c.keypair.Public+"."+c.HashedCode+".0.0.0", connection)
				} else {
					// tell server to cancel if sender doesn't exist
					sendMessage("c."+c.keypair.Public+"."+c.HashedCode+".0.0.0", connection)
				}
			}
			if c.IsSender { // this is a sender
				log.Debugf("[%d] waiting for ok from relay", id)
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
					// message is IP address, lets check next message
					log.Debugf("[%d] got ok from relay: %s", id, message)
					publicKeyRecipient := receiveMessage(connection)
					// check if okay again
					sendMessage("okay with sender", connection)
					if id == 0 {
						passphraseString := RandStringBytesMaskImprSrc(20)
						log.Debugf("passphrase: [%s]", passphraseString)
						encryptedPassword, err := c.keypair.Encrypt([]byte(passphraseString), publicKeyRecipient)
						if err != nil {
							panic(err)
						}

						// encrypt files
						if c.DontEncrypt {
							// don't encrypt
							CopyFile(path.Join(c.File.Path, c.File.Name), c.File.Name+".enc")
							c.File.IsEncrypted = false
						} else {
							// encrypt
							log.Debugf("encrypting file with passphrase [%s]", passphraseString)
							if err := EncryptFile(path.Join(c.File.Path, c.File.Name), c.File.Name+".enc", passphraseString); err != nil {
								panic(err)
							}
							c.File.IsEncrypted = true
						}
						// split file into pieces to send
						if err := SplitFile(c.File.Name+".enc", c.NumberOfConnections); err != nil {
							panic(err)
						}
						// remove the file now since we still have pieces
						if err := os.Remove(c.File.Name + ".enc"); err != nil {
							panic(err)
						}

						c.encryptedPassword = base64.StdEncoding.EncodeToString(encryptedPassword)
					}
					log.Debugf("[%d] waiting for 0 thread to encrypt", id)
					for {
						if c.encryptedPassword != "" {
							break
						}
						time.Sleep(10 * time.Millisecond)
					}
					log.Debugf("sending encrypted passphrase: %s", c.encryptedPassword)
					sendMessage(c.encryptedPassword, connection)
					// wait for relay go
					receiveMessage(connection)
					if id == 0 {
						fmt.Fprintf(os.Stderr, "\nSending (->%s@%s)..\n", publicKeyRecipient, message)
					}
					// wait for pipe to be made
					time.Sleep(100 * time.Millisecond)
					// Write data from file
					log.Debug("send file")
					responses.Lock()
					responses.startTime = time.Now()
					responses.Unlock()
					if !c.Debug {
						c.bar.Reset()
					}
					if err := c.sendFile(id, connection); err != nil {
						log.Error(err)
					}
				}
			} else { // this is a receiver
				log.Debug("waiting for meta data from sender")
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
					} else if strings.Split(sendersAddress, ":")[0] == "127.0.0.1" {
						sendersAddress = strings.Replace(sendersAddress, "127.0.0.1", c.Server, 1)
					}
					// now get public key
					publicKeySender := receiveMessage(connection)

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
						fmt.Fprintf(os.Stderr, "Incoming file from "+publicKeySender+"\n")
						if _, err := os.Stat(path.Join(c.Path, c.File.Name)); os.IsNotExist(err) {
							fmt.Fprintf(os.Stderr, "Receiving %s (%s) into: %s\n", fType, humanize.Bytes(uint64(c.File.Size)), fName)
						} else {
							fmt.Fprintf(os.Stderr, "Overwriting %s %s (%s)\n", fType, fName, humanize.Bytes(uint64(c.File.Size)))
						}
						var sentFileNames []string

						if c.AskPath {
							getPath := getInput("path: ")
							if len(getPath) > 0 {
								c.Path = path.Clean(getPath)
							}
						}
						if fileAlreadyExists(sentFileNames, c.File.Name) {
							fmt.Fprintf(os.Stderr, "Will not overwrite file!")
							os.Exit(1)
						}
						getOK := "y"
						if !c.Yes {
							getOK = getInput("ok? (y/n): ")
						}
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
						encryptedPassword := receiveMessage(connection)
						log.Debugf("[%d] got encrypted passphrase: %s", id, encryptedPassword)
						encryptedPasswordBytes, err := base64.StdEncoding.DecodeString(encryptedPassword)
						if err != nil {
							panic(err)
						}
						decryptedPassphrase, err := c.keypair.Decrypt(encryptedPasswordBytes, publicKeySender)
						c.encryptedPassword = string(decryptedPassphrase)
						log.Debugf("decrypted password to: %s", c.encryptedPassword)
						if err != nil {
							panic(err)
						}
						sendMessage("ok", connection)
						log.Debug("receive file")
						if id == 0 {
							fmt.Fprintf(os.Stderr, "\nReceiving (<-%s@%s)..\n", publicKeySender, sendersAddress)
						}
						responses.Lock()
						responses.startTime = time.Now()
						responses.Unlock()
						if !c.Debug && id == 0 {
							c.bar.SetMax(c.File.Size)
							c.bar.Reset()
						} else {
							// try to let the first thread start first
							time.Sleep(10 * time.Millisecond)
						}
						if err := c.receiveFile(id, connection); err != nil {
							log.Debug(errors.Wrap(err, "no file to recieve"))
						}
					}
				}
			}
		}(id)
	}
	wg.Wait()
	log.Debugf("moving on")

	responses.Lock()
	defer responses.Unlock()
	if responses.gotConnectionInUse {
		return nil // connection was in use, just quit cleanly
	}

	timeSinceStart := time.Since(responses.startTime).Nanoseconds()

	if c.IsSender {
		if responses.gotTimeout {
			fmt.Println("Timeout waiting for receiver")
			return nil
		}
		fileOrFolder := "File"
		if c.File.IsDir {
			fileOrFolder = "Folder"
		}
		fmt.Printf("\n%s sent", fileOrFolder)
	} else { // Is a Receiver
		if responses.notPresent {
			fmt.Println("Either code is incorrect or sender is not ready. Use -wait to wait until sender connects.")
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
				log.Error(err)
				return err
			}
		} else {
			log.Debugf("is encrypted: %+v", c.File.IsEncrypted)
			if c.File.IsEncrypted {
				log.Debugf("decrypting file with [%s]", c.encryptedPassword)
				if err := DecryptFile(path.Join(c.Path, c.File.Name+".enc"), path.Join(c.Path, c.File.Name), c.encryptedPassword); err != nil {
					log.Error(err)
					return errors.Wrap(err, "Problem decrypting file")
				}
			} else {
				if err := CopyFile(path.Join(c.Path, c.File.Name+".enc"), path.Join(c.Path, c.File.Name)); err != nil {
					log.Error(err)
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
			log.Flush()
			return fmt.Errorf("\nUh oh! %s is corrupted! Sorry, try again.\n", c.File.Name)
		}
		if c.File.IsDir { // if the file was originally a dir
			fmt.Print("\nDecompressing folder...")
			log.Debug("untarring " + c.File.Name)
			err := tarinator.UnTarinate(c.Path, path.Join(c.Path, c.File.Name))
			if err != nil {
				log.Debug("problem untarring: " + err.Error())
				return err
			}
			// we remove the old tar.gz filels
			log.Debug("removing old tar file: " + c.File.Name)
			err = os.Remove(path.Join(c.Path, c.File.Name))
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "\nReceived folder written to %s", path.Join(c.Path, c.File.Name[:len(c.File.Name)-4]))
		} else {
			outputStream := path.Join(c.Path, c.File.Name)
			if c.UseStdout {
				outputStream = "stdout"
			}
			fmt.Fprintf(os.Stderr, "\nReceived file written to %s", outputStream)
			if c.UseStdout {
				defer os.Remove(path.Join(c.Path, c.File.Name))
				b, _ := ioutil.ReadFile(path.Join(c.Path, c.File.Name))
				fmt.Printf("%s", b)
			}
		}
	}
	fmt.Fprintf(os.Stderr, " (%s/s)\n", humanize.Bytes(uint64(float64(1000000000)*float64(c.File.Size)/float64(timeSinceStart))))
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
	i := 0
	for id := 0; id < len(files); id++ {
		files[i] = path.Join(c.Path, c.File.Name+".enc."+strconv.Itoa(id))
		if _, err := os.Stat(files[id]); os.IsNotExist(err) {
			break
		}
		log.Debug(files[i])
		i++
	}
	files = files[:i]
	log.Debug(files)
	toRemove := !c.Debug
	return CatFiles(files, path.Join(c.Path, c.File.Name+".enc"), toRemove)
}

func (c *Connection) receiveFile(id int, connection net.Conn) error {
	log.Debug("waiting for chunk size from sender")
	fileSizeBuffer := make([]byte, 10)
	connection.Read(fileSizeBuffer)
	fileDataString := strings.Trim(string(fileSizeBuffer), ":")
	fileSizeInt, _ := strconv.Atoi(fileDataString)
	chunkSize := int64(fileSizeInt)
	log.Debugf("chunk size: %d", chunkSize)
	if chunkSize == 0 {
		log.Debug(fileSizeBuffer)
		return errors.New("chunk size is empty!")
	}

	os.Remove(path.Join(c.Path, c.File.Name+".enc."+strconv.Itoa(id)))
	log.Debug("Making " + c.File.Name + ".enc." + strconv.Itoa(id))
	newFile, err := os.Create(path.Join(c.Path, c.File.Name+".enc."+strconv.Itoa(id)))
	if err != nil {
		panic(err)
	}
	defer newFile.Close()

	log.Debug(id, "waiting for file")
	var receivedBytes int64
	receivedFirstBytes := false
	for {
		if (chunkSize - receivedBytes) < BUFFERSIZE {
			log.Debugf("%d at the end: %d < %d", id, (chunkSize - receivedBytes), BUFFERSIZE)
			io.CopyN(newFile, connection, (chunkSize - receivedBytes))
			// Empty the remaining bytes that we don't need from the network buffer
			if (receivedBytes+BUFFERSIZE)-chunkSize < BUFFERSIZE {
				log.Debug(id, "empty remaining bytes from network buffer")
				connection.Read(make([]byte, (receivedBytes+BUFFERSIZE)-chunkSize))
			}
			if !c.Debug {
				c.bar.Add(int((chunkSize - receivedBytes)))
			}
			break
		}
		written, _ := io.CopyN(newFile, connection, BUFFERSIZE)
		receivedBytes += written
		if !receivedFirstBytes {
			receivedFirstBytes = true
			log.Debug(id, "Receieved first bytes!")
		}
		if !c.Debug {
			c.bar.Add(int(written))
		}
	}
	log.Debug(id, "received file")
	return nil
}

func (c *Connection) sendFile(id int, connection net.Conn) error {
	defer connection.Close()

	// open encrypted file chunk, if it exists
	log.Debug("opening encrypted file chunk: " + c.File.Name + ".enc." + strconv.Itoa(id))
	file, err := os.Open(c.File.Name + ".enc." + strconv.Itoa(id))
	if err != nil {
		log.Debug(err)
		return nil
	}
	defer file.Close()

	// determine and send the file size to client
	fi, err := file.Stat()
	if err != nil {
		return err
	}
	log.Debugf("sending chunk size: %d", fi.Size())
	_, err = connection.Write([]byte(fillString(strconv.FormatInt(int64(fi.Size()), 10), 10)))
	if err != nil {
		return errors.Wrap(err, "Problem sending chunk data: ")
	}

	// rate limit the bandwidth
	log.Debug("determining rate limiting")
	bufferSizeInKilobytes := BUFFERSIZE / 1024
	rate := float64(c.rate) / float64(c.NumberOfConnections*bufferSizeInKilobytes)
	throttle := time.NewTicker(time.Second / time.Duration(rate))
	log.Debugf("rate: %+v", rate)
	defer throttle.Stop()

	// send the file
	sendBuffer := make([]byte, BUFFERSIZE)
	totalBytesSent := 0
	for range throttle.C {
		_, err := file.Read(sendBuffer)
		written, errWrite := connection.Write(sendBuffer)
		totalBytesSent += written
		if !c.Debug {
			c.bar.Add(int(written))
		}
		if errWrite != nil {
			log.Error(errWrite)
		}
		if err == io.EOF {
			//End of file reached, break out of for loop
			log.Debug("EOF")
			break
		}
	}
	log.Debug("file is sent")
	log.Debug("removing piece")
	if !c.Debug {
		file.Close()
		err = os.Remove(c.File.Name + ".enc." + strconv.Itoa(id))
	}
	if err != nil && c.File.DeleteAfterSending {
		err = os.Remove(path.Join(c.File.Path, c.File.Name))
	}

	// wait until client breaks connection
	for range throttle.C {
		_, errWrite := connection.Write([]byte("."))
		if errWrite != nil {
			break
		}
	}
	return err
}
