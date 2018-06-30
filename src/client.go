package croc

import (
	"encoding/json"
	"io"
	"io/ioutil"
	"net"
	"net/url"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	log "github.com/cihub/seelog"
	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
	"github.com/schollz/croc/src/pake"
	tarinator "github.com/schollz/tarinator-go"
)

func (c *Croc) client(role int, codePhrase string, fname ...string) (err error) {
	defer log.Flush()

	if role == 0 {
		if len(fname) == 0 {
			err = errors.New("must include filename")
			return
		}
		err = c.processFile(fname[0])
		if err != nil {
			return
		}
	}

	// initialize the channel data for this client
	c.cs.Lock()

	c.cs.channel.codePhrase = codePhrase
	if len(codePhrase) > 0 {
		if len(codePhrase) < 4 {
			err = errors.New("code phrase must be more than 4 characters")
			return
		}
		c.cs.channel.Channel = codePhrase[:3]
		c.cs.channel.passPhrase = codePhrase[3:]
	} else {
		// TODO
		// generate code phrase
		c.cs.channel.Channel = "chou"
		c.cs.channel.passPhrase = codePhrase[3:]
	}
	channel := c.cs.channel.Channel
	c.cs.Unlock()

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	// connect to the websocket
	// TODO:
	// use predefined host and HTTPS, if exists
	u := url.URL{Scheme: "ws", Host: "localhost:8003", Path: "/"}
	log.Debugf("connecting to %s", u.String())
	ws, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Error("dial:", err)
		return
	}
	defer ws.Close()

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
			//log.Debugf("recv: %s", cd.String2())
			err = c.processState(ws, cd)
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
	err = ws.WriteJSON(p)
	if err != nil {
		log.Errorf("problem opening: %s", err.Error())
		return
	}

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
				c.cs.Unlock()
				// Cleanly close the connection by sending a close message and then
				// waiting (with timeout) for the server to close the connection.
				log.Debug("sending close signal")
				errWrite := ws.WriteJSON(channelData{
					Channel: channel,
					UUID:    uuid,
					Close:   true,
				})
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

	c.cs.Lock()
	if c.cs.channel.finishedHappy {
		log.Info("file recieved!")
	}
	c.cs.Unlock()
	return
}

func (c *Croc) processState(ws *websocket.Conn, cd channelData) (err error) {
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
		ws.WriteJSON(c.cs.channel)
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
		errWrite := ws.WriteJSON(c.cs.channel)
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
				ws.WriteJSON(c.cs.channel)
				return
			}
			c.cs.channel.Update = true
			log.Debugf("updating channel")
			errWrite := ws.WriteJSON(c.cs.channel)
			if errWrite != nil {
				log.Error(errWrite)
			}
			c.cs.channel.Update = false
		}
	}
	if c.cs.channel.Role == 0 && c.cs.channel.Pake.IsVerified() && !c.cs.channel.notSentMetaData {
		go c.getFilesReady(ws)
	}

	// process the client state
	if c.cs.channel.Pake.IsVerified() && !c.cs.channel.isReady && c.cs.channel.EncryptedFileMetaData.Encrypted != nil {
		// TODO:
		// check if the user still wants to recieve file

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

		// spawn TCP connections
		c.cs.channel.isReady = true
		go c.spawnConnections(ws, c.cs.channel.Role)
	}
	return
}

func (c *Croc) getFilesReady(ws *websocket.Conn) (err error) {
	c.cs.Lock()
	defer c.cs.Unlock()
	c.cs.channel.notSentMetaData = true
	// send metadata

	// wait until data is ready
	for {
		if c.cs.channel.fileMetaData.Name != "" {
			break
		}
		c.cs.Unlock()
		time.Sleep(10 * time.Millisecond)
		c.cs.Lock()
	}

	// get passphrase
	var passphrase []byte
	passphrase, err = c.cs.channel.Pake.SessionKey()
	if err != nil {
		return
	}
	// encrypt file data
	err = encryptFile(path.Join(c.cs.channel.fileMetaData.Path, c.cs.channel.fileMetaData.Name), c.cs.channel.fileMetaData.Name+".enc", passphrase)
	if err != nil {
		return
	}
	c.cs.channel.fileMetaData.IsEncrypted = true
	// split into pieces to send
	if err = splitFile(c.cs.channel.fileMetaData.Name+".enc", len(c.cs.channel.Ports)); err != nil {
		return
	}
	// remove the file now since we still have pieces
	if err = os.Remove(c.cs.channel.fileMetaData.Name + ".enc"); err != nil {
		return
	}
	// remove compressed archive
	if c.cs.channel.fileMetaData.IsDir {
		log.Debug("removing archive: " + c.cs.channel.fileMetaData.Name)
		if err = os.Remove(c.cs.channel.fileMetaData.Name); err != nil {
			return
		}
	}
	// encrypt meta data
	var metaDataBytes []byte
	metaDataBytes, err = json.Marshal(c.cs.channel.fileMetaData)
	if err != nil {
		return
	}
	c.cs.channel.EncryptedFileMetaData = encrypt(metaDataBytes, passphrase)

	c.cs.channel.Update = true
	log.Debugf("updating channel")
	errWrite := ws.WriteJSON(c.cs.channel)
	if errWrite != nil {
		log.Error(errWrite)
	}
	c.cs.channel.Update = false
	go func() {
		// encrypt the files
		// TODO
		c.cs.Lock()
		c.cs.channel.fileReady = true
		c.cs.Unlock()
	}()
	return
}

func (c *Croc) spawnConnections(ws *websocket.Conn, role int) (err error) {
	err = c.dialUp(ws)
	if err == nil {
		if role == 1 {
			c.cs.Lock()
			c.cs.channel.Update = true
			c.cs.channel.finishedHappy = true
			c.cs.channel.FileReceived = true
			log.Debugf("got file successfully")
			errWrite := ws.WriteJSON(c.cs.channel)
			if errWrite != nil {
				log.Error(errWrite)
			}
			c.cs.channel.Update = false
			c.cs.Unlock()
		}
	} else {
		log.Error(err)
	}
	return
}

func (c *Croc) dialUp(ws *websocket.Conn) (err error) {
	c.cs.Lock()
	ports := c.cs.channel.Ports
	channel := c.cs.channel.Channel
	uuid := c.cs.channel.UUID
	role := c.cs.channel.Role
	c.cs.Unlock()
	errorChan := make(chan error, len(ports))
	for i, port := range ports {
		go func(channel, uuid, port string, i int, errorChan chan error) {
			if i == 0 {
				log.Debug("dialing up")
			}
			log.Debugf("connecting to %s", "localhost:"+port)
			connection, err := net.Dial("tcp", "localhost:"+port)
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
			c.cs.RLock()
			filename := c.cs.channel.fileMetaData.Name + ".enc." + strconv.Itoa(i)
			c.cs.RUnlock()
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
				err = sendFile(filename, i, connection)
			} else {
				go func() {
					time.Sleep(10 * time.Millisecond)
					c.cs.Lock()
					log.Debugf("updating channel with ready to read")
					c.cs.channel.Update = true
					c.cs.channel.ReadyToRead = true
					errWrite := ws.WriteJSON(c.cs.channel)
					if errWrite != nil {
						log.Error(errWrite)
					}
					c.cs.channel.Update = false
					c.cs.Unlock()
					log.Debug("receive file")
				}()

				err = receiveFile(filename, i, connection)
			}
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
			ws.WriteJSON(c.cs.channel)
		}
	}
	log.Debug("leaving dialup")
	return
}

func (c *Croc) processFile(fname string) (err error) {

	fd := FileMetaData{}

	// first check if it is stdin
	if fname == "stdin" {
		var f *os.File
		f, err = ioutil.TempFile(".", "croc-stdin-")
		if err != nil {
			return
		}
		_, err = io.Copy(f, os.Stdin)
		if err != nil {
			return
		}
		fname = f.Name()
		err = f.Close()
		if err != nil {
			return
		}
		fd.DeleteAfterSending = true
	}

	fname = filepath.Clean(fname)
	// check wether the file is a dir
	info, err := os.Stat(fname)
	if err != nil {
		return
	}

	fd.Path, fd.Name = filepath.Split(fname)
	if info.Mode().IsDir() {
		// tar folder
		err = tarinator.Tarinate([]string{fname}, fd.Name+".tar")
		if err != nil {
			log.Error(err)
			return
		}
		fd.Name = fd.Name + ".tar"
		fd.Path = "."
		fd.IsDir = true
		fname = path.Join(fd.Path, fd.Name)
	}
	fd.Hash, err = hashFile(fname)
	if err != nil {
		log.Error(err)
		return err
	}
	fd.Size, err = fileSize(fname)
	if err != nil {
		err = errors.Wrap(err, "could not determine filesize")
		log.Error(err)
		return err
	}

	c.cs.Lock()
	defer c.cs.Unlock()
	c.cs.channel.fileMetaData = fd
	return
}

func receiveFile(filename string, id int, connection net.Conn) error {
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
		if !receivedFirstBytes {
			receivedFirstBytes = true
			log.Debug(id, "Receieved first bytes!")
		}
	}
	log.Debug(id, "received file")
	return nil
}

func sendFile(filename string, id int, connection net.Conn) error {
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
