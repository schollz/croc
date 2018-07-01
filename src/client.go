package croc

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"time"

	log "github.com/cihub/seelog"
	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
	"github.com/schollz/pake"
	"github.com/schollz/progressbar"
)

var isPrinted bool

func (c *Croc) client(role int, channel string) (err error) {
	defer log.Flush()
	defer c.cleanup()
	// initialize the channel data for this client

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	// connect to the websocket
	u := url.URL{Scheme: strings.Split(c.WebsocketAddress, "://")[0], Host: strings.Split(c.WebsocketAddress, "://")[1], Path: "/"}
	log.Debugf("connecting to %s", u.String())
	ws, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		// don't return error if sender can't connect, so
		// that croc can be used locally without
		// an internet connection
		if role == 0 {
			log.Debugf("dial %s error: %s", c.WebsocketAddress, err.Error())
			err = nil
		} else {
			log.Error("dial:", err)
		}
		return
	}
	defer ws.Close()
	// add websocket to locked channel
	c.cs.Lock()
	c.cs.channel.ws = ws
	c.cs.Unlock()

	// read in the messages and process them
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			var cd channelData
			err := ws.ReadJSON(&cd)
			if err != nil {
				log.Debugf("sender read error:", err)
				return
			}
			log.Debugf("recv: %s", cd.String2())
			err = c.processState(cd)
			if err != nil {
				log.Warn(err)
				return
			}
		}
	}()

	// initialize by joining as corresponding role
	// TODO:
	// allowing suggesting a channel
	p := channelData{
		Open:    true,
		Role:    role,
		Channel: channel,
	}
	log.Debugf("sending opening payload: %+v", p)
	c.cs.Lock()
	err = c.cs.channel.ws.WriteJSON(p)
	if err != nil {
		log.Errorf("problem opening: %s", err.Error())
		c.cs.Unlock()
		return
	}
	c.cs.Unlock()

	var wg sync.WaitGroup
	wg.Add(1)
	go func(wg *sync.WaitGroup) {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			case <-interrupt:
				// send Close signal to relay on interrupt
				log.Debugf("interrupt")
				c.cs.Lock()
				channel := c.cs.channel.Channel
				uuid := c.cs.channel.UUID
				// Cleanly close the connection by sending a close message and then
				// waiting (with timeout) for the server to close the connection.
				log.Debug("sending close signal")
				errWrite := ws.WriteJSON(channelData{
					Channel: channel,
					UUID:    uuid,
					Close:   true,
				})
				c.cs.Unlock()
				if errWrite != nil {
					log.Debugf("write close:", err)
					return
				}
				select {
				case <-done:
				case <-time.After(time.Second):
				}
				return
			}
		}
	}(&wg)
	wg.Wait()

	log.Debug("waiting for unlock")
	c.cs.Lock()
	if c.cs.channel.finishedHappy {
		log.Info("file recieved!")
		if c.cs.channel.Role == 0 {
			fmt.Fprintf(os.Stderr, "\nTransfer complete.\n")
		} else {
			folderOrFile := "file"
			if c.cs.channel.fileMetaData.IsDir {
				folderOrFile = "folder"
			}
			// push to stdout if required
			if c.Stdout && !c.cs.channel.fileMetaData.IsDir {
				fmt.Fprintf(os.Stderr, "\nReceived %s written to %s", folderOrFile, "stdout")
				var bFile []byte
				bFile, err = ioutil.ReadFile(c.cs.channel.fileMetaData.Name)
				if err != nil {
					return
				}
				os.Stdout.Write(bFile)
				os.Remove(c.cs.channel.fileMetaData.Name)
			} else {

				fmt.Fprintf(os.Stderr, "\nReceived %s written to %s", folderOrFile, c.cs.channel.fileMetaData.Name)
			}
		}
	} else {
		if c.cs.channel.Error != "" {
			err = errors.New(c.cs.channel.Error)
		} else {
			err = errors.New("one party canceled, file not transfered")
		}
	}
	c.cs.Unlock()
	log.Debug("returning")
	return
}

func (c *Croc) processState(cd channelData) (err error) {
	c.cs.Lock()
	defer c.cs.Unlock()

	// first check if there is relay reported error
	if cd.Error != "" {
		err = errors.New(cd.Error)
		return
	}
	// TODO:
	// check if the state is not aligned (i.e. have h(k) but no hh(k))
	// throw error if not aligned so it can exit

	// if file received, then you are all done
	if cd.FileReceived {
		c.cs.channel.FileReceived = true
		c.cs.channel.finishedHappy = true
		log.Debug("file recieved!")
		log.Debug("sending close signal")
		c.cs.channel.Close = true
		c.cs.channel.ws.WriteJSON(c.cs.channel)
		return
	}

	// otherwise, if ready to read, then set and return
	if cd.ReadyToRead {
		c.cs.channel.ReadyToRead = true
		return
	}

	// otherwise, if transfer ready then send file
	if cd.TransferReady {
		c.cs.channel.TransferReady = true
		return
	}

	// first update the channel data
	// initialize if has UUID
	if cd.UUID != "" {
		c.cs.channel.UUID = cd.UUID
		c.cs.channel.Channel = cd.Channel
		c.cs.channel.Role = cd.Role
		c.cs.channel.Curve = cd.Curve
		c.cs.channel.Pake, err = pake.Init([]byte(c.cs.channel.passPhrase), cd.Role, getCurve(cd.Curve))
		c.cs.channel.Update = true
		log.Debugf("updating channel")
		errWrite := c.cs.channel.ws.WriteJSON(c.cs.channel)
		if errWrite != nil {
			log.Error(errWrite)
		}
		c.cs.channel.Update = false
		log.Debugf("initialized client state")
		return
	}
	// copy over the rest of the state
	c.cs.channel.Ports = cd.Ports
	c.cs.channel.EncryptedFileMetaData = cd.EncryptedFileMetaData
	c.cs.channel.Addresses = cd.Addresses
	c.bothConnected = cd.Addresses[0] != "" && cd.Addresses[1] != ""

	// update the Pake
	if cd.Pake != nil && cd.Pake.Role != c.cs.channel.Role {
		if c.cs.channel.Pake.HkA == nil {
			log.Debugf("updating pake from %d", cd.Pake.Role)
			err = c.cs.channel.Pake.Update(cd.Pake.Bytes())
			if err != nil {
				log.Error(err)
				log.Debug("sending close signal")
				c.cs.channel.Close = true
				c.cs.channel.Error = err.Error()
				c.cs.channel.ws.WriteJSON(c.cs.channel)
				return
			}
			c.cs.channel.Update = true
			log.Debugf("updating channel")
			errWrite := c.cs.channel.ws.WriteJSON(c.cs.channel)
			if errWrite != nil {
				log.Error(errWrite)
			}
			c.cs.channel.Update = false
		}
	}
	if c.cs.channel.Role == 0 && c.cs.channel.Pake.IsVerified() && !c.cs.channel.notSentMetaData && !c.cs.channel.filesReady {
		go c.getFilesReady()
		c.cs.channel.filesReady = true
	}

	// process the client state
	if c.cs.channel.Pake.IsVerified() && !c.cs.channel.isReady && c.cs.channel.EncryptedFileMetaData.Encrypted != nil {

		// decrypt the meta data
		log.Debugf("encrypted meta data: %+v", c.cs.channel.EncryptedFileMetaData)
		var passphrase, metaDataBytes []byte
		passphrase, err = c.cs.channel.Pake.SessionKey()
		if err != nil {
			log.Error(err)
			return
		}
		metaDataBytes, err = c.cs.channel.EncryptedFileMetaData.decrypt(passphrase)
		if err != nil {
			log.Error(err)
			return
		}
		err = json.Unmarshal(metaDataBytes, &c.cs.channel.fileMetaData)
		if err != nil {
			log.Error(err)
			return
		}
		log.Debugf("meta data: %+v", c.cs.channel.fileMetaData)

		// check if the user still wants to receive the file
		if c.cs.channel.Role == 1 {
			if !c.Yes {
				if !promptOkayToRecieve(c.cs.channel.fileMetaData) {
					log.Debug("sending close signal")
					c.cs.channel.Close = true
					c.cs.channel.Error = "refusing file"
					c.cs.channel.ws.WriteJSON(c.cs.channel)
				}
			}
		}

		// spawn TCP connections
		c.cs.channel.isReady = true
		go c.spawnConnections(c.cs.channel.Role)
	}
	return
}

func (c *Croc) spawnConnections(role int) (err error) {
	err = c.dialUp()
	if err == nil {
		if role == 1 {
			err = c.processReceivedFile()
		}
	} else {
		log.Error(err)
	}
	return
}

func (c *Croc) dialUp() (err error) {
	c.cs.Lock()
	ports := c.cs.channel.Ports
	channel := c.cs.channel.Channel
	uuid := c.cs.channel.UUID
	role := c.cs.channel.Role
	c.cs.Unlock()
	errorChan := make(chan error, len(ports))

	if role == 1 {
		// generate a receive filename
		c.crocFileEncrypted = tempFileName("croc-received")
	}

	for i, port := range ports {
		go func(channel, uuid, port string, i int, errorChan chan error) {
			if i == 0 {
				log.Debug("dialing up")
			}
			log.Debugf("connecting to %s", "localhost:"+port)
			address := strings.Split(strings.Split(c.WebsocketAddress, "://")[1], ":")[0]
			connection, err := net.Dial("tcp", address+":"+port)
			if err != nil {
				errorChan <- err
				return
			}
			defer connection.Close()
			connection.SetReadDeadline(time.Now().Add(1 * time.Hour))
			connection.SetDeadline(time.Now().Add(1 * time.Hour))
			connection.SetWriteDeadline(time.Now().Add(1 * time.Hour))
			message, err := receiveMessage(connection)
			if err != nil {
				errorChan <- err
				return
			}
			log.Debugf("relay says: %s", message)
			err = sendMessage(channel, connection)
			if err != nil {
				errorChan <- err
				return
			}
			err = sendMessage(uuid, connection)
			if err != nil {
				errorChan <- err
				return
			}

			// wait for transfer to be ready
			for {
				c.cs.RLock()
				ready := c.cs.channel.TransferReady
				if role == 0 {
					ready = ready && c.cs.channel.fileReady
				}
				c.cs.RUnlock()
				if ready {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if i == 0 {
				c.cs.Lock()
				c.bar = progressbar.NewOptions(c.cs.channel.fileMetaData.Size, progressbar.OptionSetWriter(os.Stderr))
				if role == 0 {
					fmt.Fprintf(os.Stderr, "\nSending (->%s)...\n", c.cs.channel.Addresses[1])
				} else {
					fmt.Fprintf(os.Stderr, "\nReceiving (<-%s)...\n", c.cs.channel.Addresses[0])
				}
				c.cs.Unlock()
			}

			if role == 0 {
				log.Debug("send file")
				for {
					c.cs.RLock()
					ready := c.cs.channel.ReadyToRead
					c.cs.RUnlock()
					if ready {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				log.Debug("sending file")
				filename := c.crocFileEncrypted + "." + strconv.Itoa(i)
				err = c.sendFile(filename, i, connection)
			} else {
				go func() {
					time.Sleep(10 * time.Millisecond)
					c.cs.Lock()
					log.Debugf("updating channel with ready to read")
					c.cs.channel.Update = true
					c.cs.channel.ReadyToRead = true
					errWrite := c.cs.channel.ws.WriteJSON(c.cs.channel)
					if errWrite != nil {
						log.Error(errWrite)
					}
					c.cs.channel.Update = false
					c.cs.Unlock()
					log.Debug("receive file")
				}()
				receiveFileName := c.crocFileEncrypted + "." + strconv.Itoa(i)
				log.Debugf("receiving file into %s", receiveFileName)
				err = c.receiveFile(receiveFileName, i, connection)
			}
			c.bar.Finish()
			errorChan <- err
		}(channel, uuid, port, i, errorChan)
	}

	// collect errors
	for i := 0; i < len(ports); i++ {
		errOne := <-errorChan
		if errOne != nil {
			log.Warn(errOne)
			log.Debug("sending close signal")
			c.cs.channel.Close = true
			c.cs.channel.ws.WriteJSON(c.cs.channel)
		}
	}
	log.Debug("leaving dialup")
	return
}

func (c *Croc) receiveFile(filename string, id int, connection net.Conn) error {
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

	os.Remove(filename)
	log.Debug("making " + filename)
	newFile, err := os.Create(filename)
	if err != nil {
		panic(err)
	}
	defer newFile.Close()

	log.Debug(id, "waiting for file")
	var receivedBytes int64
	receivedFirstBytes := false
	for {
		if (chunkSize - receivedBytes) < bufferSize {
			log.Debugf("%d at the end: %d < %d", id, (chunkSize - receivedBytes), bufferSize)
			io.CopyN(newFile, connection, (chunkSize - receivedBytes))
			// Empty the remaining bytes that we don't need from the network buffer
			if (receivedBytes+bufferSize)-chunkSize < bufferSize {
				log.Debug(id, "empty remaining bytes from network buffer")
				connection.Read(make([]byte, (receivedBytes+bufferSize)-chunkSize))
			}
			break
		}
		written, _ := io.CopyN(newFile, connection, bufferSize)
		receivedBytes += written
		c.bar.Add(int(written))

		if !receivedFirstBytes {
			receivedFirstBytes = true
			log.Debug(id, "Received first bytes!")
		}
	}
	log.Debug(id, "received file")
	return nil
}

func (c *Croc) sendFile(filename string, id int, connection net.Conn) error {

	// open encrypted file chunk, if it exists
	log.Debug("opening encrypted file chunk: " + filename)
	file, err := os.Open(filename)
	if err != nil {
		log.Error(err)
		return nil
	}
	defer file.Close()

	// determine and send the file size to client
	fi, err := file.Stat()
	if err != nil {
		log.Error(err)
		return err
	}
	log.Debugf("sending chunk size: %d", fi.Size())
	log.Debug(connection.RemoteAddr())
	_, err = connection.Write([]byte(fillString(strconv.FormatInt(int64(fi.Size()), 10), 10)))
	if err != nil {
		return errors.Wrap(err, "Problem sending chunk data: ")
	}

	// rate limit the bandwidth
	log.Debug("determining rate limiting")
	rate := 10000
	throttle := time.NewTicker(time.Second / time.Duration(rate))
	log.Debugf("rate: %+v", rate)
	defer throttle.Stop()

	// send the file
	sendBuffer := make([]byte, bufferSize)
	totalBytesSent := 0
	for range throttle.C {
		_, err := file.Read(sendBuffer)
		written, _ := connection.Write(sendBuffer)
		totalBytesSent += written
		c.bar.Add(written)
		// if errWrite != nil {
		// 	errWrite = errors.Wrap(errWrite, "problem writing to connection")
		// 	return errWrite
		// }
		if err == io.EOF {
			//End of file reached, break out of for loop
			log.Debug("EOF")
			err = nil // not really an error
			break
		}
	}
	log.Debug("file is sent")
	log.Debug("removing piece")
	os.Remove(filename)

	return err
}
